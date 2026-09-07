package decimal

import (
	"errors"
	"strings"
	"testing"

	sdecimal "github.com/shopspring/decimal"
)

// TestParse 覆盖解析的合法与非法输入。
func TestParse(t *testing.T) {
	okCases := []struct{ text, want string }{
		{"0", "0"},
		{"100.50", "100.5"}, //尾随零在数值上无意义，String 不保留
		{"-33.34", "-33.34"},
		{"7.12345678", "7.12345678"},
		{".5", "0.5"},
		{"1.", "1"},
		{"+1.5", "1.5"},
		{"1e3", "1000"},
		{"-0", "0"},
		{"0.001", "0.001"},
	}
	for _, c := range okCases {
		d, err := Parse(c.text)
		if err != nil {
			t.Errorf("Parse(%q) 报错: %v", c.text, err)
			continue
		}
		if d.String() != c.want {
			t.Errorf("Parse(%q).String() = %q, 期望 %q", c.text, d.String(), c.want)
		}
	}

	//非法文本：本包不做空格清洗、不认千分位，理由见 ErrSyntax 注释
	syntaxCases := []string{"", " ", "abc", " 1.5", "1.5 ", "1,000.50", "1_000", "0x10", "NaN", "Infinity", "--1", "1.2.3"}
	for _, text := range syntaxCases {
		if _, err := Parse(text); !errors.Is(err, ErrSyntax) {
			t.Errorf("Parse(%q) 期望 ErrSyntax, 实际 %v", text, err)
		}
	}
}

// TestParseMagnitudeLimit 验证入口的量级设界。
//
// 这道校验拦的是拒绝服务路径：底层库能在纳秒内接受 1e1000000，
// 其后任何一次舍入都会产出百万位数字并吃掉数十毫秒 CPU 与数 MB 内存。
func TestParseMagnitudeLimit(t *testing.T) {
	//整数位数越界
	intCases := []string{
		strings.Repeat("9", MaxIntDigits+1),
		strings.Repeat("9", 30),
		"1e1000000",
		"1e100",
		"-" + strings.Repeat("9", MaxIntDigits+1),
	}
	for _, text := range intCases {
		if _, err := Parse(text); !errors.Is(err, ErrIntDigitsExceeded) {
			t.Errorf("Parse(%q) 期望 ErrIntDigitsExceeded, 实际 %v", text, err)
		}
	}

	//小数位数越界
	scaleCases := []string{
		"0." + strings.Repeat("0", MaxScale) + "1",
		"1e-2000",
		"0." + strings.Repeat("1", 40),
	}
	for _, text := range scaleCases {
		if _, err := Parse(text); !errors.Is(err, ErrScaleExceeded) {
			t.Errorf("Parse(%q) 期望 ErrScaleExceeded, 实际 %v", text, err)
		}
	}

	//恰好在界内必须放行，避免误拒真实数据
	boundary := []string{
		strings.Repeat("9", MaxIntDigits),
		"0." + strings.Repeat("0", MaxScale-1) + "1",
	}
	for _, text := range boundary {
		if _, err := Parse(text); err != nil {
			t.Errorf("Parse(%q) 恰在界内, 不应报错: %v", text, err)
		}
	}
}

