package calendar

// Month 与 MonthRange 的白盒单测：年月构造与解析、**日期 → 所属月**、
// 月份算术（含与 civil.AddMonths 分歧的月末日回归）、区间的
// 「起始月 + N − 1」与「起始月 ≤ M ≤ 结束月」两项承载。

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pkg/errors"
)

// mustMonth 构造合法年月，失败即测试失败。
func mustMonth(t *testing.T, year int, month time.Month) Month {
	t.Helper()
	m, err := NewMonth(year, month)
	if err != nil {
		t.Fatalf("构造年月 %04d-%02d 意外失败: %+v", year, month, err)
	}
	return m
}

// mustParseMonth 解析合法年月文本，失败即测试失败。
func mustParseMonth(t *testing.T, text string) Month {
	t.Helper()
	m, err := ParseMonth(text)
	if err != nil {
		t.Fatalf("解析年月 %q 意外失败: %+v", text, err)
	}
	return m
}

func TestNewMonth(t *testing.T) {
	cases := []struct {
		name    string
		year    int
		month   time.Month
		wantErr error
	}{
		{"常规年月", 2026, time.January, nil},
		{"12月", 2026, time.December, nil},
		{"年份下界", MinYear, time.January, nil},
		{"年份上界", MaxYear, time.December, nil},
		{"月份为0", 2026, time.Month(0), ErrInvalidMonth},
		{"月份为13", 2026, time.Month(13), ErrInvalidMonth},
		{"月份为负", 2026, time.Month(-1), ErrInvalidMonth},
		{"年份0", 0, time.January, ErrYearOutOfRange},
		{"年份10000", 10000, time.January, ErrYearOutOfRange},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := NewMonth(c.year, c.month)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("期望成功，实际报错: %+v", err)
				}
				if m.Year() != c.year || m.Month() != c.month {
					t.Fatalf("字段不符: 期望 %04d-%02d，实际 %s", c.year, c.month, m)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("错误分档不符: 期望 %v，实际 %+v", c.wantErr, err)
			}
			if !m.IsZero() {
				t.Fatalf("失败时应返回零值，实际 %s", m)
			}
		})
	}
}

func TestParseMonth(t *testing.T) {
	cases := []struct {
		text    string
		wantErr error
	}{
		{"2026-01", nil},
		{"2026-12", nil},
		{"0001-01", nil},
		{"9999-12", nil},
		// 形态不符
		{"2026-1", ErrTextMalformed},
		{"2026/01", ErrTextMalformed},
		{"202601", ErrTextMalformed},
		{"2026-01-01", ErrTextMalformed},
		{"", ErrTextMalformed},
		{" 2026-01", ErrTextMalformed},
		{"2026-01 ", ErrTextMalformed},
		{"2026-01 out of range", ErrTextMalformed},
		// 月份越界
		{"2026-13", ErrInvalidMonth},
		{"2026-00", ErrInvalidMonth},
		// 年份越界
		{"0000-01", ErrYearOutOfRange},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			m, err := ParseMonth(c.text)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("期望成功，实际报错: %+v", err)
				}
				if got := m.String(); got != c.text {
					t.Fatalf("往返不一致: 原文 %q，回写 %q", c.text, got)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("错误分档不符: 期望 %v，实际 %+v", c.wantErr, err)
			}
			if !m.IsZero() {
				t.Fatalf("失败时应返回零值，实际 %s", m)
			}
		})
	}
}

// TestMonthOf 覆盖承载第 4 项「日期 → 所属月」，即 H-2 与 J 域未摊分口径的归月依据。
func TestMonthOf(t *testing.T) {
	cases := []struct{ date, want string }{
		{"2026-01-01", "2026-01"}, // 月初
		{"2026-01-31", "2026-01"}, // 月末（跨月摊分最敏感的一天）
		{"2026-02-28", "2026-02"},
		{"2024-02-29", "2024-02"}, // 闰日
		{"2026-09-30", "2026-09"},
		{"2026-12-31", "2026-12"}, // 年末
		{"0001-01-01", "0001-01"},
		{"9999-12-31", "9999-12"},
	}
	for _, c := range cases {
		t.Run(c.date, func(t *testing.T) {
			d, err := ParseDate(c.date)
			if err != nil {
				t.Fatalf("解析失败: %+v", err)
			}
			m, err := MonthOf(d)
			if err != nil {
				t.Fatalf("归月失败: %+v", err)
			}
			if m.String() != c.want {
				t.Fatalf("%s 应归入 %s，实际 %s", c.date, c.want, m)
			}
		})
	}

	// 零值日期不得静默归到某个月——那会让漏填日期的明细悄悄进入某月统计
	var zero Date
	if _, err := MonthOf(zero); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值日期归月应报 ErrZeroValue，实际 %+v", err)
	}
}

