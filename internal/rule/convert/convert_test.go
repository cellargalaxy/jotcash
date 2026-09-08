package convert

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// dec 是测试内的解析简写。用 MustParse：测试里的字面量写错应当立刻 panic，
// 而不是让用例带着一个零值继续跑出「碰巧通过」的结果。
func dec(s string) decimal.Decimal { return decimal.MustParse(s) }

// cur 取币种，测试内字面量同理。
func cur(s string) currency.Code { return currency.MustParse(s) }

// TestTriggerMatrixMatchesSpec 逐行比对终版 8.1 的六种触发矩阵。
//
// 这是本包最核心的用例：8.1 的第 2 列（是否重算本位币金额）与第 3 列
// （是否更新折算本位币币种）就是不变式 1a 的联动那一半。第 3 列没有任何
// 数值后果——算错了不报错、金额也对——只能靠断言锁死。
//
// 期望值直接抄自终版 8.1 表格，不从 triggerTable 反推，否则表改错了用例
// 会跟着一起错。
func TestTriggerMatrixMatchesSpec(t *testing.T) {
	// 终版 8.1 表格原文（行序即声明序）
	spec := []struct {
		trigger   Trigger
		desc      string
		recalc    bool // 是否重算本位币金额
		adoptBase bool // 是否置 折算本位币币种 = 当前本位币
	}{
		{TriggerAmountChanged, "改支出金额（汇率不动）", true, false},
		{TriggerRateChanged, "改折算汇率（含入库首次获取）", true, true},
		{TriggerSpendDateChanged, "改支出日期", true, true},
		{TriggerCurrencyChanged, "改支出币种", true, true},
		{TriggerRecalc, "I-7 重算 / 切换本位币", true, true},
		{TriggerAmortizeMonthsChanged, "改摊分月数", false, false},
	}

	if len(spec) != len(triggerTable) {
		t.Fatalf("8.1 共 %d 种触发，triggerTable 有 %d 条——两者必须一一对应",
			len(spec), len(triggerTable))
	}

	// 用行为断言验证矩阵，而不是直接读 triggerTable：读表等于把实现抄一遍，
	// 实现改了用例也跟着改，锁不住任何东西。
	const (
		amount = "100.00"
		rate   = "7.12345678"
	)
	for _, tc := range spec {
		t.Run(tc.desc, func(t *testing.T) {
			// 原口径 USD、当前本位币 CNY：一行「未收敛」的数据。
			// 两者不同才能看出 adoptBase 到底取了哪个。
			in := Input{
				Amount:            dec(amount),
				Rate:              dec(rate),
				BaseCurrency:      cur("CNY"),
				PriorBaseCurrency: cur("USD"),
			}
			got, err := Apply(in, tc.trigger)
			if err != nil {
				t.Fatalf("Apply 失败: %v", err)
			}

			// 第 3 列：折算本位币币种。
			//
			// 只对「重算」的五种触发断言取哪个币种：不重算的那一种三字段全不动，
			// 输出是零值 Output（Changed 为 false），此时 BaseCurrency 不代表
			// 该行的任何口径，断言它取 USD 还是 CNY 都没有意义——「不动」这件事
			// 由下面 else 分支的零值断言负责。
			if tc.recalc {
				wantBase := "USD"
				if tc.adoptBase {
					wantBase = "CNY"
				}
				if got.BaseCurrency.String() != wantBase {
					t.Errorf("折算本位币币种 = %s, 期望 %s（8.1 第 3 列）",
						got.BaseCurrency, wantBase)
				}
			}

			// 第 2 列：是否重算
			if tc.recalc {
				if got.BaseAmount.IsZero() {
					t.Error("本触发应重算本位币金额，实际得零值")
				}
				// 重算的必然规整汇率
				if !got.Rate.Equal(dec(rate)) {
					t.Errorf("折算汇率 = %s, 期望 %s", got.Rate, rate)
				}
				if !got.Changed {
					t.Error("本触发产出了新值，Changed 应为 true")
				}
			} else {
				// 不重算：三字段全不动。此时 Output 是零值且 Changed 为 false，
				// 三个字段都不是该行的现值——调用方据 Changed 跳过写回。
				// 逐字段断言而非 got != Output{}：Output 含 decimal.Decimal，
				// 后者刻意不可比较（decimal 包注释「为什么类型不可比较」）。
				if got.Changed {
					t.Error("本触发三字段全不动，Changed 应为 false")
				}
				if !got.Rate.IsZero() {
					t.Errorf("不重算时汇率应为零值（不得回传入参）, 得 %s", got.Rate)
				}
				if !got.BaseAmount.IsZero() {
					t.Errorf("不重算时不应产出本位币金额, 得 %s", got.BaseAmount)
				}
				if !got.BaseCurrency.IsZero() {
					t.Errorf("不重算时折算本位币币种应为零值（不得回传入参）, 得 %s",
						got.BaseCurrency)
				}
			}
		})
	}
}