// TestParseExtremeExponentMagnitude 锁死量级设界在**极端指数**下同样生效。
//
// 这是一条回归用例，防的是位数计算的整型回绕：底层库允许的指数上界恰为 int32
// 上界（实测 1e2147483647 解析成功、1e2147483648 才被拒），而「系数位数 + 指数」
// 若用 int32 相加会回绕成负数——1e2147483647 曾因此被算成「整数位数 1 位」而
// 通过校验，随后 normalize 会去计算 10^2147483648（约 850MB 的大整数）。
// 同理 -exp 在 int32 下也会回绕，使 1e-2147483648 被算成「小数位数 0 位」。
//
// 于是设界形同虚设，且恰好放行了危害最大的那一档输入，构成一条不需要任何权限的
// 拒绝服务路径（同一代码路径实测：exp=1e7 时 normalize 已耗时约 0.67 秒）。
// 判定改以 int64 计算后这两类输入均被拒。
//
// 本用例只断言「被拒」，不触发展开，因此自身耗时可忽略。
func TestParseExtremeExponentMagnitude(t *testing.T) {
	cases := []struct {
		text string
		want error
	}{
		//正向极端指数：整数位数越界
		{"1e2147483647", ErrIntDigitsExceeded},
		{"12e2147483646", ErrIntDigitsExceeded},
		{"1234567890e2147483640", ErrIntDigitsExceeded},
		{"1.5e2147483647", ErrIntDigitsExceeded},
		{"-1e2147483647", ErrIntDigitsExceeded},
		//负向极端指数：小数位数越界
		{"1e-2147483647", ErrScaleExceeded},
		{"1e-2147483648", ErrScaleExceeded},
	}
	for _, c := range cases {
		if _, err := Parse(c.text); !errors.Is(err, c.want) {
			t.Errorf("Parse(%q) 期望 %v, 实际 %v", c.text, c.want, err)
		}
		//JSON 入口与 Parse 共用同一道校验，不得存在绕过
		var d Decimal
		if err := d.UnmarshalJSON([]byte(`"` + c.text + `"`)); !errors.Is(err, c.want) {
			t.Errorf("UnmarshalJSON(%q) 期望 %v, 实际 %v", c.text, c.want, err)
		}
	}

	//指数超出 int32 时由底层库判为语法错误，同样不得放行
	if _, err := Parse("1e2147483648"); err == nil {
		t.Errorf("Parse(1e2147483648) 不应成功")
	}
}

// TestParseNormalizesRepresentation 锁死入口规范化：通过量级校验的值不得残留冗长表示。
//
// 这是量级设界能否真正生效的前提。设界看的是「有效位数」，于是「0.1 后接 12 万个 0」
// 这类文本有效位数只有 1 位、能合法通过校验，可若不规范化，内部就留下 12 万位系数，
// 此后每一次 Scale / FitsScale / Units / StringFixed 都要重新为这些零付一遍代价
// （修复前实测单次 0.4~1.7 秒），设界反而成了拒绝服务的入口。
//
// 用「有效位数」判定而非计时：性能断言在慢机器或并行跑测试时会假失败，
// 而底层表示是否压到有效位数是确定的。
func TestParseNormalizesRepresentation(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string //期望的数值（String 形态）
	}{
		{"大量尾随零", "0.1" + strings.Repeat("0", 100000), "0.1"},
		{"两位尾随零", "100.50", "100.5"},
		{"整数尾随零", "1.000", "1"},
		{"零的多位形态", "0.00", "0"},
		{"负数尾随零", "-12.340", "-12.34"},
		{"科学计数法", "1e3", "1000"},
		{"极小值补零", "0.000000005" + strings.Repeat("0", 50000), "0.000000005"},
	}
	for _, c := range cases {
		d, err := Parse(c.text)
		if err != nil {
			t.Errorf("%s: Parse 不应报错: %v", c.name, err)
			continue
		}
		if got := d.String(); got != c.want {
			t.Errorf("%s: 数值应为 %s, 实际 %s", c.name, c.want, got)
		}
		//规范化的判据：底层小数位数已等于有效位数，即再无尾随零可剥
		if exp, scale := -d.v.Exponent(), d.Scale(); exp != scale {
			t.Errorf("%s: 未规范化，底层小数位数 %d != 有效位数 %d（残留 %d 位尾随零）",
				c.name, exp, scale, exp-scale)
		}
	}
}

