package amortize

import (
	"errors"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 本文件覆盖 H-4「覆盖月份区间 + 每月分摊金额」与 J 域摊分口径份额派生。

// newTestInput 构造一份典型入参：CNY（2 位）支出、折算到 JPY（0 位）本位币。
//
// 刻意取两个**位数不同**的币种：这是「原币与本位币各自独立均分」唯一能被
// 验证到的形态，若两边都用 CNY，取错基准也看不出来。
func newTestInput() Input {
	return Input{
		SpendDate:    calendar.MustParseDate("2026-01-15"),
		Months:       3,
		Amount:       decimal.MustParse("100.00"),
		Currency:     currency.MustParse("CNY"),
		BaseAmount:   decimal.MustParse("1400"),
		BaseCurrency: currency.MustParse("JPY"),
	}
}

// TestScheduleH4Preview 验证 H-4 的派生输出：覆盖月份区间 + 每月分摊金额。
//
// 终版 H-4：「编辑摊分月数或支出日期时即时展示覆盖月份区间与每月分摊金额，
// 并提示本次修改将影响的月份范围」。分层 §六 L2 把这项列为本包承载之一。
func TestScheduleH4Preview(t *testing.T) {
	s, err := NewSchedule(newTestInput())
	if err != nil {
		t.Fatalf("派生摊分表报错: %v", err)
	}

	//「本次修改将影响的月份范围」
	if got := s.Range().String(); got != "2026-01~2026-03" {
		t.Errorf("覆盖区间 = %s, 期望 2026-01~2026-03", got)
	}
	if s.Months() != 3 {
		t.Errorf("摊分月数 = %d, 期望 3", s.Months())
	}

	//「每月分摊金额」——原币按 CNY 2 位、本位币按 JPY 0 位，各自独立
	rows := s.Rows()
	if len(rows) != 3 {
		t.Fatalf("行数 = %d, 期望 3", len(rows))
	}
	want := []struct{ month, amount, base string }{
		{"2026-01", "33.33", "466"},
		{"2026-02", "33.33", "466"},
		{"2026-03", "33.34", "468"}, // 两边的尾差都归末月
	}
	for i, w := range want {
		if got := rows[i].Month.String(); got != w.month {
			t.Errorf("第 %d 行月份 = %s, 期望 %s", i+1, got, w.month)
		}
		if got := rows[i].Amount.String(); got != w.amount {
			t.Errorf("第 %d 行原币份额 = %s, 期望 %s", i+1, got, w.amount)
		}
		if got := rows[i].BaseAmount.String(); got != w.base {
			t.Errorf("第 %d 行本位币份额 = %s, 期望 %s", i+1, got, w.base)
		}
	}
}

// TestScheduleTwoCurrenciesSplitIndependently 验证终版 8.2「原币金额与本位币
// 金额各自独立均分」。
//
// 两条独立性：位数各取各的、尾差各归各的。用 CNY(2) × JPY(0) 这组，
// 若实现里错用了同一个位数，两边必有一边的份额位数不对。
func TestScheduleTwoCurrenciesSplitIndependently(t *testing.T) {
	s, err := NewSchedule(newTestInput())
	if err != nil {
		t.Fatalf("派生摊分表报错: %v", err)
	}

	amountShares, baseShares := s.AmountShares(), s.BaseShares()

	//基准币种各取各的
	if amountShares.Currency().String() != "CNY" {
		t.Errorf("原币份额的基准币种 = %s, 期望 CNY", amountShares.Currency())
	}
	if baseShares.Currency().String() != "JPY" {
		t.Errorf("本位币份额的基准币种 = %s, 期望 JPY", baseShares.Currency())
	}

	//尾差各归各的，且数值不同（0.01 与 2）——同一个数就说明串了基准
	if amountShares.Tail().String() != "0.01" {
		t.Errorf("原币尾差 = %s, 期望 0.01", amountShares.Tail())
	}
	if baseShares.Tail().String() != "2" {
		t.Errorf("本位币尾差 = %s, 期望 2", baseShares.Tail())
	}

	//两边各自可加
	if sum := decimal.Sum(amountShares.All()...); !sum.Equal(decimal.MustParse("100.00")) {
		t.Errorf("原币份额之和 = %s, 期望 100.00", sum)
	}
	if sum := decimal.Sum(baseShares.All()...); !sum.Equal(decimal.MustParse("1400")) {
		t.Errorf("本位币份额之和 = %s, 期望 1400", sum)
	}

	//两边份额都必须符合各自币种的位数
	for i, r := range s.Rows() {
		if !r.Amount.FitsScale(2) {
			t.Errorf("第 %d 行原币份额 %s 超过 CNY 的 2 位", i+1, r.Amount)
		}
		if !r.BaseAmount.FitsScale(0) {
			t.Errorf("第 %d 行本位币份额 %s 超过 JPY 的 0 位", i+1, r.BaseAmount)
		}
	}
}

// TestScheduleAtPierce 验证 J 域摊分口径的按月取数：区间内命中、区间外落空。
//
// store 用「起始月 ≤ M 且 结束月 ≥ M」下推取回行（分层 §六 L3 store ④），
// 本方法负责回答「这一行在该月摊多少」。两者是穿刺与派生的分工。
func TestScheduleAtPierce(t *testing.T) {
	s, err := NewSchedule(newTestInput()) // 2026-01 ~ 2026-03
	if err != nil {
		t.Fatalf("派生摊分表报错: %v", err)
	}

	cases := []struct {
		month  string
		hit    bool
		amount string
		base   string
		desc   string
	}{
		{"2025-12", false, "", "", "区间前一月，落空"},
		{"2026-01", true, "33.33", "466", "起始月（闭合）"},
		{"2026-02", true, "33.33", "466", "区间中"},
		{"2026-03", true, "33.34", "468", "结束月（闭合），尾差在此"},
		{"2026-04", false, "", "", "区间后一月，落空"},
		{"2027-01", false, "", "", "远期，落空"},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			row, ok := s.At(calendar.MustParseMonth(c.month))
			if ok != c.hit {
				t.Fatalf("%s 命中 = %v, 期望 %v", c.month, ok, c.hit)
			}
			if !c.hit {
				//落空时必须返回零值 Row，不能返回半截数据
				if !row.IsZero() {
					t.Errorf("%s 落空但返回了非零 Row: %+v", c.month, row)
				}
				return
			}
			if row.Month.String() != c.month {
				t.Errorf("行月份 = %s, 期望 %s", row.Month, c.month)
			}
			if row.Amount.String() != c.amount {
				t.Errorf("%s 原币份额 = %s, 期望 %s", c.month, row.Amount, c.amount)
			}
			if row.BaseAmount.String() != c.base {
				t.Errorf("%s 本位币份额 = %s, 期望 %s", c.month, row.BaseAmount, c.base)
			}
		})
	}
}

