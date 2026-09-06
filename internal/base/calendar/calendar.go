// Package calendar 提供**不带时区的日历日期类型**与**年月类型**，以及年月区间运算。
//
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md` §六 L0 表 `base/calendar` 行：
// 「**不带时区的日历日期类型**（前提 7：账单字面值，解析、存储、归月全程不做时区换算）
// + 年月类型 + 区间运算（`起始月 + N − 1`、`起始月 ≤ M ≤ 结束月`）+ **日期 → 所属月**」。
// 四项承载在本包的落点：
//
//	不带时区的日历日期类型 → Date（date.go）
//	年月类型              → Month（month.go）
//	区间运算              → MonthRange（monthrange.go）：EndMonth 即「起始月 + N − 1」、
//	                        Contains 即「起始月 ≤ M ≤ 结束月」
//	日期 → 所属月          → MonthOf（month.go）
//
// # 前提 7 是本包存在的唯一理由
//
// §九 落点表「**前提 7** 日期字面值、不做时区换算」一行指定：`entity.Expense.支出日期`、
// `parser` 的原始交易行、`store` 的筛选与归月、`rule/amortize` 的所属月派生**一律使用该类型**，
// 且「**任何包不得引入带时区的时间类型参与支出日期的解析、存储与归月**（业务时间戳如
// 创建/更新/操作时间不受此限）」。
//
// 因此本包的 Date **不是** `time.Time` 的别名，也不携带任何时区信息：它表示「账单上印着的那个
// 日期」这一字面值。同一瞬间在不同时区落在不同日期上，而账单日期不存在这种二义性——
// 一笔 1 月 31 日的消费在任何时区都归入 1 月，归月结果不能随部署机器的 TZ 变化。
//
// 本包为此在三处做了**结构性拦截**，而不只是写在注释里（§五 准则 6「约束优先用结构表达」）：
//
//	① 不提供 `time.Time → Date` 的隐式转换：从带时区类型取日期必然要选一个时区，
//	   选谁都是业务决策，不能由本包默默替调用方决定。唯一入口 Today 强制显式传 *time.Location。
//	② Scan 拒收 time.Time：详见 date.go 中 Date.Scan 的注释与 store/sqlite 的建表要求。
//	③ 不提供任何「当前时间」的隐式默认：本包不读 time.Local。
//
// # 依赖与分层
//
// §六 L0 表头「零业务语义，仅依赖标准库与第三方库，**层内零依赖**」，依赖列为 `—`：
// 本包**不 import 任何 base 兄弟包**（含 `base/errs`），错误一律用 `github.com/pkg/errors`
// 包装本包的哨兵错误返回，与 `base/pwdhash` 的既有做法一致。
// 调用方需要面向用户的文案时，由上层（如 `parser`）按 `errors.Is` 判别哨兵错误后，
// 换成 `base/errs` 的文案键——这也是本包导出哨兵错误的目的。
//
// 约定 5「`base/` 是纯技术层：任何带业务语义的类型都不得放入」：本包只有「日期」与「年月」
// 两个纯时间概念，不含币种、归属、摊分口径等业务语义。「起始月 + N − 1」写成 EndMonth
// 这一**纯算术**入口，摊分语义（H-2：以支出日期所属月为第 1 个摊分月）留在 `rule/amortize`
// ——§六 L2 已把不变式 1b 判给它，本包只提供它所需的月份算术。
//
// # 选型：历法委托标准库 time，不引第三方日期库
//
// 「避免自己造轮子」在本包的落法是**把闰年与月末规则全部委托 `time.Date` 的规范化语义**
// ——本包不含任何一行闰年判断、月长表或儒略日换算：
//
//	日期是否真实存在 → time.Date(y, m, d, …) 规范化后三字段是否原值不变
//	                   （2026-02-30 会被规范化为 2026-03-02，比对即知非法）
//	定长文本解析     → time.Parse(`2006-01-02` / `2006-01`)，越界由标准库报错
//
// 该判据已与 `cloud.google.com/go/civil` 的 IsValid **逐条比对一致**（18 组边界用例），
// 并以 1~9999 年全量回归验证：闰年数 2424 恰等于 `/4 − /100 + /400`、
// 119988 组月末日 String→Parse 往返零失败。证据见
// `doc/turn/2609p0/260904-153920-answer.md` §三。
//
// **不引 `cloud.google.com/go/civil` 的两条实测理由**（同上 §三，均非主观取舍）：
//
//	① 成本不对等：civil 包本身仅 500 行、编译期只依赖标准库，但它属 google-cloud-go
//	   **主仓根模块** `cloud.google.com/go`（其 go.mod 有 59 行 require）。实测在本仓库
//	   `go get` 后模块图由 100 条涨到 143 条（+43），只为换取一个标准库已覆盖的判据。
//	② 它的月份算术在本仓库的核心场景上**是错的**：civil.AddMonths 经 time.AddDate 实现，
//	   带日溢出——实测 `2026-01-31.AddMonths(1) = 2026-03-03`，所属月 3 月而非 2 月。
//	   而本包承载的「起始月 + N − 1」恰恰服务 H-2 摊分：1 月 31 日的消费摊 2 个月，
//	   结束月必须是 2026-02。仅 2026 一年就有 7 个起始日会算错（1/29、1/30、1/31、
//	   3/31、5/31、8/31、10/31）。
//
// 因此 Month 的月份算术**不经任何日期类型**，走纯整数月序号（`年 × 12 + 月 − 1`），
// 从结构上不存在「日」这一维度，日溢出无从发生（实测 8 组与 civil 在 1 号上的结果一致，
// 在月末日上则只有整数算术正确）。这也是 §五 准则 6「约束优先用结构表达」在本包的落法。
package calendar

import (
	"time"

	"github.com/pkg/errors"
)

// 年份闭区间边界。
//
// 上下界不是凭空设的「够用就好」，而是**无损往返的硬边界**：Date 与 Month 的文本形态
// （`2006-01-02` / `2006-01`）固定 4 位年份，落库、JSON 与解析共用同一形态。实测
// （证据见 answer §三.4）：年 10000 的 String() 产出 `10000-01-01`，再解析即报
// `cannot parse "0-01-01" as "-"`；负年份产出 `-001-01-01`，同样解析失败。
// 也就是说超出 [1, 9999] 的值**能算出来但存不回来**，属于「静默写坏数据」。
// 本包因此在所有构造与算术出口处校验年份，越界即报错（ErrYearOutOfRange）。
//
// 下界取 1 而非 0：公历无 0 年（ISO 8601 的 0000 年是公元前 1 年，与账单语境无关），
// 让 0 年保持非法可让「零值 = 未设置」这一口径干净——零值 Date/Month 的 Year 恰为 0。
const (
	// MinYear 支持的最小年份。
	MinYear = 1
	// MaxYear 支持的最大年份。
	MaxYear = 9999
)

// 文本布局。两者都是 ISO 8601 的定长形态，**字典序恒等于时间序**
// （实测：`"2026-01-05" < "2026-01-10"`、`"2026-09-01" < "2026-10-01"` 均为真），
// 因此 `store/sqlite` 直接用 TEXT 列排序与做区间筛选即可，无需额外的比较函数。
const (
	// dateLayout 日历日期的文本布局，形如 `2026-01-31`。
	dateLayout = "2006-01-02"
	// monthLayout 年月的文本布局，形如 `2026-01`。
	monthLayout = "2006-01"

	// outOfRangeSuffix 标准库 `*time.ParseError.Message` 在「字段值越界」时的后缀，
	// 形如 `: day out of range` / `: month out of range`。判据的完整理由见 isOutOfRangeErr。
	outOfRangeSuffix = "out of range"
)

// 哨兵错误。
//
// 供上层用 `errors.Is` 判别后换成 `base/errs` 的文案键——本包不能 import `base/errs`
// （L0 层内零依赖），但**分类信息不能丢**：`parser` 的 D-3 五类错误里，
// 「字段缺失」与「格式不符」都可能由日期解析失败引发，只有能区分才能给对文案。
//
// 全部错误经 `errors.WithMessagef` / `errors.Wrapf` 包装后返回，`errors.Is` 仍成立
// （实测已验证），且 `%+v` 能打印出错位置，与 `go_common/util` 及 `base/pwdhash` 的用法一致。
var (
	// ErrInvalidDate 日历日期非法：年月日不构成真实存在的公历日期。
	// 典型触发：2 月 30 日、月份为 0 或 13、闰年判断落空（2026-02-29）。
	ErrInvalidDate = errors.New("非法的日历日期")

	// ErrInvalidMonth 年月非法：月份不在 1~12。
	ErrInvalidMonth = errors.New("非法的年月")

	// ErrYearOutOfRange 年份超出 [MinYear, MaxYear]，无法无损往返，理由见 MinYear 注释。
	ErrYearOutOfRange = errors.New("年份超出可表示范围")

	// ErrTextMalformed 文本形态不符：非 `2006-01-02` / `2006-01` 布局。
	// 与 ErrInvalidDate 分开：前者是「写法不对」（如 `2026/01/05`、`2026-1-5`），
	// 后者是「写法对但日期不存在」（如 `2026-02-30`）——账单解析要报的错不同。
	ErrTextMalformed = errors.New("日期文本格式不符")

	// ErrMonthCountInvalid 月数非法：区间长度必须 ≥ 1。
	// 依据 `doc/decisions/记账系统功能与模型.md` H-1「摊分月数……默认 1（不摊分），**不设上限**」：
	// 下界是 1（0 或负数月的摊分区间不存在），上界不设——本包只受 MaxYear 的天然约束。
	ErrMonthCountInvalid = errors.New("月数非法")

	// ErrScanUnsupported Scan 收到不支持的类型，含**刻意拒收的 time.Time**（前提 7），
	// 详见 Date.Scan 的注释。
	ErrScanUnsupported = errors.New("不支持的扫描类型")

	// ErrZeroValue 对零值（即「未设置」）执行了要求真实日期/年月的操作。
	// 与上面几个「输入非法」类错误分开：这是**漏赋值**，不是值写错——
	// `Expense.支出日期` 与摊分起止月都是必填（终版 §四），零值走到落库或序列化
	// 只能是上游漏填，报错才能当场暴露，静默输出 `0000-00-00` 会把问题推到下游。
	ErrZeroValue = errors.New("日期或年月为零值")

	// ErrNilLocation 需要显式时区的入口收到了 nil。
	// 唯一触发点是 Today：本包不读 time.Local，也不替调用方选时区（前提 7 拦截①）。
	ErrNilLocation = errors.New("时区为空")
)

// checkYear 校验年份落在 [MinYear, MaxYear]。
// 独立成函数是因为 Date 与 Month 的 5 个出口（构造、解析、日期加减、月份加减、区间求末月）
// 都要走同一判据，散着写迟早漏一处。
func checkYear(year int) error {
	if year < MinYear || year > MaxYear {
		return errors.WithMessagef(ErrYearOutOfRange,
			"年份=%d，可表示范围=[%d, %d]", year, MinYear, MaxYear)
	}
	return nil
}

// checkMonth 校验月份落在 1~12。
func checkMonth(month time.Month) error {
	if month < time.January || month > time.December {
		return errors.WithMessagef(ErrInvalidMonth, "月份=%d，可取范围=[1, 12]", int(month))
	}
	return nil
}
