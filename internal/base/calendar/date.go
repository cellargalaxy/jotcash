package calendar

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"time"

	"cloud.google.com/go/civil"
)

// Date 是不带时区的日历日期（年月日），承载 Expense.支出日期（前提 7 的字面值语义）。
//
// 零值表示「未指定」：用于 F-4 多维筛选中日期区间端点可选的场景，
// 落库时映射为 SQL NULL、序列化为 JSON null。
//
// 内部字段私有，只能经 NewDate / ParseDate / MustParseDate / DateOf 构造，
// 因此一个 Date 要么 IsZero，要么是真实存在的日期，不存在 2026-02-30 这类值。
type Date struct {
	d civil.Date
}

// NewDate 按年月日构造 Date。
// 日期不存在（如 2026-02-30、4 月 31 日）返回 ErrDateValue，
// 年份越界返回 ErrYearRange，月份越界返回 ErrMonthValue。
func NewDate(year, month, day int) (Date, error) {
	if err := checkYear(year); err != nil {
		return Date{}, err
	}
	if month < 1 || month > 12 {
		return Date{}, ErrMonthValue
	}
	d := civil.Date{Year: year, Month: time.Month(month), Day: day}
	// civil.IsValid 会把「日」规范化后与原值比对，因此 2 月 30 日、4 月 31 日
	// 这类形态合法但不存在的日期都会在此被拦下。
	if !d.IsValid() {
		return Date{}, ErrDateValue
	}
	return Date{d: d}, nil
}

// MustParseDate 解析日期文本，失败时 panic。
// 仅供测试与包内常量初始化使用，业务代码一律用 ParseDate。
func MustParseDate(value string) Date {
	date, err := ParseDate(value)
	if err != nil {
		panic(fmt.Sprintf("calendar: 解析日期失败, value=%q: %v", value, err))
	}
	return date
}

// ParseDate 解析定长的 2006-01-02 文本。
//
// 只接受定长形态：2026-1-2、2026-01-02T00:00:00Z 等一律返回 ErrDateFormat。
// 这不是苛刻，而是「字典序 = 时间序」这条性质的守门人——一旦允许变长写法，
// store/sqlite 侧对 TEXT 列的排序与 BETWEEN 就不再可靠。
func ParseDate(value string) (Date, error) {
	if len(value) != dateLen {
		return Date{}, ErrDateFormat
	}
	// 形态逐字符校验：4 位年 - 2 位月 - 2 位日。
	// 不能只靠 time.Parse——它会接受 "2026-1-02" 这类非定长写法。
	if value[4] != '-' || value[7] != '-' {
		return Date{}, ErrDateFormat
	}
	year, err := parseDigits(value[0:4])
	if err != nil {
		return Date{}, ErrDateFormat
	}
	month, err := parseDigits(value[5:7])
	if err != nil {
		return Date{}, ErrDateFormat
	}
	day, err := parseDigits(value[8:10])
	if err != nil {
		return Date{}, ErrDateFormat
	}
	return NewDate(year, month, day)
}

// parseDigits 解析纯数字串，拒绝符号与空白（strconv.Atoi 会接受 "+12"、" 12"）。
func parseDigits(s string) (int, error) {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, ErrDateFormat
		}
	}
	return strconv.Atoi(s)
}

// Year 返回年份；零值返回 0。
func (d Date) Year() int { return d.d.Year }

// Month 返回月份（1~12）；零值返回 0。
func (d Date) Month() int { return int(d.d.Month) }

// Day 返回日（1~31）；零值返回 0。
func (d Date) Day() int { return d.d.Day }

// IsZero 报告是否为零值（未指定）。
func (d Date) IsZero() bool { return d.d.IsZero() }

// String 返回定长 10 字符的 2006-01-02 文本；零值返回空串。
//
// 零值不返回 "0000-00-00"：那是个 ParseDate 解不回来的非法字面值，
// 一旦落库或下发就会变成脏数据。
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return d.d.String()
}

// ToMonth 返回日期所属的年月，即 8.2「起始月 = 支出日期所属月」这条边。
// 零值返回零值 Month。
func (d Date) ToMonth() Month {
	if d.IsZero() {
		return Month{}
	}
	return Month{seq: monthSeq(d.d.Year, int(d.d.Month))}
}

// Compare 比较两个日期：早于返回 -1，晚于返回 +1，相等返回 0。
// 零值视为最小值，因此排序时「未指定」排在最前。
func (d Date) Compare(other Date) int {
	return d.d.Compare(other.d)
}

// Before 报告 d 是否早于 other。
func (d Date) Before(other Date) bool { return d.Compare(other) < 0 }

// After 报告 d 是否晚于 other。
func (d Date) After(other Date) bool { return d.Compare(other) > 0 }

// Equal 报告两个日期是否相同。零值与零值相等。
//
// Date 是可比较的值类型，== 与 Equal 等价；提供 Equal 是为了让比较意图在
// 调用处更显眼，也避免将来内部表示变化时调用方受影响。
func (d Date) Equal(other Date) bool { return d.d == other.d }

// AddDays 返回增减 n 天后的日期，n 可为负。
// 结果年份越界返回 ErrYearRange；零值调用返回 ErrDateValue（无基准可加减）。
func (d Date) AddDays(n int) (Date, error) {
	if d.IsZero() {
		return Date{}, ErrDateValue
	}
	next := d.d.AddDays(n)
	if err := checkYear(next.Year); err != nil {
		return Date{}, err
	}
	return Date{d: next}, nil
}

// DaysSince 返回 d 与 other 相差的天数（d 在后为正），不含结束日。
func (d Date) DaysSince(other Date) int {
	return d.d.DaysSince(other.d)
}

// MarshalJSON 实现 json.Marshaler：非零值序列化为 "2026-01-02"，零值为 null。
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + d.d.String() + `"`), nil
}

// UnmarshalJSON 实现 json.Unmarshaler：接受 "2026-01-02"、null 与空串，
// 后两者一律回到零值（表示「未指定」，用于 F-4 可选的区间端点）。
func (d *Date) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == "null" {
		*d = Date{}
		return nil
	}
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return ErrDateFormat
	}
	s = s[1 : len(s)-1]
	if s == "" {
		*d = Date{}
		return nil
	}
	date, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = date
	return nil
}

// Value 实现 driver.Valuer：非零值落库为定长 TEXT，零值落库为 NULL。
//
// 不落 unix 时间戳：整数时间戳必须先选定时区才能还原「哪一天」，与前提 7 冲突。
// 零值落 NULL 而非 "0000-00-00"：必填列（如 Expense.支出日期）此时会被
// NOT NULL 约束当场拦下，而不是把一个非法字面值静默写进库。
func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.d.String(), nil
}

// Scan 实现 sql.Scanner：接受 NULL、string、[]byte 与 time.Time。
//
// 接受 time.Time 是为了兼容某些驱动开启日期解析后的返回形态，
// 此时按该时间自身时区取年月日，不做换算（与 DateOf 同口径）。
func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = Date{}
		return nil
	case string:
		if v == "" {
			*d = Date{}
			return nil
		}
		date, err := ParseDate(v)
		if err != nil {
			return err
		}
		*d = date
		return nil
	case []byte:
		if len(v) == 0 {
			*d = Date{}
			return nil
		}
		date, err := ParseDate(string(v))
		if err != nil {
			return err
		}
		*d = date
		return nil
	case time.Time:
		date, err := DateOf(v)
		if err != nil {
			return err
		}
		*d = date
		return nil
	default:
		return fmt.Errorf("%w: %T", ErrScanType, src)
	}
}