// TestScheduleAtMatchesRows 验证 At 与 Rows 两条取值路径结果一致。
//
// 两者是同一份数据的两种取法（O(1) 单月 与 O(N) 全量），J 域用前者、
// H-4 用后者。若分叉，同一笔支出在统计页与预览页会显示不同的金额。
func TestScheduleAtMatchesRows(t *testing.T) {
	in := newTestInput()
	in.Months = 26 // 跨两年多，覆盖跨年边界
	in.SpendDate = calendar.MustParseDate("2026-11-30")
	in.Amount = decimal.MustParse("12345.67")
	in.BaseAmount = decimal.MustParse("987654")

	s, err := NewSchedule(in)
	if err != nil {
		t.Fatalf("派生摊分表报错: %v", err)
	}

	rows := s.Rows()
	if len(rows) != in.Months {
		t.Fatalf("行数 = %d, 期望 %d", len(rows), in.Months)
	}
	for _, row := range rows {
		got, ok := s.At(row.Month)
		if !ok {
			t.Fatalf("Rows 中的月份 %s 在 At 中未命中", row.Month)
		}
		if !got.Equal(row) {
			t.Errorf("%s：At 得 %+v，Rows 得 %+v，两条路径分叉", row.Month, got, row)
		}
	}

	//全量之和仍须精确等于原额
	amounts := make([]decimal.Decimal, len(rows))
	bases := make([]decimal.Decimal, len(rows))
	for i, r := range rows {
		amounts[i], bases[i] = r.Amount, r.BaseAmount
	}
	if sum := decimal.Sum(amounts...); !sum.Equal(in.Amount) {
		t.Errorf("原币份额之和 = %s, 期望 %s", sum, in.Amount)
	}
	if sum := decimal.Sum(bases...); !sum.Equal(in.BaseAmount) {
		t.Errorf("本位币份额之和 = %s, 期望 %s", sum, in.BaseAmount)
	}
}

