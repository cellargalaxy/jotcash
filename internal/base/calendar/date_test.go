package calendar

import (
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"
)

// TestParseDate 覆盖定长形态校验与「非零值必合法」不变式（方案 V4/V5）。
func TestParseDate(t *testing.T) {
	okCases := []struct {
		value            string
		year, month, day int
	}{
		{"2026-01-01", 2026, 1, 1},
		{"2026-12-31", 2026, 12, 31},
		{"2024-02-29", 2024, 2, 29},  // 闰年
		{"2000-02-29", 2000, 2, 29},  // 世纪闰年
		{"0001-01-01", 1, 1, 1},      // 下界
		{"9999-12-31", 9999, 12, 31}, // 上界
	}
	for _, c := range okCases {
		got, err := ParseDate(c.value)
		if err != nil {
			t.Fatalf("ParseDate(%q) 意外报错: %v", c.value, err)
		}
		if got.Year() != c.year || got.Month() != c.month || got.Day() != c.day {
			t.Errorf("ParseDate(%q) = %d-%d-%d, 期望 %d-%d-%d",
				c.value, got.Year(), got.Month(), got.Day(), c.year, c.month, c.day)
		}
		// 往返：解析后再格式化必须逐字节还原（方案 V6）
		if got.String() != c.value {
			t.Errorf("ParseDate(%q).String() = %q, 期望原样还原", c.value, got.String())
		}
	}

	badCases := []struct {
		value string
		want  error
		desc  string
	}{
		{"2026-1-2", ErrDateFormat, "非定长"},
		{"2026-01-02T00:00:00Z", ErrDateFormat, "带时间部分"},
		{"2026/01/02", ErrDateFormat, "分隔符错误"},
		{"", ErrDateFormat, "空串"},
		{"2026-01-0a", ErrDateFormat, "非数字"},
		{"2026-01-+2", ErrDateFormat, "含符号"},
		{"2026-01- 2", ErrDateFormat, "含空白"},
		{"2026-02-30", ErrDateValue, "2 月 30 日不存在"},
		{"2026-04-31", ErrDateValue, "4 月 31 日不存在"},
		{"2025-02-29", ErrDateValue, "平年无 2 月 29 日"},
		{"1900-02-29", ErrDateValue, "世纪年 1900 非闰年"},
		{"2026-01-00", ErrDateValue, "0 日不存在"},
		{"2026-01-32", ErrDateValue, "32 日不存在"},
		{"2026-13-01", ErrMonthValue, "13 月"},
		{"2026-00-01", ErrMonthValue, "0 月"},
		{"0000-01-01", ErrYearRange, "年份下界外"},
	}
	for _, c := range badCases {
		got, err := ParseDate(c.value)
		if !errors.Is(err, c.want) {
			t.Errorf("ParseDate(%q) [%s] 错误 = %v, 期望 %v", c.value, c.desc, err, c.want)
		}
		// 出错时不得产出可用值
		if !got.IsZero() {
			t.Errorf("ParseDate(%q) [%s] 出错却返回了非零值 %v", c.value, c.desc, got)
		}
	}
}

// TestNewDate 校验工厂函数是「非零值必合法」的唯一守门人。
func TestNewDate(t *testing.T) {
	if _, err := NewDate(2026, 9, 6); err != nil {
		t.Fatalf("NewDate 合法输入报错: %v", err)
	}
	badCases := []struct {
		year, month, day int
		want             error
	}{
		{2026, 2, 30, ErrDateValue},
		{2026, 13, 1, ErrMonthValue},
		{2026, 0, 1, ErrMonthValue},
		{0, 1, 1, ErrYearRange},
		{10000, 1, 1, ErrYearRange},
		{-1, 1, 1, ErrYearRange},
	}
	for _, c := range badCases {
		got, err := NewDate(c.year, c.month, c.day)
		if !errors.Is(err, c.want) {
			t.Errorf("NewDate(%d,%d,%d) 错误 = %v, 期望 %v", c.year, c.month, c.day, err, c.want)
		}
		if !got.IsZero() {
			t.Errorf("NewDate(%d,%d,%d) 出错却返回非零值", c.year, c.month, c.day)
		}
	}
}

