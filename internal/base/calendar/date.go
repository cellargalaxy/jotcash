package calendar

// 本文件承载 §六 L0 `base/calendar` 行四项承载中的第 1 项：**不带时区的日历日期类型**
// （前提 7）。与 month.go 分开：那边是「年月」这一粒度更粗的概念与月份算术，
// 这边是「账单上印着的那个日期」本身，两者的改动理由不同。

import (
	"database/sql/driver"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// Date 不带时区的日历日期，即「账单上印着的那个日期」这一**字面值**。
//
// 前提 7（终版 §一）：「支出日期以账单字面值为准，**不做时区换算**」。
// 同一瞬间在不同时区落在不同日期上，而账单日期不存在这种二义性——一笔 1 月 31 日的
// 消费在任何时区都归入 1 月，归月结果不能随部署机器的 TZ 变化。
// 实测（answer §三.5）：同一瞬间 `2026-01-31T20:00:00Z` 在 UTC 下是 1 月 31 日、
// 在 Asia/Shanghai 与 Pacific/Kiritimati 下是 2 月 1 日——**跨月**，即跨归月口径。
//
// 三条结构性设计：
//
//	① **可比较**（与 `base/decimal.Decimal` 刻意相反）：三个字段全是值类型，
//	   `==` 即日期相等，也可直接作 map 键。这是 8.3 判重的刚需——四要素
//	   「归属用户 + 支出日期 + 支出金额 + 支出币种」等值匹配要按「(日期, 金额, 币种)」
//	   元组批量取候选（§六 L3 `store` ④「查重候选批量匹配」），元组要能进 map。
//	   金额之所以相反，是因为它内含 *big.Int，`==` 比的是指针；日期没有这个问题。
//	② **字段不导出**：越界值（2026-02-30）无法被构造出来，也无法事后改坏——
//	   一个 Date 值只要存在，就必然是真实存在的公历日期，或是零值。
//	   civil.Date 的字段是导出的裸字段，可绕过 IsValid 直接赋值，这是本包不用它的
//	   连带理由之一（主因见包注释「选型」）。
//	③ **零值即「未设置」**：零值的 Year 为 0，而 MinYear 为 1，故零值恒非法日期，
//	   与 `Expense.删除时间`「零值 = 未删除」这类口径一致（终版 §四 表头）。
//	   `IsZero` 是唯一能判出它的入口。
type Date struct {
	year  int
	month time.Month
	day   int
}

// NewDate 构造日历日期，是本包**唯一的数值入口**。
//
// 三类失败：年份越界返回 ErrYearOutOfRange；月份不在 1~12 返回 ErrInvalidMonth；
// 年月日不构成真实存在的公历日期（2 月 30 日、闰年落空）返回 ErrInvalidDate。
// 失败时一律返回零值，调用方即便忽略 error 也拿不到「半个合法」的日期。
//
// 月份与日期分开报错是刻意的：`parser` 的 D-3 要区分「字段缺失」与「格式不符」，
// 而账单里「13 月」与「2 月 30 日」是两类不同的数据问题，报同一个错就无从下手。
func NewDate(year int, month time.Month, day int) (Date, error) {
	if err := checkYear(year); err != nil {
		return Date{}, err
	}
	if err := checkMonth(month); err != nil {
		return Date{}, err
	}
	// 历法校验一律委托 time.Date 的规范化语义，本包不实现闰年与月长规则（见包注释「选型」）：
	// 越界字段会被规范化（2026-02-30 → 2026-03-02），规范化后三字段若变了即非法。
	// 该判据已与 civil.IsValid 逐条比对一致，并经 1~9999 年闰年密度回归（answer §三.2）。
	normalized := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if ny, nm, nd := normalized.Date(); ny != year || nm != month || nd != day {
		return Date{}, errors.WithMessagef(ErrInvalidDate,
			"日期=%04d-%02d-%02d，规范化后=%04d-%02d-%02d", year, month, day, ny, nm, nd)
	}
	return Date{year: year, month: month, day: day}, nil
}

// ParseDate 解析 `2006-01-02` 定长文本，供 `parser` 读账单字段、`store/sqlite` 读 TEXT 列、
// `dto` 解 JSON 三处共用同一形态。
//
// **刻意严格**（实测见 answer §三.3）：`2026-1-5`（非定长）、`2026/01/05`（分隔符不符）、
// `20260105`、`2026-01-05T00:00:00Z`（带时刻）、前后带空格者一律返回 ErrTextMalformed；
// 写法对但日期不存在（`2026-02-30`）返回 ErrInvalidDate。两者分开的理由见 ErrTextMalformed。
//
// 不 trim 空格：账单字段的清洗归 `parser`（§六 L3 `parser` 行的「原始交易行」映射），
// 本包若顺手 trim，「这个字段到底是什么」的判断就分散到了两处——与 `base/decimal`
// 的 NewFromString 同一取向。
func ParseDate(text string) (Date, error) {
	// time.Parse 对定长布局是严格的：位数不符、分隔符不符、有多余文本都会报错，
	// 因此「形态不符」与「日期不存在」这两类都会先落到这里。
	parsed, err := time.Parse(dateLayout, text)
	if err != nil {
		// 区分两类错误：日期字段本身越界（day/month out of range）属「写法对但日期不存在」，
		// 其余属「写法不对」。标准库对前者的报错文本恒含 "out of range"（实测：
		// `2026-02-30` → `day out of range`、`2026-13-01` → `month out of range`）。
		if isOutOfRangeErr(err) {
			return Date{}, errors.WithMessagef(ErrInvalidDate, "日期文本=%q", text)
		}
		return Date{}, errors.WithMessagef(ErrTextMalformed,
			"日期文本=%q，要求形态=%s", text, dateLayout)
	}
	// 走 NewDate 而非直接落字段：年份校验（无损往返的硬边界）必须在此生效，
	// 否则 `0000-01-01` 这类能解析但年份越界的值会绕过校验进来。
	return NewDate(parsed.Year(), parsed.Month(), parsed.Day())
}

// isOutOfRangeErr 判断标准库时间解析错误是否为「字段值越界」（而非「文本形态不符」）。
//
// 判据：`*time.ParseError.Message` 以 `out of range` 结尾。三点理由（均经实测，
// 24 组用例见 answer §三.3）：
//
//	① 用 Message 字段而非 err.Error()：后者含**输入文本本身**，若账单里出现
//	   `out of range` 这样的字面内容就会被误判成越界。实测 `out of range`、
//	   `2026-01-05 out of range` 两个输入在本判据下都正确落到「形态不符」。
//	② 用 HasSuffix 而非 Contains：Message 还会取 `: extra text: "…"` 这一形态
//	   （`2026-01-05 ` 尾随空格、`2026-01-05T00:00:00Z` 带时刻都属此类），
//	   而 extra text 是**形态不符**、不是值越界。它把用户文本嵌在引号里，
//	   Contains 会连带把 `2026-01-05out of range` 误判成越界。
//	③ 不匹配具体字段名（day / month）：两者都要归到「值越界」，
//	   且将来标准库若新增字段名，后缀判据不必跟着改。
//
// 判据失配的后果是**错报一档**（D-3 的「格式不符」与「内容有误」互换），不会放行非法值
// ——两条路径都返回错误。单测对上述 24 组逐条断言，标准库行为漂移会当场暴露。
func isOutOfRangeErr(err error) bool {
	var parseErr *time.ParseError
	if !errors.As(err, &parseErr) {
		return false
	}
	return strings.HasSuffix(parseErr.Message, outOfRangeSuffix)
}

// Today 返回指定时区的当天日期，是本包**唯一**从「当前时间」取日期的入口。
//
// loc 强制显式传入、且**不接受 nil**：从带时区类型取日期必然要选一个时区，选谁都是
// 业务决策（前提 7 拦截①）。若允许 nil 回落到 time.Local，归月结果就会随部署机器的
// TZ 变化——而本包存在的理由正是消除这种漂移。本包也因此不读 time.Local。
//
// 注：本包**不提供** `time.Time → Date` 的通用转换，理由同上；Today 是唯一例外，
// 因为「今天是几号」这个问题本身就必须先选定时区才有答案。
func Today(loc *time.Location) (Date, error) {
	if loc == nil {
		return Date{}, errors.WithMessage(ErrNilLocation, "取当天日期")
	}
	now := time.Now().In(loc)
	return NewDate(now.Year(), now.Month(), now.Day())
}

// Year 年份。零值 Date 返回 0。
func (d Date) Year() int { return d.year }

// Month 月份。零值 Date 返回 0（非合法 time.Month）。
func (d Date) Month() time.Month { return d.month }

// Day 日。零值 Date 返回 0。
func (d Date) Day() int { return d.day }

// IsZero 是否为零值，即「未设置」。
// 零值的年份为 0 而 MinYear 为 1，故零值恒不是一个合法日期。
func (d Date) IsZero() bool {
	return d.year == 0 && d.month == 0 && d.day == 0
}

// String 返回 `2006-01-02` 定长文本。
//
// 定长是刻意的：**字典序恒等于时间序**（实测 `"2026-01-05" < "2026-01-10"`、
// `"2026-09-01" < "2026-10-01"` 均为真），因此 `store/sqlite` 直接用 TEXT 列排序与做
// 区间筛选即可（F-3 按日期排序、F-4 日期区间筛选），无需额外的比较函数或转换。
//
// 零值返回 `0000-00-00`——一个刻意非法的形态，肉眼与日志里都能立刻看出「这里没设值」，
// 且它无法被 ParseDate 读回（年份 0 越界），不会悄悄变成一个合法日期。
func (d Date) String() string {
	return formatDate(d.year, d.month, d.day)
}

// formatDate 按 `2006-01-02` 拼装定长文本。
// 不用 time.Format：零值与越界年份并不构成合法 time.Time，走标准库反而要先造一个
// 被规范化过的时间值；本包只是按位补零，自行拼装更直接也更可控。
func formatDate(year int, month time.Month, day int) string {
	buf := make([]byte, 0, len(dateLayout))
	buf = appendPadded(buf, year, 4)
	buf = append(buf, '-')
	buf = appendPadded(buf, int(month), 2)
	buf = append(buf, '-')
	buf = appendPadded(buf, day, 2)
	return string(buf)
}

// appendPadded 把 value 按 width 位左补零写入 buf，供 Date 与 Month 的 String 共用。
//
// 只处理非负值：全部调用点（年 / 月 / 日 / 月数）传入的都是非负数——合法值已被构造入口
// 校验过，零值的三字段也都是 0，而 MonthRange.count 恒 ≥ 1。故此处不设负值分支，
// 免得留下一段永不执行、也就永远测不到的代码。
// 位数超过 width 时原样输出（不截断）：`String` 不该有失败分支，
// 而年份最多 4 位、月数可以更宽，截断只会产出更难排查的错误文本。
func appendPadded(buf []byte, value, width int) []byte {
	digits := [20]byte{}
	n := 0
	for {
		digits[n] = byte('0' + value%10)
		n++
		value /= 10
		if value == 0 {
			break
		}
	}
	for pad := width - n; pad > 0; pad-- {
		buf = append(buf, '0')
	}
	for i := n - 1; i >= 0; i-- {
		buf = append(buf, digits[i])
	}
	return buf
}

// Compare 比较日期先后：d < d2 返回 -1、相等返回 0、d > d2 返回 1。
// 供 F-4 日期区间筛选与 F-3 排序使用。逐字段比较即可，无需换算成天数。
func (d Date) Compare(d2 Date) int {
	if d.year != d2.year {
		return signOf(d.year - d2.year)
	}
	if d.month != d2.month {
		return signOf(int(d.month) - int(d2.month))
	}
	return signOf(d.day - d2.day)
}

// Before 是否早于 d2。
func (d Date) Before(d2 Date) bool { return d.Compare(d2) < 0 }

// After 是否晚于 d2。
func (d Date) After(d2 Date) bool { return d.Compare(d2) > 0 }

// Equal 是否为同一日期。与 `==` 等价（本类型可比较），提供具名方法只为与 Compare 配套读起来顺。
func (d Date) Equal(d2 Date) bool { return d == d2 }

// signOf 返回整数的符号。
func signOf(delta int) int {
	if delta < 0 {
		return -1
	}
	if delta > 0 {
		return 1
	}
	return 0
}

// AddDays 返回 n 天之后的日期（n 可为负）。
//
// 天数加减委托 time.AddDate，跨月跨年与闰年由标准库负责（见包注释「选型」）。
// 结果年份越界返回 ErrYearOutOfRange——「能算出来但存不回来」的值一律不放行。
//
// 注意：**月份加减不在本类型上提供**。承载里的「起始月 + N − 1」是年月粒度的算术，
// 落在 Month.AddMonths；在日期上做月份加减会带日溢出（实测
// `2026-01-31` 加 1 月得 `2026-03-03`，所属月错成 3 月），详见包注释「选型」②。
func (d Date) AddDays(n int) (Date, error) {
	if d.IsZero() {
		return Date{}, errors.WithMessage(ErrZeroValue, "日期加减天数")
	}
	shifted := time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
	return NewDate(shifted.Year(), shifted.Month(), shifted.Day())
}

// MarshalText 实现 encoding.TextMarshaler，产出 `2006-01-02`。
// `encoding/json` 会自动经它把本类型序列化为 JSON 字符串，因此无需另写 MarshalJSON。
//
// 零值返回错误而非 `0000-00-00`：`Expense.支出日期` 是必填字段（终版 §四），
// 对外契约里出现一个非法日期形态，只会把问题推到前端或下游去炸。
func (d Date) MarshalText() ([]byte, error) {
	if d.IsZero() {
		return nil, errors.WithMessage(ErrZeroValue, "序列化日期")
	}
	return []byte(d.String()), nil
}

// UnmarshalText 实现 encoding.TextUnmarshaler，接受 `2006-01-02`。
// 错误直接来自 ParseDate，故 `dto` 可用 errors.Is 判出 ErrTextMalformed 与 ErrInvalidDate
// 两类，分别对应 D-3 的「格式不符」与「内容有误」，换成 `base/errs` 的不同文案键。
func (d *Date) UnmarshalText(data []byte) error {
	parsed, err := ParseDate(string(data))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Value 实现 driver.Valuer，以 `2006-01-02` 文本落库。
//
// 落 TEXT 而非 DATE/INTEGER：§六 L4 `store/sqlite` 无原生日期类型，而定长文本的
// 字典序恒等于时间序（见 String），排序与区间筛选可直接下推给 SQL。
func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, errors.WithMessage(ErrZeroValue, "日期落库")
	}
	return d.String(), nil
}