// TestScheduleCrossYear 验证跨年与跨多年区间的月份序列正确。
func TestScheduleCrossYear(t *testing.T) {
	cases := []struct {
		date       string
		months     int
		start, end string
		desc       string
	}{
		{"2026-11-30", 12, "2026-11", "2027-10", "跨年 12 个月"},
		{"2026-12-31", 2, "2026-12", "2027-01", "恰好跨年"},
		{"2026-01-01", 25, "2026-01", "2028-01", "跨两年"},
		{"2026-01-15", 1, "2026-01", "2026-01", "不摊分"},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			in := newTestInput()
			in.SpendDate = calendar.MustParseDate(c.date)
			in.Months = c.months

			s, err := NewSchedule(in)
			if err != nil {
				t.Fatalf("派生报错: %v", err)
			}
			if s.Range().Start().String() != c.start || s.Range().End().String() != c.end {
				t.Errorf("区间 = %s, 期望 %s~%s", s.Range(), c.start, c.end)
			}

			rows := s.Rows()
			if len(rows) != c.months {
				t.Fatalf("行数 = %d, 期望 %d", len(rows), c.months)
			}
			//月份必须严格递增且连续，中间不得跳月
			for i := 1; i < len(rows); i++ {
				if rows[i].Month.MonthsSince(rows[i-1].Month) != 1 {
					t.Errorf("第 %d 行 %s 与第 %d 行 %s 不连续",
						i, rows[i-1].Month, i+1, rows[i].Month)
				}
			}
			if rows[0].Month.String() != c.start {
				t.Errorf("首行月份 = %s, 期望 %s", rows[0].Month, c.start)
			}
			if rows[len(rows)-1].Month.String() != c.end {
				t.Errorf("末行月份 = %s, 期望 %s", rows[len(rows)-1].Month, c.end)
			}
		})
	}
}

// TestScheduleNegativeAmount 验证退款（负金额）的摊分：H-2 要求完全对称，
// 且**从退款发生月起摊、不改写历史月份**。
func TestScheduleNegativeAmount(t *testing.T) {
	in := newTestInput()
	in.SpendDate = calendar.MustParseDate("2026-06-10")
	in.Amount = decimal.MustParse("-100.00")
	in.BaseAmount = decimal.MustParse("-1400")

	s, err := NewSchedule(in)
	if err != nil {
		t.Fatalf("派生报错: %v", err)
	}

	//起始月 = 退款发生月，不回溯到更早的月份
	if s.Range().Start().String() != "2026-06" {
		t.Errorf("起始月 = %s, 期望 2026-06（从退款发生月起摊）", s.Range().Start())
	}
	if s.Range().Start().Before(in.SpendDate.ToMonth()) {
		t.Error("起始月早于退款发生月，违反 H-2「不改写历史月份」")
	}

	//份额与正金额镜像
	rows := s.Rows()
	want := []struct{ amount, base string }{
		{"-33.33", "-466"},
		{"-33.33", "-466"},
		{"-33.34", "-468"},
	}
	for i, w := range want {
		if rows[i].Amount.String() != w.amount {
			t.Errorf("第 %d 月原币份额 = %s, 期望 %s", i+1, rows[i].Amount, w.amount)
		}
		if rows[i].BaseAmount.String() != w.base {
			t.Errorf("第 %d 月本位币份额 = %s, 期望 %s", i+1, rows[i].BaseAmount, w.base)
		}
	}
}

