package calendar

import (
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"
)

// TestParseMonth 覆盖定长形态校验与「非零值必合法」不变式。
func TestParseMonth(t *testing.T) {
	okCases := []struct {
		value       string
		year, month int
	}{
		{"2026-01", 2026, 1},
		{"2026-12", 2026, 12},
		{"0001-01", 1, 1},
		{"9999-12", 9999, 12},
	}
	for _, c := range okCases {
		got, err := ParseMonth(c.value)
		if err != nil {
			t.Fatalf("ParseMonth(%q) 意外报错: %v", c.value, err)
		}
		if got.Year() != c.year || got.Month() != c.month {
			t.Errorf("ParseMonth(%q) = %d-%d, 期望 %d-%d",
				c.value, got.Year(), got.Month(), c.year, c.month)
		}
		if got.String() != c.value {
			t.Errorf("ParseMonth(%q).String() = %q, 期望原样还原", c.value, got.String())
		}
	}

	badCases := []struct {
		value string
		want  error
		desc  string
	}{
		{"2026-1", ErrMonthFormat, "非定长"},
		{"2026-01-01", ErrMonthFormat, "含日部分"},
		{"2026/01", ErrMonthFormat, "分隔符错误"},
		{"", ErrMonthFormat, "空串"},
		{"2026-0a", ErrMonthFormat, "非数字"},
		{"2026-13", ErrMonthValue, "13 月"},
		{"2026-00", ErrMonthValue, "0 月"},
		{"0000-01", ErrYearRange, "年份下界外"},
	}
	for _, c := range badCases {
		got, err := ParseMonth(c.value)
		if !errors.Is(err, c.want) {
			t.Errorf("ParseMonth(%q) [%s] 错误 = %v, 期望 %v", c.value, c.desc, err, c.want)
		}
		if !got.IsZero() {
			t.Errorf("ParseMonth(%q) [%s] 出错却返回非零值", c.value, c.desc)
		}
	}
}

// TestNewMonth 校验工厂函数的边界校验。
func TestNewMonth(t *testing.T) {
	if _, err := NewMonth(2026, 9); err != nil {
		t.Fatalf("NewMonth 合法输入报错: %v", err)
	}
	badCases := []struct {
		year, month int
		want        error
	}{
		{2026, 13, ErrMonthValue},
		{2026, 0, ErrMonthValue},
		{2026, -1, ErrMonthValue},
		{0, 1, ErrYearRange},
		{10000, 1, ErrYearRange},
	}
	for _, c := range badCases {
		got, err := NewMonth(c.year, c.month)
		if !errors.Is(err, c.want) {
			t.Errorf("NewMonth(%d,%d) 错误 = %v, 期望 %v", c.year, c.month, err, c.want)
		}
		if !got.IsZero() {
			t.Errorf("NewMonth(%d,%d) 出错却返回非零值", c.year, c.month)
		}
	}
}

// TestMonthZero 校验零值语义。
func TestMonthZero(t *testing.T) {
	var zero Month
	if !zero.IsZero() {
		t.Error("零值 Month 的 IsZero 应为 true")
	}
	if zero.String() != "" {
		t.Errorf("零值 String() = %q, 期望空串", zero.String())
	}
	if zero.Year() != 0 || zero.Month() != 0 {
		t.Error("零值 Month 的年月应均为 0")
	}
	if !zero.FirstDate().IsZero() || !zero.LastDate().IsZero() {
		t.Error("零值 Month 的 FirstDate/LastDate 应为零值 Date")
	}
	if _, err := zero.AddMonths(1); !errors.Is(err, ErrMonthValue) {
		t.Errorf("零值 AddMonths 错误 = %v, 期望 ErrMonthValue", err)
	}
}

// TestMonthAddMonths 覆盖 J-3 环比/同比运算与跨年进位、越界拦截。
func TestMonthAddMonths(t *testing.T) {
	cases := []struct {
		from string
		n    int
		want string
		desc string
	}{
		{"2026-01", 1, "2026-02", "月内递增"},
		{"2026-12", 1, "2027-01", "跨年进位"},
		{"2026-01", -1, "2025-12", "跨年退位（J-3 环比）"},
		{"2026-09", -12, "2025-09", "同比（J-3）"},
		{"2026-01", 11, "2026-12", "同年末月"},
		{"2026-01", 12, "2027-01", "整年"},
		{"2026-01", 0, "2026-01", "零位移"},
		{"2026-01", 24, "2028-01", "跨两年"},
		{"2026-06", -18, "2024-12", "负向跨两年"},
	}
	for _, c := range cases {
		got, err := MustParseMonth(c.from).AddMonths(c.n)
		if err != nil {
			t.Fatalf("%s AddMonths(%d) [%s] 报错: %v", c.from, c.n, c.desc, err)
		}
		if got.String() != c.want {
			t.Errorf("%s AddMonths(%d) [%s] = %s, 期望 %s", c.from, c.n, c.desc, got, c.want)
		}
	}

	// 越界拦截（方案 §五 5.2）
	if _, err := MustParseMonth("9999-12").AddMonths(1); !errors.Is(err, ErrYearRange) {
		t.Error("上界溢出应返回 ErrYearRange")
	}
	if _, err := MustParseMonth("0001-01").AddMonths(-1); !errors.Is(err, ErrYearRange) {
		t.Error("下界溢出应返回 ErrYearRange")
	}
	if _, err := MustParseMonth("2026-01").AddMonths(-1000000); !errors.Is(err, ErrYearRange) {
		t.Error("大幅负向溢出应返回 ErrYearRange")
	}
}