// TestMonthAddMonthsAvoidsDayOverflow 是本包**最关键**的回归用例。
//
// 锁住：月份算术走纯整数月序号，**不带日溢出**。若哪天有人把它改成
// 在日期上调 AddDate/civil.AddMonths，本用例会立刻失败。
// 期望值取自实测（answer §三.4）：civil 在这些起始日上会多跳一个月，
// 如 `2026-01-31 + 1月 = 2026-03-03`（所属月 3 月），而正确答案是 2026-02。
func TestMonthAddMonthsAvoidsDayOverflow(t *testing.T) {
	// 这些是实测中 civil.AddMonths 会算错的全部 2026 年起始日
	for _, day := range []int{29, 30, 31} {
		t.Run("起始日"+string(rune('0'+day/10))+string(rune('0'+day%10)), func(t *testing.T) {
			for month := time.January; month <= time.December; month++ {
				d, err := NewDate(2026, month, day)
				if err != nil {
					continue // 该月没有这一天（如 2 月 30 日），跳过
				}
				start, err := MonthOf(d)
				if err != nil {
					t.Fatalf("归月失败: %+v", err)
				}
				// 摊 2 个月：结束月必须恰为次月，与「日」无关
				end, err := start.AddMonths(1)
				if err != nil {
					t.Fatalf("月份加减失败: %+v", err)
				}
				wantIdx := 2026*12 + int(month) - 1 + 1
				wantYear, wantMonth := wantIdx/12, time.Month(wantIdx%12+1)
				if end.Year() != wantYear || end.Month() != wantMonth {
					t.Fatalf("日溢出: 支出日期 %s 摊 2 月，结束月应为 %04d-%02d，实际 %s",
						d, wantYear, wantMonth, end)
				}
			}
		})
	}
}

func TestMonthAddMonths(t *testing.T) {
	cases := []struct {
		from string
		n    int
		want string
	}{
		{"2026-01", 0, "2026-01"},
		{"2026-01", 1, "2026-02"},
		{"2026-01", 11, "2026-12"},
		{"2026-01", 12, "2027-01"}, // 跨年进位
		{"2026-12", 1, "2027-01"},
		{"2026-01", -1, "2025-12"}, // 跨年借位
		{"2026-03", -1, "2026-02"},
		{"2026-01", 24, "2028-01"},
		{"2026-01", -12, "2025-01"},
		{"2026-06", 120, "2036-06"}, // H-1 摊分月数不设上限
	}
	for _, c := range cases {
		t.Run(c.from, func(t *testing.T) {
			from := mustParseMonth(t, c.from)
			got, err := from.AddMonths(c.n)
			if err != nil {
				t.Fatalf("月份加减失败: %+v", err)
			}
			if got.String() != c.want {
				t.Fatalf("%s 加 %d 月: 期望 %s，实际 %s", c.from, c.n, c.want, got)
			}
			// 逆运算与 MonthsSince 自洽
			if back := got.MonthsSince(from); back != c.n {
				t.Fatalf("MonthsSince 不符: 期望 %d，实际 %d", c.n, back)
			}
		})
	}

	// 年份边界：H-1 不设上限，故越界是可达的，必须报错而非产出存不回来的值
	upper := mustMonth(t, MaxYear, time.December)
	if _, err := upper.AddMonths(1); !errors.Is(err, ErrYearOutOfRange) {
		t.Fatalf("越过上界应报 ErrYearOutOfRange，实际 %+v", err)
	}
	lower := mustMonth(t, MinYear, time.January)
	for _, n := range []int{-1, -12, -13, -100000} {
		if _, err := lower.AddMonths(n); !errors.Is(err, ErrYearOutOfRange) {
			t.Fatalf("加 %d 月越过下界应报 ErrYearOutOfRange，实际 %+v", n, err)
		}
	}
	// 极大月数：不得因整数溢出或负序号还原而静默产出合法年月
	if _, err := mustMonth(t, 2026, time.January).AddMonths(1 << 40); !errors.Is(err, ErrYearOutOfRange) {
		t.Fatalf("极大月数应报 ErrYearOutOfRange，实际 %+v", err)
	}
}