// TestScheduleErrors 验证非法入参各自返回可判定的哨兵错误，
// 且原币与本位币两侧的失败可被区分。
func TestScheduleErrors(t *testing.T) {
	t.Run("支出日期为零值", func(t *testing.T) {
		in := newTestInput()
		in.SpendDate = calendar.Date{}
		if _, err := NewSchedule(in); !errors.Is(err, ErrSpendDateEmpty) {
			t.Errorf("err = %v, 期望 ErrSpendDateEmpty", err)
		}
	})

	t.Run("月数非正", func(t *testing.T) {
		in := newTestInput()
		in.Months = 0
		if _, err := NewSchedule(in); !errors.Is(err, ErrMonthsNotPositive) {
			t.Errorf("err = %v, 期望 ErrMonthsNotPositive", err)
		}
	})

	t.Run("支出币种为零值，错误须标明是原币侧", func(t *testing.T) {
		in := newTestInput()
		in.Currency = currency.Code{}
		_, err := NewSchedule(in)
		if !errors.Is(err, ErrCurrencyEmpty) {
			t.Fatalf("err = %v, 期望 ErrCurrencyEmpty", err)
		}
		if !strings.Contains(err.Error(), "原币") {
			t.Errorf("错误信息 %q 未标明是原币侧失败", err.Error())
		}
	})

	t.Run("本位币币种为零值，错误须标明是本位币侧", func(t *testing.T) {
		in := newTestInput()
		in.BaseCurrency = currency.Code{}
		_, err := NewSchedule(in)
		if !errors.Is(err, ErrCurrencyEmpty) {
			t.Fatalf("err = %v, 期望 ErrCurrencyEmpty", err)
		}
		if !strings.Contains(err.Error(), "本位币") {
			t.Errorf("错误信息 %q 未标明是本位币侧失败", err.Error())
		}
	})

	t.Run("原币金额位数超过支出币种位数", func(t *testing.T) {
		in := newTestInput()
		in.Amount = decimal.MustParse("100.005") // 3 位喂给 CNY 的 2 位
		_, err := NewSchedule(in)
		if !errors.Is(err, ErrAmountScale) {
			t.Errorf("err = %v, 期望 ErrAmountScale", err)
		}
		if !strings.Contains(err.Error(), "原币") {
			t.Errorf("错误信息 %q 未标明是原币侧", err.Error())
		}
	})

	t.Run("本位币金额位数超过本位币位数", func(t *testing.T) {
		in := newTestInput()
		in.BaseAmount = decimal.MustParse("1400.5") // 带小数喂给 JPY 的 0 位
		_, err := NewSchedule(in)
		if !errors.Is(err, ErrAmountScale) {
			t.Errorf("err = %v, 期望 ErrAmountScale", err)
		}
		if !strings.Contains(err.Error(), "本位币") {
			t.Errorf("错误信息 %q 未标明是本位币侧", err.Error())
		}
	})

	t.Run("结束月越界", func(t *testing.T) {
		in := newTestInput()
		in.SpendDate = calendar.MustParseDate("9999-12-01")
		in.Months = 2
		if _, err := NewSchedule(in); !errors.Is(err, calendar.ErrYearRange) {
			t.Errorf("err = %v, 期望 calendar.ErrYearRange", err)
		}
	})
}

