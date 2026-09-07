package decimal

import (
	"errors"
	"math"
	"strings"
	"testing"

	sdecimal "github.com/shopspring/decimal"
)

// TestRoundHalfUp 验证四舍五入，重点是**负数对称**。
//
// 若底层换成银行家舍入，1.005→1.00、2.5→2 这类用例会立刻失败——
// 这正是本包不暴露银行家舍入的原因：两者签名一致，误用后差异只有一分钱。
func TestRoundHalfUp(t *testing.T) {
	cases := []struct {
		in    string
		scale int32
		want  string
	}{
		//进位边界：5 一律进位，不看前一位奇偶（区别于银行家舍入）
		{"1.005", 2, "1.01"},
		{"1.015", 2, "1.02"},
		{"1.025", 2, "1.03"},
		{"2.5", 0, "3"},
		{"0.5", 0, "1"},
		{"5.45", 1, "5.5"}, //银行家舍入会得 5.4
		//负数必须与正数完全对称
		{"-1.005", 2, "-1.01"},
		{"-1.015", 2, "-1.02"},
		{"-2.5", 0, "-3"},
		{"-0.5", 0, "-1"},
		{"-5.45", 1, "-5.5"},
		//舍去
		{"1.004", 2, "1"},
		{"-1.004", 2, "-1"},
		{"1.994", 2, "1.99"},
		//已在位数内不变
		{"1.5", 2, "1.5"},
		{"100", 2, "100"},
		{"0", 2, "0"},
	}
	for _, c := range cases {
		got, err := MustParse(c.in).Round(c.scale, RoundHalfUp)
		if err != nil {
			t.Errorf("Round(%q, %d, 四舍五入) 报错: %v", c.in, c.scale, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("Round(%q, %d, 四舍五入) = %s, 期望 %s", c.in, c.scale, got.String(), c.want)
		}
	}
}

// TestRoundHalfUpNegativeSymmetry 用「取绝对值后舍入」与「舍入后取绝对值」互证对称性。
//
// 对称性是退款与冲正（负金额）能与正常支出走同一套逻辑的前提。
func TestRoundHalfUpNegativeSymmetry(t *testing.T) {
	samples := []string{"1.005", "1.015", "2.5", "0.5", "5.45", "33.334", "0.001", "99.999"}
	for _, s := range samples {
		pos := MustParse(s)
		neg := pos.Neg()
		for _, scale := range []int32{0, 1, 2, 3} {
			roundedPos, err := pos.Round(scale, RoundHalfUp)
			if err != nil {
				t.Fatalf("Round(%s, %d) 报错: %v", s, scale, err)
			}
			roundedNeg, err := neg.Round(scale, RoundHalfUp)
			if err != nil {
				t.Fatalf("Round(-%s, %d) 报错: %v", s, scale, err)
			}
			//先取负再舍入，应等于先舍入再取负
			if !roundedNeg.Equal(roundedPos.Neg()) {
				t.Errorf("四舍五入不对称: Round(-%s, %d) = %s, 而 -Round(%s, %d) = %s",
					s, scale, roundedNeg.String(), s, scale, roundedPos.Neg().String())
			}
		}
	}
}

// TestRoundTruncate 验证向零截断，含负数对称。
//
// 摊分份额用截断而非四舍五入：若用四舍五入，各月份额之和可能超过原金额，
// 尾差会变成负数（如 100.00÷3 各月取 33.33 相加为 99.99，尾差 +0.01；
// 而若某位进位到 33.34，相加为 100.02，尾差 -0.02）。
func TestRoundTruncate(t *testing.T) {
	cases := []struct {
		in    string
		scale int32
		want  string
	}{
		{"1.999", 2, "1.99"},
		{"-1.999", 2, "-1.99"}, //向零截断：负数同样朝零方向
		{"1.005", 2, "1"},
		{"-1.005", 2, "-1"},
		{"0.005", 2, "0"},
		{"-0.005", 2, "0"},
		{"2.9", 0, "2"},
		{"-2.9", 0, "-2"},
		{"33.3333", 2, "33.33"},
		{"0", 2, "0"},
		{"1.5", 2, "1.5"},
	}
	for _, c := range cases {
		got, err := MustParse(c.in).Round(c.scale, RoundTruncate)
		if err != nil {
			t.Errorf("Round(%q, %d, 向零截断) 报错: %v", c.in, c.scale, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("Round(%q, %d, 向零截断) = %s, 期望 %s", c.in, c.scale, got.String(), c.want)
		}
	}

	//截断的绝对值恒不大于原值的绝对值（不会凭空多出钱）
	for _, s := range []string{"1.999", "-1.999", "0.001", "-0.001", "99.994"} {
		d := MustParse(s)
		got, err := d.Round(2, RoundTruncate)
		if err != nil {
			t.Fatalf("Round(%s) 报错: %v", s, err)
		}
		if got.Abs().Cmp(d.Abs()) > 0 {
			t.Errorf("向零截断后绝对值变大: %s -> %s", s, got.String())
		}
	}
}

// TestRoundInvalidArgs 验证位数与舍入方式的非法入参。
func TestRoundInvalidArgs(t *testing.T) {
	d := MustParse("1.5")

	//负位数：把 545 舍成 550 对金额没有消费方，出现即代表调用方算错了位数
	for _, scale := range []int32{-1, -2, -100} {
		if _, err := d.Round(scale, RoundHalfUp); !errors.Is(err, ErrScaleInvalid) {
			t.Errorf("Round 位数 %d 应报 ErrScaleInvalid, 实际 %v", scale, err)
		}
	}
	//位数上界
	if _, err := d.Round(MaxScale+1, RoundHalfUp); !errors.Is(err, ErrScaleInvalid) {
		t.Errorf("Round 位数 %d 应报 ErrScaleInvalid, 实际 %v", MaxScale+1, err)
	}
	//恰好等于上界应放行
	if _, err := d.Round(MaxScale, RoundHalfUp); err != nil {
		t.Errorf("Round 位数 %d 恰在界内, 不应报错: %v", MaxScale, err)
	}

	//零值舍入方式不默许任何一种方式：忘传参数必须报错
	if _, err := d.Round(2, RoundMode(0)); !errors.Is(err, ErrRoundMode) {
		t.Errorf("Round 零值舍入方式应报 ErrRoundMode, 实际 %v", err)
	}
	if _, err := d.Round(2, RoundMode(99)); !errors.Is(err, ErrRoundMode) {
		t.Errorf("Round 未知舍入方式应报 ErrRoundMode, 实际 %v", err)
	}
}

// TestRoundModeString 验证舍入方式的可读名称。
func TestRoundModeString(t *testing.T) {
	if got := RoundHalfUp.String(); got != "四舍五入" {
		t.Errorf("RoundHalfUp.String() = %q, 期望 \"四舍五入\"", got)
	}
	if got := RoundTruncate.String(); got != "向零截断" {
		t.Errorf("RoundTruncate.String() = %q, 期望 \"向零截断\"", got)
	}
	if got := RoundMode(0).String(); !strings.Contains(got, "未知") {
		t.Errorf("RoundMode(0).String() = %q, 期望含「未知」", got)
	}
}

// TestRoundRate 验证汇率规整到 8 位、四舍五入。
//
// 对应终版 I-5/I-6 与 T39①：自动获取与用户手填两条入口共用同一个规整结果。
func TestRoundRate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"7.12345678", "7.12345678"},       //恰好 8 位，原样保留
		{"7.123456784", "7.12345678"},      //第 9 位舍去
		{"7.123456785", "7.12345679"},      //第 9 位进位
		{"7.1234567849999", "7.12345678"},  //多余位数舍去
		{"1", "1"},                         //整数
		{"0.000000005", "0.00000001"},      //第 9 位进位到第 8 位
		{"0.000000004", "0"},               //小于半个最小单位，舍为 0
		{"-7.123456785", "-7.12345679"},    //负汇率同样对称（形态上支持）
		{"6.8888888888888", "6.88888889"},  //连续 8
		{"0.12345678912345", "0.12345679"}, //长小数
	}
	for _, c := range cases {
		got := MustParse(c.in).RoundRate()
		if got.String() != c.want {
			t.Errorf("RoundRate(%q) = %s, 期望 %s", c.in, got.String(), c.want)
		}
		//规整结果必须能被 8 位无损容纳，即可直接落汇率列
		if !got.FitsScale(RateScale) {
			t.Errorf("RoundRate(%q) = %s 的位数 %d 超过 %d",
				c.in, got.String(), got.Scale(), RateScale)
		}
	}

	//RoundRate 与显式 Round(RateScale, RoundHalfUp) 必须等价——
	//否则「汇率精度」就有了两种口径
	for _, s := range []string{"7.123456785", "0.000000005", "1.5", "0"} {
		byRate := MustParse(s).RoundRate()
		byRound, err := MustParse(s).Round(RateScale, RoundHalfUp)
		if err != nil {
			t.Fatalf("Round(%s, %d) 报错: %v", s, RateScale, err)
		}
		if !byRate.Equal(byRound) {
			t.Errorf("%s: RoundRate = %s 与 Round(%d, 四舍五入) = %s 不等价",
				s, byRate.String(), RateScale, byRound.String())
		}
	}

	//幂等：已规整的汇率再规整一次不变
	once := MustParse("7.123456785").RoundRate()
	if !once.RoundRate().Equal(once) {
		t.Error("RoundRate 应幂等")
	}
}