// TestMonthStringLexicalOrderEqualsChronological 锁住年月的「字典序 = 时间序」，
// store/sqlite 的摊分起止月列据此用 TEXT 做区间穿刺（8.2）。
func TestMonthStringLexicalOrderEqualsChronological(t *testing.T) {
	ordered := []Month{
		mustMonth(t, 1, time.January),
		mustMonth(t, 2025, time.December),
		mustMonth(t, 2026, time.January),
		mustMonth(t, 2026, time.February),
		mustMonth(t, 2026, time.September), // 9 月 vs 10 月：补零位最易错处
		mustMonth(t, 2026, time.October),
		mustMonth(t, 2026, time.December),
		mustMonth(t, 2027, time.January),
		mustMonth(t, 9999, time.December),
	}
	for i := 0; i+1 < len(ordered); i++ {
		lo, hi := ordered[i], ordered[i+1]
		if !(lo.String() < hi.String()) {
			t.Fatalf("字典序与时间序不符: %q 应 < %q", lo.String(), hi.String())
		}
		if lo.Compare(hi) != -1 || !lo.Before(hi) || !hi.After(lo) {
			t.Fatalf("比较不符: %s 与 %s", lo, hi)
		}
	}
}

func TestMonthComparableAndZero(t *testing.T) {
	// 可比较、可作 map 键：J 域按月分组聚合拿它当键
	a := mustMonth(t, 2026, time.January)
	b := mustParseMonth(t, "2026-01")
	if a != b || !a.Equal(b) {
		t.Fatalf("经不同路径构造的同一年月应相等")
	}
	grouped := map[Month]int{}
	grouped[a]++
	grouped[b]++
	if len(grouped) != 1 || grouped[a] != 2 {
		t.Fatalf("作 map 键应归并: len=%d count=%d", len(grouped), grouped[a])
	}

	var zero Month
	if !zero.IsZero() {
		t.Fatalf("零值 IsZero 应为真")
	}
	if _, err := zero.AddMonths(1); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值加减应报 ErrZeroValue，实际 %+v", err)
	}
	if _, err := zero.MarshalText(); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值序列化应报 ErrZeroValue，实际 %+v", err)
	}
	if _, err := zero.Value(); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值落库应报 ErrZeroValue，实际 %+v", err)
	}
	if _, err := zero.FirstDate(); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值取首日应报 ErrZeroValue，实际 %+v", err)
	}
	if _, err := zero.LastDate(); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值取末日应报 ErrZeroValue，实际 %+v", err)
	}
	// 零值文本年份 0 越界，无法被读回，不会悄悄变成合法年月
	if _, err := ParseMonth(zero.String()); err == nil {
		t.Fatalf("零值文本不应能被解析回来")
	}
}

// TestMonthFirstLastDate 覆盖 J 域「按支出日期归月」等价区间的两个端点，
// 月长与闰年一律由标准库负责（本包不含月长表）。
func TestMonthFirstLastDate(t *testing.T) {
	cases := []struct{ month, first, last string }{
		{"2026-01", "2026-01-01", "2026-01-31"},
		{"2026-02", "2026-02-01", "2026-02-28"}, // 平年
		{"2024-02", "2024-02-01", "2024-02-29"}, // 闰年
		{"2000-02", "2000-02-01", "2000-02-29"}, // 世纪闰年
		{"1900-02", "1900-02-01", "1900-02-28"}, // 非闰的世纪年
		{"2026-04", "2026-04-01", "2026-04-30"}, // 小月
		{"2026-12", "2026-12-01", "2026-12-31"}, // 年末（末日跨年计算）
		{"9999-12", "9999-12-01", "9999-12-31"}, // 上界年
	}
	for _, c := range cases {
		t.Run(c.month, func(t *testing.T) {
			m := mustParseMonth(t, c.month)
			first, err := m.FirstDate()
			if err != nil {
				t.Fatalf("取首日失败: %+v", err)
			}
			if first.String() != c.first {
				t.Fatalf("首日不符: 期望 %s，实际 %s", c.first, first)
			}
			last, err := m.LastDate()
			if err != nil {
				t.Fatalf("取末日失败: %+v", err)
			}
			if last.String() != c.last {
				t.Fatalf("末日不符: 期望 %s，实际 %s", c.last, last)
			}
			// 端点须归回本月，且末日次日必然跨出本月——归月与区间口径自洽
			for _, d := range []Date{first, last} {
				got, err := MonthOf(d)
				if err != nil {
					t.Fatalf("归月失败: %+v", err)
				}
				if got != m {
					t.Fatalf("端点 %s 应归入 %s，实际 %s", d, m, got)
				}
			}
		})
	}
}