// TestTriggersExhaustive 锁死 Triggers() 与 triggerTable、Valid() 三者同步。
//
// 上层（service/expense 的编辑链路）要靠 Triggers() 做穷尽分派，若它漏了
// 一种触发，漏的那种在运行期会静默走默认分支。
func TestTriggersExhaustive(t *testing.T) {
	got := Triggers()
	if len(got) != len(triggerTable) {
		t.Fatalf("Triggers() 返回 %d 种, triggerTable 有 %d 条", len(got), len(triggerTable))
	}
	seen := make(map[Trigger]bool, len(got))
	for _, tr := range got {
		if seen[tr] {
			t.Errorf("Triggers() 中 %s 重复出现", tr)
		}
		seen[tr] = true
		if !tr.Valid() {
			t.Errorf("Triggers() 返回了 Valid() 为 false 的 %v", uint8(tr))
		}
		if tr.String() == "" || strings.HasPrefix(tr.String(), "未知触发") {
			t.Errorf("触发 %d 缺中文名", uint8(tr))
		}
	}

	// 返回副本：调用方改动不得污染包状态
	got[0] = TriggerRecalc
	if Triggers()[0] != TriggerAmountChanged {
		t.Error("Triggers() 返回的不是副本，调用方改动污染了包级状态")
	}

	// 零值与越界值必须非法
	for _, bad := range []Trigger{0, 7, 200, 255} {
		if bad.Valid() {
			t.Errorf("Trigger(%d) 不应合法", uint8(bad))
		}
		if _, err := Apply(Input{
			Amount:       dec("1"),
			Rate:         dec("1"),
			BaseCurrency: cur("CNY"),
		}, bad); !errors.Is(err, ErrTrigger) {
			t.Errorf("Trigger(%d) 应返回 ErrTrigger, 得 %v", uint8(bad), err)
		}
	}
}

// TestZeroTriggerRejected 单拎出零值触发。
//
// 零值是「忘传参数」的形态，也是唯一会被 Go 的零值语义自动产生的非法值。
// 它若被默许为某一种触发，忘传的调用点会静默按那种触发改（或不改）
// 折算本位币币种——不变式 1a 第 2 条就此失守，且没有任何数值迹象。
func TestZeroTriggerRejected(t *testing.T) {
	var zero Trigger
	_, err := Apply(Input{
		Amount:       dec("100"),
		Rate:         dec("7"),
		BaseCurrency: cur("CNY"),
	}, zero)
	if !errors.Is(err, ErrTrigger) {
		t.Fatalf("零值触发应返回 ErrTrigger, 得 %v", err)
	}
}

// TestRateRegularizedToEightDigits 锁死 T39：入口规整汇率到 8 位。
//
// 兼顾「输出的是规整后的值」——分层 §六 L2 明写「落库以它为准」。
func TestRateRegularizedToEightDigits(t *testing.T) {
	cases := []struct {
		in   string
		want string
		desc string
	}{
		{"7.12345678", "7.12345678", "恰好 8 位，原样"},
		{"7.123456785", "7.12345679", "第 9 位为 5，四舍五入进位"},
		{"7.123456784", "7.12345678", "第 9 位小于 5，舍去"},
		{"7.1234567849999", "7.12345678", "长尾舍去"},
		{"7", "7", "整数汇率"},
		{"0.00000001", "0.00000001", "最小可表示汇率"},
		{"123456.789012345678", "123456.78901235", "大数 + 长尾"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := Apply(Input{
				Amount:       dec("1"),
				Rate:         dec(tc.in),
				BaseCurrency: cur("CNY"),
			}, TriggerRateChanged)
			if err != nil {
				t.Fatalf("Apply 失败: %v", err)
			}
			if got.Rate.String() != tc.want {
				t.Errorf("规整 %s 得 %s, 期望 %s", tc.in, got.Rate, tc.want)
			}
			// 幂等：规整后的值再进一次不应再变
			again, err := Apply(Input{
				Amount:       dec("1"),
				Rate:         got.Rate,
				BaseCurrency: cur("CNY"),
			}, TriggerRateChanged)
			if err != nil {
				t.Fatalf("二次 Apply 失败: %v", err)
			}
			if !again.Rate.Equal(got.Rate) {
				t.Errorf("规整不幂等: %s -> %s", got.Rate, again.Rate)
			}
		})
	}
}