// Scan 实现 sql.Scanner，**刻意只接受文本形态，拒收 time.Time**。
//
// 这是前提 7 的三处结构性拦截之一（拦截②）。拒收的理由是实测出来的：驱动若把某列
// 解成 time.Time，其日期取决于连接的时区设置——同一瞬间 `2026-01-31T20:00:00Z`
// 在 UTC 下是 1 月 31 日、在 Asia/Shanghai 下是 2 月 1 日（answer §三.5），
// **跨月**即跨归月口径，J 域月度统计会随部署机器的 TZ 漂移。
// civil.Date.Scan 恰恰接收 time.Time 并按其 Location 取日期，这是本包不直接用它的
// 连带理由之一。
//
// 连带的建表要求（须在阶段 5 `store/sqlite` 遵守）：**支出日期列必须是 TEXT**，
// 且 DSN 不得开启把该列自动解析为时间的选项；否则本方法会返回 ErrScanUnsupported
// 而不是静默换个日期——这正是期望的行为，配置错配当场暴露。
//
// NULL 处理：`Expense.支出日期` 必填（终版 §四），故收到 nil 即数据异常，返回错误。
func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case string:
		parsed, err := ParseDate(v)
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	case []byte:
		parsed, err := ParseDate(string(v))
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	case nil:
		return errors.WithMessage(ErrScanUnsupported, "扫描日期，值为 NULL，而支出日期为必填")
	case time.Time:
		// 单列出来给一条能指向根因的错误：泛化到 default 只会说「不支持的类型」，
		// 而真正要改的是建表类型或 DSN 的时间解析选项。
		return errors.WithMessagef(ErrScanUnsupported,
			"扫描日期，拒收 time.Time（前提 7：不做时区换算）；列须为 TEXT 且 DSN 不得自动解析为时间，实际值=%s",
			v.Format(time.RFC3339))
	default:
		return errors.WithMessagef(ErrScanUnsupported, "扫描日期，类型=%T", src)
	}
}