// TestScheduleZeroValue 验证零值 Schedule 不可用但不 panic。
func TestScheduleZeroValue(t *testing.T) {
	var s Schedule
	if !s.IsZero() {
		t.Error("零值 Schedule 的 IsZero 应为 true")
	}
	if s.Months() != 0 {
		t.Errorf("零值 Months = %d, 期望 0", s.Months())
	}
	if s.Rows() != nil {
		t.Error("零值 Rows() 应返回 nil")
	}
	if _, ok := s.At(calendar.MustParseMonth("2026-01")); ok {
		t.Error("零值 Schedule 不应命中任何月份")
	}
	//访问器不得 panic
	_ = s.Range()
	_ = s.AmountShares()
	_ = s.BaseShares()
}

// TestScheduleZeroMonthNeverHits 验证零值 Month 不参与穿刺。
//
// 取向同 calendar.MonthRange.Contains：「未指定」不是一个可命中的月份。
// 若命中，一条 AmortizeStart 为 NULL 的坏数据会被算进某个月的统计。
func TestScheduleZeroMonthNeverHits(t *testing.T) {
	s, err := NewSchedule(newTestInput())
	if err != nil {
		t.Fatalf("派生报错: %v", err)
	}
	var zero calendar.Month
	if _, ok := s.At(zero); ok {
		t.Error("零值 Month 不应命中")
	}
}

// TestScheduleIsPure 验证 NewSchedule 是纯函数（准则 2）。
func TestScheduleIsPure(t *testing.T) {
	in := newTestInput()
	first, err := NewSchedule(in)
	if err != nil {
		t.Fatalf("派生报错: %v", err)
	}
	firstRows := first.Rows()

	for i := 0; i < 50; i++ {
		got, err := NewSchedule(in)
		if err != nil {
			t.Fatalf("第 %d 次派生报错: %v", i, err)
		}
		rows := got.Rows()
		if len(rows) != len(firstRows) {
			t.Fatalf("第 %d 次行数不一致", i)
		}
		for j := range rows {
			if !rows[j].Equal(firstRows[j]) {
				t.Fatalf("第 %d 次第 %d 行 %+v ≠ 首次 %+v", i, j, rows[j], firstRows[j])
			}
		}
	}
}

// TestScheduleRowsDoesNotAliasShares 验证 Rows() 返回的切片是独立分配，
// 调用方改动不会污染 Schedule 内部状态。
//
// Shares 内部只存商与尾差、不存数组，本性质天然成立；本用例把它钉住，
// 防止将来有人为了「优化」而缓存一份数组并直接返回它。
func TestScheduleRowsDoesNotAliasShares(t *testing.T) {
	s, err := NewSchedule(newTestInput())
	if err != nil {
		t.Fatalf("派生报错: %v", err)
	}
	rows := s.Rows()
	rows[0].Amount = decimal.MustParse("99999.99")
	rows[0].Month = calendar.MustParseMonth("1999-01")

	again := s.Rows()
	if again[0].Amount.String() != "33.33" {
		t.Errorf("改动返回值后再次取值得 %s, 期望 33.33（Rows 应返回独立分配）", again[0].Amount)
	}
	if again[0].Month.String() != "2026-01" {
		t.Errorf("改动返回值后月份被污染: %s", again[0].Month)
	}
}