// TestRegularizeBeforeMultiply 锁死「先规整、后乘」的顺序。
//
// 这是本包唯一一处顺序敏感的地方，且两种顺序的差异**只在特定数量级下
// 才显现**——用 100.00 这种小额金额测不出来，必须构造放大到分位的用例。
// 实测：100000000 × 7.123456785
//
//	先乘后按 2 位舍入 = 712345678.5
//	先规整（7.12345679）再乘 = 712345679
//
// 差 0.5 元。若将来有人把规整挪到乘法之后，本用例失败。
func TestRegularizeBeforeMultiply(t *testing.T) {
	in := Input{
		Amount:       dec("100000000"),
		Rate:         dec("7.123456785"),
		BaseCurrency: cur("CNY"),
	}
	got, err := Apply(in, TriggerRateChanged)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}

	const wantRegularizedFirst = "712345679"
	const wrongMultiplyFirst = "712345678.5"

	if got.BaseAmount.String() == wrongMultiplyFirst {
		t.Fatalf("本位币金额 = %s，说明先乘后规整——T39 要求入口即规整", got.BaseAmount)
	}
	if got.BaseAmount.String() != wantRegularizedFirst {
		t.Errorf("本位币金额 = %s, 期望 %s", got.BaseAmount, wantRegularizedFirst)
	}

	// 恒等式必须对**输出的**汇率成立：用输出三元组自洽验算
	want, err := got.Rate.Mul(in.Amount).Round(cur("CNY").Scale(), decimal.RoundHalfUp)
	if err != nil {
		t.Fatalf("验算失败: %v", err)
	}
	if !got.BaseAmount.Equal(want) {
		t.Errorf("恒等式不成立: 输出金额 %s ≠ 输出汇率 × 支出金额 = %s", got.BaseAmount, want)
	}
}

// TestRoundByBaseCurrencyScale 锁死 T35 + T38：按**本位币**小数位数舍入，
// 而不是按固定 2 位。
//
// 这是分层 §三 问题 1 的修法。用 JPY（0 位）最能暴露：按 2 位会算出
// 「712.35 JPY」这种该币种不存在的面额。
func TestRoundByBaseCurrencyScale(t *testing.T) {
	// 100.00 × 7.12345678 = 712.345678
	const amount = "100.00"
	const rate = "7.12345678"

	cases := []struct {
		base  string
		scale int32
		want  string
		desc  string
	}{
		{"JPY", 0, "712", "0 位本位币：不得出现小数面额"},
		{"CNY", 2, "712.35", "2 位本位币：四舍五入"},
		{"USD", 2, "712.35", "2 位本位币"},
		{"KRW", 0, "712", "0 位本位币"},
	}
	for _, tc := range cases {
		t.Run(tc.desc+"/"+tc.base, func(t *testing.T) {
			base := cur(tc.base)
			if base.Scale() != tc.scale {
				t.Fatalf("前提失效: %s 的小数位数为 %d, 用例假定 %d",
					tc.base, base.Scale(), tc.scale)
			}
			got, err := Apply(Input{
				Amount:       dec(amount),
				Rate:         dec(rate),
				BaseCurrency: base,
			}, TriggerRecalc)
			if err != nil {
				t.Fatalf("Apply 失败: %v", err)
			}
			if got.BaseAmount.String() != tc.want {
				t.Errorf("%s 本位币金额 = %s, 期望 %s", tc.base, got.BaseAmount, tc.want)
			}
			// T38 的意义：按本位币位数算出的结果必然能被 2 位定点列无损容纳
			if !got.BaseAmount.FitsScale(decimal.BaseAmountStoreScale) {
				t.Errorf("%s 算出的 %s 无法被 %d 位定点列无损容纳",
					tc.base, got.BaseAmount, decimal.BaseAmountStoreScale)
			}
			if _, err := got.BaseAmount.Units(decimal.BaseAmountStoreScale); err != nil {
				t.Errorf("%s 算出的 %s 无法落库: %v", tc.base, got.BaseAmount, err)
			}
		})
	}
}

// TestRoundHalfUpNotBankers 锁死四舍五入，排除银行家舍入。
//
// 实测 govalues/money 的 RoundToCurr 用的是银行家舍入（0.005→0.00、
// 5.445→5.44），与已定案口径差一分钱且签名一致。本用例专防「有人换了
// 底层实现或改了 RoundMode」。
func TestRoundHalfUpNotBankers(t *testing.T) {
	// 构造乘积恰好落在半分位的用例：金额 × 1 = 金额本身
	cases := []struct {
		amount   string
		want     string
		bankers  string
		baseCurr string
	}{
		{"0.005", "0.01", "0.00", "CNY"},
		{"0.015", "0.02", "0.02", "CNY"},
		{"5.445", "5.45", "5.44", "CNY"},
		{"5.455", "5.46", "5.46", "CNY"},
		{"0.5", "1", "0", "JPY"},
		{"1.5", "2", "2", "JPY"},
		{"2.5", "3", "2", "JPY"},
	}
	for _, tc := range cases {
		t.Run(tc.amount+"/"+tc.baseCurr, func(t *testing.T) {
			got, err := Apply(Input{
				Amount:       dec(tc.amount),
				Rate:         dec("1"),
				BaseCurrency: cur(tc.baseCurr),
			}, TriggerRecalc)
			if err != nil {
				t.Fatalf("Apply 失败: %v", err)
			}
			if got.BaseAmount.String() != tc.want {
				t.Errorf("%s 舍入得 %s, 期望 %s（四舍五入）", tc.amount, got.BaseAmount, tc.want)
			}
			if tc.want != tc.bankers && got.BaseAmount.String() == tc.bankers {
				t.Errorf("%s 得 %s，这是银行家舍入的结果——口径要求四舍五入",
					tc.amount, got.BaseAmount)
			}
		})
	}
}

