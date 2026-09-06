package calendar

// 本文件承载 §六 L0 `base/calendar` 行四项承载中的第 2、4 项：
// **年月类型**（Month）与**日期 → 所属月**（MonthOf）。区间运算在 monthrange.go。

import (
	"database/sql/driver"
	"time"

	"github.com/pkg/errors"
)

// Month 年月，即「某年某月」这一粒度，不含日。
//
// 存在的理由（§六 L1.1 `entity` 行「日期与年月用 base/calendar，不用裸 string」）：
// `Expense.摊分起始月` 与 `摊分结束月` 是年月而非日期（终版 §四 只读派生 4 项），
// J 域月度统计的归月键也是年月。用 Date 表示年月要额外约定「日恒为 1」，
// 而这个约定没有任何结构能保证，迟早出现「起始月 = 2026-01-15」这种值。
//
// **内部以月序号存储**（`年 × 12 + 月 − 1`），不存 (年, 月) 两个字段。理由是
// 承载的「起始月 + N − 1」与「起始月 ≤ M ≤ 结束月」两项运算在序号上都是**整数加减与比较**，
// 不存在进位与借位分支，也就没有「12 月加 1 月忘了进年」这类错法。
// 与 Date 一样**可比较**（单一整数字段），`==` 即年月相等、可作 map 键——
// J 域按月分组聚合正是拿它当键。
type Month struct {
	// index 月序号 = 年 × 12 + 月 − 1。
	// 零值 0 对应「0 年 1 月」，而 MinYear 为 1，故零值恒非法年月，即「未设置」。
	index int
}

// monthIndexOf 把 (年, 月) 压成月序号。调用方须已校验过年月合法。
func monthIndexOf(year int, month time.Month) int {
	return year*12 + int(month) - 1
}

// splitMonthIndex 把月序号还原为 (年, 月)。
//
// 仅对非负序号成立：Go 的整除向零截断，负序号会得到错误的年月
// （实测：0001-01 减 13 月得序号 −1，还原为「0 年 0 月」）。
// 本包所有产出 Month 的出口都会校验年份 ≥ MinYear（即序号 ≥ 12），
// 故负序号不会出现在合法值上；此处只作还原，越界由各出口的 checkYear 拦。
func splitMonthIndex(index int) (int, time.Month) {
	return index / 12, time.Month(index%12 + 1)
}

// NewMonth 构造年月。
//
// 两类失败：年份越界返回 ErrYearOutOfRange；月份不在 1~12 返回 ErrInvalidMonth。
// 年月不像日期那样存在「写法对但不存在」的情况（任何年份都有 1~12 月），
// 因此没有第三类错误——这也是 ErrInvalidMonth 与 ErrInvalidDate 分设的原因。
func NewMonth(year int, month time.Month) (Month, error) {
	if err := checkYear(year); err != nil {
		return Month{}, err
	}
	if err := checkMonth(month); err != nil {
		return Month{}, err
	}
	return Month{index: monthIndexOf(year, month)}, nil
}

// MonthOf 返回日期所属的年月，即承载第 4 项「**日期 → 所属月**」。
//
// 这是 H-2「以支出日期所属月为第 1 个摊分月」与 J 域未摊分口径「按支出日期归月」
// 的共同基础。全程不经时区（入参已是无时区的 Date），因此**不可能随部署机器的 TZ 漂移**
// ——前提 7 要守住的正是这一点。
//
// 零值 Date 返回 ErrZeroValue：零值不是「1 年 1 月」，是「没设值」，
// 静默归到某个月会让一笔漏填日期的明细悄悄进入某月统计。
func MonthOf(d Date) (Month, error) {
	if d.IsZero() {
		return Month{}, errors.WithMessage(ErrZeroValue, "取日期所属月")
	}
	// d 的年月已在构造时校验过，此处无需重复校验
	return Month{index: monthIndexOf(d.year, d.month)}, nil
}