// TestMonthNoThirteenthMonth 校验内部月序号表示使「13 月」结构上不可能出现。
func TestMonthNoThirteenthMonth(t *testing.T) {
	m := MustParseMonth("2026-01")
	// 连续走 36 个月，每一步的月份都必须落在 1~12
	for i := 0; i < 36; i++ {
		next, err := m.AddMonths(i)
		if err != nil {
			t.Fatalf("AddMonths(%d) 报错: %v", i, err)
		}
		if next.Month() < 1 || next.Month() > 12 {
			t.Fatalf("AddMonths(%d) 产出非法月份 %d", i, next.Month())
		}
		// 文本必须能被 ParseMonth 解回同值（不存在 2026-13 这种输出）
		back, err := ParseMonth(next.String())
		if err != nil || !back.Equal(next) {
			t.Fatalf("AddMonths(%d) = %q 无法解析回原值: %v", i, next.String(), err)
		}
	}
}

// TestMonthsSince 校验月数差，兼作 AddMonths 的逆运算验证。
func TestMonthsSince(t *testing.T) {
	cases := []struct {
		later, earlier string
		want           int
	}{
		{"2026-02", "2026-01", 1},
		{"2026-01", "2026-02", -1},
		{"2026-01", "2026-01", 0},
		{"2027-01", "2026-01", 12},
		{"2026-01", "2025-12", 1}, // 跨年
		{"2026-12", "2026-01", 11},
	}
	for _, c := range cases {
		got := MustParseMonth(c.later).MonthsSince(MustParseMonth(c.earlier))
		if got != c.want {
			t.Errorf("%s MonthsSince %s = %d, 期望 %d", c.later, c.earlier, got, c.want)
		}
	}
}

// TestMonthCompare 覆盖 J-6「已发生（≤ 当前月）/ 未来待摊」分段所需的比较语义。
func TestMonthCompare(t *testing.T) {
	a := MustParseMonth("2026-01")
	b := MustParseMonth("2026-02")
	c := MustParseMonth("2027-01")

	if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Error("Compare 的三分支不正确")
	}
	if !a.Before(b) || !b.Before(c) {
		t.Error("Before 未能逐级区分月/年")
	}
	if !c.After(b) || !b.After(a) {
		t.Error("After 不正确")
	}
	if !a.Equal(MustParseMonth("2026-01")) || a.Equal(b) {
		t.Error("Equal 不正确")
	}

	// J-6 分段：以 2026-02 为「当前月」，≤ 它的算已发生
	current := b
	for _, tc := range []struct {
		month    string
		happened bool
	}{
		{"2025-12", true},
		{"2026-01", true},
		{"2026-02", true}, // 当前月本身算已发生（闭合）
		{"2026-03", false},
		{"2027-01", false},
	} {
		m := MustParseMonth(tc.month)
		got := !m.After(current)
		if got != tc.happened {
			t.Errorf("以 %s 为当前月，%s 已发生 = %v, 期望 %v", current, tc.month, got, tc.happened)
		}
	}

	var zero Month
	if !zero.Before(a) {
		t.Error("零值应视为最小值")
	}
}

// TestMonthLexicographicOrder 校验「定长 ⇒ 字典序 = 时间序」（方案 V5）。
// 8.2 的区间穿刺在 SQL 侧靠这条性质用朴素字符串比较完成。
func TestMonthLexicographicOrder(t *testing.T) {
	raw := []string{"2026-10", "2026-02", "2025-12", "2026-01", "2026-09", "2027-01", "0001-01", "9999-12"}
	months := make([]Month, 0, len(raw))
	for _, s := range raw {
		m := MustParseMonth(s)
		if len(m.String()) != monthLen {
			t.Fatalf("%s 文本长度 = %d, 期望恒为 %d", s, len(m.String()), monthLen)
		}
		months = append(months, m)
	}

	byString := append([]Month(nil), months...)
	sort.Slice(byString, func(i, j int) bool { return byString[i].String() < byString[j].String() })

	byCompare := append([]Month(nil), months...)
	sort.Slice(byCompare, func(i, j int) bool { return byCompare[i].Compare(byCompare[j]) < 0 })

	for i := range byString {
		if byString[i] != byCompare[i] {
			t.Fatalf("第 %d 位：字符串序 %s != 时间序 %s（字典序性质被破坏）",
				i, byString[i], byCompare[i])
		}
	}
}