// TestNegativeAmount 覆盖负金额（退款 / 冲正，终版 §四 3 允许）。
//
// 关注两点：符号不得翻转；舍入在负数侧与正数侧对称（-0.005 → -0.01）。
func TestNegativeAmount(t *testing.T) {
	cases := []struct {
		amount string
		rate   string
		base   string
		want   string
	}{
		{"-100.00", "7.12345678", "CNY", "-712.35"},
		{"-100.00", "7.12345678", "JPY", "-712"},
		{"-0.005", "1", "CNY", "-0.01"},
		{"-0.5", "1", "JPY", "-1"},
	}
	for _, tc := range cases {
		t.Run(tc.amount+"×"+tc.rate+"→"+tc.base, func(t *testing.T) {
			got, err := Apply(Input{
				Amount:       dec(tc.amount),
				Rate:         dec(tc.rate),
				BaseCurrency: cur(tc.base),
			}, TriggerRecalc)
			if err != nil {
				t.Fatalf("负金额应当合法（退款 / 冲正）, 得错误: %v", err)
			}
			if got.BaseAmount.String() != tc.want {
				t.Errorf("本位币金额 = %s, 期望 %s", got.BaseAmount, tc.want)
			}
			if got.BaseAmount.Sign() > 0 {
				t.Errorf("负金额折算后符号翻转: %s", got.BaseAmount)
			}
		})
	}

	// 与正数侧严格对称
	pos, err := Apply(Input{
		Amount: dec("123.456"), Rate: dec("7.12345678"), BaseCurrency: cur("CNY"),
	}, TriggerRecalc)
	if err != nil {
		t.Fatal(err)
	}
	neg, err := Apply(Input{
		Amount: dec("-123.456"), Rate: dec("7.12345678"), BaseCurrency: cur("CNY"),
	}, TriggerRecalc)
	if err != nil {
		t.Fatal(err)
	}
	if pos.BaseAmount.String() != strings.TrimPrefix(neg.BaseAmount.String(), "-") {
		t.Errorf("正负不对称: %s vs %s", pos.BaseAmount, neg.BaseAmount)
	}
}

// TestZeroAmount 零金额：0 × 任何汇率恒为 0，且不应报错。
func TestZeroAmount(t *testing.T) {
	got, err := Apply(Input{
		Amount:       decimal.Decimal{},
		Rate:         dec("7.12345678"),
		BaseCurrency: cur("CNY"),
	}, TriggerRecalc)
	if err != nil {
		t.Fatalf("零金额应当合法, 得错误: %v", err)
	}
	if !got.BaseAmount.IsZero() {
		t.Errorf("0 × 汇率应为 0, 得 %s", got.BaseAmount)
	}
}

// TestRateNotPositiveRejected 锁死汇率必须为正。
//
// 零是 I-5 的「未填」形态（明写「不静默按 1:1 处理」），负数会让退款与
// 支出符号整体翻转。两者放行都得到「不报错但错误」的金额。
func TestRateNotPositiveRejected(t *testing.T) {
	for _, tc := range []struct {
		rate string
		desc string
	}{
		{"0", "零汇率即 I-5 的未填，不得静默按 1:1"},
		{"-1", "负汇率会让退款与支出符号翻转"},
		{"-7.12345678", "负汇率"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := Apply(Input{
				Amount:       dec("100"),
				Rate:         dec(tc.rate),
				BaseCurrency: cur("CNY"),
			}, TriggerRecalc)
			if !errors.Is(err, ErrRateNotPositive) {
				t.Errorf("汇率 %s 应返回 ErrRateNotPositive, 得 %v", tc.rate, err)
			}
		})
	}

	// 零值 Decimal（未填字段的自然形态）同样拦下
	_, err := Apply(Input{
		Amount:       dec("100"),
		Rate:         decimal.Decimal{},
		BaseCurrency: cur("CNY"),
	}, TriggerRecalc)
	if !errors.Is(err, ErrRateNotPositive) {
		t.Errorf("零值汇率应返回 ErrRateNotPositive, 得 %v", err)
	}
}