// TestScale 验证有效小数位数，即剥掉尾随零后实际需要的位数。
//
// 取「有效位数」而非底层指数：1.50 与 1.5 数值相同，若因构造方式不同得出不同位数，
// 从账单解析来的金额与计算得出的金额就会被判为需要不同的存储位数。
func TestScale(t *testing.T) {
	cases := []struct {
		in   string
		want int32
	}{
		{"1.5", 1},
		{"1.50", 1}, //尾随零不计入
		{"1.500", 1},
		{"0", 0},
		{"0.00", 0}, //零的任何写法都是 0 位
		{"-0.00", 0},
		{"100", 0},
		{"1e3", 0}, //正指数形态
		{"-1.20", 1},
		{"7.12345678", 8},
		{"0.001", 3},
		{"1.005", 3},
		{"123.456", 3},
		{"0.10", 1},
		{"10.01", 2},
	}
	for _, c := range cases {
		if got := MustParse(c.in).Scale(); got != c.want {
			t.Errorf("Scale(%q) = %d, 期望 %d", c.in, got, c.want)
		}
	}

	//同一数值的不同写法必须得到相同位数
	if MustParse("1.5").Scale() != MustParse("1.50").Scale() {
		t.Error("1.5 与 1.50 的有效位数应相同")
	}

	//公开的 Scale 返回 int32，而实际位数以 int64 计算（见 scale64）：位数超过 int32
	//上界时必须钳位，不得回绕成负数——负位数会让 FitsScale / Units 之类的下游判断
	//全部失真。这类值只在量级校验拒绝它之前的瞬间存在（Parse 不会放行），因此这里
	//绕过 Parse 直接构造。
	huge := wrap(sdecimal.New(1, math.MinInt32)) //1e-2147483648，即 2147483648 位小数
	if got := huge.scale64(); got != 2147483648 {
		t.Errorf("scale64 = %d, 期望 2147483648", got)
	}
	if got := huge.Scale(); got != math.MaxInt32 {
		t.Errorf("Scale 未钳位, = %d, 期望 %d", got, int32(math.MaxInt32))
	}
}

