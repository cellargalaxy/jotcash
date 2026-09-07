package calendar

import (
	"errors"
	"testing"
)

// TestNewMonthRange 覆盖不变式 1b：结束月 = 起始月 + 月数 − 1（方案 V2）。
func TestNewMonthRange(t *testing.T) {
	cases := []struct {
		start string
		count int
		end   string
		desc  string
	}{
		{"2026-01", 1, "2026-01", "月数 1：不摊分，区间退化为起始月本身"},
		{"2026-01", 2, "2026-02", "月数 2"},
		{"2026-01", 3, "2026-03", "月数 3"},
		{"2026-01", 12, "2026-12", "月数 12：同年末月，不得溢出到下一年"},
		{"2026-01", 13, "2027-01", "月数 13：跨年"},
		{"2026-12", 2, "2027-01", "起始月为年末，跨年"},
		{"2026-01", 120, "2035-12", "月数 120：无上限（H-1）"},
		{"2024-01", 14, "2025-02", "跨闰年"},
		{"2023-11", 4, "2024-02", "跨年且落在闰年 2 月"},
	}
	for _, c := range cases {
		r, err := NewMonthRange(MustParseMonth(c.start), c.count)
		if err != nil {
			t.Fatalf("NewMonthRange(%s, %d) [%s] 报错: %v", c.start, c.count, c.desc, err)
		}
		if r.Start().String() != c.start {
			t.Errorf("NewMonthRange(%s, %d) 起始月 = %s, 期望 %s", c.start, c.count, r.Start(), c.start)
		}
		if r.End().String() != c.end {
			t.Errorf("NewMonthRange(%s, %d) [%s] 结束月 = %s, 期望 %s",
				c.start, c.count, c.desc, r.End(), c.end)
		}
		// Len 与 count 互为逆运算：摊分份数必须等于摊分月数
		if r.Len() != c.count {
			t.Errorf("NewMonthRange(%s, %d) Len = %d, 期望 %d", c.start, c.count, r.Len(), c.count)
		}
	}
}

// TestNewMonthRangeInvalid 校验非法入参一律报错且不产出可用区间。
func TestNewMonthRangeInvalid(t *testing.T) {
	start := MustParseMonth("2026-01")

	badCounts := []struct {
		count int
		want  error
		desc  string
	}{
		{0, ErrMonthCount, "月数 0"},
		{-1, ErrMonthCount, "月数负数"},
	}
	for _, c := range badCounts {
		r, err := NewMonthRange(start, c.count)
		if !errors.Is(err, c.want) {
			t.Errorf("NewMonthRange(count=%d) [%s] 错误 = %v, 期望 %v", c.count, c.desc, err, c.want)
		}
		if !r.IsZero() {
			t.Errorf("NewMonthRange(count=%d) 出错却返回非零区间", c.count)
		}
	}

	// 起始月为零值：无基准
	var zero Month
	if _, err := NewMonthRange(zero, 3); !errors.Is(err, ErrMonthValue) {
		t.Errorf("零值起始月错误 = %v, 期望 ErrMonthValue", err)
	}

	// 结束月越界
	if _, err := NewMonthRange(MustParseMonth("9999-12"), 2); !errors.Is(err, ErrYearRange) {
		t.Errorf("结束月越界错误 = %v, 期望 ErrYearRange", err)
	}
}

// TestNewMonthRangeBetween 覆盖由已落库的起止月两字段还原区间。
func TestNewMonthRangeBetween(t *testing.T) {
	r, err := NewMonthRangeBetween(MustParseMonth("2026-01"), MustParseMonth("2026-03"))
	if err != nil {
		t.Fatalf("NewMonthRangeBetween 报错: %v", err)
	}
	if r.Len() != 3 {
		t.Errorf("Len = %d, 期望 3", r.Len())
	}

	// 单月区间（起止相同）合法
	if r, err := NewMonthRangeBetween(MustParseMonth("2026-01"), MustParseMonth("2026-01")); err != nil {
		t.Errorf("起止相同应合法: %v", err)
	} else if r.Len() != 1 {
		t.Errorf("起止相同的 Len = %d, 期望 1", r.Len())
	}

	// 结束月早于起始月
	got, err := NewMonthRangeBetween(MustParseMonth("2026-03"), MustParseMonth("2026-01"))
	if !errors.Is(err, ErrMonthOrder) {
		t.Errorf("逆序区间错误 = %v, 期望 ErrMonthOrder", err)
	}
	if !got.IsZero() {
		t.Error("逆序区间出错却返回非零区间")
	}

	// 任一端零值
	var zero Month
	if _, err := NewMonthRangeBetween(zero, MustParseMonth("2026-01")); !errors.Is(err, ErrMonthValue) {
		t.Error("起始月零值应返回 ErrMonthValue")
	}
	if _, err := NewMonthRangeBetween(MustParseMonth("2026-01"), zero); !errors.Is(err, ErrMonthValue) {
		t.Error("结束月零值应返回 ErrMonthValue")
	}
}

