package calendar

import (
	"database/sql/driver"
	"fmt"
	"time"

	"cloud.google.com/go/civil"
)

// Month 是年月，承载 Expense.摊分起始月 与 摊分结束月（终版 §四 只读派生 4 项之二）。
//
// 零值表示「未指定」，落库为 SQL NULL、序列化为 JSON null。
//
// 内部用「月序号」表示：seq = 年*12 + (月-1)，从 1 开始（seq = 0 即零值）。
// 这样加减月是整数加减、比较是整数比较，结构上不可能出现「13 月」或「0 月」，
// 也不需要在每次运算后重新规范化——这是不变式 1b（结束月 = 起始月 + 月数 − 1）
// 能一行写完且不出错的原因。
type Month struct {
	seq int
}

// monthSeq 把年、月折算为月序号。调用方须保证 year、month 已校验。
func monthSeq(year, month int) int {
	return year*12 + (month - 1)
}

// NewMonth 按年月构造 Month。
// 月份不在 1~12 返回 ErrMonthValue，年份越界返回 ErrYearRange。
func NewMonth(year, month int) (Month, error) {
	if err := checkYear(year); err != nil {
		return Month{}, err
	}
	if month < 1 || month > 12 {
		return Month{}, ErrMonthValue
	}
	return Month{seq: monthSeq(year, month)}, nil
}

// MustParseMonth 解析年月文本，失败时 panic。
// 仅供测试与包内常量初始化使用，业务代码一律用 ParseMonth。
func MustParseMonth(value string) Month {
	month, err := ParseMonth(value)
	if err != nil {
		panic(fmt.Sprintf("calendar: 解析年月失败, value=%q: %v", value, err))
	}
	return month
}

// ParseMonth 解析定长的 2006-01 文本。
// 只接受定长形态：2026-1 一律返回 ErrMonthFormat，理由同 ParseDate。
func ParseMonth(value string) (Month, error) {
	if len(value) != monthLen {
		return Month{}, ErrMonthFormat
	}
	if value[4] != '-' {
		return Month{}, ErrMonthFormat
	}
	year, err := parseDigits(value[0:4])
	if err != nil {
		return Month{}, ErrMonthFormat
	}
	month, err := parseDigits(value[5:7])
	if err != nil {
		return Month{}, ErrMonthFormat
	}
	return NewMonth(year, month)
}

// Year 返回年份；零值返回 0。
func (m Month) Year() int {
	if m.IsZero() {
		return 0
	}
	return m.seq / 12
}

// Month 返回月份（1~12）；零值返回 0。
func (m Month) Month() int {
	if m.IsZero() {
		return 0
	}
	return m.seq%12 + 1
}

// IsZero 报告是否为零值（未指定）。
func (m Month) IsZero() bool { return m.seq == 0 }

// String 返回定长 7 字符的 2006-01 文本；零值返回空串。
func (m Month) String() string {
	if m.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d", m.Year(), m.Month())
}

// AddMonths 返回增减 n 个月后的年月，n 可为负。
// 结果年份越界返回 ErrYearRange；零值调用返回 ErrMonthValue（无基准可加减）。
//
// 这是 J-3 环比（AddMonths(-1)）与同比（AddMonths(-12)）的运算入口，
// 也是不变式 1b 派生结束月的底层运算。
func (m Month) AddMonths(n int) (Month, error) {
	if m.IsZero() {
		return Month{}, ErrMonthValue
	}
	seq := m.seq + n
	// 先算年份再校验，避免越界值参与后续运算。
	// seq 为负或过小时 seq/12 同样会落在越界区间，由 checkYear 统一拦下。
	if err := checkYear(seq / 12); err != nil {
		return Month{}, err
	}
	return Month{seq: seq}, nil
}

// MonthsSince 返回 m 与 other 相差的月数（m 在后为正）。
// 供 J-3 多月趋势按月步进使用。
func (m Month) MonthsSince(other Month) int {
	return m.seq - other.seq
}

