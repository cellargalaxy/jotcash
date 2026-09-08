package fxrate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 测试辅助：构造一个合法 Query。
func validQuery() Query {
	return Query{
		From: currency.MustParse("USD"),
		To:   currency.MustParse("CNY"),
		Date: calendar.MustParseDate("2026-09-08"),
	}
}

// TestQueryValidate 逐项覆盖 Query.Validate 的四种结果。
//
// 三个字段都必填（终版 §四 3 支出日期必填、I-4 按支出日期的日汇率折算），
// 各对应一个哨兵。
func TestQueryValidate(t *testing.T) {
	usd := currency.MustParse("USD")
	cny := currency.MustParse("CNY")
	date := calendar.MustParseDate("2026-09-08")

	cases := []struct {
		name string
		q    Query
		want error
	}{
		{"三项齐备", Query{From: usd, To: cny, Date: date}, nil},
		{"原币为零值", Query{To: cny, Date: date}, ErrFromCurrencyEmpty},
		{"目标币为零值", Query{From: usd, Date: date}, ErrToCurrencyEmpty},
		{"日期为零值", Query{From: usd, To: cny}, ErrDateEmpty},
		{"全为零值", Query{}, ErrFromCurrencyEmpty}, // 顺序：From 先判
		{"同币种也算合法", Query{From: cny, To: cny, Date: date}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.q.Validate()
			if c.want == nil {
				if err != nil {
					t.Errorf("Validate() = %v, 期望 nil", err)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Errorf("Validate() = %v, 期望 %v", err, c.want)
			}
		})
	}
}

// TestQueryValidateOrderFromBeforeSource 锁死「先判 From 为零值，再谈路由」。
//
// 这不是形式上的顺序偏好：零值 currency.Code 的 RateSource() 返回空串
// （currency/currency.go 的 RateSource 注释明写「零值返回空串」），
// 若跳过校验直接路由，真实原因「币种没填」会被报成 ErrSourceUnregistered
// 「汇率源未注册」，把排错引向 app 的注册代码。
func TestQueryValidateOrderFromBeforeSource(t *testing.T) {
	// 前提断言：零值币种的汇率源标识确为空串（本用例存在的理由）
	if got := (currency.Code{}).RateSource(); got != "" {
		t.Fatalf("前提失败：零值 RateSource() = %q, 期望空串", got)
	}

	r := mustRegistry(t, &constFetcher{rate: decimal.MustParse("7.1")})
	_, err := r.Fetch(context.Background(), Query{
		To:   currency.MustParse("CNY"),
		Date: calendar.MustParseDate("2026-09-08"),
	})
	if !errors.Is(err, ErrFromCurrencyEmpty) {
		t.Errorf("原币为零值时应报 ErrFromCurrencyEmpty, 实得 %v", err)
	}
	if errors.Is(err, ErrSourceUnregistered) {
		t.Error("原币为零值被误报成「汇率源未注册」，会把排错引向 app 的注册代码")
	}
}

// TestQueryString 覆盖 String 的正常形态与零值占位。
//
// 零值以 ? 占位而非留空：错误信息里 "?→CNY@2026-09-08" 一眼可见原币没填。
func TestQueryString(t *testing.T) {
	usd := currency.MustParse("USD")
	cny := currency.MustParse("CNY")
	date := calendar.MustParseDate("2026-09-08")

	cases := []struct {
		q    Query
		want string
	}{
		{Query{From: usd, To: cny, Date: date}, "USD→CNY@2026-09-08"},
		{Query{To: cny, Date: date}, "?→CNY@2026-09-08"},
		{Query{From: usd, Date: date}, "USD→?@2026-09-08"},
		{Query{From: usd, To: cny}, "USD→CNY@?"},
		{Query{}, "?→?@?"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			if got := c.q.String(); got != c.want {
				t.Errorf("String() = %q, 期望 %q", got, c.want)
			}
		})
	}
}

// TestQueryStringNeverEmpty 断言 String 对任何输入都不返回空串。
//
// 与 base/errs 的 Kind.String、rule/convert 的 Trigger.String 同一取向：
// 非法值要打印得出来，空串会让日志里出现看不见的字段。
func TestQueryStringNeverEmpty(t *testing.T) {
	if got := (Query{}).String(); got == "" {
		t.Error("零值 Query 的 String() 为空串，日志里会出现看不见的字段")
	}
}