// TestMonthRangeRoundTrip 校验两个构造入口彼此一致：
// NewMonthRange(start, n) 与 NewMonthRangeBetween(start, start+n-1) 必须等价。
// 这保证「录入时派生的区间」与「从库中两列还原的区间」是同一个东西。
func TestMonthRangeRoundTrip(t *testing.T) {
	start := MustParseMonth("2026-05")
	for _, n := range []int{1, 2, 3, 6, 12, 13, 25, 100} {
		byCount, err := NewMonthRange(start, n)
		if err != nil {
			t.Fatalf("NewMonthRange(n=%d) 报错: %v", n, err)
		}
		byBetween, err := NewMonthRangeBetween(byCount.Start(), byCount.End())
		if err != nil {
			t.Fatalf("NewMonthRangeBetween(n=%d) 报错: %v", n, err)
		}
		if byCount != byBetween {
			t.Errorf("n=%d 两个入口不等价: %s vs %s", n, byCount, byBetween)
		}
	}
}

// TestMonthRangeContains 覆盖 8.2 的区间穿刺判据「起始月 ≤ M 且 结束月 ≥ M」（方案 V3）。
func TestMonthRangeContains(t *testing.T) {
	// 摊分 3 个月：2026-01 ~ 2026-03
	r, err := NewMonthRange(MustParseMonth("2026-01"), 3)
	if err != nil {
		t.Fatalf("构造区间报错: %v", err)
	}

	cases := []struct {
		month string
		want  bool
		desc  string
	}{
		{"2025-12", false, "早于起始月"},
		{"2026-01", true, "起始月本身（左端闭合）"},
		{"2026-02", true, "区间内"},
		{"2026-03", true, "结束月本身（右端闭合）"},
		{"2026-04", false, "晚于结束月"},
		{"2027-01", false, "晚一年"},
		{"2025-01", false, "早一年"},
	}
	for _, c := range cases {
		if got := r.Contains(MustParseMonth(c.month)); got != c.want {
			t.Errorf("%s Contains %s [%s] = %v, 期望 %v", r, c.month, c.desc, got, c.want)
		}
	}

	// 单月区间（不摊分）：只命中自己
	single, _ := NewMonthRange(MustParseMonth("2026-01"), 1)
	if !single.Contains(MustParseMonth("2026-01")) {
		t.Error("单月区间应命中自身")
	}
	if single.Contains(MustParseMonth("2026-02")) {
		t.Error("单月区间不应命中次月")
	}

	// 「未指定」不参与穿刺
	var zeroMonth Month
	if r.Contains(zeroMonth) {
		t.Error("零值 Month 不应被任何区间命中")
	}
	var zeroRange MonthRange
	if zeroRange.Contains(MustParseMonth("2026-01")) {
		t.Error("零值区间不应命中任何月份")
	}
}

// TestMonthRangeContainsMatchesMonths 交叉验证：Contains 的判定与 Months 的枚举必须一致。
// 两者一个供 store 做区间穿刺、一个供 H-4 展示覆盖月份，口径不一致会导致
// 「统计里算进去了但预览里没显示」这类静默不一致。
func TestMonthRangeContainsMatchesMonths(t *testing.T) {
	r, err := NewMonthRange(MustParseMonth("2026-11"), 5) // 跨年：2026-11 ~ 2027-03
	if err != nil {
		t.Fatalf("构造区间报错: %v", err)
	}

	months := r.Months()
	// 枚举出的每个月都必须被 Contains 命中
	for _, m := range months {
		if !r.Contains(m) {
			t.Errorf("Months 枚举出 %s 但 Contains 未命中", m)
		}
	}

	// 在区间前后各扫 6 个月，Contains 的结果必须与「是否在枚举集合内」一致
	inSet := make(map[Month]bool, len(months))
	for _, m := range months {
		inSet[m] = true
	}
	probe := MustParseMonth("2026-05")
	for i := 0; i < 18; i++ {
		m, err := probe.AddMonths(i)
		if err != nil {
			t.Fatalf("AddMonths(%d) 报错: %v", i, err)
		}
		if r.Contains(m) != inSet[m] {
			t.Errorf("%s：Contains = %v 与枚举集合 %v 不一致", m, r.Contains(m), inSet[m])
		}
	}
}

