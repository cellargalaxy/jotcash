// Package amortize 是 jotcash 中不变式 1b 的唯一实现处（L2 规则层）。
//
// # 承载
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md（下称「分层」）§六 L2 表
// rule/amortize 行，本包承载四项：
//
//	不变式 1b 的唯一实现处：起止月派生（起始月 = 支出日期所属月、
//	结束月 = 起始月 + 月数 − 1）；8.2 摊分：按币种小数位数均分
//	（原币按支出币种、本位币按本位币，均不是存储用的 2 位——存储精度与
//	均分基准是两件事）、尾差归末月、原币与本位币各自独立均分、负金额对称
//	处理；H-4 的「覆盖月份区间 + 每月分摊金额」派生输出，同时是 J 域摊分
//	口径份额的唯一来源
//
// 四项各自的落点：
//
//	① 起止月派生          Derive
//	② 8.2 均分与尾差      Shares（NewShares 构造，At / Rows 取值）
//	③ H-4 预览           Schedule.Rows
//	④ J 域份额唯一来源     Schedule.At
//
// 「唯一实现处」是准则 1「不变式内聚」的要求：起止月不得在其他包派生
// （分层 §九 不变式 1b 行）。
//
// # 均分基准是币种小数位数，不是存储的 2 位
//
// 这是本包最容易被写错的一处，分层 §三 问题 1 专门登记过：
//
//	原币份额    按 Expense.支出币种 的 currency.Code.Scale()
//	本位币份额  按 Expense.折算本位币币种 的 currency.Code.Scale()
//	存储精度    decimal.BaseAmountStoreScale（2 位），与均分无关
//
// 两个位数**可以不同**，且必须各自独立均分——一笔以 CNY（2 位）支出、
// 折算到 JPY（0 位）本位币的明细，原币份额是 33.33 而本位币份额是 466，
// 用其中一个去推另一个必然错。这正是 entity 把 Currency 与 BaseCurrency
// 分成两个字段的原因（见 entity/consumer_check_test.go 的
// TestConsumerAmortizeSplit）。
//
// 同样不得与 8.1 折算的舍入混用：两者基准同源（都是 currency.Code.Scale()）
// 但**作用对象不同**——rule/convert 算的是单笔结果，本包切的是份额
// （分层 §九 不变式 1a 行末句）。
//
// # 尾差固定归末月，不轮转
//
// 终版 8.2 明写「尾差归末月」。实现是 decimal.QuoRem 取商与余，前 N−1 月
// 取商、末月取商 + 余。因 QuoRem 恒满足「商 × 除数 + 余 = 被除数」，
// 各月份额之和**精确等于**原金额，不会因舍入而多出或丢失分位。
//
// 负金额（退款、冲正）**天然对称**，不需要任何额外分支：QuoRem 的商向零
// 截断、余数与被除数同号，故 -100.00 摊 3 个月得 -33.33/-33.33/-33.34，
// 与正金额镜像。终版 H-2 要求的「从退款发生月起摊、不改写历史月份」由
// 起始月 = 支出日期所属月 自动满足——本包不看金额符号决定起始月。
//
// # 为什么不用第三方库
//
// 调研了 4 个候选，结论是不引入，但**底层定点算术已经下沉给第三方**：
// 本包经 base/decimal 用 shopspring/decimal（7470★），不自行实现任何
// 算术内核。本包只做「取哪个位数、尾差归谁」这两项业务口径决策。
//
// 否决 Rhymond/go-money（1912★，唯一提供均分 API 的候选）的实质理由是
// **尾差分配规则与 8.2 相反**：其 Split 与 Allocate 把余数轮转分给最前面
// 的几方（源码注释原文「parties listed first will likely receive more
// pennies」），对 100.00 / 3 得 33.34/33.33/33.33，而 8.2 要求
// 33.33/33.33/33.34。数值都平，归属月份不同，直接影响 J-1 月度总额与
// J-6 未来待摊分段，且无法通过配置消除。
//
// 另三条同 rule/convert 的记录：币种表必须来自本仓库 currency.Code.Scale()
// （已按 CLDR 48.2.0 交叉校验，IDR 刻意取账面位数 2 而非现金位数 0），
// 换用库自带的表就有了第二个位数来源；bojanz/currency 已在 currency
// 那一轮被否决为运行时依赖（Register 改包级 map、与读并发即 DATA RACE）；
// go-money 内部是 int64 最小单位，与本仓库的 decimal.Decimal 往返需再写
// 一层转换，反而增加失配面。
//
// # 不做什么
//
//   - **不读库、不落库**。终版 H-3 明写摊分份额**不落库、查询时实时派生、
//     不产生额外数据行**。本包是纯函数：同样的入参恒得同样的出参，
//     无 IO、无状态、无时间依赖，可脱库单测（准则 2）。
//   - **不做区间穿刺**。8.2 的「起始月 ≤ M 且 结束月 ≥ M」由 store 下推到
//     SQL 完成（分层 §六 L3 store ④），本包只提供 Schedule.At 供取回行后
//     派生份额。两者不重复实现：穿刺是取哪些行，派生是这些行在该月各摊多少。
//   - **不判定 J-6 的「已发生 / 未来待摊」**。判据是「份额所属月 ≤ 当前月」，
//     需要读系统时间，属 service/stat。本包无时间依赖。
//   - **不做折算**。那是不变式 1a，归 rule/convert。终版 8.1 第 3 行
//     「改支出日期」同时触发 1a 与 1b，两者由 service 在同一次保存内一并调用
//     （分层 §九 不变式 1b 行）。
//   - **不做存储精度转换**。份额按币种位数均分，落 2 位定点列由 store 完成。
//
// # 为什么不收 entity.Expense
//
// 分层 §六 L2 的依赖列写着 entity，但本包的入参是 Input 与散列参数而非
// entity.Expense，理由是**前提 6**：摊分起始月 / 结束月 是只读派生项、
// 无人工指定入口。若签名收 Expense，「传进来的那两个字段」在语义上就是
// 必须被无视的输入——一个存在却无意义的字段，迟早有人当它有意义。
// 不收则它们根本不在入参里，与 dto 用「结构缺席 > 运行时忽略」守住前提 6
// 是同一手法（准则 6「约束优先用结构表达」），也与同层 rule/convert 一致。
//
// 副作用是本包比承载的依赖列更收敛：只依赖 currency（L1.0）、
// base/calendar 与 base/decimal（L0），**不依赖 entity**。字段装配由调用方
// （service/expense、service/intake）完成，一行赋值。
//
// # 错误是哨兵
//
// 与 base/decimal、base/calendar、currency、enum、rule/convert 一致：
// 错误一律为本包的哨兵错误，**不返回 base/errs 的分档错误**。同一个
// 「摊分月数非法」在不同调用方是不同的档位——经 api/http/dto 进来是
// **用户输入错误**（外部提交了 0 或负数），经 J 域统计出现则是**系统错误**
// （库里存着非法月数，属数据损坏，不能当成 400 级错误吐给前端）。本包无从
// 判断自己被谁调用，一旦在此选定档位必有一方是错的。故由调用方用 errors.Is
// 判定后自行套档位与文案键。本包因此**不 import base/errs**。
//
// # 依赖边界
//
// 依赖恰为 currency（L1.0）、base/calendar 与 base/decimal（L0）三个包。
// 本包处于 L2，不得依赖 L3 及以上（store / fxrate / service / 上层），
// 由 TestNoUpwardDependency 以 AST 扫描锁死。
//
// # 并发
//
// 全部类型均为不可变值类型，方法无副作用，可并发使用。
package amortize