// TestRateUnderflowRejected 锁死「规整后才出现的零」这条路径。
//
// 极小汇率（< 0.000000005）本身为正、能通过入口校验，但规整到 8 位后
// **变成 0**（实测 RoundRate(0.000000001) = 0）。若只在规整前判正，
// 这条路径会静默把本位币金额算成 0——恒等式照样成立（0 = 金额 × 0），
// 钱却没了。
func TestRateUnderflowRejected(t *testing.T) {
	for _, rate := range []string{"0.000000001", "0.0000000049", "0.0000000001"} {
		t.Run(rate, func(t *testing.T) {
			// 前提：这个汇率本身是正数，能过入口校验
			if !dec(rate).IsPositive() {
				t.Fatalf("用例前提失效: %s 不是正数", rate)
			}
			// 前提：它规整后确实为零
			if !dec(rate).RoundRate().IsZero() {
				t.Fatalf("用例前提失效: %s 规整后不为零", rate)
			}
			_, err := Apply(Input{
				Amount:       dec("100"),
				Rate:         dec(rate),
				BaseCurrency: cur("CNY"),
			}, TriggerRecalc)
			if !errors.Is(err, ErrRateNotPositive) {
				t.Errorf("汇率 %s 规整后为零, 应返回 ErrRateNotPositive, 得 %v", rate, err)
			}
		})
	}

	// 边界另一侧：恰好能规整出非零的最小汇率必须放行
	got, err := Apply(Input{
		Amount:       dec("100"),
		Rate:         dec("0.000000005"),
		BaseCurrency: cur("CNY"),
	}, TriggerRecalc)
	if err != nil {
		t.Fatalf("0.000000005 规整为 0.00000001, 应当放行, 得错误: %v", err)
	}
	if got.Rate.String() != "0.00000001" {
		t.Errorf("规整得 %s, 期望 0.00000001", got.Rate)
	}
}

// TestBaseCurrencyEmptyRejected 锁死本位币零值必须拦下。
//
// currency.Code.Scale() 对零值返回 0，与 JPY 的 0 位无法区分。放行的后果是
// 本位币是 CNY 却按 0 位舍入——712.35 变 712，钱少一截且不报错。
func TestBaseCurrencyEmptyRejected(t *testing.T) {
	var empty currency.Code
	if !empty.IsZero() {
		t.Fatal("用例前提失效: 零值 Code 应 IsZero")
	}
	// 前提：零值的 Scale() 确实与 JPY 撞车，所以必须显式拦
	if empty.Scale() != cur("JPY").Scale() {
		t.Fatal("用例前提失效: 零值 Code 的 Scale 本应与 JPY 同为 0")
	}

	// 五种「置为当前本位币」的触发：BaseCurrency 为零值
	for _, tr := range []Trigger{
		TriggerRateChanged, TriggerSpendDateChanged,
		TriggerCurrencyChanged, TriggerRecalc,
	} {
		t.Run(tr.String(), func(t *testing.T) {
			_, err := Apply(Input{
				Amount:       dec("100"),
				Rate:         dec("7.12345678"),
				BaseCurrency: empty,
			}, tr)
			if !errors.Is(err, ErrBaseCurrencyEmpty) {
				t.Errorf("应返回 ErrBaseCurrencyEmpty, 得 %v", err)
			}
		})
	}
}

// TestPriorBaseCurrencyEmptyRejected 锁死「改支出金额」下原口径为零值的情形。
//
// 该触发是唯一取 PriorBaseCurrency 作舍入基准的，零值同样会误按 0 位舍入。
// 且终版 §四 3 把 折算本位币币种 列为必填的只读派生项——为零意味着数据异常。
func TestPriorBaseCurrencyEmptyRejected(t *testing.T) {
	_, err := Apply(Input{
		Amount:            dec("100"),
		Rate:              dec("7.12345678"),
		BaseCurrency:      cur("CNY"),
		PriorBaseCurrency: currency.Code{},
	}, TriggerAmountChanged)
	if !errors.Is(err, ErrPriorBaseCurrencyEmpty) {
		t.Fatalf("应返回 ErrPriorBaseCurrencyEmpty, 得 %v", err)
	}

	// 反面：其余五种触发不读 PriorBaseCurrency，零值不应导致失败
	// （D-7 入库首次折算就是这个形态）
	for _, tr := range []Trigger{
		TriggerRateChanged, TriggerSpendDateChanged,
		TriggerCurrencyChanged, TriggerRecalc,
	} {
		t.Run("新建行/"+tr.String(), func(t *testing.T) {
			got, err := Apply(Input{
				Amount:            dec("100"),
				Rate:              dec("7.12345678"),
				BaseCurrency:      cur("CNY"),
				PriorBaseCurrency: currency.Code{},
			}, tr)
			if err != nil {
				t.Fatalf("新建行（原口径为零）应当合法, 得错误: %v", err)
			}
			if got.BaseCurrency.String() != "CNY" {
				t.Errorf("折算本位币币种 = %s, 期望 CNY", got.BaseCurrency)
			}
		})
	}
}