// constFetcher 是恒返回同一汇率的测试替身。
type constFetcher struct {
	rate  decimal.Decimal
	err   error
	calls int // 非并发用例下统计调用次数
}

func (f *constFetcher) Fetch(_ context.Context, _ Query) (decimal.Decimal, error) {
	f.calls++
	return f.rate, f.err
}

// mustRegistry 用同一个实现覆盖全部汇率源标识，构造一个可用注册表。
//
// 不硬编码 RateSourceDefault：币种表将来若出现第二个标识，本函数自动覆盖，
// 而 TestAllCurrenciesShareOneSource 会单独提示该情形需要回读包注释。
func mustRegistry(t *testing.T, f Fetcher) *Registry {
	t.Helper()
	m := map[currency.RateSource]Fetcher{}
	for _, c := range currency.All() {
		m[c.RateSource()] = f
	}
	r, err := NewRegistry(m)
	if err != nil {
		t.Fatalf("构造注册表失败: %v", err)
	}
	return r
}

// TestFetchSuccess 走通正常路径，并锁死「精度不设限、本包不规整」（T39）。
func TestFetchSuccess(t *testing.T) {
	// 12 位小数，远超汇率列的 8 位：本包必须原样返回，规整点在 rule/convert
	const raw = "7.123456789012"
	f := &constFetcher{rate: decimal.MustParse(raw)}
	r := mustRegistry(t, f)

	got, err := r.Fetch(context.Background(), validQuery())
	if err != nil {
		t.Fatalf("Fetch 失败: %v", err)
	}
	if got.String() != raw {
		t.Errorf("Fetch 返回 %s, 期望原样的 %s——本包不得规整精度（T39 规整点唯一，在 rule/convert）", got, raw)
	}
	if f.calls != 1 {
		t.Errorf("汇率源被调用 %d 次, 期望 1 次", f.calls)
	}
}

// TestFetchDoesNotRoundToRateScale 单独锁死「不规整到 8 位」。
//
// 与上一个用例分开写，是因为这条是 T39 的**结构性要求**而非顺带性质：
// 在本包顺手规整不会让任何数值出错，但会让自动获取与 I-5 手填不再共用同一个
// 规整点，而「共用」正是 T39 要解决的问题本身（分层 §二 T39 ②）。
func TestFetchDoesNotRoundToRateScale(t *testing.T) {
	// 第 9 位是 5，若本包做了 8 位四舍五入会变成 7.12345679
	f := &constFetcher{rate: decimal.MustParse("7.123456785")}
	r := mustRegistry(t, f)

	got, err := r.Fetch(context.Background(), validQuery())
	if err != nil {
		t.Fatalf("Fetch 失败: %v", err)
	}
	if got.FitsScale(decimal.RateScale) {
		t.Errorf("Fetch 返回 %s 恰好落在 %d 位内，说明本包做了规整——规整点应唯一地在 rule/convert",
			got, decimal.RateScale)
	}
	if got.String() != "7.123456785" {
		t.Errorf("Fetch 返回 %s, 期望原样的 7.123456785", got)
	}
}

