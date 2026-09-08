package amortize

import (
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 本文件覆盖终版 8.2 的均分与尾差：按币种小数位数均分、尾差归末月、
// 负金额对称、0/2/3 位币种各一组。

// TestSharesSplitByCurrencyScale 是 8.2 的主用例：按**币种小数位数**均分，
// 尾差固定计入末月。
//
// 分层 §十 明写本包属「全仓库单测密度最高处」，要求「8.2 尾差与负金额
// （按币种小数位，不是存储的 2 位）」。故 0 位（JPY）、2 位（CNY）、
// 3 位（KWD）三种位数各有覆盖。
func TestSharesSplitByCurrencyScale(t *testing.T) {
	cases := []struct {
		total  string
		months int
		code   string
		want   []string
		desc   string
	}{
		//2 位币种（CNY）
		{"100.00", 3, "CNY", []string{"33.33", "33.33", "33.34"}, "CNY 除不尽，尾差 0.01 归末月"},
		{"100.00", 4, "CNY", []string{"25", "25", "25", "25"}, "CNY 整除，无尾差"},
		{"100.00", 1, "CNY", []string{"100"}, "不摊分（H-1 默认），份额即全额"},
		{"999.99", 7, "CNY", []string{"142.85", "142.85", "142.85", "142.85", "142.85", "142.85", "142.89"}, "质数份数"},

		//0 位币种（JPY）——尾差不是 0.01 而是整数，这正是「按币种位数」的意义
		{"1400", 3, "JPY", []string{"466", "466", "468"}, "JPY 0 位，尾差 2 归末月"},
		{"1000", 3, "JPY", []string{"333", "333", "334"}, "JPY 0 位，尾差 1"},
		{"1400", 2, "JPY", []string{"700", "700"}, "JPY 整除"},

		//3 位币种（KWD）——可作支出币种，不可作本位币（T38）
		{"100.000", 3, "KWD", []string{"33.333", "33.333", "33.334"}, "KWD 3 位，尾差 0.001"},

		//负金额（退款、冲正），H-2 要求完全对称
		{"-100.00", 3, "CNY", []string{"-33.33", "-33.33", "-33.34"}, "负金额 CNY，与正金额镜像"},
		{"-1400", 3, "JPY", []string{"-466", "-466", "-468"}, "负金额 JPY"},
		{"-100.00", 4, "CNY", []string{"-25", "-25", "-25", "-25"}, "负金额整除"},

		//金额小于份数：前几月为 0，末月扛全部
		{"0.02", 5, "CNY", []string{"0", "0", "0", "0", "0.02"}, "金额小于份数，末月扛全部"},
		{"-0.02", 5, "CNY", []string{"0", "0", "0", "0", "-0.02"}, "负的小额"},

		//零金额：终版未禁止，0 均分到任何月数都是 0
		{"0", 3, "CNY", []string{"0", "0", "0"}, "零金额"},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			total := decimal.MustParse(c.total)
			s, err := NewShares(total, c.months, currency.MustParse(c.code))
			if err != nil {
				t.Fatalf("均分报错: %v", err)
			}

			got := s.All()
			if len(got) != len(c.want) {
				t.Fatalf("份额个数 = %d, 期望 %d", len(got), len(c.want))
			}
			for i := range got {
				if got[i].String() != c.want[i] {
					t.Errorf("第 %d 月份额 = %s, 期望 %s", i+1, got[i], c.want[i])
				}
			}

			//可加性：各月份额之和必须**精确等于**原额，不多不少
			if sum := decimal.Sum(got...); !sum.Equal(total) {
				t.Errorf("份额之和 = %s, 期望精确等于 %s", sum, total)
			}

			//每月份额都必须能被该币种位数无损容纳（含末月）——
			//这是 ErrAmountScale 存在的理由，此处反向确认它确实成立
			scale := currency.MustParse(c.code).Scale()
			for i, share := range got {
				if !share.FitsScale(scale) {
					t.Errorf("第 %d 月份额 %s 位数 %d 超过 %s 的 %d 位",
						i+1, share, share.Scale(), c.code, scale)
				}
			}

			//At(i) 必须与 All()[i] 一致：两条取值路径不得分叉
			for i := range got {
				at, err := s.At(i)
				if err != nil {
					t.Fatalf("At(%d) 报错: %v", i, err)
				}
				if !at.Equal(got[i]) {
					t.Errorf("At(%d) = %s 与 All()[%d] = %s 不一致", i, at, i, got[i])
				}
			}
		})
	}
}