// TestParseNormalizeIsLossless 锁死规范化只动表示、不动数值。
//
// 规范化被放在金额入口上，一旦它改变数值就是最严重的一类错误，
// 故逐项核对数值本身与三种字符串输出、以及最小单位整数。
func TestParseNormalizeIsLossless(t *testing.T) {
	cases := []string{
		"0", "0.00", "-0", "1", "1.5", "1.50", "1.500", "100", "100.00",
		"1e3", "1e-3", "0.1", "0.10", "-1.50", "-0.05", "9999999999999999",
		"0.123456789012345678", "7.12345678", "0.000000005", "1000.000", "-12.340",
	}
	for _, text := range cases {
		//未经规范化的参照值：直连底层库，绕开 Parse
		want, err := sdecimal.NewFromString(text)
		if err != nil {
			t.Fatalf("%q: 参照值构造失败: %v", text, err)
		}
		got, err := Parse(text)
		if err != nil {
			t.Errorf("%q: Parse 报错: %v", text, err)
			continue
		}
		if !got.v.Equal(want) {
			t.Errorf("%q: 规范化改变了数值: %s != %s", text, got.v, want)
		}
		if got.String() != want.String() {
			t.Errorf("%q: String 变了: %q → %q", text, want.String(), got.String())
		}
		for _, scale := range []int32{0, 2, 8} {
			if g, w := got.v.StringFixed(scale), want.StringFixed(scale); g != w {
				t.Errorf("%q: StringFixed(%d) 变了: %q → %q", text, scale, w, g)
			}
			if g, w := got.v.Shift(scale).BigInt().String(), want.Shift(scale).BigInt().String(); g != w {
				t.Errorf("%q: 最小单位整数(%d 位) 变了: %s → %s", text, scale, w, g)
			}
		}
	}
}

// TestFromUnitsNormalizesRepresentation 锁死 FromUnits 与 Parse 得到一致的内部表示。
//
// FromUnits 的 scale 由列口径决定（金额固定 2 位），units 末位常是零：10050 → 100.50。
// 若不规范化，从库里读出的 100.50 与从账单解析的 100.5 数值相等而表示不同，
// 后续行为会因来源而异。
func TestFromUnitsNormalizesRepresentation(t *testing.T) {
	cases := []struct {
		units int64
		scale int32
		want  string
	}{
		{10050, 2, "100.5"},
		{10000, 2, "100"},
		{0, 2, "0"},
		{-10050, 2, "-100.5"},
		{712345678, 8, "7.12345678"},
		{10000000, 8, "0.1"},
	}
	for _, c := range cases {
		d, err := FromUnits(c.units, c.scale)
		if err != nil {
			t.Errorf("FromUnits(%d, %d) 报错: %v", c.units, c.scale, err)
			continue
		}
		if got := d.String(); got != c.want {
			t.Errorf("FromUnits(%d, %d) = %s, 期望 %s", c.units, c.scale, got, c.want)
		}
		if exp, scale := -d.v.Exponent(), d.Scale(); exp != scale {
			t.Errorf("FromUnits(%d, %d) 未规范化: 底层 %d 位 != 有效 %d 位", c.units, c.scale, exp, scale)
		}
		//与 Parse 同值时表示必须完全一致
		viaParse := MustParse(c.want)
		if d.v.Exponent() != viaParse.v.Exponent() {
			t.Errorf("FromUnits(%d, %d) 与 Parse(%q) 表示不一致: 指数 %d != %d",
				c.units, c.scale, c.want, d.v.Exponent(), viaParse.v.Exponent())
		}
	}
}

// TestMustParse 验证 MustParse 的成功与 panic 两条路径。
func TestMustParse(t *testing.T) {
	if got := MustParse("12.34").String(); got != "12.34" {
		t.Errorf("MustParse(\"12.34\") = %s, 期望 12.34", got)
	}

	defer func() {
		if recover() == nil {
			t.Error("MustParse 解析非法文本时应 panic")
		}
	}()
	MustParse("非法")
}