import (
	"errors"
	"fmt"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
)

// 哨兵错误。上层用 errors.Is 判定后，按自身场景映射到 base/errs 的档位与文案键
// （为什么不在本包分档，见包注释「错误是哨兵」）。
var (
	// ErrSpendDateEmpty 表示支出日期为零值（未指定）。
	//
	// 起始月 = 支出日期所属月，没有支出日期就没有起始月，整条不变式 1b 无从谈起。
	//
	// 单出一个哨兵而不是任由 calendar 报错：零值 Date 的 ToMonth() 得到零值
	// Month，NewMonthRange 对它报的是 ErrMonthValue「月份超出 1~12」——
	// 该文案指向「月份数值非法」，与真实原因「日期压根没填」相去甚远，
	// 排错时会把人引向错误的方向。终版 §四 3 明列支出日期为**必填**。
	ErrSpendDateEmpty = errors.New("amortize: 支出日期未指定")

	// ErrMonthsNotPositive 表示摊分月数不是正整数。
	//
	// 终版 H-1 明写「默认 1（不摊分），不设上限」——下限是 1，0 与负数都非法。
	// 必须显式拦下，两处后果都不是报错而是崩溃或算错：
	//   - 月数为 0 时 decimal.QuoRem 除零（该方法返回 ErrDivideByZero，
	//     但错误文案指向「除数为零」，同样掩盖真实原因）；
	//   - 若不校验就按月数分配切片，shares[months-1] 在月数为 0 时下标越界 panic。
	ErrMonthsNotPositive = errors.New("amortize: 摊分月数必须为正整数")

	// ErrCurrencyEmpty 表示均分基准的币种为零值（未指定）。
	//
	// 必须显式拦下，不能放行：currency.Code.Scale() 对零值返回 0，与 JPY 的
	// 0 位**无法区分**（该方法的注释明写「调用前须先判 IsZero」）。
	//
	// 实测放行的后果：712.35 会被按 0 位均分成 237/237/238.35——本该是
	// 2 位币种的金额被切成整数份额，钱的形态整个变了且不报错。
	// 取向同 rule/convert 的 ErrBaseCurrencyEmpty。
	ErrCurrencyEmpty = errors.New("amortize: 均分基准的币种未指定")

	// ErrAmountScale 表示待均分的金额位数超过该币种的小数位数。
	//
	// 这条拦的是一条**只在总额位数与币种位数不匹配时才存在**的静默出错路径。
	// 实测：100.005（3 位）按 CNY 的 2 位均分 3 份，商 33.33、余 0.015，
	// 末月得 33.345——**突破了币种位数**。同理 1400.5 按 JPY 的 0 位均分
	// 得末月 468.5，一个不存在的日元面额。
	//
	// 这不是 decimal.QuoRem 的缺陷：它的契约「商 × 除数 + 余 = 被除数」
	// 在三例中全部成立。问题在于没有任何一层校验过「Expense.支出金额 的位数
	// 是否与 Expense.支出币种 的位数相符」——decimal.Parse 放行到 18 位，
	// parser 从账单读到的金额位数不受约束。
	//
	// 为什么报错而不是先舍入：舍入会改钱（100.005 舍到 2 位丢掉 0.005，
	// 而本包无权决定这半分钱的去向）；放行则末月份额落 2 位定点列时被截断，
	// 错误发生在离出错点很远的地方。报错是唯一既不改钱又不留隐患的选择。
	// 取向同 rule/convert 拒绝 3 位本位币：本该不可能，发生了就必须响亮失败。
	ErrAmountScale = errors.New("amortize: 金额小数位数超过币种小数位数")

	// ErrShareIndex 表示份额下标越界。
	//
	// Shares.At 的下标是「第几个摊分月」，取值域 [0, Months())。
	// 返回错误而非 panic：下标可能来自 J 域按月定位的计算结果，
	// 一次算错不应崩掉整个统计请求。
	ErrShareIndex = errors.New("amortize: 份额下标越界")
)

