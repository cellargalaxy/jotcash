package fxrate

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// reflectTypeOfRegistry 返回 *Registry 的反射类型，供 TestRegistryHasNoWriteMethod
// 枚举导出方法。单独成函数是为了让该用例的意图（枚举方法集）与取类型的机械动作分开。
func reflectTypeOfRegistry() reflect.Type {
	return reflect.TypeOf(&Registry{})
}

// namedFetcher 是能报出自己身份的测试替身，用于验证「路由到了哪一个实现」。
type namedFetcher struct {
	name string
	rate decimal.Decimal
}

func (f namedFetcher) Fetch(_ context.Context, _ Query) (decimal.Decimal, error) {
	return f.rate, nil
}

// TestNewRegistryRejectsBadInput 覆盖构造校验①：空表、空标识、nil 实现。
func TestNewRegistryRejectsBadInput(t *testing.T) {
	ok := namedFetcher{name: "ok", rate: decimal.FromInt(7)}

	cases := []struct {
		name     string
		in       map[currency.RateSource]Fetcher
		wantPart string
	}{
		{"nil 映射", nil, "注册表为空"},
		{"空映射", map[currency.RateSource]Fetcher{}, "注册表为空"},
		{"标识为空串", map[currency.RateSource]Fetcher{"": ok}, "标识为空串"},
		{"实现为 nil", map[currency.RateSource]Fetcher{currency.RateSourceDefault: nil}, "实现为 nil"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := NewRegistry(c.in)
			if err == nil {
				t.Fatalf("应报错，实际构造成功")
			}
			if r != nil {
				t.Error("构造失败时不应返回非 nil 注册表")
			}
			if !strings.Contains(err.Error(), c.wantPart) {
				t.Errorf("错误文案 %q 未包含 %q", err.Error(), c.wantPart)
			}
		})
	}
}

// TestNewRegistryRequiresFullCoverage 覆盖构造校验②：全部已知币种都要能路由。
//
// 这是本注册表与 parser 注册表最实质的差别：汇率源标识对用户不可见，漏注册
// 一个源不会有任何征兆，直到某用户第一次记该币种的支出——表现为 I-5「汇率转
// 必填」，与「网络不通」的表象完全一致。故必须在构造时暴露。
func TestNewRegistryRequiresFullCoverage(t *testing.T) {
	// 注册一个「没有任何币种指向」的标识，于是全部真实币种都未覆盖
	_, err := NewRegistry(map[currency.RateSource]Fetcher{
		currency.RateSource("nobody_points_here"): namedFetcher{name: "x", rate: decimal.FromInt(1)},
	})
	if err == nil {
		t.Fatal("漏注册币种的汇率源时应构造失败")
	}
	if !strings.Contains(err.Error(), "币种的汇率源未注册") {
		t.Errorf("错误文案 %q 未指出是哪一类问题", err.Error())
	}
	// 错误里必须点出具体币种，否则运维不知道该补哪一个
	if !strings.Contains(err.Error(), "CNY") {
		t.Errorf("错误文案 %q 未列出未覆盖的币种，无法据此修配置", err.Error())
	}
}

// TestNewRegistryCoversDisabledCurrencies 断言全覆盖校验的口径是 All() 而非 Enabled()。
//
// 终版 I-7 明写重算含已软删除明细，而历史明细可能引用**已停用**的币种
// （I-1「枚举项只能停用不能删除」），它们同样要能取到汇率。
// 若校验只看 Enabled()，停用币种就会在 I-7 时才暴露成「汇率源未注册」。
func TestNewRegistryCoversDisabledCurrencies(t *testing.T) {
	// 用例前提：当前币种表全部启用，故 All 与 Enabled 等长。
	// 这条断言不是为了验证「现在相等」，而是为了在将来出现停用币种时提醒：
	// 那时本用例应改为构造一个只覆盖 Enabled 的注册表并断言构造失败。
	if len(currency.All()) != len(currency.Enabled()) {
		t.Skip("币种表已出现停用项，请把本用例改为「只覆盖 Enabled 的注册表必须构造失败」")
	}

	// 退而求其次：直接断言实现按 All() 遍历——覆盖 All() 即可构造成功
	m := map[currency.RateSource]Fetcher{}
	for _, c := range currency.All() {
		m[c.RateSource()] = namedFetcher{name: "all", rate: decimal.FromInt(7)}
	}
	if _, err := NewRegistry(m); err != nil {
		t.Errorf("覆盖 All() 的注册表应构造成功: %v", err)
	}
}