// TestFromInt 验证整数构造。
func TestFromInt(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{{0, "0"}, {1, "1"}, {-1, "-1"}, {100, "100"}}
	for _, c := range cases {
		if got := FromInt(c.in).String(); got != c.want {
			t.Errorf("FromInt(%d) = %s, 期望 %s", c.in, got, c.want)
		}
	}
}

// TestFromUnitsAndUnits 验证「整数最小单位」列形态的无损往返。
func TestFromUnitsAndUnits(t *testing.T) {
	cases := []struct {
		units int64
		scale int32
		want  string
	}{
		{10050, 2, "100.5"},
		{0, 2, "0"},
		{-3334, 2, "-33.34"},
		{712345678, 8, "7.12345678"},
		{200, 0, "200"},
		{1, 2, "0.01"},
		{-1, 2, "-0.01"},
	}
	for _, c := range cases {
		d, err := FromUnits(c.units, c.scale)
		if err != nil {
			t.Errorf("FromUnits(%d, %d) 报错: %v", c.units, c.scale, err)
			continue
		}
		if d.String() != c.want {
			t.Errorf("FromUnits(%d, %d) = %s, 期望 %s", c.units, c.scale, d.String(), c.want)
		}
		//往返回去必须拿到同一个整数
		back, err := d.Units(c.scale)
		if err != nil {
			t.Errorf("%s.Units(%d) 报错: %v", d.String(), c.scale, err)
			continue
		}
		if back != c.units {
			t.Errorf("%s.Units(%d) = %d, 期望往返回到 %d", d.String(), c.scale, back, c.units)
		}
	}

	//位数非法
	if _, err := FromUnits(1, -1); !errors.Is(err, ErrScaleInvalid) {
		t.Errorf("FromUnits 位数为负应报 ErrScaleInvalid, 实际 %v", err)
	}
	if _, err := FromUnits(1, MaxScale+1); !errors.Is(err, ErrScaleInvalid) {
		t.Errorf("FromUnits 位数越界应报 ErrScaleInvalid, 实际 %v", err)
	}

	//还原出来的值同样受量级设界约束：库里若存了一个超量级的脏数据，
	//回读时必须报错而不是把它当合法金额放进系统。
	//int64 上界为 19 位，按 0 位还原即 19 位整数，超过 MaxIntDigits。
	if _, err := FromUnits(9223372036854775807, 0); !errors.Is(err, ErrIntDigitsExceeded) {
		t.Errorf("FromUnits 还原出 19 位整数应报 ErrIntDigitsExceeded, 实际 %v", err)
	}
	if _, err := FromUnits(-9223372036854775808, 0); !errors.Is(err, ErrIntDigitsExceeded) {
		t.Errorf("FromUnits 还原出负 19 位整数应报 ErrIntDigitsExceeded, 实际 %v", err)
	}
	//而 16 位整数按 0 位还原恰在界内，必须放行
	if _, err := FromUnits(9999999999999999, 0); err != nil {
		t.Errorf("FromUnits 还原出 %d 位整数恰在界内, 不应报错: %v", MaxIntDigits, err)
	}
}

// TestUnitsPrecisionLossAndOverflow 验证落库前的两道显式校验。
func TestUnitsPrecisionLossAndOverflow(t *testing.T) {
	//无损性不足：1.005 需要 3 位，目标 2 位
	if _, err := MustParse("1.005").Units(2); !errors.Is(err, ErrPrecisionLoss) {
		t.Errorf("1.005 按 2 位换算应报 ErrPrecisionLoss, 实际 %v", err)
	}
	//汇率 8 位可容纳，不应报错
	if _, err := MustParse("7.12345678").Units(RateScale); err != nil {
		t.Errorf("8 位汇率按 %d 位换算不应报错: %v", RateScale, err)
	}

	//溢出：16 位整数按 8 位换算需 24 位，远超 int64
	big := MustParse(strings.Repeat("9", MaxIntDigits))
	if _, err := big.Units(RateScale); !errors.Is(err, ErrUnitsOverflow) {
		t.Errorf("%s 按 %d 位换算应报 ErrUnitsOverflow, 实际 %v", big.String(), RateScale, err)
	}
	//而按 2 位换算必须成功——这正是 MaxIntDigits 取 16 的理由
	if _, err := big.Units(BaseAmountStoreScale); err != nil {
		t.Errorf("%s 按 %d 位换算应成功（MaxIntDigits=%d 的设计前提）: %v",
			big.String(), BaseAmountStoreScale, MaxIntDigits, err)
	}

	if _, err := MustParse("1").Units(-1); !errors.Is(err, ErrScaleInvalid) {
		t.Errorf("Units 位数为负应报 ErrScaleInvalid, 实际 %v", err)
	}
}

