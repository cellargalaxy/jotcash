package decimal

// 包内单测：三处精度口径（原币金额 / 本位币金额算与存 / 汇率 8 位）与舍入方式。
// 每组用例都直接对应决策文档的一条规则，注释里写明是哪一条。

import (
	"testing"

	"github.com/pkg/errors"
)

// TestRoundHalfUpDirection 四舍五入必须是「半值远离零」且负数对称，
// 不是银行家舍入（底层库另有 RoundBank，本包不用）。
// 负数对称是前提 2 的要求：一笔退款与其对应支出若舍入方向不对称，I-7 重算后正负不相消。
func TestRoundHalfUpDirection(t *testing.T) {
	cases := []struct {
		in    string
		scale int32
		want  string
	}{
		{"2.5", 0, "3"}, // 半值远离零，非银行家舍入（那会得 2）
		{"3.5", 0, "4"},
		{"-2.5", 0, "-3"}, // 负数对称
		{"-3.5", 0, "-4"},
		{"0.005", 2, "0.01"},
		{"-0.005", 2, "-0.01"},
		{"2.675", 2, "2.68"}, // float64 下会得 2.67，定点下必须是 2.68
		{"1.234", 2, "1.23"}, // 不足半值，舍去
		{"1.235", 2, "1.24"},
		{"1.2", 5, "1.2"}, // 目标位数更多，值不变
		{"0", 2, "0"},
	}
	for _, c := range cases {
		got, err := Round(MustFromString(c.in), c.scale, RoundHalfUp)
		if err != nil {
			t.Errorf("Round(%s, %d) 异常: %+v", c.in, c.scale, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("Round(%s, %d, 四舍五入) = %v, 期望 %v", c.in, c.scale, got.String(), c.want)
		}
	}

	// 对称性总校验：Round(-x) 必须恒等于 -Round(x)
	for _, s := range []string{"2.5", "0.005", "1.235", "2.675", "1204.505", "0.125"} {
		pos := MustFromString(s)
		gotPos, err := Round(pos, 2, RoundHalfUp)
		if err != nil {
			t.Fatalf("Round(%s) 异常: %+v", s, err)
		}
		gotNeg, err := Round(pos.Neg(), 2, RoundHalfUp)
		if err != nil {
			t.Fatalf("Round(-%s) 异常: %+v", s, err)
		}
		if !gotNeg.Equal(gotPos.Neg()) {
			t.Errorf("舍入不对称: Round(-%s) = %v, 期望 %v", s, gotNeg.String(), gotPos.Neg().String())
		}
	}
}

// TestRoundTruncate 向零截断，负数同样对称。
func TestRoundTruncate(t *testing.T) {
	cases := []struct {
		in    string
		scale int32
		want  string
	}{
		{"2.9", 0, "2"},
		{"-2.9", 0, "-2"}, // 向零，不是向下
		{"33.3333", 2, "33.33"},
		{"-33.3333", 2, "-33.33"},
		{"1.999", 2, "1.99"},
		{"1.2", 5, "1.2"},
	}
	for _, c := range cases {
		got, err := Truncate(MustFromString(c.in), c.scale)
		if err != nil {
			t.Errorf("Truncate(%s, %d) 异常: %+v", c.in, c.scale, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("Truncate(%s, %d) = %v, 期望 %v", c.in, c.scale, got.String(), c.want)
		}
	}
}

// TestRoundInvalidArgs 位数与舍入方式非法必须报错。
// 舍入方式不设默认分支：零值若被默认成四舍五入，调用方漏传参数时会静默走错口径。
func TestRoundInvalidArgs(t *testing.T) {
	d := MustFromString("1.5")
	if _, err := Round(d, -1, RoundHalfUp); !errors.Is(err, ErrScale) {
		t.Errorf("负位数期望 ErrScale，实际 %+v", err)
	}
	if _, err := Round(d, MaxScale+1, RoundHalfUp); !errors.Is(err, ErrScale) {
		t.Errorf("超上限位数期望 ErrScale，实际 %+v", err)
	}
	if _, err := Round(d, 2, RoundMode(0)); !errors.Is(err, ErrRoundMode) {
		t.Errorf("零值舍入方式期望 ErrRoundMode（不得默认成四舍五入），实际 %+v", err)
	}
	if _, err := Round(d, 2, RoundMode(99)); !errors.Is(err, ErrRoundMode) {
		t.Errorf("未定义舍入方式期望 ErrRoundMode，实际 %+v", err)
	}
	// 枚举自校验
	if !RoundHalfUp.Valid() || !RoundTruncate.Valid() || RoundMode(0).Valid() {
		t.Errorf("RoundMode.Valid 判定有误")
	}
	if RoundHalfUp.String() != "四舍五入" || RoundTruncate.String() != "向零截断" {
		t.Errorf("RoundMode.String 中文名称有误")
	}
}

// TestRoundAmountByCurrencyScale 精度口径①②的「算」：按币种小数位数舍入。
// 位数由调用方从 currency 取来传入——本包不认识币种（约束 2）。
func TestRoundAmountByCurrencyScale(t *testing.T) {
	cases := []struct {
		name          string
		in            string
		currencyScale int32
		want          string
	}{
		{"JPY 0 位", "1204.56", 0, "1205"},
		{"JPY 0 位·半值", "1204.5", 0, "1205"},
		{"某 1 位币种", "1204.55", 1, "1204.6"},
		{"CNY 2 位", "1204.555", 2, "1204.56"},
		{"CNY 2 位·恰好", "1204.50", 2, "1204.5"},
		{"KWD 3 位（可作支出币种，T38 不约束支出币种）", "1204.5555", 3, "1204.556"},
		{"负金额（退款）按 2 位", "-1204.555", 2, "-1204.56"},
	}
	for _, c := range cases {
		got, err := RoundAmount(MustFromString(c.in), c.currencyScale)
		if err != nil {
			t.Errorf("%s 异常: %+v", c.name, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("%s: RoundAmount(%s, %d) = %v, 期望 %v", c.name, c.in, c.currencyScale, got.String(), c.want)
		}
	}
}

// TestRoundRate 精度口径③：汇率 8 位、四舍五入（T39）。
func TestRoundRate(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"7.834567891", "7.83456789"}, // 第 9 位为 1，舍去
		{"7.834567895", "7.8345679"},  // 第 9 位为 5，进位（String 去尾零）
		{"7.1234567849", "7.12345678"},
		{"1", "1"},                   // I-4：原币 = 本位币时汇率取 1
		{"0.00000001", "0.00000001"}, // 8 位下限
		{"0.000000004", "0"},         // 小于半个最小单位，舍为 0
		{"7.83", "7.83"},             // 位数不足，值不变
	}
	for _, c := range cases {
		got, err := RoundRate(MustFromString(c.in))
		if err != nil {
			t.Errorf("RoundRate(%s) 异常: %+v", c.in, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("RoundRate(%s) = %v, 期望 %v", c.in, got.String(), c.want)
		}
		if got.Scale() > RateScale {
			t.Errorf("RoundRate(%s) 结果小数位 %d 超过 %d", c.in, got.Scale(), RateScale)
		}
	}
	// 常量口径不得被改动
	if RateScale != 8 {
		t.Errorf("RateScale = %d, 期望 8（T39）", RateScale)
	}
	if BaseAmountStoreScale != 2 {
		t.Errorf("BaseAmountStoreScale = %d, 期望 2（T35）", BaseAmountStoreScale)
	}
}

// TestFixedStringAssertsNoLoss 落库规整必须**断言无损**而非静默舍入。
// 底层 StringFixed 会把 1.567 直接写成 "1.57"，若落库依赖它，
// 口径错配会表现为「库里的钱少了几分」且无任何报错。
func TestFixedStringAssertsNoLoss(t *testing.T) {
	// 无损：补零、不省略尾零
	ok := []struct {
		in    string
		scale int32
		want  string
	}{
		{"1204.5", 2, "1204.50"},
		{"1204", 2, "1204.00"},
		{"0", 2, "0.00"},
		{"-1204.5", 2, "-1204.50"},
		{"1205", 0, "1205"},
		{"7.83456789", 8, "7.83456789"},
		{"1", 8, "1.00000000"},
	}
	for _, c := range ok {
		got, err := FixedString(MustFromString(c.in), c.scale)
		if err != nil {
			t.Errorf("FixedString(%s, %d) 异常: %+v", c.in, c.scale, err)
			continue
		}
		if got != c.want {
			t.Errorf("FixedString(%s, %d) = %v, 期望 %v", c.in, c.scale, got, c.want)
		}
	}

	// 有损：必须报错，绝不静默舍入
	loss := []struct {
		in    string
		scale int32
	}{
		{"1.567", 2},
		{"1204.555", 2},
		{"1204.5", 0},
		{"7.123456789", 8},
	}
	for _, c := range loss {
		got, err := FixedString(MustFromString(c.in), c.scale)
		if !errors.Is(err, ErrPrecisionLoss) {
			t.Errorf("FixedString(%s, %d) 期望 ErrPrecisionLoss（不得静默舍入），实际 got=%v err=%+v",
				c.in, c.scale, got, err)
		}
	}

	// 位数非法
	if _, err := FixedString(MustFromString("1.5"), -1); !errors.Is(err, ErrScale) {
		t.Errorf("FixedString 负位数期望 ErrScale，实际 %+v", err)
	}
}

// TestStoreEntries 两个落库入口：本位币金额 2 位（T35）、汇率 8 位（T39）。
func TestStoreEntries(t *testing.T) {
	// 本位币金额：0/1/2 位结果均能被 2 位定点无损容纳（T35 边界）
	for _, c := range []struct{ in, want string }{
		{"1205", "1205.00"},   // 本位币为 0 位币种时的结果
		{"1204.5", "1204.50"}, // 1 位
		{"1204.56", "1204.56"},
		{"0", "0.00"},
		{"-1204.56", "-1204.56"},
	} {
		got, err := StoreBaseAmount(MustFromString(c.in))
		if err != nil {
			t.Errorf("StoreBaseAmount(%s) 异常: %+v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("StoreBaseAmount(%s) = %v, 期望 %v", c.in, got, c.want)
		}
	}
	// 3 位小数说明上游漏舍或本位币被配成 >2 位币种（T38 限定 ≤2 位），必须当场暴露
	if _, err := StoreBaseAmount(MustFromString("1204.555")); !errors.Is(err, ErrPrecisionLoss) {
		t.Errorf("本位币金额 3 位小数期望 ErrPrecisionLoss，实际 %+v", err)
	}

	// 汇率：8 位定点
	if got, err := StoreRate(MustFromString("7.83456789")); err != nil || got != "7.83456789" {
		t.Errorf("StoreRate = %v, err=%+v, 期望 7.83456789", got, err)
	}
	if got, err := StoreRate(One()); err != nil || got != "1.00000000" {
		t.Errorf("StoreRate(1) = %v, err=%+v, 期望 1.00000000（I-4 汇率取 1）", got, err)
	}
	if _, err := StoreRate(MustFromString("7.123456789")); !errors.Is(err, ErrPrecisionLoss) {
		t.Errorf("汇率 9 位小数期望 ErrPrecisionLoss（应先经 RoundRate），实际 %+v", err)
	}
}

// TestConvertChain 不变式 1a 全链路预演：
// 汇率先规整 8 位 → 精确相乘（中途不舍）→ 按**本位币**小数位舍一次 → 落库 2 位无损。
// 这是本包三处口径的联合验证，也是 rule/convert 将来的调用形状。
func TestConvertChain(t *testing.T) {
	amount := MustFromString("1234.56")      // 原币金额（CNY 2 位）
	rawRate := MustFromString("7.834567891") // 汇率源返回，位数不设限（T39②）

	rate, err := RoundRate(rawRate)
	if err != nil {
		t.Fatalf("汇率规整异常: %+v", err)
	}
	if rate.String() != "7.83456789" {
		t.Fatalf("汇率规整 = %v, 期望 7.83456789", rate.String())
	}

	product, err := amount.Mul(rate)
	if err != nil {
		t.Fatalf("折算相乘异常: %+v", err)
	}
	// 精确乘积，未预舍入。
	// 独立核对（整数乘法，不经小数）：123456 × 783456789 = 96722441342784，
	// 两个因子共 2 + 8 = 10 位小数，故精确积为 9672.2441342784。
	if product.String() != "9672.2441342784" {
		t.Fatalf("精确乘积 = %v, 期望 9672.2441342784", product.String())
	}
	if product.Scale() != 10 {
		t.Errorf("精确乘积小数位 = %d, 期望 10（2 + 8，未预舍入）", product.Scale())
	}

	// 按本位币小数位各舍一次；三种位数的结果都必须能被 2 位定点无损容纳
	for _, c := range []struct {
		name      string
		baseScale int32
		wantRound string
		wantStore string
	}{
		{"本位币 2 位（CNY）", 2, "9672.24", "9672.24"},
		{"本位币 1 位", 1, "9672.2", "9672.20"},
		{"本位币 0 位（JPY）", 0, "9672", "9672.00"},
	} {
		rounded, err := RoundAmount(product, c.baseScale)
		if err != nil {
			t.Errorf("%s 舍入异常: %+v", c.name, err)
			continue
		}
		if rounded.String() != c.wantRound {
			t.Errorf("%s 舍入 = %v, 期望 %v", c.name, rounded.String(), c.wantRound)
		}
		stored, err := StoreBaseAmount(rounded)
		if err != nil {
			t.Errorf("%s 落库异常（T35 声称 0/1 位结果无损容纳）: %+v", c.name, err)
			continue
		}
		if stored != c.wantStore {
			t.Errorf("%s 落库 = %v, 期望 %v", c.name, stored, c.wantStore)
		}
	}

	// I-4：原币 = 本位币时汇率取 1，折算后金额不变
	same, err := amount.Mul(One())
	if err != nil {
		t.Fatalf("汇率 1 折算异常: %+v", err)
	}
	if !same.Equal(amount) {
		t.Errorf("汇率取 1 折算后 = %v, 期望与原币相等 %v", same.String(), amount.String())
	}
}

// TestAmortizeOperators 8.2 摊分算子预演：「按币种小数位数均分、尾差归末月」，
// 前 N−1 月同值、末月吸收尾差，三份之和必须与总额**恒等**。
// 前 N−1 月用截断还是四舍五入由 rule/amortize 定案（answer §四 待确认 2），
// 本包只保证两种算子都可用、且恒等式在两种下都成立。
func TestAmortizeOperators(t *testing.T) {
	cases := []struct {
		name   string
		total  string
		months int64
		scale  int32
		want   []string // 逐月金额，末位为末月
	}{
		{"100 / 3，2 位（CNY）", "100", 3, 2, []string{"33.33", "33.33", "33.34"}},
		{"负金额（退款）-100 / 3，2 位", "-100", 3, 2, []string{"-33.33", "-33.33", "-33.34"}},
		{"100 / 3，0 位（JPY）", "100", 3, 0, []string{"33", "33", "34"}},
		{"整除无尾差", "90", 3, 2, []string{"30", "30", "30"}},
		{"单月", "100", 1, 2, []string{"100"}},
	}
	for _, c := range cases {
		total := MustFromString(c.total)
		months := NewFromInt(c.months)

		// 前 N−1 月：按币种小数位截断
		per, err := total.DivTruncate(months, c.scale)
		if err != nil {
			t.Errorf("%s 均分异常: %+v", c.name, err)
			continue
		}
		got := make([]string, 0, c.months)
		sum := Zero()
		for i := int64(0); i < c.months-1; i++ {
			got = append(got, per.String())
			sum = sum.Add(per)
		}
		// 末月吸收尾差
		last := total.Sub(sum)
		got = append(got, last.String())
		sum = sum.Add(last)

		if len(got) != len(c.want) {
			t.Errorf("%s 月份数 = %d, 期望 %d", c.name, len(got), len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s 第 %d 月 = %v, 期望 %v", c.name, i+1, got[i], c.want[i])
			}
		}
		// 恒等式：逐月之和必须精确等于总额，一分不差
		if !sum.Equal(total) {
			t.Errorf("%s 逐月之和 = %v, 期望恒等于总额 %v", c.name, sum.String(), total.String())
		}
	}

	// DivRound 同样可用，且恒等式在「末月吸收尾差」下依然成立
	total := MustFromString("100")
	per, err := total.DivRound(NewFromInt(3), 2)
	if err != nil {
		t.Fatalf("DivRound 异常: %+v", err)
	}
	if per.String() != "33.33" {
		t.Errorf("DivRound(100, 3, 2) = %v, 期望 33.33", per.String())
	}
	twice, err := per.Mul(NewFromInt(2))
	if err != nil {
		t.Fatalf("乘法异常: %+v", err)
	}
	if !twice.Add(total.Sub(twice)).Equal(total) {
		t.Errorf("DivRound 路径下恒等式不成立")
	}

	// 除零：报错而非 panic
	if _, err := total.DivTruncate(Zero(), 2); !errors.Is(err, ErrDivZero) {
		t.Errorf("DivTruncate 除零期望 ErrDivZero，实际 %+v", err)
	}
	if _, err := total.DivRound(Zero(), 2); !errors.Is(err, ErrDivZero) {
		t.Errorf("DivRound 除零期望 ErrDivZero，实际 %+v", err)
	}
	// 位数非法
	if _, err := total.DivTruncate(NewFromInt(3), -1); !errors.Is(err, ErrScale) {
		t.Errorf("DivTruncate 负位数期望 ErrScale，实际 %+v", err)
	}
	if _, err := total.DivRound(NewFromInt(3), MaxScale+1); !errors.Is(err, ErrScale) {
		t.Errorf("DivRound 超上限位数期望 ErrScale，实际 %+v", err)
	}
}