// Compare 比较两个年月：早于返回 -1，晚于返回 +1，相等返回 0。
// 零值视为最小值。J-6「已发生（≤ 当前月）/ 未来待摊」的分段即用此判据。
func (m Month) Compare(other Month) int {
	switch {
	case m.seq < other.seq:
		return -1
	case m.seq > other.seq:
		return +1
	default:
		return 0
	}
}

// Before 报告 m 是否早于 other。
func (m Month) Before(other Month) bool { return m.seq < other.seq }

// After 报告 m 是否晚于 other。
func (m Month) After(other Month) bool { return m.seq > other.seq }

// Equal 报告两个年月是否相同。
func (m Month) Equal(other Month) bool { return m.seq == other.seq }

// FirstDate 返回该月第一天；零值返回零值 Date。
//
// 与 LastDate 配对，供 store 把「某月」翻译成
// 「支出日期 BETWEEN 月首 AND 月末」——J-1 未摊分口径按支出日期归月时，
// 用区间比较而不是在 SQL 里对日期列做字符串截取或函数运算。
func (m Month) FirstDate() Date {
	if m.IsZero() {
		return Date{}
	}
	return Date{d: civil.Date{Year: m.Year(), Month: time.Month(m.Month()), Day: 1}}
}

// LastDate 返回该月最后一天；零值返回零值 Date。
// 月末日数（28/29/30/31，含闰年与世纪年规则）由标准库日历算术给出，本包不自算。
func (m Month) LastDate() Date {
	if m.IsZero() {
		return Date{}
	}
	// 取下个月 1 日再回退 1 天，闰年与各月天数差异由标准库处理。
	// 对 MaxYear 的 12 月，这里会临时构造出 10000-01-01，
	// 但 civil.Date 本身不限年份上界，回退后仍落在 9999-12-31，不越界。
	first := civil.Date{Year: m.Year(), Month: time.Month(m.Month()), Day: 1}
	return Date{d: first.AddMonths(1).AddDays(-1)}
}

// MarshalJSON 实现 json.Marshaler：非零值序列化为 "2026-01"，零值为 null。
func (m Month) MarshalJSON() ([]byte, error) {
	if m.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + m.String() + `"`), nil
}

// UnmarshalJSON 实现 json.Unmarshaler：接受 "2026-01"、null 与空串，
// 后两者一律回到零值。
func (m *Month) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == "null" {
		*m = Month{}
		return nil
	}
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return ErrMonthFormat
	}
	s = s[1 : len(s)-1]
	if s == "" {
		*m = Month{}
		return nil
	}
	month, err := ParseMonth(s)
	if err != nil {
		return err
	}
	*m = month
	return nil
}

// Value 实现 driver.Valuer：非零值落库为定长 TEXT，零值落库为 NULL。
//
// 定长文本使 8.2 的区间穿刺（起始月 ≤ M AND 结束月 ≥ M）可用朴素字符串
// 比较完成，无需 SQL 侧函数，也保证两个条件各自可走索引。
func (m Month) Value() (driver.Value, error) {
	if m.IsZero() {
		return nil, nil
	}
	return m.String(), nil
}

// Scan 实现 sql.Scanner：接受 NULL、string、[]byte 与 time.Time。
func (m *Month) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*m = Month{}
		return nil
	case string:
		if v == "" {
			*m = Month{}
			return nil
		}
		month, err := ParseMonth(v)
		if err != nil {
			return err
		}
		*m = month
		return nil
	case []byte:
		if len(v) == 0 {
			*m = Month{}
			return nil
		}
		month, err := ParseMonth(string(v))
		if err != nil {
			return err
		}
		*m = month
		return nil
	case time.Time:
		date, err := DateOf(v)
		if err != nil {
			return err
		}
		*m = date.ToMonth()
		return nil
	default:
		return fmt.Errorf("%w: %T", ErrScanType, src)
	}
}