// TestArithmetic 覆盖四则运算与符号操作。
func TestArithmetic(t *testing.T) {
	a, b := MustParse("100.50"), MustParse("33.34")

	if got := a.Add(b).String(); got != "133.84" {
		t.Errorf("100.50 + 33.34 = %s, 期望 133.84", got)
	}
	if got := a.Sub(b).String(); got != "67.16" {
		t.Errorf("100.50 - 33.34 = %s, 期望 67.16", got)
	}
	//乘法不舍入，保留全部有效位
	if got := a.Mul(MustParse("7.12345678")).String(); got != "715.90740639" {
		t.Errorf("100.50 × 7.12345678 = %s, 期望 715.90740639", got)
	}
	if got := b.Neg().String(); got != "-33.34" {
		t.Errorf("Neg = %s, 期望 -33.34", got)
	}
	if got := b.Neg().Abs().String(); got != "33.34" {
		t.Errorf("Abs = %s, 期望 33.34", got)
	}
}

// TestFloatingPointDrift 验证本包不受二进制浮点误差影响。
//
// 这是本包存在的直接理由：同样的运算用 float64 做会得到 0.30000000000000004，
// 逐笔累加后偏差会进入金额合计。
func TestFloatingPointDrift(t *testing.T) {
	//0.1 + 0.2 必须精确等于 0.3
	sum := MustParse("0.1").Add(MustParse("0.2"))
	if !sum.Equal(MustParse("0.3")) {
		t.Errorf("0.1 + 0.2 = %s, 期望精确等于 0.3", sum.String())
	}
	//float64 下的对照：确认这个坑真实存在，本包确实规避了它
	if 0.1+0.2 == 0.3 {
		t.Log("注意：本机 float64 下 0.1+0.2 == 0.3 成立，该对照未能体现浮点误差")
	}

	//1 分钱累加 100 次必须精确等于 1 元
	cent := MustParse("0.01")
	var total Decimal
	for i := 0; i < 100; i++ {
		total = total.Add(cent)
	}
	if !total.Equal(MustParse("1")) {
		t.Errorf("0.01 累加 100 次 = %s, 期望精确等于 1", total.String())
	}
}

// TestSum 验证求和，含空入参。
func TestSum(t *testing.T) {
	if got := Sum().String(); got != "0" {
		t.Errorf("Sum() 空入参 = %s, 期望 0（免去调用方为空列表分支）", got)
	}
	if got := Sum(MustParse("1.1")).String(); got != "1.1" {
		t.Errorf("Sum(单项) = %s, 期望 1.1", got)
	}
	got := Sum(MustParse("10.01"), MustParse("20.02"), MustParse("-5.03"))
	if got.String() != "25" {
		t.Errorf("Sum(10.01, 20.02, -5.03) = %s, 期望 25", got.String())
	}
}