// TestScheduleDefensiveBranches 验证三条「按构造不可达」的防御分支确实按注释
// 所写的方式失败——安全地落空，而不是 panic 或返回错位的数据。
//
// 这些分支在正常构造下走不到（区间与份额同源于 Input.Months，长度恒相等），
// 覆盖率工具会把它们标为未覆盖。但「走不到」是当前实现的性质、不是语言保证：
// 若将来有人给 Schedule 加了单独设置区间或份额的入口，脱钩就会真实发生。
// 本用例用同包可见性直接构造脱钩状态，把注释里的承诺变成可执行断言。
func TestScheduleDefensiveBranches(t *testing.T) {
	cny := currency.MustParse("CNY")

	//构造一个区间为 3 个月、但份额只有 1 个月的 Schedule——两者脱钩
	shortShares, err := NewShares(decimal.MustParse("100.00"), 1, cny)
	if err != nil {
		t.Fatalf("构造份额失败: %v", err)
	}
	r, err := calendar.NewMonthRange(calendar.MustParseMonth("2026-01"), 3)
	if err != nil {
		t.Fatalf("构造区间失败: %v", err)
	}
	desynced := Schedule{r: r, amount: shortShares, base: shortShares}

	t.Run("At 在份额短于区间时安全落空", func(t *testing.T) {
		//首月仍在份额范围内，可正常取到
		if _, ok := desynced.At(calendar.MustParseMonth("2026-01")); !ok {
			t.Error("2026-01 在份额范围内，应命中")
		}
		//第 2、3 月落在区间内但超出份额下标——必须落空而非 panic
		for _, m := range []string{"2026-02", "2026-03"} {
			row, ok := desynced.At(calendar.MustParseMonth(m))
			if ok {
				t.Errorf("%s 份额已脱钩，应落空而非返回 %+v", m, row)
			}
			if !row.IsZero() {
				t.Errorf("%s 落空时应返回零值 Row", m)
			}
		}
	})

	t.Run("Rows 在份额短于区间时返回 nil 而非错位表", func(t *testing.T) {
		//宁可返回 nil 让调用方当场发现，也不返回一张月份与金额错位的表
		if rows := desynced.Rows(); rows != nil {
			t.Errorf("份额与区间脱钩时 Rows 应返回 nil, 实际 %d 行", len(rows))
		}
	})

	t.Run("仅本位币份额脱钩时同样落空", func(t *testing.T) {
		//只让 base 侧短一截，验证两侧的 At 错误都被接住
		fullShares, err := NewShares(decimal.MustParse("100.00"), 3, cny)
		if err != nil {
			t.Fatalf("构造份额失败: %v", err)
		}
		halfBase := Schedule{r: r, amount: fullShares, base: shortShares}
		if _, ok := halfBase.At(calendar.MustParseMonth("2026-03")); ok {
			t.Error("本位币份额脱钩时应落空")
		}
		if rows := halfBase.Rows(); rows != nil {
			t.Errorf("本位币份额脱钩时 Rows 应返回 nil, 实际 %d 行", len(rows))
		}
	})
}

// TestScheduleLargeMonths 验证 H-1「摊分月数无上限」在完整链路上成立，
// 且 At 对大区间仍能正确定位。
func TestScheduleLargeMonths(t *testing.T) {
	in := newTestInput()
	in.Months = 600 // 50 年
	in.Amount = decimal.MustParse("100000.00")
	in.BaseAmount = decimal.MustParse("1400000")

	s, err := NewSchedule(in)
	if err != nil {
		t.Fatalf("600 个月应可派生（H-1 无上限）: %v", err)
	}
	if s.Range().End().String() != "2075-12" {
		t.Errorf("结束月 = %s, 期望 2075-12", s.Range().End())
	}
	if s.Months() != 600 {
		t.Errorf("月数 = %d, 期望 600", s.Months())
	}

	//抽查首月、中间月、末月三点
	for _, c := range []struct {
		month string
		last  bool
	}{
		{"2026-01", false},
		{"2050-06", false},
		{"2075-12", true},
	} {
		row, ok := s.At(calendar.MustParseMonth(c.month))
		if !ok {
			t.Fatalf("%s 应命中", c.month)
		}
		//末月扛尾差，其余月取商
		wantAmount := s.AmountShares().Quotient()
		if c.last {
			wantAmount = wantAmount.Add(s.AmountShares().Tail())
		}
		if !row.Amount.Equal(wantAmount) {
			t.Errorf("%s 原币份额 = %s, 期望 %s", c.month, row.Amount, wantAmount)
		}
	}

	//全量之和仍须精确等于原额
	rows := s.Rows()
	amounts := make([]decimal.Decimal, len(rows))
	for i, r := range rows {
		amounts[i] = r.Amount
	}
	if sum := decimal.Sum(amounts...); !sum.Equal(in.Amount) {
		t.Errorf("600 个月份额之和 = %s, 期望 %s", sum, in.Amount)
	}
}