// ParseMonth 解析 `2006-01` 定长文本。
//
// 严格性与 ParseDate 同构（实测见 answer §三.6）：`2026-1`、`2026/01`、`202601`
// 一律 ErrTextMalformed；`2026-13`、`2026-00` 属月份越界，返回 ErrInvalidMonth。
func ParseMonth(text string) (Month, error) {
	parsed, err := time.Parse(monthLayout, text)
	if err != nil {
		if isOutOfRangeErr(err) {
			// 该布局下唯一可能越界的字段就是月份（实测：`2026-13` → `month out of range`）
			return Month{}, errors.WithMessagef(ErrInvalidMonth, "年月文本=%q", text)
		}
		return Month{}, errors.WithMessagef(ErrTextMalformed,
			"年月文本=%q，要求形态=%s", text, monthLayout)
	}
	// 走 NewMonth 以套用年份校验，理由同 ParseDate
	return NewMonth(parsed.Year(), parsed.Month())
}

// Year 年份。零值 Month 返回 0。
func (m Month) Year() int {
	year, _ := splitMonthIndex(m.index)
	return year
}

// Month 月份。零值 Month 返回 1（因序号 0 对应「0 年 1 月」），
// 但该值无意义——零值须先由 IsZero 判出，不应读取其年月。
func (m Month) Month() time.Month {
	_, month := splitMonthIndex(m.index)
	return month
}

// IsZero 是否为零值，即「未设置」。
func (m Month) IsZero() bool { return m.index == 0 }

// String 返回 `2006-01` 定长文本。字典序恒等于时间序（实测 `"2026-09" < "2026-10"`），
// 故 `store/sqlite` 的摊分起止月列可直接用 TEXT 做区间穿刺比较（8.2）。
// 零值返回 `0000-01`——年份 0 越界，无法被 ParseMonth 读回。
func (m Month) String() string {
	year, month := splitMonthIndex(m.index)
	buf := make([]byte, 0, len(monthLayout))
	buf = appendPadded(buf, year, 4)
	buf = append(buf, '-')
	buf = appendPadded(buf, int(month), 2)
	return string(buf)
}

// AddMonths 返回 n 个月之后的年月（n 可为负），是承载第 3 项「起始月 + N − 1」的算子。
//
// **纯整数序号加减，不经任何日期类型**——这正是本包不用 civil.AddMonths 的核心理由
// （包注释「选型」②）：后者经 time.AddDate 实现、带日溢出，实测
// `2026-01-31.AddMonths(1) = 2026-03-03`，所属月错成 3 月而非 2 月；
// 仅 2026 一年就有 7 个起始日会算错。序号算术里根本没有「日」这一维度，
// 溢出无从发生（§五 准则 6「约束优先用结构表达」）。
//
// 结果年份越界返回 ErrYearOutOfRange：H-1 的摊分月数「不设上限」，
// 因此 `起始月 + N − 1` 越过 MaxYear 是**可达**的（如 2026-01 摊 100000 个月），
// 不是理论边界。越界即报错，不产出「能算出来但存不回来」的年月。
func (m Month) AddMonths(n int) (Month, error) {
	if m.IsZero() {
		return Month{}, errors.WithMessage(ErrZeroValue, "年月加减月数")
	}
	shifted := m.index + n
	// 先按序号还原再校验年份：序号可能为负（减到 0 年之前），
	// splitMonthIndex 对负值不成立，故这里用 checkYear 前置拦住。
	if shifted < monthIndexOf(MinYear, time.January) ||
		shifted > monthIndexOf(MaxYear, time.December) {
		year, month := splitMonthIndex(shifted)
		if shifted < 0 {
			// 负序号还原出的年月无意义，只报序号本身，避免给出误导性的年月
			return Month{}, errors.WithMessagef(ErrYearOutOfRange,
				"年月=%s 加 %d 个月后早于 %04d-01", m.String(), n, MinYear)
		}
		return Month{}, errors.WithMessagef(ErrYearOutOfRange,
			"年月=%s 加 %d 个月后为 %04d-%02d，可表示范围=[%d, %d]",
			m.String(), n, year, month, MinYear, MaxYear)
	}
	return Month{index: shifted}, nil
}