// TestNewRegistryRejectsUnusedSource 覆盖构造校验③：注册了却无币种指向。
//
// 它不会造成运行期故障，但意味着「注册了却不生效」，多半是标识写错了字母。
func TestNewRegistryRejectsUnusedSource(t *testing.T) {
	f := namedFetcher{name: "f", rate: decimal.FromInt(7)}
	m := map[currency.RateSource]Fetcher{}
	for _, c := range currency.All() {
		m[c.RateSource()] = f
	}
	// 追加一个多余项
	m[currency.RateSource("typo_source")] = f

	_, err := NewRegistry(m)
	if err == nil {
		t.Fatal("存在无币种指向的汇率源时应构造失败")
	}
	if !strings.Contains(err.Error(), "typo_source") {
		t.Errorf("错误文案 %q 未点出多余的标识", err.Error())
	}
}

// TestNewRegistryClonesInput 断言构造时深拷贝：调用方改原 map 不影响注册表。
//
// 「构造后只读」若不深拷贝就只是口头约定——app 持有的那份 map 仍是同一个对象，
// 任何后续写入都会造成与 Fetch 的并发读冲突（正是 bojanz/currency 被否决的形态）。
func TestNewRegistryClonesInput(t *testing.T) {
	original := namedFetcher{name: "original", rate: decimal.FromInt(7)}
	m := map[currency.RateSource]Fetcher{}
	for _, c := range currency.All() {
		m[c.RateSource()] = original
	}
	r, err := NewRegistry(m)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}

	// 构造后篡改原 map
	for k := range m {
		m[k] = namedFetcher{name: "tampered", rate: decimal.FromInt(999)}
	}
	// 再往原 map 里塞一个新键
	m[currency.RateSource("injected")] = original

	got, err := r.Fetch(context.Background(), validQuery())
	if err != nil {
		t.Fatalf("Fetch 失败: %v", err)
	}
	if !got.Equal(decimal.FromInt(7)) {
		t.Errorf("篡改原 map 后 Fetch 得 %s, 期望 7——注册表未深拷贝入参", got)
	}
	if _, ok := r.Fetcher(currency.RateSource("injected")); ok {
		t.Error("向原 map 注入的新键出现在了注册表里——未深拷贝")
	}
}