// TestDateZero 校验零值语义：未指定（F-4 可选区间端点）。
func TestDateZero(t *testing.T) {
	var zero Date
	if !zero.IsZero() {
		t.Error("零值 Date 的 IsZero 应为 true")
	}
	// 零值不得输出 "0000-00-00"——那是个 ParseDate 解不回来的非法字面值
	if zero.String() != "" {
		t.Errorf("零值 String() = %q, 期望空串", zero.String())
	}
	if !zero.ToMonth().IsZero() {
		t.Error("零值 Date 的 ToMonth 应为零值 Month")
	}
	if zero.Year() != 0 || zero.Month() != 0 || zero.Day() != 0 {
		t.Error("零值 Date 的年月日应均为 0")
	}
	// 零值无基准，加减天数应报错而不是从 0 年开始算
	if _, err := zero.AddDays(1); !errors.Is(err, ErrDateValue) {
		t.Errorf("零值 AddDays 错误 = %v, 期望 ErrDateValue", err)
	}
}

// TestDateToMonth 覆盖 8.2「起始月 = 支出日期所属月」这条边（方案 §三 #2）。
func TestDateToMonth(t *testing.T) {
	cases := []struct{ date, month string }{
		{"2026-01-01", "2026-01"}, // 月首
		{"2026-01-31", "2026-01"}, // 月末
		{"2026-12-31", "2026-12"}, // 年末：不得溢出到下一年
		{"2024-02-29", "2024-02"}, // 闰日
	}
	for _, c := range cases {
		got := MustParseDate(c.date).ToMonth()
		if got.String() != c.month {
			t.Errorf("%s 所属月 = %q, 期望 %q", c.date, got.String(), c.month)
		}
	}
}

// TestDateCompare 覆盖 F-4 日期区间筛选与 F-3 排序所需的比较语义。
func TestDateCompare(t *testing.T) {
	a := MustParseDate("2026-01-01")
	b := MustParseDate("2026-01-02")
	c := MustParseDate("2026-02-01")
	d := MustParseDate("2027-01-01")

	if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Error("Compare 的三分支不正确")
	}
	// 逐级进位都要能区分：日 → 月 → 年
	if !a.Before(b) || !b.Before(c) || !c.Before(d) {
		t.Error("Before 未能逐级区分日/月/年")
	}
	if !d.After(c) || !c.After(b) {
		t.Error("After 不正确")
	}
	if !a.Equal(MustParseDate("2026-01-01")) {
		t.Error("相同日期 Equal 应为 true")
	}
	if a.Equal(b) {
		t.Error("不同日期 Equal 应为 false")
	}
	// 零值视为最小值：排序时「未指定」排最前
	var zero Date
	if !zero.Before(a) {
		t.Error("零值应视为最小值")
	}
}

// TestDateLexicographicOrder 校验「定长 ⇒ 字典序 = 时间序」（方案 V5）。
// store/sqlite 的 ORDER BY（F-3）、BETWEEN（F-4）直接依赖这条性质。
func TestDateLexicographicOrder(t *testing.T) {
	raw := []string{
		"2026-10-01", "2026-02-01", "2026-01-15", "2025-12-31",
		"2026-01-02", "2026-09-30", "2027-01-01", "0001-01-01", "9999-12-31",
	}
	dates := make([]Date, 0, len(raw))
	for _, s := range raw {
		d := MustParseDate(s)
		if len(d.String()) != dateLen {
			t.Fatalf("%s 文本长度 = %d, 期望恒为 %d", s, len(d.String()), dateLen)
		}
		dates = append(dates, d)
	}

	byString := append([]Date(nil), dates...)
	sort.Slice(byString, func(i, j int) bool { return byString[i].String() < byString[j].String() })

	byCompare := append([]Date(nil), dates...)
	sort.Slice(byCompare, func(i, j int) bool { return byCompare[i].Compare(byCompare[j]) < 0 })

	for i := range byString {
		if byString[i] != byCompare[i] {
			t.Fatalf("第 %d 位：字符串序 %s != 时间序 %s（字典序性质被破坏）",
				i, byString[i], byCompare[i])
		}
	}
}