// TestQuoRemIdentity 验证带余除法的恒等式「商 × 除数 + 余 = 被除数」。
//
// 该恒等式是 8.2 摊分的算术底座：各月取商、尾差取余，两者相加必然等于原金额，
// 不会因舍入而多出或丢失分位。这里对正负金额与 0/2 位币种各出用例。
func TestQuoRemIdentity(t *testing.T) {
	cases := []struct {
		total    string
		months   int64
		scale    int32
		wantQuo  string
		wantRem  string
		scenario string
	}{
		{"100.00", 3, 2, "33.33", "0.01", "2 位币种正金额"},
		{"-100.00", 3, 2, "-33.33", "-0.01", "2 位币种负金额（退款对称）"},
		{"1000", 3, 0, "333", "1", "0 位币种（如 JPY）"},
		{"100.00", 4, 2, "25", "0", "整除无尾差"},
		{"0.01", 3, 2, "0", "0.01", "金额小于份数"},
		{"-0.01", 3, 2, "0", "-0.01", "负金额小于份数"},
		{"1", 7, 2, "0.14", "0.02", "循环小数"},
		{"100.00", 1, 2, "100", "0", "不摊分（H-1 默认月数 1）"},
	}
	for _, c := range cases {
		total := MustParse(c.total)
		months := FromInt(c.months)
		quo, rem, err := total.QuoRem(months, c.scale)
		if err != nil {
			t.Errorf("[%s] QuoRem 报错: %v", c.scenario, err)
			continue
		}
		if quo.String() != c.wantQuo {
			t.Errorf("[%s] %s÷%d@%d 商 = %s, 期望 %s", c.scenario, c.total, c.months, c.scale, quo.String(), c.wantQuo)
		}
		if rem.String() != c.wantRem {
			t.Errorf("[%s] %s÷%d@%d 余 = %s, 期望 %s", c.scenario, c.total, c.months, c.scale, rem.String(), c.wantRem)
		}
		//恒等式：这是摊分不丢分位的保证
		back := quo.Mul(months).Add(rem)
		if !back.Equal(total) {
			t.Errorf("[%s] 商×份数+余 = %s, 期望回到 %s", c.scenario, back.String(), c.total)
		}
		//余数与被除数同号，保证负金额与正金额对称
		if !rem.IsZero() && total.Sign() != rem.Sign() {
			t.Errorf("[%s] 余数符号 %d 与被除数符号 %d 不一致", c.scenario, rem.Sign(), total.Sign())
		}
	}
}

// TestQuoRemDivideByZero 验证除零返回错误而非 panic。
//
// 底层库遇除零直接 panic，而摊分月数来自用户输入（F-1），
// 一次非法输入不应崩掉整个请求。
func TestQuoRemDivideByZero(t *testing.T) {
	if _, _, err := MustParse("100").QuoRem(FromInt(0), 2); !errors.Is(err, ErrDivideByZero) {
		t.Errorf("除零应报 ErrDivideByZero, 实际 %v", err)
	}
	//零值除数同样按除零处理
	var zero Decimal
	if _, _, err := MustParse("100").QuoRem(zero, 2); !errors.Is(err, ErrDivideByZero) {
		t.Errorf("零值除数应报 ErrDivideByZero, 实际 %v", err)
	}
	if _, _, err := MustParse("100").QuoRem(FromInt(3), -1); !errors.Is(err, ErrScaleInvalid) {
		t.Errorf("QuoRem 位数为负应报 ErrScaleInvalid, 实际 %v", err)
	}
}