// TestFitsScale 验证无损容纳判定。
func TestFitsScale(t *testing.T) {
	cases := []struct {
		in    string
		scale int32
		want  bool
	}{
		{"1.50", 2, true},
		{"1.50", 1, true}, //有效位数只有 1 位
		{"1.55", 1, false},
		{"1.005", 2, false},
		{"100", 0, true},
		{"100.5", 0, false},
		{"0", 0, true},
		{"7.12345678", 8, true},
		{"7.12345678", 7, false},
		{"0.001", 2, false},
	}
	for _, c := range cases {
		if got := MustParse(c.in).FitsScale(c.scale); got != c.want {
			t.Errorf("FitsScale(%q, %d) = %v, 期望 %v", c.in, c.scale, got, c.want)
		}
	}

	//位数非法时返回 false 而不报错：本方法是谓词，调用方走的分支与「不能容纳」一致
	if MustParse("1.5").FitsScale(-1) {
		t.Error("FitsScale 位数为负应返回 false")
	}
	if MustParse("1.5").FitsScale(MaxScale + 1) {
		t.Error("FitsScale 位数越界应返回 false")
	}
}

// TestThreePrecisionScopes 验证三处精度口径在本包的落点（本轮任务的核心要求）。
//
//	原币金额   按币种小数位数（含 KWD 这类 3 位币种）
//	本位币金额 算按本位币小数位数（0/1/2 位），存按 2 位定点
//	折算汇率   8 位四舍五入
func TestThreePrecisionScopes(t *testing.T) {
	//口径一：原币金额按币种小数位数。位数由 currency 提供，本包只接收参数。
	originCases := []struct {
		currency string
		scale    int32
		raw      string
		want     string
	}{
		{"JPY 0 位", 0, "1234.56", "1235"},
		{"CNY 2 位", 2, "1234.567", "1234.57"},
		{"KWD 3 位", 3, "1234.5678", "1234.568"},
	}
	for _, c := range originCases {
		got, err := MustParse(c.raw).Round(c.scale, RoundHalfUp)
		if err != nil {
			t.Errorf("[%s] 报错: %v", c.currency, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("[%s] Round(%s, %d) = %s, 期望 %s", c.currency, c.raw, c.scale, got.String(), c.want)
		}
		if !got.FitsScale(c.scale) {
			t.Errorf("[%s] 舍入结果 %s 位数超过 %d", c.currency, got.String(), c.scale)
		}
	}

	//口径二：本位币金额「算按本位币小数位数、存按 2 位定点」。
	//本位币小数位数恒 ≤ 2，故 0/1/2 位的结果必然能被 2 位无损存储——
	//这是两个口径能共存的关键前提，在此逐位验证。
	for _, baseScale := range []int32{0, 1, 2} {
		for _, raw := range []string{"1234.5678", "0.005", "-9876.5432", "0", "99.999"} {
			calculated, err := MustParse(raw).Round(baseScale, RoundHalfUp)
			if err != nil {
				t.Fatalf("本位币 %d 位舍入 %s 报错: %v", baseScale, raw, err)
			}
			//按本位币位数算出的金额，必须能被 2 位定点存储无损容纳
			if !calculated.FitsScale(BaseAmountStoreScale) {
				t.Errorf("本位币 %d 位算出的 %s 无法被 %d 位定点无损存储",
					baseScale, calculated.String(), BaseAmountStoreScale)
			}
			//落库两种形态都应成功，且能无损还原
			text, err := calculated.StringFixed(BaseAmountStoreScale)
			if err != nil {
				t.Errorf("本位币 %d 位算出的 %s 落定点文本报错: %v", baseScale, calculated.String(), err)
				continue
			}
			units, err := calculated.Units(BaseAmountStoreScale)
			if err != nil {
				t.Errorf("本位币 %d 位算出的 %s 落最小单位报错: %v", baseScale, calculated.String(), err)
				continue
			}
			back, err := FromUnits(units, BaseAmountStoreScale)
			if err != nil {
				t.Errorf("还原 %d 报错: %v", units, err)
				continue
			}
			if !back.Equal(calculated) {
				t.Errorf("本位币 %d 位：%s 经最小单位 %d 往返后变成 %s（文本形态 %s）",
					baseScale, calculated.String(), units, back.String(), text)
			}
		}
	}

	//口径三：折算汇率 8 位四舍五入，且常量取值不得被改动
	if RateScale != 8 {
		t.Errorf("RateScale = %d, 应为 8（T39① 定死）", RateScale)
	}
	if BaseAmountStoreScale != 2 {
		t.Errorf("BaseAmountStoreScale = %d, 应为 2（T35 定死）", BaseAmountStoreScale)
	}
}

// TestConvertChain 模拟 8.1 折算链路：原币金额 × 规整后汇率 → 按本位币位数舍入。
//
// 验证「单次舍入」而非「多次舍入累积」：乘法保留全部有效位，只在最后舍入一次。
func TestConvertChain(t *testing.T) {
	//一笔 100.55 美元，汇率 7.123456789（源数据 9 位，规整到 8 位）
	amount := MustParse("100.55")
	rate := MustParse("7.123456789").RoundRate()
	if rate.String() != "7.12345679" {
		t.Fatalf("汇率规整 = %s, 期望 7.12345679", rate.String())
	}

	//乘积保留全部有效位，不在中途舍入
	product := amount.Mul(rate)
	if product.String() != "716.2635802345" {
		t.Errorf("100.55 × 7.12345679 = %s, 期望 716.2635802345", product.String())
	}

	//本位币 2 位：单次舍入
	got, err := product.Round(2, RoundHalfUp)
	if err != nil {
		t.Fatalf("舍入报错: %v", err)
	}
	if got.String() != "716.26" {
		t.Errorf("折算结果 = %s, 期望 716.26", got.String())
	}
	//结果可无损落库
	if _, err := got.Units(BaseAmountStoreScale); err != nil {
		t.Errorf("折算结果无法落库: %v", err)
	}

	//本位币 0 位（如 JPY 作本位币）
	gotJPY, err := product.Round(0, RoundHalfUp)
	if err != nil {
		t.Fatalf("舍入报错: %v", err)
	}
	if gotJPY.String() != "716" {
		t.Errorf("0 位本位币折算结果 = %s, 期望 716", gotJPY.String())
	}
}

// TestAmortizeChain 模拟 8.2 摊分链路：均分取商、尾差归末月。
//
// 尾差归属本身属 rule/amortize，此处只验证本包提供的算术底座足以支撑该规则：
// 各月份额之和加尾差必然等于原金额。
func TestAmortizeChain(t *testing.T) {
	cases := []struct {
		total  string
		months int64
		scale  int32
		desc   string
	}{
		{"100.00", 3, 2, "2 位币种除不尽"},
		{"-100.00", 3, 2, "负金额（退款）除不尽"},
		{"1000", 3, 0, "0 位币种除不尽"},
		{"100.00", 4, 2, "整除"},
		{"0.01", 3, 2, "金额小于份数"},
		{"1234.56", 12, 2, "12 个月"},
		{"100.00", 1, 2, "不摊分"},
		{"999.99", 7, 2, "质数份数"},
	}
	for _, c := range cases {
		total := MustParse(c.total)
		months := FromInt(c.months)
		share, tail, err := total.QuoRem(months, c.scale)
		if err != nil {
			t.Errorf("[%s] 报错: %v", c.desc, err)
			continue
		}

		//模拟 rule/amortize 的分配：前 N-1 月取 share，末月取 share + tail
		monthly := make([]Decimal, c.months)
		for i := range monthly {
			monthly[i] = share
		}
		monthly[c.months-1] = share.Add(tail)

		//各月份额之和必须精确等于原金额，不多不少
		if sum := Sum(monthly...); !sum.Equal(total) {
			t.Errorf("[%s] 各月份额之和 = %s, 期望精确等于 %s", c.desc, sum.String(), c.total)
		}
		//每月份额都必须能被该币种位数无损容纳（末月含尾差后同样成立）
		for i, m := range monthly {
			if !m.FitsScale(c.scale) {
				t.Errorf("[%s] 第 %d 月份额 %s 位数超过 %d", c.desc, i+1, m.String(), c.scale)
			}
		}
		//尾差绝对值必须小于一个最小单位乘份数，否则说明份额算错了
		if !tail.IsZero() && tail.Abs().Cmp(monthly[0].Abs().Add(MustParse("1"))) > 0 {
			t.Errorf("[%s] 尾差 %s 异常偏大", c.desc, tail.String())
		}
	}
}