// TestDateAddDays 覆盖跨月、跨年、闰年与越界拦截。
func TestDateAddDays(t *testing.T) {
	cases := []struct {
		from string
		n    int
		want string
	}{
		{"2026-01-31", 1, "2026-02-01"},  // 跨月
		{"2026-12-31", 1, "2027-01-01"},  // 跨年
		{"2024-02-28", 1, "2024-02-29"},  // 闰年 2 月
		{"2025-02-28", 1, "2025-03-01"},  // 平年 2 月
		{"2026-03-01", -1, "2026-02-28"}, // 负向跨月
		{"2026-01-01", -1, "2025-12-31"}, // 负向跨年
		{"2026-09-06", 0, "2026-09-06"},  // 零位移
	}
	for _, c := range cases {
		got, err := MustParseDate(c.from).AddDays(c.n)
		if err != nil {
			t.Fatalf("%s AddDays(%d) 报错: %v", c.from, c.n, err)
		}
		if got.String() != c.want {
			t.Errorf("%s AddDays(%d) = %s, 期望 %s", c.from, c.n, got, c.want)
		}
	}

	// 越界必须返回错误，不得静默产出破坏定长的年份（方案 §五 5.2）
	if _, err := MustParseDate("9999-12-31").AddDays(1); !errors.Is(err, ErrYearRange) {
		t.Error("上界溢出应返回 ErrYearRange")
	}
	if _, err := MustParseDate("0001-01-01").AddDays(-1); !errors.Is(err, ErrYearRange) {
		t.Error("下界溢出应返回 ErrYearRange")
	}
}

// TestDateDaysSince 校验天数差，兼作 AddDays 的逆运算验证。
func TestDateDaysSince(t *testing.T) {
	cases := []struct {
		later, earlier string
		want           int
	}{
		{"2026-01-02", "2026-01-01", 1},
		{"2026-01-01", "2026-01-02", -1},
		{"2026-01-01", "2026-01-01", 0},
		{"2024-03-01", "2024-02-01", 29}, // 闰年 2 月
		{"2025-03-01", "2025-02-01", 28}, // 平年 2 月
		{"2027-01-01", "2026-01-01", 365},
	}
	for _, c := range cases {
		got := MustParseDate(c.later).DaysSince(MustParseDate(c.earlier))
		if got != c.want {
			t.Errorf("%s DaysSince %s = %d, 期望 %d", c.later, c.earlier, got, c.want)
		}
	}
}

