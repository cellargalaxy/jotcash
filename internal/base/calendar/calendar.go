// Package calendar 提供不带时区的日历日期类型、年月类型与月区间运算。
//
// # 为什么需要本包
//
// 「记账系统功能与模型」前提 7 规定：支出日期以账单字面值为准，不做时区换算。
// 若用 time.Time 承载支出日期，同一字面值（如 2026-01-01）在不同时区下取
// 到的「所属月」会整体偏移一天，导致三处同时出错且不报错：
//   - 摊分起始月（8.2「起始月 = 支出日期所属月」）
//   - J-1 未摊分口径「按支出日期归月」
//   - I-4「按支出日期的日汇率折算」
//
// 因此本包的 Date 是前提 7 的唯一实现处：任何包都不得引入带时区的时间类型
// 参与支出日期的解析、存储与归月。业务时间戳（创建时间、更新时间、操作时间、
// 上传时间）不受此限，它们仍用 time.Time，不进本包。
//
// # 三个类型
//
//   - Date       日历日期（年月日），承载 Expense.支出日期
//   - Month      年月，承载 Expense.摊分起始月 / 摊分结束月
//   - MonthRange 月区间，承载「起始月 + N − 1」与「起始月 ≤ M ≤ 结束月」的区间穿刺
//
// # 两条全局性质
//
// 一、非零值必合法。三个类型的字段全部私有，只能经 NewXxx / ParseXxx / DateOf
// 构造，构造时即校验。于是全仓库成立：一个值要么 IsZero，要么合法——上层无需
// （也无法）传播非法日期，不必逐处调用校验函数。
//
// 二、文本形态定长，字典序 = 时间序。Date 恒为 10 字符 2006-01-02，Month 恒为
// 7 字符 2006-01。store/sqlite 直接依赖这条性质：日期以 TEXT 落库后，排序
// （F-3）、区间筛选（F-4）与摊分区间穿刺（8.2）都能用朴素字符串比较完成。
// 这也是本包把年份限制在 [MinYear, MaxYear] 的原因——越界会破坏定长。
//
// # 与 time.Time 的边界
//
// 本包只留入口不留出口：DateOf 取 time.Time 自身时区下的年月日（不做换算），
// 是唯一的接触点；不提供反向的 In(loc) / Time() 出口，也不提供 Today()——
// 「今天」隐含「按哪个时区判定」这一部署决策，不应由基础层默默替上层选定，
// 调用方须显式写出 calendar.DateOf(time.Now().In(loc))，让时区选择在调用点可见。
//
// # 依赖
//
// 本包处于分层的 L0 且层内零依赖：不依赖 base/errs 等同层包，错误一律为本包的
// 哨兵错误，由上层用 errors.Is 判定后映射到 base/errs 的「用户输入错误」档与
// 对应文案键。日历算术下沉给 cloud.google.com/go/civil（time-zone-independent
// 语义与前提 7 同构），本包不自行实现闰年与月末进位。
package calendar

import (
	"errors"
	"time"

	"cloud.google.com/go/civil"
)

const (
	// DateLayout 是 Date 的唯一文本形态：RFC3339 full-date，定长 10 字符。
	DateLayout = "2006-01-02"
	// MonthLayout 是 Month 的唯一文本形态：定长 7 字符。
	MonthLayout = "2006-01"

	// dateLen、monthLen 为对应形态的定长长度，供解析前的快速形态校验，
	// 避免 time.Parse 接受 "2026-1-2" 这类非定长写法而破坏字典序性质。
	dateLen  = len(DateLayout)
	monthLen = len(MonthLayout)
)

const (
	// MinYear、MaxYear 是本包支持的年份闭区间。
	// 上下界不是随意设定：文本形态用 %04d 定长格式化年份，越界即破坏定长，
	// 而定长是「字典序 = 时间序」这条被 store/sqlite 依赖的性质的前提。
	MinYear = 1
	MaxYear = 9999
)

// 哨兵错误。本包不依赖 base/errs（L0 层内零依赖），错误分类由这组哨兵表达，
// 上层用 errors.Is 判定后附加文案键与错误档位（例如 parser 的 D-3 五类错误）。
var (
	// ErrDateFormat 表示日期文本不是定长的 2006-01-02 形态。
	ErrDateFormat = errors.New("calendar: 日期文本格式非法，应为 2006-01-02")
	// ErrDateValue 表示日期文本形态正确但日期不存在，如 2026-02-30。
	ErrDateValue = errors.New("calendar: 日期不存在")
	// ErrMonthFormat 表示年月文本不是定长的 2006-01 形态。
	ErrMonthFormat = errors.New("calendar: 年月文本格式非法，应为 2006-01")
	// ErrMonthValue 表示月份不在 1~12 之间。
	ErrMonthValue = errors.New("calendar: 月份超出 1~12")
	// ErrYearRange 表示年份超出 [MinYear, MaxYear]，含加减运算后的溢出。
	ErrYearRange = errors.New("calendar: 年份超出 1~9999")
	// ErrMonthCount 表示摊分月数不是正整数。
	ErrMonthCount = errors.New("calendar: 月数必须为正整数")
	// ErrMonthOrder 表示月区间的结束月早于起始月。
	ErrMonthOrder = errors.New("calendar: 结束月不能早于起始月")
	// ErrScanType 表示从数据库扫描到的类型不受支持。
	ErrScanType = errors.New("calendar: 不支持的数据库扫描类型")
)

// DateOf 取 t 在其自身时区下的年月日，不做任何时区换算。
//
// 这是本包与带时区时间类型的唯一接触点。调用方须自行明确 t 的时区含义，
// 例如按东八区取「今天」应写作 DateOf(time.Now().In(loc))，
// 由调用点显式表达时区选择，本包不代为决定。
//
// 与 NewDate / ParseDate 一样**返回错误而非静默产出非法值**：time.Time 的年份
// 取值域远宽于本包的 [MinYear, MaxYear]，越界返回 ErrYearRange。这道校验不是
// 冗余——它是「非零值必合法」与「文本形态定长」两条全局性质的守门人之一，
// 少了它 DateOf 就成了唯一能造出非法 Date 的构造函数：10000 年会产出 11 字符的
// "10000-03-05"（破坏定长，令 store/sqlite 依赖的「字典序 = 时间序」失效，
// 且 Value 写得进库、Scan 读不回来），0 年则退化成 IsZero 的「未指定」，
// 把一个真实日期静默当成无值。
func DateOf(t time.Time) (Date, error) {
	// civil.DateOf 取的是 t 所在时区的年月日，本身不做换算，与前提 7 一致。
	d := civil.DateOf(t)
	if err := checkYear(d.Year); err != nil {
		return Date{}, err
	}
	return Date{d: d}, nil
}

// checkYear 校验年份是否落在 [MinYear, MaxYear]。
// 加减运算的溢出统一走这里，保证越界一律返回错误而不是静默产出破坏定长的值。
func checkYear(year int) error {
	if year < MinYear || year > MaxYear {
		return ErrYearRange
	}
	return nil
}