func TestMonthScanAndJSON(t *testing.T) {
	t.Run("落库往返", func(t *testing.T) {
		m := mustMonth(t, 2026, time.January)
		stored, err := m.Value()
		if err != nil {
			t.Fatalf("落库失败: %+v", err)
		}
		if stored != "2026-01" {
			t.Fatalf("落库形态应为定点文本，实际 %#v", stored)
		}
		for _, src := range []any{"2026-01", []byte("2026-01")} {
			var back Month
			if err := back.Scan(src); err != nil {
				t.Fatalf("扫描 %T 失败: %+v", src, err)
			}
			if back != m {
				t.Fatalf("往返不一致: %s vs %s", m, back)
			}
		}
	})
	t.Run("拒收time.Time与NULL", func(t *testing.T) {
		var m Month
		for _, src := range []any{time.Now(), nil, int64(202601), true} {
			if err := m.Scan(src); !errors.Is(err, ErrScanUnsupported) {
				t.Fatalf("应拒收 %T，实际 %+v", src, err)
			}
			if !m.IsZero() {
				t.Fatalf("拒收后不应写入接收者，实际 %s", m)
			}
		}
	})
	t.Run("JSON为字符串", func(t *testing.T) {
		type payload struct {
			StartMonth Month `json:"amortizeStartMonth"`
		}
		in := payload{StartMonth: mustMonth(t, 2026, time.January)}
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("序列化失败: %+v", err)
		}
		if want := `{"amortizeStartMonth":"2026-01"}`; string(raw) != want {
			t.Fatalf("JSON 形态不符: 期望 %s，实际 %s", want, raw)
		}
		var out payload
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("反序列化失败: %+v", err)
		}
		if out.StartMonth != in.StartMonth {
			t.Fatalf("往返不一致")
		}
	})
}

// TestNewMonthRange 覆盖 H-1「默认 1（不摊分），不设上限」的边界。
func TestNewMonthRange(t *testing.T) {
	start := mustMonth(t, 2026, time.January)

	t.Run("月数下界为1", func(t *testing.T) {
		r, err := NewMonthRange(start, 1)
		if err != nil {
			t.Fatalf("月数 1 应合法: %+v", err)
		}
		// 摊 1 个月 = 只摊在起始月，结束月恒等于起始月（少减那个 1 就会错成 2 个月）
		end, err := r.EndMonth()
		if err != nil {
			t.Fatalf("取末月失败: %+v", err)
		}
		if end != start {
			t.Fatalf("摊 1 月时结束月应等于起始月，实际 %s", end)
		}
	})

	t.Run("月数不设上限", func(t *testing.T) {
		for _, count := range []int{2, 12, 120, 1200} {
			if _, err := NewMonthRange(start, count); err != nil {
				t.Fatalf("月数 %d 应合法（H-1 不设上限）: %+v", count, err)
			}
		}
	})

	t.Run("月数非法", func(t *testing.T) {
		for _, count := range []int{0, -1, -12} {
			r, err := NewMonthRange(start, count)
			if !errors.Is(err, ErrMonthCountInvalid) {
				t.Fatalf("月数 %d 应报 ErrMonthCountInvalid，实际 %+v", count, err)
			}
			if !r.IsZero() {
				t.Fatalf("失败时应返回零值")
			}
		}
	})

	t.Run("末月不可表示时构造即失败", func(t *testing.T) {
		// 不产出「构造成功但 EndMonth 恒报错」的值
		late := mustMonth(t, MaxYear, time.December)
		if _, err := NewMonthRange(late, 2); !errors.Is(err, ErrYearOutOfRange) {
			t.Fatalf("应报 ErrYearOutOfRange，实际 %+v", err)
		}
	})

	t.Run("零值起始月", func(t *testing.T) {
		var zero Month
		if _, err := NewMonthRange(zero, 1); !errors.Is(err, ErrZeroValue) {
			t.Fatalf("应报 ErrZeroValue，实际 %+v", err)
		}
	})
}

