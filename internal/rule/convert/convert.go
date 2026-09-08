// Package convert 是 jotcash 中不变式 1a 的唯一实现处（L2 规则层）。
//
// # 承载
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md（下称「分层」）§六 L2 表
// rule/convert 行，本包承载：
//
//	不变式 1a 的唯一实现处：8.1 六种触发的三字段联动矩阵；入口先把折算汇率
//	规整到 8 位小数（T39，全仓库唯一规整点，自动获取与手填共用）；
//	本位币金额 = round(支出金额 × 折算汇率, 本位币的 currency.小数位数, 四舍五入)
//	（全仓库唯一一处金额乘法与舍入）；折算本位币币种 仅在「汇率重拉或用户手填」
//	时更新；输出三元组含规整后的汇率，落库以它为准
//
// 「唯一实现处」是准则 1「不变式内聚」的要求，也是本包全部设计取向的来源：
// 其余任何包不得自行做「金额 × 汇率」乘法、不得自行规整汇率（分层 §九
// 不变式 1a 行）。
//
// # 两个精度口径，作用对象不同，不得混用
//
//	折算汇率    8 位小数、四舍五入          T39，decimal.RateScale
//	本位币金额  按**本位币**的小数位数四舍五入  T35 + T38，currency.Code.Scale()
//
// 本位币金额的舍入基准是「本位币小数位数」而**不是**存储用的 2 位——这是
// 分层 §三 问题 1 的修法。本位币为 JPY（0 位）时按 2 位舍入会算出
// 「712.35 JPY」这种该币种不存在的面额，紧接着 8.2 要按 JPY 的 0 位均分
// 这个带小数的总额，尾差归末月也归不平。T38 已把本位币限定为小数位 ≤ 2，
// 故按本位币位数算出的结果必然能被 2 位定点列无损容纳（存储精度归 store）。
//
// 同样不得与 8.2 摊分的「按币种小数位均分」混用：两者基准同源但**作用对象
// 不同**——本包算的是单笔结果，rule/amortize 切的是份额（分层 §九
// 不变式 1a 行末句）。
//
// # 三字段联动，与恒等式同等重要
//
// 不变式 1a 是两件事，缺一不可：
//
//  1. 恒等式 `本位币金额 = 支出金额 × 折算汇率` 恒成立；
//  2. 折算本位币币种 仅在「汇率按当前本位币重拉或用户手填」时更新。
//
// 第 2 条没有任何数值后果，算错了不报错、金额也对，因此最容易被忽略。
// 它的作用是让「该行是否已按当前本位币折算」始终可判定（不变式 2 的唯一
// 判据即 `折算本位币币种 ≠ User.本位币`）。六种触发中只有「改支出金额」
// 不更新它——若在此顺手更新，手工兜底的行永远收敛不了，且手填值会被
// 下一次 I-7 覆盖（终版 8.1 末段）。故触发是本包的一等概念，见 trigger.go。
//
// # 为什么不用第三方货币库
//
// 调研并实测了 3 个候选（bojanz/currency 644★、govalues/money 56★ +
// govalues/decimal 248★、Rhymond/go-money 1912★）。三者都能算「金额 × 汇率
// 按币种位数舍入」，但**没有一家能承担本包的职责**，原因不在算术而在三处：
//
//   - **币种表是它们自带的，不是本仓库的**。本包的舍入基准必须来自
//     currency.Code.Scale()——那张表已按 CLDR 48.2.0 交叉校验，且在 IDR 上
//     刻意取账面位数 2 而非 CLDR 现金位数 0（见 currency/table.go 的登记）。
//     换用库自带的表，同一个币种就有了两个位数来源，这正是最该避免的形态。
//   - **联动矩阵是本系统专有语义**，任何库都不提供。而本包的实质价值恰在
//     这张矩阵——恒等式那一半只是一次乘法加一次舍入。
//   - **舍入方式实测分歧**。govalues/money 用银行家舍入：实测
//     RoundToCurr(0.005 USD) 得 0.00、(5.445) 得 5.44，而已定案口径是四舍五入
//     （0.01 / 5.45）。两者签名一致、差异只有一分钱，误用后几乎不可能在评审中
//     被发现——与 base/decimal 隔离银行家舍入是同一条取向。
//     bojanz/currency 的 Round() 实测确为四舍五入，但它的另外三处问题
//     （Register 改包级 map、与读并发即 DATA RACE、IsValid("") 为 true）
//     已在 currency 那一轮被否决为运行时依赖，本包不重新引入。
//
// 底层定点算术**已经**下沉给第三方库：本包经 base/decimal 用
// shopspring/decimal，不自行实现任何算术内核。本包只做「取哪个位数、
// 何时规整、哪种触发改哪个字段」这三项决策——它们全部是本系统的业务口径。
//
// # 不做什么
//
//   - **不读库、不取汇率**。汇率从哪来（fxrate 自动获取还是 I-5 用户手填）
//     归 service/fx；I-4 的短路判据「支出币种 = 当前本位币时汇率取 1」同样
//     在 service/fx——那需要读 User.本位币。本包是纯函数：同样的入参恒得
//     同样的出参，无 IO、无状态、无时间依赖，可脱库单测（准则 2）。
//   - **不派生摊分起止月**。那是不变式 1b，归 rule/amortize。终版 8.1 第 3 行
//     「改支出日期」同时触发 1a 与 1b，两者由 service 在同一次保存内一并调用。
//   - **不判定「该行是否需要重算」**。判据是 `折算本位币币种 ≠ User.本位币`，
//     需要读用户偏好，归 service/fx（不变式 2）。
//   - **不接触 entity.Expense**。见「为什么不收 entity.Expense」。
//   - **不做存储精度转换**。输出按本位币位数舍入，落 2 位定点列由 store 完成。
//
// # 为什么不收 entity.Expense
//
// 本包的入参是 Input 三元组而非 entity.Expense，尽管分层 §六 L2 的依赖列
// 写着 entity。理由是**前提 6**：本位币金额是只读因变量、无人工指定入口。
// 若签名收 Expense，「传进来的那个 BaseAmount」在语义上就是个必须被无视的
// 输入——一个存在却无意义的字段，迟早有人当它有意义。收三元组则该字段
// 根本不在入参里，与 dto 用「结构缺席 > 运行时忽略」守住前提 6 是同一手法
// （准则 6「约束优先用结构表达」）。
//
// 副作用是本包比承载的依赖列更收敛：只依赖 currency 与 base/decimal，
// 不依赖 entity。字段装配由调用方（service/expense、service/intake）完成，
// 一行赋值，且赋的是哪三个字段在调用点一目了然。
//
// # 错误是哨兵
//
// 与 base/decimal、base/calendar、currency、enum 一致：错误一律为本包的
// 哨兵错误，**不返回 base/errs 的分档错误**。同一个「本位币未指定」在不同
// 调用方是不同的档位——经 dto 进来是用户输入错误，经 I-7 批处理出现则是
// 系统错误（用户偏好里存了非法本位币，属数据损坏）。本包无从判断自己被谁
// 调用，一旦在此选定档位必有一方是错的。故由调用方用 errors.Is 判定后
// 自行套档位与文案键。本包因此**不 import base/errs**。
//
// # 依赖边界
//
// 依赖恰为 currency（L1.0）与 base/decimal（L0）两个包。本包处于 L2，
// 不得依赖 L3 及以上（store / fxrate / service / entity 的上层），
// 由 TestNoUpwardDependency 以 AST 扫描锁死。
package convert