// TestBaseCurrencyMustBeCandidate 锁死 T38：本位币须满足候选口径。
//
// 3 位本位币（KWD/BHD/OMR）算出的金额无法被 2 位定点列无损容纳
// （实测 100.00 × 7.12345678 按 3 位得 712.346，FitsScale(2) 为 false），
// 落库会被静默截断。停用币种同理不得作本位币。
func TestBaseCurrencyMustBeCandidate(t *testing.T) {
	// 非候选集合从币种表现算，不写死码值：表里今天只有 KWD 是 3 位，
	// 将来新增 BHD / OMR 或停用某个币种时，本用例自动覆盖到，无需改动。
	var nonCandidates []currency.Code
	for _, c := range currency.All() {
		if !c.CanBeBase() {
			nonCandidates = append(nonCandidates, c)
		}
	}
	if len(nonCandidates) == 0 {
		t.Fatal("币种表中没有任何非候选币种——本用例形同虚设")
	}

	for _, c := range nonCandidates {
		t.Run("非候选/"+c.String(), func(t *testing.T) {
			// 前提：非候选必因「停用」或「小数位 > 2」，二者至少居其一
			if c.Enabled() && c.Scale() <= decimal.BaseAmountStoreScale {
				t.Fatalf("用例前提失效: %s 既已启用又只有 %d 位, 不该是非候选",
					c, c.Scale())
			}
			// 小数位超限的，前提是按其位数算出的结果确实无法被 2 位列容纳
			if c.Scale() > decimal.BaseAmountStoreScale {
				raw, err := dec("100.00").Mul(dec("7.12345678")).
					Round(c.Scale(), decimal.RoundHalfUp)
				if err != nil {
					t.Fatal(err)
				}
				if raw.FitsScale(decimal.BaseAmountStoreScale) {
					t.Fatalf("用例前提失效: %d 位结果 %s 本应无法被 %d 位定点列容纳",
						c.Scale(), raw, decimal.BaseAmountStoreScale)
				}
			}

			_, err := Apply(Input{
				Amount:       dec("100.00"),
				Rate:         dec("7.12345678"),
				BaseCurrency: c,
			}, TriggerRecalc)
			if !errors.Is(err, ErrBaseCurrencyNotCandidate) {
				t.Errorf("%s 应返回 ErrBaseCurrencyNotCandidate, 得 %v", c, err)
			}
		})
	}

	// 全部候选币种都应放行，且结果都能无损落库
	candidates := currency.BaseCandidates()
	if len(candidates) == 0 {
		t.Fatal("本位币候选集为空")
	}
	for _, base := range candidates {
		t.Run("候选/"+base.String(), func(t *testing.T) {
			got, err := Apply(Input{
				Amount:       dec("100.00"),
				Rate:         dec("7.12345678"),
				BaseCurrency: base,
			}, TriggerRecalc)
			if err != nil {
				t.Fatalf("候选币种 %s 应当放行, 得错误: %v", base, err)
			}
			if _, err := got.BaseAmount.Units(decimal.BaseAmountStoreScale); err != nil {
				t.Errorf("%s 算出的 %s 无法落库: %v", base, got.BaseAmount, err)
			}
		})
	}
}

// TestRoundingBasisMatchesOutputCurrency 锁死本包刻意维持的性质：
// 舍入基准与输出的 折算本位币币种 恒为同一个币种。
//
// 用 A 币种的位数算出一个标着 B 币种的金额，这个数在任何口径下都不成立。
// 用「原口径 JPY（0 位）、当前本位币 CNY（2 位）」这种两侧位数不同的组合，
// 一旦取错基准立刻显形。
func TestRoundingBasisMatchesOutputCurrency(t *testing.T) {
	in := Input{
		Amount:            dec("100.00"),
		Rate:              dec("7.12345678"),
		BaseCurrency:      cur("CNY"),
		PriorBaseCurrency: cur("JPY"),
	}

	// 改支出金额：取原口径 JPY，故应按 0 位舍入
	got, err := Apply(in, TriggerAmountChanged)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if got.BaseCurrency.String() != "JPY" {
		t.Fatalf("折算本位币币种 = %s, 期望 JPY", got.BaseCurrency)
	}
	if got.BaseAmount.String() != "712" {
		t.Errorf("标着 JPY 却按 %s 舍入: 得 %s, 期望 712（JPY 的 0 位）",
			"非 JPY 的位数", got.BaseAmount)
	}

	// 本位币重算：取当前本位币 CNY，故应按 2 位舍入
	got, err = Apply(in, TriggerRecalc)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if got.BaseCurrency.String() != "CNY" {
		t.Fatalf("折算本位币币种 = %s, 期望 CNY", got.BaseCurrency)
	}
	if got.BaseAmount.String() != "712.35" {
		t.Errorf("标着 CNY 却按非 CNY 的位数舍入: 得 %s, 期望 712.35", got.BaseAmount)
	}

	// 通用断言：任意触发下，输出金额都必须能被输出币种的位数无损表示
	for _, tr := range Triggers() {
		out, err := Apply(in, tr)
		if err != nil {
			t.Fatalf("%s: %v", tr, err)
		}
		if out.BaseCurrency.IsZero() {
			continue // 不重算的触发原样回传，可能为零值
		}
		if !out.BaseAmount.FitsScale(out.BaseCurrency.Scale()) {
			t.Errorf("%s: 金额 %s 无法被输出币种 %s 的 %d 位无损表示",
				tr, out.BaseAmount, out.BaseCurrency, out.BaseCurrency.Scale())
		}
	}
}