// TestMonthRangeEndMonth 覆盖承载「**起始月 + N − 1**」。
func TestMonthRangeEndMonth(t *testing.T) {
	cases := []struct {
		start string
		count int
		end   string
	}{
		{"2026-01", 1, "2026-01"}, // 不摊分
		{"2026-01", 2, "2026-02"},
		{"2026-01", 3, "2026-03"},
		{"2026-01", 12, "2026-12"}, // 整年，恰不进位
		{"2026-01", 13, "2027-01"}, // 进位
		{"2026-12", 2, "2027-01"},  // 跨年
		{"2026-11", 3, "2027-01"},
		{"2026-06", 120, "2036-05"},
	}
	for _, c := range cases {
		t.Run(c.start, func(t *testing.T) {
			r, err := NewMonthRange(mustParseMonth(t, c.start), c.count)
			if err != nil {
				t.Fatalf("构造区间失败: %+v", err)
			}
			end, err := r.EndMonth()
			if err != nil {
				t.Fatalf("取末月失败: %+v", err)
			}
			if end.String() != c.end {
				t.Fatalf("%s 摊 %d 月: 结束月期望 %s，实际 %s", c.start, c.count, c.end, end)
			}
			// 与承载公式逐字对齐：结束月 − 起始月 == N − 1
			if span := end.MonthsSince(r.StartMonth()); span != c.count-1 {
				t.Fatalf("跨度应为 N−1=%d，实际 %d", c.count-1, span)
			}
		})
	}
}

// TestMonthRangeContains 覆盖承载「**起始月 ≤ M ≤ 结束月**」，
// 即 8.2 区间穿刺在内存侧的判据，必须与 store 下推的 SQL 同口径（闭区间、两端都含）。
func TestMonthRangeContains(t *testing.T) {
	r, err := NewMonthRange(mustParseMonth(t, "2026-11"), 4) // 覆盖 2026-11 ~ 2027-02
	if err != nil {
		t.Fatalf("构造区间失败: %+v", err)
	}
	cases := []struct {
		month string
		want  bool
		index int // 期望的 IndexOf，从 1 起计；0 表示不在区间内
	}{
		{"2026-10", false, 0}, // 起始月前一月
		{"2026-11", true, 1},  // 起始月（闭区间下界）
		{"2026-12", true, 2},
		{"2027-01", true, 3},
		{"2027-02", true, 4},  // 结束月（闭区间上界）
		{"2027-03", false, 0}, // 结束月后一月
		{"2020-01", false, 0},
		{"2030-01", false, 0},
	}
	for _, c := range cases {
		t.Run(c.month, func(t *testing.T) {
			m := mustParseMonth(t, c.month)
			if got := r.Contains(m); got != c.want {
				t.Fatalf("%s 是否落在 %s 内: 期望 %v，实际 %v", c.month, r, c.want, got)
			}
			if got := r.IndexOf(m); got != c.index {
				t.Fatalf("%s 在 %s 内的序号: 期望 %d，实际 %d", c.month, r, c.index, got)
			}
		})
	}

	// 与 Compare 交叉核对：Contains 必须等价于「≥ 起始月 且 ≤ 结束月」
	end, err := r.EndMonth()
	if err != nil {
		t.Fatalf("取末月失败: %+v", err)
	}
	for offset := -6; offset <= 12; offset++ {
		m, err := r.StartMonth().AddMonths(offset)
		if err != nil {
			t.Fatalf("构造对照年月失败: %+v", err)
		}
		byCompare := m.Compare(r.StartMonth()) >= 0 && m.Compare(end) <= 0
		if got := r.Contains(m); got != byCompare {
			t.Fatalf("%s: Contains=%v 与比较式=%v 不一致", m, got, byCompare)
		}
	}

	// 零值一律不落在任何区间内
	var zeroMonth Month
	if r.Contains(zeroMonth) {
		t.Fatalf("零值年月不应落在区间内")
	}
	var zeroRange MonthRange
	if zeroRange.Contains(mustParseMonth(t, "2026-01")) {
		t.Fatalf("零值区间不应包含任何年月")
	}
	if _, err := zeroRange.EndMonth(); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值区间取末月应报 ErrZeroValue，实际 %+v", err)
	}
}