// TestRegistryRoutesByFromCurrency 锁死「按 Query.From 而非 Query.To 路由」。
//
// # 为什么这条用例是源码扫描而不是行为断言
//
// 当前币种表 27 项全部指向同一个标识（见 TestAllCurrenciesShareOneSource），
// 于是「取 From 的标识」与「取 To 的标识」在任何输入下都得到同一个源——
// 这条决定**在行为上无法被区分**。而 currency.Code 的内部 entry 不导出，
// 本包也无法伪造一个「汇率源不同」的币种来制造差异。
//
// 面对这种情形有两条路：跳过（等于放弃这条契约），或换一种能落地的判据。
// 此处取后者：直接扫源码，断言路由键取自 From、且 To 不参与路由。判据虽弱于
// 行为断言，但它拦得住唯一现实的破坏方式——有人在加第二个汇率源时顺手把
// From 改成 To。真正的行为用例应在那一天补上，届时
// TestAllCurrenciesShareOneSource 会失败并给出提示。
func TestRegistryRoutesByFromCurrency(t *testing.T) {
	src, err := os.ReadFile("registry.go")
	if err != nil {
		t.Fatalf("读取 registry.go 失败: %v", err)
	}
	code := string(src)

	// 去掉注释行后再扫，避免包注释里讨论 To 的文字造成误判
	var body strings.Builder
	for _, line := range strings.Split(code, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	stripped := body.String()

	if !strings.Contains(stripped, "q.From.RateSource()") {
		t.Error("registry.go 未按 q.From.RateSource() 路由。" +
			"路由依据取 From（原币）一侧：To 恒为本位币、受 T38 限制只能是候选币种，" +
			"而 From 是支出币种、不受该限制，汇率源的差异来自被定价的那一侧。" +
			"详见 fxrate 包注释「路由按 Query.From 的汇率源标识」")
	}
	if strings.Contains(stripped, "q.To.RateSource()") {
		t.Error("registry.go 用 q.To.RateSource() 参与了路由。" +
			"当前币种表全部同源，这个改动不会让任何行为用例失败，但在加入第二个汇率源后" +
			"会让支出币种的源被目标币种的源覆盖")
	}

	// 配套的正向路径：按 From 的标识注册后确实能路由成功
	q := validQuery()
	if q.From.RateSource() == "" {
		t.Fatal("前提失败：From 的汇率源标识为空")
	}
	r, err := NewRegistry(map[currency.RateSource]Fetcher{
		q.From.RateSource(): namedFetcher{name: "byFrom", rate: decimal.FromInt(7)},
	})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if _, err := r.Fetch(context.Background(), q); err != nil {
		t.Errorf("按 From 的标识注册后应能路由成功: %v", err)
	}
}

// TestFetchUnregisteredSource 覆盖「构造后币种表又被改动」这一路径。
//
// 正常链路走不到（NewRegistry 已做全覆盖校验），它拦的是那道校验之后表被改动
// 的情形。用白盒方式构造一个残缺注册表来触发。
func TestFetchUnregisteredSource(t *testing.T) {
	// 白盒构造：绕过 NewRegistry 的全覆盖校验，模拟「校验之后表被改」
	r := &Registry{fetchers: map[currency.RateSource]Fetcher{
		currency.RateSource("some_other_source"): namedFetcher{name: "x", rate: decimal.FromInt(7)},
	}}

	got, err := r.Fetch(context.Background(), validQuery())
	if !errors.Is(err, ErrSourceUnregistered) {
		t.Errorf("未注册的汇率源应报 ErrSourceUnregistered, 实得 %v", err)
	}
	if got.Equal(decimal.FromInt(1)) {
		t.Error("未注册时返回了汇率 1——I-5 明写「不静默按 1:1 处理」")
	}
	if !got.IsZero() {
		t.Errorf("失败时应返回零值汇率, 实得 %s", got)
	}
	// 错误里要带上查询与标识，否则排错时不知道是哪个币种、哪个标识
	if !strings.Contains(err.Error(), "USD") {
		t.Errorf("错误文案 %q 未带上查询信息", err.Error())
	}
}

// TestNilRegistryFetch 断言零值 *Registry 返回错误而非 panic。
//
// 一次接线错误（app 没构造就注入）不该崩掉进程，但也绝不能静默取到汇率。
func TestNilRegistryFetch(t *testing.T) {
	var r *Registry
	got, err := r.Fetch(context.Background(), validQuery())
	if !errors.Is(err, ErrSourceUnregistered) {
		t.Errorf("零值注册表应报 ErrSourceUnregistered, 实得 %v", err)
	}
	if !got.IsZero() {
		t.Errorf("应返回零值汇率, 实得 %s", got)
	}
	// 其余方法在零值上同样不得 panic
	if got := r.Sources(); got != nil {
		t.Errorf("零值注册表的 Sources() = %v, 期望 nil", got)
	}
	if _, ok := r.Fetcher(currency.RateSourceDefault); ok {
		t.Error("零值注册表不应取到任何实现")
	}
}

// TestRegistryHasNoWriteMethod 以反射断言 Registry **没有任何写入口**。
//
// 这不是风格检查：currency 与 rule/convert 的包注释都记着否决 bojanz/currency
// 的第一条理由是「Register 改包级 map、与读并发即 DATA RACE」。本包若加回一个
// Register，I-7 逐笔重算（并发读）与运行期注册（写）会让进程崩在 runtime 的
// 并发写检测上。用例把「没有写入口」变成 CI 约束。
func TestRegistryHasNoWriteMethod(t *testing.T) {
	// 导出方法白名单，新增导出方法必须在此登记并确认它不写内部 map
	allowed := map[string]bool{
		"Fetch":   true, // 只读
		"Fetcher": true, // 只读
		"Sources": true, // 只读，返回副本
	}
	typ := reflectTypeOfRegistry()
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		if !allowed[name] {
			t.Errorf("Registry 新增了导出方法 %q。若它会写内部 map，"+
				"就会与 I-7 的并发 Fetch 构成 DATA RACE（正是 bojanz/currency 被否决的形态）；"+
				"确认只读后请加入本用例的白名单", name)
		}
	}
	// 反向：白名单里的方法必须真实存在，否则白名单会随重命名而失效
	for name := range allowed {
		if _, ok := typ.MethodByName(name); !ok {
			t.Errorf("白名单方法 %q 不存在，白名单已失效", name)
		}
	}
}