// TestUnconvergedRowStaysIdentifiable 锁死不变式 1a 第 2 条的实际后果。
//
// 未收敛行（折算本位币币种 ≠ 当前本位币）在「改支出金额」后必须**仍然**
// 未收敛，否则它再也不会被 I-7 识别为待重算行——金额按旧汇率算、口径却
// 标成新本位币，错得不留痕迹（终版 8.1 末段）。
func TestUnconvergedRowStaysIdentifiable(t *testing.T) {
	const current = "CNY"
	in := Input{
		Amount:            dec("200.00"),
		Rate:              dec("7.12345678"),
		BaseCurrency:      cur(current),
		PriorBaseCurrency: cur("USD"), // 未收敛
	}

	got, err := Apply(in, TriggerAmountChanged)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	// 不变式 2 的判据：折算本位币币种 ≠ User.本位币 即待重算
	if got.BaseCurrency.Equal(cur(current)) {
		t.Fatal("改支出金额后该行被标成已收敛，I-7 将不再重算它——" +
			"手填汇率会被静默沿用，且该行永远收敛不了")
	}
	if got.BaseCurrency.String() != "USD" {
		t.Errorf("折算本位币币种 = %s, 期望保持 USD", got.BaseCurrency)
	}

	// 反面：I-7 重算后该行必须收敛
	got, err = Apply(in, TriggerRecalc)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if !got.BaseCurrency.Equal(cur(current)) {
		t.Errorf("I-7 重算后应收敛为 %s, 得 %s", current, got.BaseCurrency)
	}
}

// TestIdentityHolds 锁死不变式 1a 的恒等式那一半：
// 用**输出三元组**验算，本位币金额 = 支出金额 × 折算汇率（按输出币种位数舍入）。
//
// 强调「用输出的汇率」：落库的是输出汇率，若拿输入汇率验算，规整带来的
// 差异会被掩盖。
func TestIdentityHolds(t *testing.T) {
	cases := []struct{ amount, rate, base string }{
		{"100.00", "7.12345678", "CNY"},
		{"0.01", "7.123456789", "CNY"},
		{"-88.88", "0.13800001", "JPY"},
		{"123456.78", "1.00000001", "USD"},
		{"1", "0.00000001", "CNY"},
		{"999999.99", "7.99999999", "KRW"},
	}
	for _, tc := range cases {
		t.Run(tc.amount+"×"+tc.rate+"→"+tc.base, func(t *testing.T) {
			in := Input{
				Amount:       dec(tc.amount),
				Rate:         dec(tc.rate),
				BaseCurrency: cur(tc.base),
			}
			got, err := Apply(in, TriggerRecalc)
			if err != nil {
				t.Fatalf("Apply 失败: %v", err)
			}
			want, err := in.Amount.Mul(got.Rate).Round(got.BaseCurrency.Scale(), decimal.RoundHalfUp)
			if err != nil {
				t.Fatalf("验算失败: %v", err)
			}
			if !got.BaseAmount.Equal(want) {
				t.Errorf("恒等式不成立: 输出 %s, 验算 %s", got.BaseAmount, want)
			}
		})
	}
}