// TestSharesTailGoesToLastMonth 单独验证「尾差归末月」而非轮转分给前几月。
//
// 这条是否决 Rhymond/go-money 的判据（其 Split/Allocate 把余数轮转分给
// 最前面的几方，对 100.00/3 得 33.34/33.33/33.33）。若将来有人改用该库
// 或照抄其算法，本用例会当场失败。
func TestSharesTailGoesToLastMonth(t *testing.T) {
	s, err := NewShares(decimal.MustParse("100.00"), 3, currency.MustParse("CNY"))
	if err != nil {
		t.Fatalf("均分报错: %v", err)
	}

	//前 N−1 月一律等于商，一分不多
	for i := 0; i < s.Months()-1; i++ {
		share, err := s.At(i)
		if err != nil {
			t.Fatalf("At(%d) 报错: %v", i, err)
		}
		if !share.Equal(s.Quotient()) {
			t.Errorf("第 %d 月份额 = %s, 期望等于商 %s（尾差不得轮转到前几月）",
				i+1, share, s.Quotient())
		}
	}

	//末月 = 商 + 尾差
	last, err := s.At(s.Months() - 1)
	if err != nil {
		t.Fatalf("末月 At 报错: %v", err)
	}
	if !last.Equal(s.Quotient().Add(s.Tail())) {
		t.Errorf("末月份额 = %s, 期望 商 %s + 尾差 %s", last, s.Quotient(), s.Tail())
	}
	if last.String() != "33.34" {
		t.Errorf("末月份额 = %s, 期望 33.34（尾差归末月，不是轮转给首月）", last)
	}
}

// TestSharesNegativeSymmetry 验证 H-2「负金额（退款）规则完全对称」：
// 对每一个正金额用例，其负值的份额恰为逐月取反。
//
// 这条性质由 decimal.QuoRem 的「商向零截断、余数与被除数同号」保证，
// 本包**刻意不写符号分支**——写了反而会在边界上引入不对称。
func TestSharesNegativeSymmetry(t *testing.T) {
	cases := []struct {
		total  string
		months int
		code   string
	}{
		{"100.00", 3, "CNY"},
		{"1400", 3, "JPY"},
		{"100.000", 7, "KWD"},
		{"0.01", 5, "CNY"},
		{"12345.67", 12, "CNY"},
		{"99", 4, "JPY"},
	}

	for _, c := range cases {
		code := currency.MustParse(c.code)
		pos, err := NewShares(decimal.MustParse(c.total), c.months, code)
		if err != nil {
			t.Fatalf("%s 正金额均分报错: %v", c.total, err)
		}
		neg, err := NewShares(decimal.MustParse(c.total).Neg(), c.months, code)
		if err != nil {
			t.Fatalf("%s 负金额均分报错: %v", c.total, err)
		}

		posAll, negAll := pos.All(), neg.All()
		for i := range posAll {
			if !negAll[i].Equal(posAll[i].Neg()) {
				t.Errorf("%s 摊 %d 月：第 %d 月正 %s 负 %s，不对称",
					c.total, c.months, i+1, posAll[i], negAll[i])
			}
		}
		//尾差也必须对称
		if !neg.Tail().Equal(pos.Tail().Neg()) {
			t.Errorf("%s：尾差正 %s 负 %s，不对称", c.total, pos.Tail(), neg.Tail())
		}
	}
}

// TestSharesIndependentByCurrency 验证「原币与本位币各自独立均分」：
// 同一笔金额按不同币种的位数切，结果必须不同。
//
// 这条性质是 entity 把 Currency 与 BaseCurrency 分成两个字段的原因
// （见 entity/consumer_check_test.go 的 TestConsumerAmortizeSplit）。
func TestSharesIndependentByCurrency(t *testing.T) {
	//同一个数值 1400，按 JPY（0 位）与按 CNY（2 位）切出的份额不同
	jpy, err := NewShares(decimal.MustParse("1400"), 3, currency.MustParse("JPY"))
	if err != nil {
		t.Fatalf("JPY 均分报错: %v", err)
	}
	cny, err := NewShares(decimal.MustParse("1400"), 3, currency.MustParse("CNY"))
	if err != nil {
		t.Fatalf("CNY 均分报错: %v", err)
	}

	if jpy.Quotient().Equal(cny.Quotient()) {
		t.Errorf("JPY 商 %s 与 CNY 商 %s 相同，说明未按各自币种位数均分",
			jpy.Quotient(), cny.Quotient())
	}
	if jpy.Quotient().String() != "466" {
		t.Errorf("JPY 商 = %s, 期望 466（0 位）", jpy.Quotient())
	}
	if cny.Quotient().String() != "466.66" {
		t.Errorf("CNY 商 = %s, 期望 466.66（2 位）", cny.Quotient())
	}
	//两者都必须各自可加
	if !decimal.Sum(jpy.All()...).Equal(decimal.MustParse("1400")) {
		t.Error("JPY 份额之和 ≠ 1400")
	}
	if !decimal.Sum(cny.All()...).Equal(decimal.MustParse("1400")) {
		t.Error("CNY 份额之和 ≠ 1400")
	}
}