// TestFetchSourceError 覆盖汇率源自身失败：包成 ErrFetch，且源的原始错误可穿透。
func TestFetchSourceError(t *testing.T) {
	srcErr := errors.New("模拟：汇率源超时")
	r := mustRegistry(t, &constFetcher{err: srcErr})

	got, err := r.Fetch(context.Background(), validQuery())
	if err == nil {
		t.Fatal("汇率源失败时 Fetch 应返回错误")
	}
	if !errors.Is(err, ErrFetch) {
		t.Errorf("错误未命中 ErrFetch: %v", err)
	}
	// 源的原始错误必须可穿透：service/fx 据 ErrFetch 走 I-5，据原始错误区分超时与限流
	if !errors.Is(err, srcErr) {
		t.Errorf("源的原始错误未能穿透，排错时拿不到根因: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("失败时应返回零值汇率, 实得 %s", got)
	}
}

// TestFetchRejectsNonPositiveRate 是 I-5「禁止静默 1:1」在本包最关键的一条：
// 汇率源返回非正数时 err 为 nil，不在端口拦下就会静默把钱算成 0 或翻转符号。
func TestFetchRejectsNonPositiveRate(t *testing.T) {
	cases := []struct {
		name string
		rate decimal.Decimal
		why  string
	}{
		{"零", decimal.MustParse("0"),
			"某些源以 0 表示「无此币种对」；放行则本位币金额被算成 0，而恒等式照样成立"},
		{"负数", decimal.MustParse("-7.1"),
			"汇率是「1 单位原币可兑换的本位币数量」，负值会让退款变支出、支出变退款"},
		{"零值 Decimal", decimal.Decimal{},
			"实现返回零值且忘了返回错误"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := mustRegistry(t, &constFetcher{rate: c.rate})
			got, err := r.Fetch(context.Background(), validQuery())
			if !errors.Is(err, ErrRateNotPositive) {
				t.Errorf("汇率为%s时应报 ErrRateNotPositive（%s）, 实得 err=%v", c.name, c.why, err)
			}
			if !got.IsZero() {
				t.Errorf("失败时应返回零值汇率, 实得 %s", got)
			}
		})
	}
}

// TestFetchNeverReturnsOneOnFailure 是 I-5「禁止静默 1:1」的正面断言：
// 遍历本包**全部**失败路径，确认没有任何一条返回汇率 1、也没有任何一条返回 nil error。
//
// 这条用例是本包存在的核心理由之一，比逐个错误分支的断言更硬——它是对
// 「路径集合」的断言，将来新增失败路径而忘了在此登记，会被最后一条计数断言拦下。
func TestFetchNeverReturnsOneOnFailure(t *testing.T) {
	one := decimal.FromInt(1)
	usd := currency.MustParse("USD")
	cny := currency.MustParse("CNY")
	date := calendar.MustParseDate("2026-09-08")

	// 覆盖 Fetch 的六条失败路径
	type failCase struct {
		name  string
		reg   *Registry
		q     Query
		want  error
		setup func() *Registry
	}
	okFetcher := &constFetcher{rate: decimal.MustParse("7.1")}

	// 空注册表：单独构造一个只注册了「其它标识」的注册表拿不到——NewRegistry
	// 有全覆盖校验。故用零值 *Registry 与「构造后币种表被改」两条来覆盖。
	cases := []failCase{
		{name: "注册表未构造", reg: nil, q: Query{From: usd, To: cny, Date: date}, want: ErrSourceUnregistered},
		{name: "原币为零值", reg: mustRegistry(t, okFetcher), q: Query{To: cny, Date: date}, want: ErrFromCurrencyEmpty},
		{name: "目标币为零值", reg: mustRegistry(t, okFetcher), q: Query{From: usd, Date: date}, want: ErrToCurrencyEmpty},
		{name: "日期为零值", reg: mustRegistry(t, okFetcher), q: Query{From: usd, To: cny}, want: ErrDateEmpty},
		{name: "汇率源失败", reg: mustRegistry(t, &constFetcher{err: errors.New("模拟失败")}),
			q: Query{From: usd, To: cny, Date: date}, want: ErrFetch},
		{name: "汇率非正数", reg: mustRegistry(t, &constFetcher{rate: decimal.MustParse("0")}),
			q: Query{From: usd, To: cny, Date: date}, want: ErrRateNotPositive},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.reg.Fetch(context.Background(), c.q)
			if err == nil {
				t.Fatalf("失败路径「%s」却返回了 nil error——这正是 I-5 禁止的静默处理", c.name)
			}
			if !errors.Is(err, c.want) {
				t.Errorf("错误未命中期望哨兵 %v: %v", c.want, err)
			}
			if got.Equal(one) {
				t.Errorf("失败路径「%s」返回了汇率 1——I-5 明写「不静默按 1:1 处理」", c.name)
			}
			if !got.IsZero() {
				t.Errorf("失败路径「%s」应返回零值汇率, 实得 %s", c.name, got)
			}
		})
	}

	// 计数断言：新增失败路径而忘了在此登记时失败。
	// 6 = 注册表未构造 + 三项入参校验 + 源失败 + 非正数
	const wantPaths = 6
	if len(cases) != wantPaths {
		t.Errorf("失败路径用例数 %d ≠ %d：新增失败路径后须在本用例登记，"+
			"否则「无静默 1:1」这条断言就出现了未覆盖的路径", len(cases), wantPaths)
	}
}