// TestPureFunction 锁死「纯函数」：无状态、可并发、不修改入参。
//
// 分层准则 2 要求 L2 可脱库单测；service 层会在 I-7 批量重算里并发调用。
func TestPureFunction(t *testing.T) {
	in := Input{
		Amount:            dec("100.00"),
		Rate:              dec("7.123456789"),
		BaseCurrency:      cur("CNY"),
		PriorBaseCurrency: cur("USD"),
	}
	snapshot := in

	first, err := Apply(in, TriggerRecalc)
	if err != nil {
		t.Fatal(err)
	}
	// 入参不得被修改
	if !in.Amount.Equal(snapshot.Amount) || !in.Rate.Equal(snapshot.Rate) ||
		!in.BaseCurrency.Equal(snapshot.BaseCurrency) ||
		!in.PriorBaseCurrency.Equal(snapshot.PriorBaseCurrency) {
		t.Error("Apply 修改了入参")
	}
	// 幂等：同样入参恒得同样出参
	for i := 0; i < 50; i++ {
		got, err := Apply(in, TriggerRecalc)
		if err != nil {
			t.Fatal(err)
		}
		if !got.BaseAmount.Equal(first.BaseAmount) || !got.Rate.Equal(first.Rate) ||
			!got.BaseCurrency.Equal(first.BaseCurrency) {
			t.Fatalf("第 %d 次结果不同: %+v vs %+v", i, got, first)
		}
	}

	// 并发：-race 下跑，读共享的 triggerTable 不得有写
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 200; j++ {
				for _, tr := range Triggers() {
					out, err := Apply(in, tr)
					if err != nil {
						t.Errorf("并发 Apply 失败: %v", err)
						return
					}
					_ = out
				}
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

// TestRoundNeverFailsForAnyCurrency 锁死 Apply 中那条 Round 失败分支的前提。
//
// 该分支在当前币种表下不可达（覆盖率里显示为未覆盖），Apply 的注释据此写着
// 「按当前币种表不可达」。本用例把这句话变成可执行断言：遍历**全部**币种，
// 确认按其位数舍入恒不失败。若将来有人给币种表加了一个位数 > MaxScale 的
// 条目，这句注释就不再成立——本用例会先失败，而不是留一句过期的注释。
func TestRoundNeverFailsForAnyCurrency(t *testing.T) {
	all := currency.All()
	if len(all) == 0 {
		t.Fatal("币种表为空——用例形同虚设")
	}
	// 取一批容易触发进位与长尾的乘积
	amounts := []string{"100.00", "-0.005", "999999.99", "0.01", "123456.789"}
	for _, c := range all {
		if c.Scale() < 0 || c.Scale() > decimal.MaxScale {
			t.Errorf("币种 %s 的小数位数 %d 越出 [0,%d]，Apply 中的 Round 会失败",
				c, c.Scale(), decimal.MaxScale)
			continue
		}
		for _, a := range amounts {
			if _, err := dec(a).Mul(dec("7.12345678")).
				Round(c.Scale(), decimal.RoundHalfUp); err != nil {
				t.Errorf("%s × 汇率按 %s 的 %d 位舍入失败: %v", a, c, c.Scale(), err)
			}
		}
	}
}

// TestSentinelsDistinct 确认五个哨兵互不相等——否则上层无法用 errors.Is
// 区分「本位币未指定」（可能是系统错误）与「汇率未填」（用户输入错误）。
func TestSentinelsDistinct(t *testing.T) {
	sentinels := map[string]error{
		"ErrTrigger":                  ErrTrigger,
		"ErrBaseCurrencyEmpty":        ErrBaseCurrencyEmpty,
		"ErrBaseCurrencyNotCandidate": ErrBaseCurrencyNotCandidate,
		"ErrPriorBaseCurrencyEmpty":   ErrPriorBaseCurrencyEmpty,
		"ErrRateNotPositive":          ErrRateNotPositive,
	}
	for nameA, a := range sentinels {
		if a == nil {
			t.Errorf("%s 为 nil", nameA)
			continue
		}
		if !strings.HasPrefix(a.Error(), "convert: ") {
			t.Errorf("%s 的错误文案未带包名前缀: %q", nameA, a.Error())
		}
		for nameB, b := range sentinels {
			if nameA == nameB {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("%s 与 %s 无法用 errors.Is 区分", nameA, nameB)
			}
		}
	}
}

// TestSentinelCarriesNoInitStack 与 currency / decimal 同构：哨兵不得自带
// 包初始化时的调用栈。
//
// 用 pkg/errors.New 构造包级哨兵会在 init 阶段抓栈，使 base/errs 的补栈
// 逻辑跳过，middleware 打 %+v 时栈顶变成 convert.init 而非真实出错点。
func TestSentinelCarriesNoInitStack(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析包源码失败: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("未解析到任何生产代码文件——用例形同虚设")
	}

	scanned := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			scanned++
			for _, imp := range file.Imports {
				if imp.Path.Value == `"github.com/pkg/errors"` {
					t.Errorf("%s import 了 github.com/pkg/errors。哨兵一律用标准库 "+
						"errors.New + fmt.Errorf(\"%%w: …\")", path)
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("未扫描到任何生产代码文件——用例形同虚设")
	}

	type stackTracer interface{ StackTrace() []uintptr }
	for name, sentinel := range map[string]error{
		"ErrTrigger":                  ErrTrigger,
		"ErrBaseCurrencyEmpty":        ErrBaseCurrencyEmpty,
		"ErrBaseCurrencyNotCandidate": ErrBaseCurrencyNotCandidate,
		"ErrPriorBaseCurrencyEmpty":   ErrPriorBaseCurrencyEmpty,
		"ErrRateNotPositive":          ErrRateNotPositive,
	} {
		var st stackTracer
		if errors.As(sentinel, &st) {
			t.Errorf("%s 自带调用栈，会让 base/errs 跳过补栈", name)
		}
	}
}

// TestNoUpwardDependency 锁死依赖边界。
//
// 本包处于 L2，只应依赖 currency（L1.0）与 base/decimal（L0）。
// 出现 entity / store / fxrate / service / api 等同层或上层包即构成越界
// （entity 的理由见包注释「为什么不收 entity.Expense」）；出现 base/errs
// 则违反「错误是哨兵、本包不选档位」。
func TestNoUpwardDependency(t *testing.T) {
	allowed := map[string]bool{
		`"github.com/cellargalaxy/jotcash/internal/base/decimal"`: true,
		`"github.com/cellargalaxy/jotcash/internal/currency"`:     true,
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析包源码失败: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("未解析到任何生产代码文件——用例形同虚设")
	}

	const modulePrefix = `"github.com/cellargalaxy/jotcash/`
	scanned := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			scanned++
			for _, imp := range file.Imports {
				p := imp.Path.Value
				if !strings.HasPrefix(p, modulePrefix) {
					continue
				}
				if !allowed[p] {
					t.Errorf("%s import 了 %s。本包处于 L2，只应依赖 currency 与 "+
						"base/decimal；错误一律为哨兵，不得 import base/errs", path, p)
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("未扫描到任何生产代码文件——用例形同虚设")
	}
}