// TestDateJSON 覆盖 dto 的 JSON 契约往返，含零值 ⇔ null（方案 V6）。
func TestDateJSON(t *testing.T) {
	type payload struct {
		Date Date `json:"date"`
	}

	// 非零值
	out, err := json.Marshal(payload{Date: MustParseDate("2026-09-06")})
	if err != nil {
		t.Fatalf("Marshal 报错: %v", err)
	}
	if string(out) != `{"date":"2026-09-06"}` {
		t.Errorf("Marshal = %s, 期望 {\"date\":\"2026-09-06\"}", out)
	}

	// 零值序列化为 null
	out, err = json.Marshal(payload{})
	if err != nil {
		t.Fatalf("Marshal 零值报错: %v", err)
	}
	if string(out) != `{"date":null}` {
		t.Errorf("Marshal 零值 = %s, 期望 {\"date\":null}", out)
	}

	// 反序列化：正常值、null、空串
	for _, c := range []struct{ in, want string }{
		{`{"date":"2026-09-06"}`, "2026-09-06"},
		{`{"date":null}`, ""},
		{`{"date":""}`, ""},
	} {
		var p payload
		if err := json.Unmarshal([]byte(c.in), &p); err != nil {
			t.Fatalf("Unmarshal(%s) 报错: %v", c.in, err)
		}
		if p.Date.String() != c.want {
			t.Errorf("Unmarshal(%s) = %q, 期望 %q", c.in, p.Date.String(), c.want)
		}
	}

	// 非法输入必须报错，不得静默落成零值
	for _, in := range []string{`{"date":"2026-2-30"}`, `{"date":"2026-02-30"}`, `{"date":123}`} {
		var p payload
		if err := json.Unmarshal([]byte(in), &p); err == nil {
			t.Errorf("Unmarshal(%s) 应报错", in)
		}
	}
}

// TestDateSQL 覆盖 store/sqlite 的落库与回读往返（方案 V6）。
func TestDateSQL(t *testing.T) {
	// 非零值落定长 TEXT
	v, err := MustParseDate("2026-09-06").Value()
	if err != nil {
		t.Fatalf("Value 报错: %v", err)
	}
	if v != "2026-09-06" {
		t.Errorf("Value = %v, 期望 \"2026-09-06\"", v)
	}

	// 零值落 NULL：必填列此时会被 NOT NULL 当场拦下，而非写入非法字面值
	var zero Date
	v, err = zero.Value()
	if err != nil {
		t.Fatalf("零值 Value 报错: %v", err)
	}
	if v != nil {
		t.Errorf("零值 Value = %v, 期望 nil(NULL)", v)
	}

	// Scan 各种来源形态
	scanCases := []struct {
		src  any
		want string
		desc string
	}{
		{"2026-09-06", "2026-09-06", "string"},
		{[]byte("2026-09-06"), "2026-09-06", "[]byte"},
		{nil, "", "NULL"},
		{"", "", "空串"},
		{[]byte(nil), "", "空字节"},
		{time.Date(2026, 9, 6, 23, 30, 0, 0, time.UTC), "2026-09-06", "time.Time"},
	}
	for _, c := range scanCases {
		var d Date
		if err := d.Scan(c.src); err != nil {
			t.Fatalf("Scan(%s) 报错: %v", c.desc, err)
		}
		if d.String() != c.want {
			t.Errorf("Scan(%s) = %q, 期望 %q", c.desc, d.String(), c.want)
		}
	}

	// 不支持的类型与非法文本
	var d Date
	if err := d.Scan(12345); !errors.Is(err, ErrScanType) {
		t.Errorf("Scan(int) 错误 = %v, 期望 ErrScanType", err)
	}
	if err := d.Scan("2026-02-30"); !errors.Is(err, ErrDateValue) {
		t.Errorf("Scan 非法日期错误 = %v, 期望 ErrDateValue", err)
	}
}