// TestCompare 验证比较语义，重点是「数值相等而内部表示不同」。
func TestCompare(t *testing.T) {
	//1.50 与 1.5 数值相等——这正是 == 会判错、必须走 Equal 的场景
	if !MustParse("1.50").Equal(MustParse("1.5")) {
		t.Error("1.50 与 1.5 应数值相等")
	}
	//零值与 FromInt(0) 相等
	var zero Decimal
	if !zero.Equal(FromInt(0)) {
		t.Error("零值应与 FromInt(0) 相等")
	}
	if !MustParse("0.00").Equal(zero) {
		t.Error("0.00 应与零值相等")
	}

	small, large := MustParse("9.99"), MustParse("10.00")
	if small.Cmp(large) != -1 {
		t.Errorf("9.99.Cmp(10.00) = %d, 期望 -1", small.Cmp(large))
	}
	if large.Cmp(small) != 1 {
		t.Errorf("10.00.Cmp(9.99) = %d, 期望 1", large.Cmp(small))
	}
	if MustParse("1.50").Cmp(MustParse("1.5")) != 0 {
		t.Error("1.50.Cmp(1.5) 应为 0")
	}

	//符号判定：零既不算正也不算负
	if zero.IsNegative() || zero.IsPositive() || !zero.IsZero() || zero.Sign() != 0 {
		t.Error("零值的符号判定有误")
	}
	neg := MustParse("-0.01")
	if !neg.IsNegative() || neg.IsPositive() || neg.Sign() != -1 {
		t.Error("负数的符号判定有误")
	}
	pos := MustParse("0.01")
	if pos.IsNegative() || !pos.IsPositive() || pos.Sign() != 1 {
		t.Error("正数的符号判定有误")
	}
}

// TestZeroValueUsable 验证零值可直接使用而不 panic。
//
// 实体结构体以零值创建，业务上「不适用」的场景以零值表达，
// 上层不应被迫先判空再取值。
func TestZeroValueUsable(t *testing.T) {
	var z Decimal

	if z.String() != "0" {
		t.Errorf("零值 String = %q, 期望 \"0\"", z.String())
	}
	if !z.IsZero() || z.Sign() != 0 {
		t.Error("零值应判定为零")
	}
	if got := z.Add(FromInt(1)).String(); got != "1" {
		t.Errorf("零值 Add = %s, 期望 1", got)
	}
	if got := z.Sub(FromInt(1)).String(); got != "-1" {
		t.Errorf("零值 Sub = %s, 期望 -1", got)
	}
	if got := z.Mul(FromInt(5)).String(); got != "0" {
		t.Errorf("零值 Mul = %s, 期望 0", got)
	}
	if got := z.Neg().String(); got != "0" {
		t.Errorf("零值 Neg = %s, 期望 0", got)
	}
	if got := z.Abs().String(); got != "0" {
		t.Errorf("零值 Abs = %s, 期望 0", got)
	}
	if z.Scale() != 0 {
		t.Errorf("零值 Scale = %d, 期望 0", z.Scale())
	}
	if !z.FitsScale(0) || !z.FitsScale(BaseAmountStoreScale) {
		t.Error("零值应能被任何位数无损容纳")
	}
	if got := z.RoundRate().String(); got != "0" {
		t.Errorf("零值 RoundRate = %s, 期望 0", got)
	}
	//零值参与舍入、输出、换算均不应 panic
	rounded, err := z.Round(BaseAmountStoreScale, RoundHalfUp)
	if err != nil || !rounded.IsZero() {
		t.Errorf("零值 Round 结果 = %v, err = %v", rounded.String(), err)
	}
	text, err := z.StringFixed(BaseAmountStoreScale)
	if err != nil || text != "0.00" {
		t.Errorf("零值 StringFixed(2) = %q, err = %v, 期望 \"0.00\"", text, err)
	}
	units, err := z.Units(BaseAmountStoreScale)
	if err != nil || units != 0 {
		t.Errorf("零值 Units(2) = %d, err = %v, 期望 0", units, err)
	}
	//零值参与求和与带余除法
	if got := Sum(z, z).String(); got != "0" {
		t.Errorf("零值求和 = %s, 期望 0", got)
	}
	quo, rem, err := z.QuoRem(FromInt(3), BaseAmountStoreScale)
	if err != nil || !quo.IsZero() || !rem.IsZero() {
		t.Errorf("零值 QuoRem = (%v, %v), err = %v, 期望 (0, 0)", quo.String(), rem.String(), err)
	}
}