import (
	"errors"
	"fmt"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 哨兵错误。上层用 errors.Is 判定后，按自身场景映射到 base/errs 的档位与文案键
// （为什么不在本包分档，见包注释「错误是哨兵」）。
var (
	// ErrTrigger 表示触发非法，含零值 Trigger(0)。
	//
	// 零值不默许任何一种触发：忘传参数必须报错，不能静默按某一种触发把
	// 折算本位币币种 改掉或不改掉——那是不变式 1a 第 2 条的全部内容。
	ErrTrigger = errors.New("convert: 未知的折算触发")

	// ErrBaseCurrencyEmpty 表示当前本位币未指定（零值 currency.Code）。
	//
	// 必须显式拦下，不能放行：currency.Code.Scale() 对零值返回 0，与 JPY 的
	// 0 位**无法区分**（该方法的注释明写「调用前须先判 IsZero」）。放行的
	// 后果是 712.35 被静默舍成 712——本位币是 CNY 却按 JPY 的位数算，钱少
	// 一截且不报错。
	ErrBaseCurrencyEmpty = errors.New("convert: 当前本位币未指定")

	// ErrBaseCurrencyNotCandidate 表示当前本位币不满足 T38 的候选口径
	// （已启用 且 小数位数 ≤ 2）。
	//
	// 本包据此**拒绝按 3 位舍入**。3 位本位币（KWD/BHD/OMR）算出的金额
	// 无法被 2 位定点列无损容纳（实测：100.00 × 7.12345678 按 3 位得
	// 712.346，FitsScale(2) 为 false），落库时会被静默截断成 712.35 或
	// 直接报 ErrPrecisionLoss——前者改钱，后者报在离出错点很远的地方。
	//
	// 正常链路走不到这里：service/preference 的 K-1 切换已用
	// currency.ParseBase 拦住非候选（T38④）。它拦的是那条链路失效后的情形，
	// 属「本该不可能，发生了就必须响亮失败」。
	ErrBaseCurrencyNotCandidate = errors.New("convert: 当前本位币不满足本位币候选口径")

	// ErrPriorBaseCurrencyEmpty 表示「改支出金额」触发下该行原有的
	// 折算本位币币种 为零值。
	//
	// 仅本触发会遇到：它是六种触发中唯一保持原口径的一种，因而舍入基准取
	// 该行原有的 折算本位币币种（见 Apply）。零值意味着这行从未折算过，
	// 而「从未折算过的行被用户改了金额」是不该出现的状态——终版 §四 3 把
	// 折算本位币币种 列为**必填**的只读派生项。放行同样会误按 0 位舍入。
	ErrPriorBaseCurrencyEmpty = errors.New("convert: 改支出金额时该行原有的折算本位币币种为空")

	// ErrRateNotPositive 表示折算汇率不是正数（零或负数）。
	//
	// 三种情形都必须拦下：
	//   - **零**：I-5 明写「重拉失败时该行汇率转必填、未填不可提交 / 不可保存，
	//     不静默按 1:1 处理」。零值在 entity.Expense.Rate 上的语义是「未填」
	//     而非 1，放行会把本位币金额静默算成 0——恒等式照样成立，钱却没了。
	//   - **负数**：汇率是「1 单位原币可兑换的本位币数量」（终版 §四 3），
	//     负汇率会让退款变成支出、支出变成退款，符号整体翻转。
	//   - **规整后为零**：极小汇率（< 0.000000005）经 8 位规整后**变成 0**
	//     （实测 RoundRate(0.000000001) = 0）。这是一条只在规整之后才存在的
	//     零值路径，必须在规整后再判一次，见 Apply。
	ErrRateNotPositive = errors.New("convert: 折算汇率必须为正数")
)

// Input 是一次折算的全部输入，恰为「算得出三字段」所需的最小集合。
//
// 不收 entity.Expense 的理由见包注释「为什么不收 entity.Expense」：
// 前提 6 要求本位币金额没有人工指定入口，把它放进入参就等于开了一个
// 「存在但必须被无视」的口子。
type Input struct {
	// Amount 支出金额（原币），允许为负（退款、冲正，终版 §四 3）。
	//
	// 零值合法：终版未禁止零金额明细，且 0 × 任何汇率恒为 0，无歧义。
	Amount decimal.Decimal

	// Rate 折算汇率，**规整前**的原始值。
	//
	// 两条入口共用本字段：fxrate 自动获取（返回值精度不设限）与 I-5 用户
	// 手填。本包在乘法前把它规整到 8 位（T39），**输出的是规整后的值**，
	// 落库以输出为准——这正是「全仓库唯一规整点」的含义，两条入口因此
	// 不会出现「库里存 12 位、算的时候用 8 位」。
	//
	// 必须为正数，否则 ErrRateNotPositive（见该哨兵的注释）。
	Rate decimal.Decimal

	// BaseCurrency 当前本位币，即 User.本位币。
	//
	// 是「汇率重拉或手填」五种触发下的舍入基准与输出的 折算本位币币种。
	// 必须非零且满足 T38 候选口径。
	BaseCurrency currency.Code

	// PriorBaseCurrency 该行**原有的** 折算本位币币种，即 Expense.BaseCurrency
	// 在本次修改前的值。
	//
	// **仅 TriggerAmountChanged 读取本字段**，其余五种触发一律忽略它——
	// 那五种都置 折算本位币币种 = 当前本位币，原值不参与任何计算。
	// 因此新建行（D-7 入库首次折算）留零值即可。
	//
	// 单独出一个字段而不是复用 BaseCurrency，是因为二者在「未收敛行」上
	// 恰恰不同：一行的 折算本位币币种 是 USD 而当前本位币是 CNY，正是
	// 不变式 2 用来识别待重算行的判据。合并成一个字段，这个差异就没了。
	PriorBaseCurrency currency.Code
}

// Output 是折算输出的三元组，与终版 §四 3 的三个字段一一对应。
//
// 三者必须**同进退**：不变式 1a 要求恒等式与联动同时成立，调用方应当整体
// 写回，不得只取其中一两个。分层 §六 L2 明写「输出三元组含规整后的汇率，
// 落库以它为准」。
//
// # 写回前必须先判 Changed
//
// 「整体写回」只对 Changed 为 true 的输出成立。TriggerAmortizeMonthsChanged
// 按 8.1 第 6 行三字段全不动，此时本结构体是**零值**、Changed 为 false，
// 三个字段都不是该行的现值，写回会把本位币金额抹成 0（见 Changed 的注释）。
type Output struct {
	// Rate 规整到 8 位后的折算汇率（T39）。
	//
	// **落库以本字段为准**，不是 Input.Rate。调用方若只写回 BaseAmount 而
	// 沿用原始汇率，库里的三字段就不再满足恒等式——用库里的汇率乘一遍
	// 得不到库里的本位币金额。
	//
	// Changed 为 false 时本字段是零值，不代表该行的汇率。
	Rate decimal.Decimal

	// BaseAmount 本位币金额 = 支出金额 × 规整后汇率，按 BaseCurrency 的
	// 小数位数四舍五入。
	//
	// Changed 为 false 时本字段是零值，**不代表该行的本位币金额为 0**。
	BaseAmount decimal.Decimal

	// BaseCurrency 折算本位币币种，即「这笔的本位币金额是哪个币种」。
	//
	// 六种触发中五种置为当前本位币，唯「改支出金额」保持该行原口径
	// （终版 8.1 第 1 行）。它是不变式 2 识别未收敛行的唯一依据。
	//
	// Changed 为 false 时本字段是零值，不代表该行的折算本位币币种。
	BaseCurrency currency.Code

	// Changed 报告本次触发是否产出了新的三字段值，即「要不要写回」。
	//
	// 六种触发中五种为 true；唯 TriggerAmortizeMonthsChanged 为 false——
	// 8.1 第 6 行规定它三字段全不动，仅刷新摊分结束月（那归 rule/amortize）。
	//
	// # 为什么必须有这个字段
	//
	// 本包的输出契约是「三字段同进退、整体写回」。若「不动」也用一个看起来
	// 正常的 Output 表达，调用方就无从区分「算出来的值」与「没算」：
	// 实测改摊分月数时若按契约整体写回，一笔 100.00 × 7.12345678 的支出，
	// 库里的本位币金额会从 712.35 变成 0，而恒等式 `本位币金额 = 支出金额 ×
	// 折算汇率` 当场被打破——且不报错。
	//
	// 用布尔量而不是「回传原值」来表达「不动」：本包收的入参里根本没有该行
	// 现有的 BaseAmount（见包注释「为什么不收 entity.Expense」——前提 6 要求
	// 本位币金额没有人工指定入口），因此**无法**回传它的现值。既然回传不了，
	// 就必须让「不要写」这件事在结构上无法被忽略，而不是回传一个半真半假的
	// 三元组。这与准则 6「约束优先用结构表达」一致。
	Changed bool
}

// Apply 按给定触发执行一次折算，是不变式 1a 的**唯一实现**。
//
// 纯函数：无 IO、无状态、无时间依赖，同样的入参恒得同样的出参，可并发调用。
//
// # 三步，顺序不可调换
//
//  1. **规整汇率到 8 位**（T39）。必须先于乘法：先乘后规整与先规整后乘的
//     结果**确实不同**（实测 100000000 × 7.123456785，先乘后按 2 位舍入得
//     712345678.5，先规整再乘得 712345679）。全仓库唯一规整点即此处。
//  2. **乘**。乘积不做中途舍入（decimal.Mul 保留全部有效位），只在第 3 步
//     舍入一次——中途舍入会引入二次误差。
//  3. **按本位币的小数位数四舍五入**。基准是「输出三元组里的那个
//     折算本位币币种」，不是固定的 2 位，也不总是当前本位币（见下）。
//
// # 舍入基准与 折算本位币币种 恒为同一个币种
//
// 这不是巧合而是本包刻意维持的性质：本位币金额的单位就是 折算本位币币种，
// 用 A 币种的位数算出一个标着 B 币种的金额，这个数在任何口径下都不成立。
// 故 TriggerAmountChanged 下两者同取 PriorBaseCurrency，其余五种同取
// BaseCurrency——代码里它们是同一个变量 base，从结构上杜绝了取错的可能。
//
// # 校验只做「不做就会静默出错」的那几项
//
// 四项校验（触发合法、本位币非零且属候选、汇率为正、规整后汇率仍为正）
// 无一是形式主义：每一项放行都会得到一个**不报错但错误**的金额，见各哨兵
// 错误的注释。反之，支出金额不校验——它允许为负、允许为零，量级已由
// decimal 的入口设界兜住，本包没有可加的判据。
//
// TriggerAmortizeMonthsChanged 是纯直通：三字段全不动，不做任何校验、
// 恒不返回错误，返回**零值 Output 且 Changed 为 false**。它不产生新数值，
// 对一行合法数据施加校验只会凭空造出失败路径。调用方必须先判 Changed
// 再决定是否写回，否则会把该行的本位币金额抹成 0（见 Output.Changed）。
func Apply(in Input, trigger Trigger) (Output, error) {
	b, ok := triggerTable[trigger]
	if !ok {
		return Output{}, fmt.Errorf("%w: %s", ErrTrigger, trigger)
	}

	//改摊分月数：三字段全不动（终版 8.1 第 6 行），返回零值 Output + Changed=false。
	//提前返回而不走下面的校验：本触发不产生新数值，无可算错之处。
	//
	//**不回传入参**：本包收不到该行现有的 BaseAmount（前提 6，见包注释
	//「为什么不收 entity.Expense」），凑不出一个真实的三元组。回传半真半假的
	//三元组会让按契约「整体写回」的调用方把本位币金额抹成 0，恒等式当场失守。
	//故这里只回答「不用写」，由 Changed 表达，三个字段一律留零值。
	if !b.recalc {
		return Output{Changed: false}, nil
	}

	//舍入基准 = 输出的 折算本位币币种，二者恒为同一个币种（见方法注释）
	base := in.BaseCurrency
	if !b.adoptBase {
		//「改支出金额」保持原口径：未收敛行必须仍可被不变式 2 识别
		base = in.PriorBaseCurrency
		if base.IsZero() {
			return Output{}, ErrPriorBaseCurrencyEmpty
		}
	}
	if base.IsZero() {
		return Output{}, ErrBaseCurrencyEmpty
	}
	//T38：本位币小数位 ≤ 2 是「按本位币位数算、按 2 位存」无损的前提。
	//3 位本位币算出的结果落 2 位列会被静默截断，故在此拒绝。
	if !base.CanBeBase() {
		return Output{}, fmt.Errorf("%w: code=%s, scale=%d, enabled=%t",
			ErrBaseCurrencyNotCandidate, base, base.Scale(), base.Enabled())
	}

	//汇率为零即 I-5 的「未填」，为负会让退款与支出符号翻转，一律拒绝
	if !in.Rate.IsPositive() {
		return Output{}, fmt.Errorf("%w: rate=%s", ErrRateNotPositive, in.Rate)
	}

	//第 1 步：规整到 8 位（T39，全仓库唯一规整点）
	rate := in.Rate.RoundRate()
	//规整后再判一次零：极小汇率（< 0.000000005）在 8 位下会被规整成 0，
	//这条零值路径只在规整之后才存在。放行会把本位币金额静默算成 0。
	if !rate.IsPositive() {
		return Output{}, fmt.Errorf("%w: 汇率 %s 规整到 %d 位后为 %s",
			ErrRateNotPositive, in.Rate, decimal.RateScale, rate)
	}

	//第 2、3 步：乘（不中途舍入）后按本位币位数四舍五入
	amount, err := in.Amount.Mul(rate).Round(base.Scale(), decimal.RoundHalfUp)
	if err != nil {
		//按当前币种表不可达：Round 只在「位数越界」或「舍入方式非法」时失败，
		//而本位币位数经 CanBeBase 后恒在 [0,2] ⊂ [0,MaxScale]、舍入方式是常量。
		//保留而非改 panic：它是币种表将来被改坏时的兜底，返回错误让调用方按自身
		//场景选档位，与本包「错误是哨兵、不选档位」一致。
		//TestRoundNeverFailsForAnyCurrency 锁死这条前提。
		return Output{}, fmt.Errorf("convert: 按本位币 %s 的 %d 位舍入失败: %w",
			base, base.Scale(), err)
	}

	return Output{
		Rate:         rate,
		BaseAmount:   amount,
		BaseCurrency: base,
		Changed:      true,
	}, nil
}