// TestDateOfKeepsOwnZone 校验 DateOf 取的是 time.Time 自身时区的年月日、不做换算。
// 这是本包与带时区时间类型唯一的接触点，语义错了前提 7 就守不住。
func TestDateOfKeepsOwnZone(t *testing.T) {
	// 同一绝对时刻，在不同时区下本就属于不同的「那一天」，
	// DateOf 必须如实反映各自时区的渲染结果，而不是统一折算到某个时区。
	instant := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cases := []struct {
		loc  *time.Location
		want string
		desc string
	}{
		{time.UTC, "2026-09-06", "UTC"},
		{time.FixedZone("CST", 8*3600), "2026-09-07", "UTC+8 已跨到次日"},
		{time.FixedZone("KIR", 14*3600), "2026-09-07", "UTC+14"},
		{time.FixedZone("MIT", -11*3600), "2026-09-06", "UTC-11"},
	}
	for _, c := range cases {
		got, err := DateOf(instant.In(c.loc))
		if err != nil {
			t.Errorf("DateOf(%s) 返回错误 = %v, 期望成功", c.desc, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("DateOf(%s) = %s, 期望 %s", c.desc, got, c.want)
		}
	}
}

// TestDateOfRejectsYearRange 校验 DateOf 与 NewDate / ParseDate 同口径拦下越界年份。
//
// DateOf 收的是 time.Time，其年份取值域远宽于本包的 [MinYear, MaxYear]。放行会
// 同时破坏两条全局性质：10000 年产出 11 字符文本（定长失效，令 store/sqlite 依赖
// 的「字典序 = 时间序」出错——"10000-01-01" 按字符串比较反而小于 "2026-01-01"），
// 且 Value 写得进库、Scan 读不回来；0 年则退化成 IsZero 的「未指定」，把真实日期
// 静默当成无值。故它必须是「非零值必合法」的守门人，而非唯一的缺口。
func TestDateOfRejectsYearRange(t *testing.T) {
	cases := []struct {
		in   time.Time
		desc string
	}{
		{time.Date(10000, 3, 5, 0, 0, 0, 0, time.UTC), "上界外：10000 年，文本变 11 字符"},
		{time.Date(0, 3, 5, 0, 0, 0, 0, time.UTC), "下界外：0 年，会退化为 IsZero"},
		{time.Date(-1, 3, 5, 0, 0, 0, 0, time.UTC), "下界外：公元前"},
	}
	for _, c := range cases {
		got, err := DateOf(c.in)
		if !errors.Is(err, ErrYearRange) {
			t.Errorf("DateOf(%s) 错误 = %v, 期望 ErrYearRange", c.desc, err)
		}
		if !got.IsZero() {
			t.Errorf("DateOf(%s) 越界时返回值 = %q, 期望零值", c.desc, got)
		}
	}

	// 边界内两端必须放行，避免校验写成误拒。
	for _, c := range []struct {
		in   time.Time
		want string
	}{
		{time.Date(MinYear, 1, 1, 0, 0, 0, 0, time.UTC), "0001-01-01"},
		{time.Date(MaxYear, 12, 31, 0, 0, 0, 0, time.UTC), "9999-12-31"},
	} {
		got, err := DateOf(c.in)
		if err != nil || got.String() != c.want {
			t.Errorf("DateOf(%s) = (%q, %v), 期望 (%q, nil)", c.want, got, err, c.want)
		}
	}
}

// TestScanTimeRejectsYearRange 校验 Date.Scan / Month.Scan 接到越界 time.Time 时
// 返回错误而非静默写入非法值——这是 DateOf 校验对驱动侧入口的延伸。
func TestScanTimeRejectsYearRange(t *testing.T) {
	out := time.Date(10000, 3, 5, 0, 0, 0, 0, time.UTC)

	var d Date
	if err := d.Scan(out); !errors.Is(err, ErrYearRange) {
		t.Errorf("Date.Scan(越界 time.Time) 错误 = %v, 期望 ErrYearRange", err)
	}
	if !d.IsZero() {
		t.Errorf("Date.Scan 越界后目标值 = %q, 期望保持零值", d)
	}

	var m Month
	if err := m.Scan(out); !errors.Is(err, ErrYearRange) {
		t.Errorf("Month.Scan(越界 time.Time) 错误 = %v, 期望 ErrYearRange", err)
	}
	if !m.IsZero() {
		t.Errorf("Month.Scan 越界后目标值 = %q, 期望保持零值", m)
	}
}

// TestMustParseDatePanics 校验 Must 系列在非法输入下 panic（仅测试与包内初始化可用）。
func TestMustParseDatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustParseDate 对非法输入应 panic")
		}
	}()
	MustParseDate("2026-02-30")
}