// TestRegistryConcurrentFetch 在 -race 下并发调用 Fetch。
//
// 「构造后只读」这条性质的价值全在并发上，故必须有一条并发用例；
// 单跑不足以说明问题，需配合 go test -race。
func TestRegistryConcurrentFetch(t *testing.T) {
	r := mustRegistry(t, namedFetcher{name: "concurrent", rate: decimal.MustParse("7.1234")})

	const goroutines = 32
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errCh := make(chan error, goroutines*iterations)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				got, err := r.Fetch(context.Background(), validQuery())
				if err != nil {
					errCh <- err
					return
				}
				if !got.Equal(decimal.MustParse("7.1234")) {
					errCh <- errors.New("并发下取到了错误的汇率: " + got.String())
					return
				}
				// 只读方法也一并并发调用
				_ = r.Sources()
				_, _ = r.Fetcher(currency.RateSourceDefault)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("并发 Fetch 失败: %v", err)
	}
}

// TestSources 断言 Sources 返回全部标识、有序、且为副本。
func TestSources(t *testing.T) {
	r := mustRegistry(t, namedFetcher{name: "s", rate: decimal.FromInt(7)})

	got := r.Sources()
	if len(got) == 0 {
		t.Fatal("Sources() 为空")
	}
	// 有序
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("Sources() 未按字典序: %v", got)
			break
		}
	}
	// 副本：改动不污染注册表
	got[0] = currency.RateSource("tampered")
	again := r.Sources()
	if again[0] == currency.RateSource("tampered") {
		t.Error("Sources() 返回的不是副本，调用方改动污染了注册表")
	}
}

// TestFetcherLookup 断言按标识取实现的两种结果。
func TestFetcherLookup(t *testing.T) {
	want := namedFetcher{name: "target", rate: decimal.FromInt(7)}
	r := mustRegistry(t, want)

	got, ok := r.Fetcher(currency.RateSourceDefault)
	if !ok {
		t.Fatal("已注册的标识应能取到实现")
	}
	if got.(namedFetcher).name != "target" {
		t.Errorf("取到的实现是 %q, 期望 target", got.(namedFetcher).name)
	}

	if _, ok := r.Fetcher(currency.RateSource("nope")); ok {
		t.Error("未注册的标识不应取到实现")
	}
}

// TestAllCurrenciesShareOneSource 是一条**提醒型**用例，不是行为断言。
//
// 当前 currency 表 27 项全部指向 RateSourceDefault，故「按 From 还是按 To 路由」
// 在行为上无法被区分（见包注释「路由按 Query.From 的汇率源标识」）。
// 一旦表里出现第二个标识，这条决定就会产生可观察的差异，本用例届时失败，
// 强制那一轮回来重读包注释、并为路由依据补一条真正的行为用例。
func TestAllCurrenciesShareOneSource(t *testing.T) {
	seen := map[currency.RateSource][]string{}
	for _, c := range currency.All() {
		seen[c.RateSource()] = append(seen[c.RateSource()], c.String())
	}
	if len(seen) > 1 {
		t.Errorf("币种表出现了 %d 个汇率源标识（%v）。"+
			"请回读 fxrate 包注释「路由按 Query.From 的汇率源标识」一节，"+
			"确认路由依据仍取 From 一侧，并为此补一条可区分 From/To 的行为用例", len(seen), seen)
	}
	// 顺带确认当前唯一标识就是 RateSourceDefault，避免表被改成别的值而无人察觉
	if _, ok := seen[currency.RateSourceDefault]; !ok {
		t.Errorf("币种表的汇率源标识不含 RateSourceDefault: %v", seen)
	}
}