// Derive 派生摊分月区间，是**不变式 1b 的唯一实现**：
//
//	起始月 = 支出日期所属月
//	结束月 = 起始月 + 摊分月数 − 1
//
// 调用方取 r.Start() 与 r.End() 写回 Expense.摊分起始月 / 摊分结束月
// （终版 §四 3 的只读派生项之二，前提 6 规定它们不接受任何上游传入值）。
//
// 纯函数：无 IO、无状态、无时间依赖。
//
// # 月数无上限，但年份有上界
//
// 终版 H-1 与前提 5 明写摊分月数「不设上限」，本包因此**不设月数上界**。
// 实际边界由 base/calendar 的年份上界（MaxYear = 9999）给出：结束月越界时
// 透传 calendar.ErrYearRange，不重新包装——那是 calendar 的既定语义，
// 调用方用 errors.Is 判定即可。实测 2026-01 起最多可摊 95688 个月（到 9999-12）。
//
// # 负金额不影响起始月
//
// 终版 H-2 要求退款「从退款发生月起摊，不改写历史月份」。本函数不接收金额、
// 不看符号，起始月恒为支出日期所属月，该要求自动满足。
//
// # 与不变式 1a 的关系
//
// 终版 8.1 第 3 行「改支出日期」**同时**触发 1a（重拉汇率、重算折算三字段）
// 与 1b（刷新起止月）；第 6 行「改摊分月数」只触发 1b。两者必须由
// service/expense / service/intake 在同一次保存内一并调用（分层 §九
// 不变式 1b 行）——本包只管 1b。
func Derive(spendDate calendar.Date, months int) (calendar.MonthRange, error) {
	//先判日期：零值 Date 的 ToMonth() 是零值 Month，交给 NewMonthRange 会
	//报 ErrMonthValue「月份超出 1~12」，指向的原因与事实不符
	if spendDate.IsZero() {
		return calendar.MonthRange{}, ErrSpendDateEmpty
	}
	//再判月数：NewMonthRange 对非正月数报 ErrMonthCount，语义正确但不是本包的
	//哨兵，上层要同时判两个包的错误才能覆盖同一种输入错误
	if months < 1 {
		return calendar.MonthRange{}, fmt.Errorf("%w: months=%d", ErrMonthsNotPositive, months)
	}
	//区间构造本身即「结束月 = 起始月 + 月数 − 1」，减 1 在 calendar 内完成，
	//本包不重复实现该算术（否则不变式 1b 会有两处减 1）
	return calendar.NewMonthRange(spendDate.ToMonth(), months)
}