// TestSharesErrors 验证四类非法输入各自返回可判定的哨兵错误。
func TestSharesErrors(t *testing.T) {
	cny := currency.MustParse("CNY")

	t.Run("月数非正报 ErrMonthsNotPositive", func(t *testing.T) {
		for _, months := range []int{0, -1} {
			_, err := NewShares(decimal.MustParse("100.00"), months, cny)
			if !errors.Is(err, ErrMonthsNotPositive) {
				t.Errorf("months=%d: err = %v, 期望 ErrMonthsNotPositive", months, err)
			}
			//不得穿透成 decimal 的除零错误——那个文案指向「除数为零」，
			//掩盖了真实原因「摊分月数非法」
			if errors.Is(err, decimal.ErrDivideByZero) {
				t.Errorf("months=%d: 不应返回 ErrDivideByZero", months)
			}
		}
	})

	t.Run("币种为零值报 ErrCurrencyEmpty", func(t *testing.T) {
		var zero currency.Code
		_, err := NewShares(decimal.MustParse("712.35"), 3, zero)
		if !errors.Is(err, ErrCurrencyEmpty) {
			t.Fatalf("err = %v, 期望 ErrCurrencyEmpty", err)
		}
		//反向确认放行的后果：零值币种 Scale() 返回 0，与 JPY 的 0 位无法区分。
		//若不拦，712.35 会被按 0 位切成 237/237/238.35
		q, _, qerr := decimal.MustParse("712.35").QuoRem(decimal.FromInt(3), zero.Scale())
		if qerr != nil {
			t.Fatalf("对照计算报错: %v", qerr)
		}
		if q.String() != "237" {
			t.Fatalf("对照前提失败：零值币种下的商应为 237，实际 %s", q)
		}
	})

	t.Run("金额位数超过币种位数报 ErrAmountScale", func(t *testing.T) {
		cases := []struct {
			total string
			code  string
			desc  string
		}{
			{"100.005", "CNY", "3 位金额喂给 2 位币种"},
			{"1400.5", "JPY", "带小数金额喂给 0 位币种"},
			{"10.9999", "KWD", "4 位金额喂给 3 位币种"},
		}
		for _, c := range cases {
			_, err := NewShares(decimal.MustParse(c.total), 3, currency.MustParse(c.code))
			if !errors.Is(err, ErrAmountScale) {
				t.Errorf("%s: err = %v, 期望 ErrAmountScale", c.desc, err)
				continue
			}
			//错误信息须同时给出金额与币种两侧的位数，否则不知道该改哪一边
			msg := err.Error()
			if !strings.Contains(msg, c.total) || !strings.Contains(msg, c.code) {
				t.Errorf("%s: 错误信息 %q 未同时包含金额与币种", c.desc, msg)
			}
		}
	})

	t.Run("金额位数少于币种位数不报错", func(t *testing.T) {
		//100 是 0 位，喂给 2 位的 CNY 完全合法——少位数可无损容纳
		s, err := NewShares(decimal.MustParse("100"), 3, cny)
		if err != nil {
			t.Fatalf("位数少于币种位数不应报错: %v", err)
		}
		if s.Quotient().String() != "33.33" {
			t.Errorf("商 = %s, 期望 33.33", s.Quotient())
		}
	})

	t.Run("下标越界报 ErrShareIndex", func(t *testing.T) {
		s, err := NewShares(decimal.MustParse("100.00"), 3, cny)
		if err != nil {
			t.Fatalf("均分报错: %v", err)
		}
		for _, i := range []int{-1, 3, 100} {
			if _, err := s.At(i); !errors.Is(err, ErrShareIndex) {
				t.Errorf("At(%d): err = %v, 期望 ErrShareIndex", i, err)
			}
		}
	})
}