// TestFetchSameCurrencyNotShortCircuited 锁死「本包不做 I-4 同币种短路」。
//
// 分层 §四 修正 ② 写死落点：短路在 service/fx，本包对 From == To 照常路由。
// 两个方向都要断言：既不能返回 1（那是在端口造出一条返回 1 的路径），
// 也不能报错（那会把 service/fx 的接线问题变成用户面前的 I-5 表单故障）。
func TestFetchSameCurrencyNotShortCircuited(t *testing.T) {
	cny := currency.MustParse("CNY")
	// 用一个刻意不为 1 的汇率：若本包短路，返回的会是 1 而不是它
	f := &constFetcher{rate: decimal.MustParse("2.5")}
	r := mustRegistry(t, f)

	got, err := r.Fetch(context.Background(), Query{
		From: cny, To: cny, Date: calendar.MustParseDate("2026-09-08"),
	})
	if err != nil {
		t.Fatalf("同币种不应报错（I-4 短路归 service/fx，在此报错会把接线问题变成用户表单故障）: %v", err)
	}
	if f.calls != 1 {
		t.Errorf("同币种时汇率源被调用 %d 次, 期望 1 次——本包不得短路", f.calls)
	}
	if !got.Equal(decimal.MustParse("2.5")) {
		t.Errorf("同币种返回 %s, 期望原样透传 2.5——返回 1 即在端口造出了一条返回 1 的路径", got)
	}
}

// ctxErrFetcher 在 ctx 已取消时返回 ctx 错误，用于验证取消语义可穿透端口。
type ctxErrFetcher struct{}

func (ctxErrFetcher) Fetch(ctx context.Context, _ Query) (decimal.Decimal, error) {
	if err := ctx.Err(); err != nil {
		return decimal.Decimal{}, err
	}
	return decimal.FromInt(7), nil
}

// TestFetchContextCancellationPropagates 断言 context 取消可穿过端口。
//
// 终版 I-7 明写重算「可中断可续跑」。service/fx 需要能区分「整批被中断」与
// 「该行汇率转必填（I-5）」——前者不该让用户看到任何表单变化。
// 故 context.Canceled 必须与 ErrFetch 同时可判定。
func TestFetchContextCancellationPropagates(t *testing.T) {
	r := mustRegistry(t, ctxErrFetcher{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Fetch(ctx, validQuery())
	if err == nil {
		t.Fatal("ctx 已取消时应返回错误")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("context.Canceled 未能穿透端口，service/fx 无法区分「整批中断」与「I-5 该行汇率转必填」: %v", err)
	}
	if !errors.Is(err, ErrFetch) {
		t.Errorf("源错误应同时被包成 ErrFetch: %v", err)
	}
}

// TestSentinelsAreDistinct 断言六个哨兵两两不同、互不 Is。
//
// 若两个哨兵指向同一个 error 值，service/fx 的分档就会把两种完全不同的处置
// （I-5 兜底 vs 记系统错误）混为一谈。
func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := map[string]error{
		"ErrFromCurrencyEmpty":  ErrFromCurrencyEmpty,
		"ErrToCurrencyEmpty":    ErrToCurrencyEmpty,
		"ErrDateEmpty":          ErrDateEmpty,
		"ErrSourceUnregistered": ErrSourceUnregistered,
		"ErrFetch":              ErrFetch,
		"ErrRateNotPositive":    ErrRateNotPositive,
	}
	for nameA, a := range sentinels {
		for nameB, b := range sentinels {
			if nameA == nameB {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("哨兵 %s 与 %s 可互相 Is，service/fx 的分档会把两种处置混为一谈", nameA, nameB)
			}
		}
	}
	// 文案均以包名开头，便于日志检索（约定 3：注释与日志一律中文）
	for name, e := range sentinels {
		if !strings.HasPrefix(e.Error(), "fxrate: ") {
			t.Errorf("哨兵 %s 的文案 %q 未以 \"fxrate: \" 开头", name, e.Error())
		}
	}
}