// MonthsSince 返回 m 与 m2 相差的月数（m − m2），可为负。
// 供 J-3 趋势与同环比定位「往前第 N 个月」，以及 8.2 摊分求某月是第几个摊分月。
func (m Month) MonthsSince(m2 Month) int { return m.index - m2.index }

// Compare 比较年月先后：m < m2 返回 -1、相等返回 0、m > m2 返回 1。
func (m Month) Compare(m2 Month) int { return signOf(m.index - m2.index) }

// Before 是否早于 m2。
func (m Month) Before(m2 Month) bool { return m.index < m2.index }

// After 是否晚于 m2。
func (m Month) After(m2 Month) bool { return m.index > m2.index }

// Equal 是否为同一年月。与 `==` 等价（本类型可比较）。
func (m Month) Equal(m2 Month) bool { return m.index == m2.index }

// FirstDate 返回该月 1 号。供 J 域按月取日期区间的下界。
func (m Month) FirstDate() (Date, error) {
	if m.IsZero() {
		return Date{}, errors.WithMessage(ErrZeroValue, "取年月首日")
	}
	year, month := splitMonthIndex(m.index)
	return NewDate(year, month, 1)
}

// LastDate 返回该月末日，天数由标准库负责（闰年 2 月得 29 日，实测见 answer §三.2）。
// 供 J 域按月取日期区间的上界；与 FirstDate 配对即「未摊分口径按支出日期归月」的
// 等价区间条件，供 `store` 下推给 SQL（§六 L3 `store` ④）。
func (m Month) LastDate() (Date, error) {
	if m.IsZero() {
		return Date{}, errors.WithMessage(ErrZeroValue, "取年月末日")
	}
	year, month := splitMonthIndex(m.index)
	// 下月 1 号减一天即本月末日，闰年与月长一律由标准库判定，本包不实现月长表
	last := time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	return NewDate(last.Year(), last.Month(), last.Day())
}

// MarshalText 实现 encoding.TextMarshaler，产出 `2006-01`。零值报错，理由同 Date。
func (m Month) MarshalText() ([]byte, error) {
	if m.IsZero() {
		return nil, errors.WithMessage(ErrZeroValue, "序列化年月")
	}
	return []byte(m.String()), nil
}

// UnmarshalText 实现 encoding.TextUnmarshaler，接受 `2006-01`。
func (m *Month) UnmarshalText(data []byte) error {
	parsed, err := ParseMonth(string(data))
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

// Value 实现 driver.Valuer，以 `2006-01` 文本落库。
func (m Month) Value() (driver.Value, error) {
	if m.IsZero() {
		return nil, errors.WithMessage(ErrZeroValue, "年月落库")
	}
	return m.String(), nil
}

// Scan 实现 sql.Scanner，**同样拒收 time.Time**，理由与建表要求见 Date.Scan。
// 摊分起止月是必填的只读派生字段（终版 §四），故 NULL 亦视为数据异常。
func (m *Month) Scan(src any) error {
	switch v := src.(type) {
	case string:
		parsed, err := ParseMonth(v)
		if err != nil {
			return err
		}
		*m = parsed
		return nil
	case []byte:
		parsed, err := ParseMonth(string(v))
		if err != nil {
			return err
		}
		*m = parsed
		return nil
	case nil:
		return errors.WithMessage(ErrScanUnsupported, "扫描年月，值为 NULL，而摊分起止月为必填")
	case time.Time:
		return errors.WithMessagef(ErrScanUnsupported,
			"扫描年月，拒收 time.Time（前提 7：不做时区换算）；列须为 TEXT 且 DSN 不得自动解析为时间，实际值=%s",
			v.Format(time.RFC3339))
	default:
		return errors.WithMessagef(ErrScanUnsupported, "扫描年月，类型=%T", src)
	}
}