// TestMonthRangeMonths 覆盖 H-4「覆盖月份区间」的逐月展开，
// 并与 Contains / IndexOf / EndMonth 交叉核对，确保四者同口径。
func TestMonthRangeMonths(t *testing.T) {
	for _, count := range []int{1, 2, 3, 12, 14} {
		r, err := NewMonthRange(mustParseMonth(t, "2026-11"), count)
		if err != nil {
			t.Fatalf("构造区间失败: %+v", err)
		}
		months, err := r.Months()
		if err != nil {
			t.Fatalf("展开失败: %+v", err)
		}
		if len(months) != count {
			t.Fatalf("展开月数不符: 期望 %d，实际 %d", count, len(months))
		}
		end, err := r.EndMonth()
		if err != nil {
			t.Fatalf("取末月失败: %+v", err)
		}
		if months[0] != r.StartMonth() {
			t.Fatalf("首项应为起始月，实际 %s", months[0])
		}
		if months[len(months)-1] != end {
			t.Fatalf("末项应为结束月，实际 %s", months[len(months)-1])
		}
		for i, m := range months {
			// 严格递增、无重复、无缺漏
			if i > 0 && m.MonthsSince(months[i-1]) != 1 {
				t.Fatalf("第 %d 项与前一项不相邻: %s / %s", i, months[i-1], m)
			}
			if !r.Contains(m) {
				t.Fatalf("展开项 %s 应落在区间内", m)
			}
			// IndexOf 从 1 起计，与 H-2「第 1 个摊分月」逐字对齐；
			// rule/amortize 判「是否末月」用 IndexOf == Count()
			if got := r.IndexOf(m); got != i+1 {
				t.Fatalf("%s 序号应为 %d，实际 %d", m, i+1, got)
			}
		}
		// 末月判据：尾差归末月依赖它
		if r.IndexOf(end) != r.Count() {
			t.Fatalf("末月序号应等于月数 %d，实际 %d", r.Count(), r.IndexOf(end))
		}
	}

	var zeroRange MonthRange
	if _, err := zeroRange.Months(); !errors.Is(err, ErrZeroValue) {
		t.Fatalf("零值区间展开应报 ErrZeroValue，实际 %+v", err)
	}
}

func TestMonthRangeString(t *testing.T) {
	r, err := NewMonthRange(mustParseMonth(t, "2026-01"), 3)
	if err != nil {
		t.Fatalf("构造区间失败: %+v", err)
	}
	if want := "2026-01~2026-03(3)"; r.String() != want {
		t.Fatalf("形态不符: 期望 %s，实际 %s", want, r.String())
	}
	// 多位月数：appendPadded 的 width=1 不应截断
	wide, err := NewMonthRange(mustParseMonth(t, "2026-01"), 120)
	if err != nil {
		t.Fatalf("构造区间失败: %+v", err)
	}
	if want := "2026-01~2035-12(120)"; wide.String() != want {
		t.Fatalf("多位月数形态不符: 期望 %s，实际 %s", want, wide.String())
	}
	var zero MonthRange
	if zero.String() != "空区间" {
		t.Fatalf("零值形态不符: %s", zero.String())
	}
}

// TestMonthUnmarshalTextErrors 覆盖 UnmarshalText 的失败路径：
// 非法输入必须原样透出 ParseMonth 的错误分档，且**不写坏接收者**。
func TestMonthUnmarshalTextErrors(t *testing.T) {
	original := mustMonth(t, 2026, time.January)
	for _, tc := range []struct {
		text    string
		wantErr error
	}{
		{"2026/01", ErrTextMalformed},
		{"2026-1", ErrTextMalformed},
		{"", ErrTextMalformed},
		{"2026-13", ErrInvalidMonth},
		{"0000-01", ErrYearOutOfRange},
	} {
		m := original
		if err := m.UnmarshalText([]byte(tc.text)); !errors.Is(err, tc.wantErr) {
			t.Fatalf("解析 %q 应报 %v，实际 %+v", tc.text, tc.wantErr, err)
		}
		if m != original {
			t.Fatalf("失败时不应改写接收者: 原值 %s，实际 %s", original, m)
		}
	}
}

// TestMonthScanErrorPaths 覆盖 Scan 在文本形态非法时的失败路径（string 与 []byte 两支）。
func TestMonthScanErrorPaths(t *testing.T) {
	for _, src := range []any{"2026/01", []byte("2026/01")} {
		var m Month
		if err := m.Scan(src); !errors.Is(err, ErrTextMalformed) {
			t.Fatalf("扫描 %T 非法文本应报 ErrTextMalformed，实际 %+v", src, err)
		}
		if !m.IsZero() {
			t.Fatalf("失败时不应改写接收者，实际 %s", m)
		}
	}
	for _, src := range []any{"2026-13", []byte("2026-13")} {
		var m Month
		if err := m.Scan(src); !errors.Is(err, ErrInvalidMonth) {
			t.Fatalf("扫描 %T 越界月份应报 ErrInvalidMonth，实际 %+v", src, err)
		}
	}
}