// TestSharesZeroValue 验证零值 Shares 的行为：不可用但不 panic。
func TestSharesZeroValue(t *testing.T) {
	var s Shares
	if !s.IsZero() {
		t.Error("零值 Shares 的 IsZero 应为 true")
	}
	if s.Months() != 0 {
		t.Errorf("零值 Months = %d, 期望 0", s.Months())
	}
	if s.All() != nil {
		t.Error("零值 All() 应返回 nil")
	}
	//任何下标都必须越界，而不是 panic
	if _, err := s.At(0); !errors.Is(err, ErrShareIndex) {
		t.Errorf("零值 At(0): err = %v, 期望 ErrShareIndex", err)
	}
	//访问器不得 panic
	_ = s.Total()
	_ = s.Currency()
	_ = s.Quotient()
	_ = s.Tail()
}

// TestSharesAccessors 验证访问器返回的值与构造入参一致。
func TestSharesAccessors(t *testing.T) {
	total := decimal.MustParse("100.00")
	code := currency.MustParse("CNY")
	s, err := NewShares(total, 3, code)
	if err != nil {
		t.Fatalf("均分报错: %v", err)
	}
	if !s.Total().Equal(total) {
		t.Errorf("Total() = %s, 期望 %s", s.Total(), total)
	}
	if !s.Currency().Equal(code) {
		t.Errorf("Currency() = %s, 期望 %s", s.Currency(), code)
	}
	if s.Months() != 3 {
		t.Errorf("Months() = %d, 期望 3", s.Months())
	}
	if s.IsZero() {
		t.Error("构造出的 Shares 不应为零值")
	}
}

// TestSharesPropertyInvariants 用 5 万次随机用例验证三条不变量恒成立：
//
//	① 份额之和精确等于原额（不多不少一分）
//	② 每月份额都能被币种位数无损容纳（含末月）
//	③ 尾差与原额同号或为零
//
// 随机测试补的是穷举表格覆盖不到的组合：位数 × 月数 × 金额符号 × 整除性
// 四个维度的笛卡尔积远超手写用例的规模，而摊分是「算错也不报错」的场景，
// 只靠表格用例容易在某个组合上留下盲区。
func TestSharesPropertyInvariants(t *testing.T) {
	rng := rand.New(rand.NewSource(20260907))
	codes := []currency.Code{
		currency.MustParse("JPY"), // 0 位
		currency.MustParse("CNY"), // 2 位
		currency.MustParse("KWD"), // 3 位
	}

	const rounds = 50000
	for i := 0; i < rounds; i++ {
		code := codes[rng.Intn(len(codes))]
		scale := code.Scale()

		//构造一个恰好符合该币种位数的金额（模拟已校验的 Expense.支出金额），
		//覆盖正、负、零三种符号
		units := rng.Int63n(4000001) - 2000000
		total, err := decimal.FromUnits(units, scale)
		if err != nil {
			t.Fatalf("第 %d 轮构造金额失败: %v", i, err)
		}
		months := rng.Intn(240) + 1

		s, err := NewShares(total, months, code)
		if err != nil {
			t.Fatalf("第 %d 轮均分失败 (total=%s, months=%d, code=%s): %v",
				i, total, months, code, err)
		}

		all := s.All()
		//① 可加性
		if sum := decimal.Sum(all...); !sum.Equal(total) {
			t.Fatalf("第 %d 轮 (total=%s, months=%d, code=%s): 份额之和 %s ≠ 原额",
				i, total, months, code, sum)
		}
		//② 位数
		for j, share := range all {
			if !share.FitsScale(scale) {
				t.Fatalf("第 %d 轮 (total=%s, months=%d, code=%s): 第 %d 月份额 %s 突破 %d 位",
					i, total, months, code, j+1, share, scale)
			}
		}
		//③ 尾差符号
		if !s.Tail().IsZero() && total.Sign() != 0 && s.Tail().Sign() != total.Sign() {
			t.Fatalf("第 %d 轮 (total=%s): 尾差 %s 与原额符号不一致", i, total, s.Tail())
		}
		//④ At 与 All 一致（抽查末月与首月，两者是仅有的两个不同值）
		first, err := s.At(0)
		if err != nil {
			t.Fatalf("第 %d 轮 At(0) 报错: %v", i, err)
		}
		last, err := s.At(months - 1)
		if err != nil {
			t.Fatalf("第 %d 轮 At(末月) 报错: %v", i, err)
		}
		if !first.Equal(all[0]) || !last.Equal(all[months-1]) {
			t.Fatalf("第 %d 轮：At 与 All 结果不一致", i)
		}
	}
}