// TestMonthRangeMonths 覆盖 H-4「覆盖月份区间」的派生输出（方案 §三 #4）。
func TestMonthRangeMonths(t *testing.T) {
	cases := []struct {
		start string
		count int
		want  []string
		desc  string
	}{
		{"2026-01", 1, []string{"2026-01"}, "不摊分"},
		{"2026-01", 3, []string{"2026-01", "2026-02", "2026-03"}, "摊分 3 个月"},
		{"2026-11", 4, []string{"2026-11", "2026-12", "2027-01", "2027-02"}, "跨年摊分"},
		{"2023-12", 3, []string{"2023-12", "2024-01", "2024-02"}, "跨年进入闰年"},
	}
	for _, c := range cases {
		r, err := NewMonthRange(MustParseMonth(c.start), c.count)
		if err != nil {
			t.Fatalf("构造区间 [%s] 报错: %v", c.desc, err)
		}
		months := r.Months()
		if len(months) != len(c.want) {
			t.Fatalf("%s [%s] Months 长度 = %d, 期望 %d", r, c.desc, len(months), len(c.want))
		}
		for i, want := range c.want {
			if months[i].String() != want {
				t.Errorf("%s [%s] Months[%d] = %s, 期望 %s", r, c.desc, i, months[i], want)
			}
		}
		// 长度必须等于 Len()，即摊分份数
		if len(months) != r.Len() {
			t.Errorf("%s Months 长度 %d != Len() %d", r, len(months), r.Len())
		}
		// 必须按时间正序（摊分份额逐月对齐依赖此序）
		for i := 1; i < len(months); i++ {
			if !months[i-1].Before(months[i]) {
				t.Errorf("%s Months 未按正序排列: %s 未早于 %s", r, months[i-1], months[i])
			}
		}
	}

	// 零值区间返回 nil
	var zero MonthRange
	if zero.Months() != nil {
		t.Error("零值区间的 Months 应为 nil")
	}
	if zero.Len() != 0 {
		t.Errorf("零值区间的 Len = %d, 期望 0", zero.Len())
	}
	if zero.String() != "" {
		t.Errorf("零值区间的 String = %q, 期望空串", zero.String())
	}
	if !zero.Start().IsZero() || !zero.End().IsZero() {
		t.Error("零值区间的起止月应均为零值")
	}
}

// TestMonthRangeString 校验可读文本（仅日志与调试用，非落库形态）。
func TestMonthRangeString(t *testing.T) {
	r, _ := NewMonthRange(MustParseMonth("2026-01"), 3)
	if got := r.String(); got != "2026-01~2026-03" {
		t.Errorf("String = %q, 期望 \"2026-01~2026-03\"", got)
	}
}

// TestMonthRangeLargeCount 校验摊分月数无上限（H-1、前提 5）时仍正确。
func TestMonthRangeLargeCount(t *testing.T) {
	const count = 1200 // 100 年
	r, err := NewMonthRange(MustParseMonth("2026-01"), count)
	if err != nil {
		t.Fatalf("大月数报错: %v", err)
	}
	if r.Len() != count {
		t.Errorf("Len = %d, 期望 %d", r.Len(), count)
	}
	if r.End().String() != "2125-12" {
		t.Errorf("结束月 = %s, 期望 2125-12", r.End())
	}
	if len(r.Months()) != count {
		t.Errorf("Months 长度 = %d, 期望 %d", len(r.Months()), count)
	}
	// 两端与中点的穿刺判定
	if !r.Contains(MustParseMonth("2026-01")) || !r.Contains(MustParseMonth("2125-12")) {
		t.Error("大区间两端应命中")
	}
	if !r.Contains(MustParseMonth("2075-06")) {
		t.Error("大区间中点应命中")
	}
	if r.Contains(MustParseMonth("2126-01")) {
		t.Error("大区间结束月次月不应命中")
	}
}