// TestMonthFirstLastDate 覆盖 J-1 未摊分口径归月所需的月首月末（方案 V7）。
// 月末日数含闰年与世纪年规则。
func TestMonthFirstLastDate(t *testing.T) {
	cases := []struct {
		month, first, last string
		desc               string
	}{
		{"2026-01", "2026-01-01", "2026-01-31", "31 天月"},
		{"2026-02", "2026-02-01", "2026-02-28", "平年 2 月"},
		{"2024-02", "2024-02-01", "2024-02-29", "闰年 2 月"},
		{"2000-02", "2000-02-01", "2000-02-29", "世纪闰年 2 月"},
		{"1900-02", "1900-02-01", "1900-02-28", "世纪年 1900 非闰年"},
		{"2026-04", "2026-04-01", "2026-04-30", "30 天月"},
		{"2026-12", "2026-12-01", "2026-12-31", "年末月"},
		{"9999-12", "9999-12-01", "9999-12-31", "上界年末月（不得越界）"},
		{"0001-01", "0001-01-01", "0001-01-31", "下界年首月"},
	}
	for _, c := range cases {
		m := MustParseMonth(c.month)
		if got := m.FirstDate().String(); got != c.first {
			t.Errorf("%s [%s] FirstDate = %s, 期望 %s", c.month, c.desc, got, c.first)
		}
		if got := m.LastDate().String(); got != c.last {
			t.Errorf("%s [%s] LastDate = %s, 期望 %s", c.month, c.desc, got, c.last)
		}
		// 闭环：月首月末的所属月必须仍是该月本身
		if !m.FirstDate().ToMonth().Equal(m) || !m.LastDate().ToMonth().Equal(m) {
			t.Errorf("%s [%s] 月首/月末的所属月回环失败", c.month, c.desc)
		}
	}
}

// TestMonthJSON 覆盖 dto 的 JSON 契约往返，含零值 ⇔ null。
func TestMonthJSON(t *testing.T) {
	type payload struct {
		Month Month `json:"month"`
	}

	out, err := json.Marshal(payload{Month: MustParseMonth("2026-09")})
	if err != nil {
		t.Fatalf("Marshal 报错: %v", err)
	}
	if string(out) != `{"month":"2026-09"}` {
		t.Errorf("Marshal = %s, 期望 {\"month\":\"2026-09\"}", out)
	}

	out, err = json.Marshal(payload{})
	if err != nil {
		t.Fatalf("Marshal 零值报错: %v", err)
	}
	if string(out) != `{"month":null}` {
		t.Errorf("Marshal 零值 = %s, 期望 {\"month\":null}", out)
	}

	for _, c := range []struct{ in, want string }{
		{`{"month":"2026-09"}`, "2026-09"},
		{`{"month":null}`, ""},
		{`{"month":""}`, ""},
	} {
		var p payload
		if err := json.Unmarshal([]byte(c.in), &p); err != nil {
			t.Fatalf("Unmarshal(%s) 报错: %v", c.in, err)
		}
		if p.Month.String() != c.want {
			t.Errorf("Unmarshal(%s) = %q, 期望 %q", c.in, p.Month.String(), c.want)
		}
	}

	for _, in := range []string{`{"month":"2026-13"}`, `{"month":"2026-9"}`, `{"month":123}`} {
		var p payload
		if err := json.Unmarshal([]byte(in), &p); err == nil {
			t.Errorf("Unmarshal(%s) 应报错", in)
		}
	}
}

// TestMonthSQL 覆盖 store/sqlite 的落库与回读往返。
func TestMonthSQL(t *testing.T) {
	v, err := MustParseMonth("2026-09").Value()
	if err != nil {
		t.Fatalf("Value 报错: %v", err)
	}
	if v != "2026-09" {
		t.Errorf("Value = %v, 期望 \"2026-09\"", v)
	}

	var zero Month
	v, err = zero.Value()
	if err != nil {
		t.Fatalf("零值 Value 报错: %v", err)
	}
	if v != nil {
		t.Errorf("零值 Value = %v, 期望 nil(NULL)", v)
	}

	scanCases := []struct {
		src  any
		want string
		desc string
	}{
		{"2026-09", "2026-09", "string"},
		{[]byte("2026-09"), "2026-09", "[]byte"},
		{nil, "", "NULL"},
		{"", "", "空串"},
		{time.Date(2026, 9, 6, 23, 30, 0, 0, time.UTC), "2026-09", "time.Time"},
	}
	for _, c := range scanCases {
		var m Month
		if err := m.Scan(c.src); err != nil {
			t.Fatalf("Scan(%s) 报错: %v", c.desc, err)
		}
		if m.String() != c.want {
			t.Errorf("Scan(%s) = %q, 期望 %q", c.desc, m.String(), c.want)
		}
	}

	var m Month
	if err := m.Scan(12345); !errors.Is(err, ErrScanType) {
		t.Errorf("Scan(int) 错误 = %v, 期望 ErrScanType", err)
	}
	if err := m.Scan("2026-13"); !errors.Is(err, ErrMonthValue) {
		t.Errorf("Scan 非法月份错误 = %v, 期望 ErrMonthValue", err)
	}
}

// TestMustParseMonthPanics 校验 Must 系列在非法输入下 panic。
func TestMustParseMonthPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustParseMonth 对非法输入应 panic")
		}
	}()
	MustParseMonth("2026-13")
}
