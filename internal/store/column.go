package store

import (
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 数值列口径（承载 ⑥）——金额与汇率一律非浮点，且**位数分三套**。
//
// # 承载的措辞与实测的冲突，以及裁定
//
// 分层 §六 L3 store ⑥ 写的是「金额 2 位定点、**汇率 8 位定点**（T39），一律
// 非浮点」。这句话里的「金额」按字面套到 Expense.支出金额 上会**静默丢数**，
// 已实测：
//
//	KWD 12.345 → Units(2) → 报错「值 12.345 需 3 位, 目标 2 位」
//	KWD 12.345 → Units(3) → 12345 ✓
//
// KWD 是 3 位小数币种，而 currency 表刻意保留了它（见 currency/table.go
// 「为什么保留 KWD」），分层 §六 L1.0 亦明写「**支出币种不受此限**（3 位币种
// 仍可正常记账与摊分）」。所以「2 位」讲的只能是**本位币金额**那一列。
//
// 这个读法有三处独立印证，不是本包的自行发挥：
//
//   - decimal.BaseAmountStoreScale 的注释就叫「**本位币**金额的存储精度」，
//     并写明「与『计算精度』是两件事」；
//   - entity.Expense.BaseAmount 的注释：「**算**按本位币的 currency.Scale()
//     四舍五入（T38 保证 ≤ 2 位），**存**为 decimal.BaseAmountStoreScale」；
//   - rule/dedup.Key.Amount 的注释已经为原币金额做过同一个裁定：「**不是**定点
//     文本：8.3 的金额是**原币**金额，位数随支出币种而定——KWD 是 3 位，按本位币
//     存储的 2 位会直接报错」。
//
// 故三套口径如下，本包不制造第四套：
//
//	列                        位数                              基准来源
//	Expense.支出金额          随支出币种 currency.Code.Scale()  0（JPY）/2（CNY）/3（KWD）
//	Expense.本位币金额        恒 2                              decimal.BaseAmountStoreScale
//	Expense.折算汇率          恒 8                              decimal.RateScale（T39）
//
// # 为什么推荐「整数最小单位」而不是「定点文本」
//
// 分层 §六 L4 允许 store/sqlite 用「整数最小单位**或**定点文本」两种形态落地。
// 本包的三个转换函数都产出整数最小单位，理由是**定点文本的字典序不等于数值序**
// （实测）：
//
//	定点文本按字典序   [-1.00 100.00 12.00 2.00 9.99]
//	数值序应为        [-1.00 2.00 9.99 12.00 100.00]
//	最小单位按数值序   [-100 200 999 1200 10000]      ✓
//
// F-3 明写「支持按日期、**金额**等排序」，F-4 有金额区间筛选，F-6 要按币种分组
// 合计——三者都依赖数值序。负数让这个问题更糟：'-' 的码位小于所有数字，一列
// 混有正负金额的定点文本，字典序会把负数排到最前，看起来「像是对的」，直到某天
// 出现 -100.00 与 -9.99 的相对顺序（字典序下前者在前，数值序下后者在前）。
//
// **日期与年月一侧相反，刻意不用整数**：calendar.Date/Month 是定长文本
// （calendar.MinYear 的注释写明「文本形态用 %04d 定长格式化年份，而定长是
// 『字典序 = 时间序』这条被 store/sqlite 依赖的性质的前提」），直接用它们的
// Value() 形态即可，既可走索引又保持可读。
//
// # 对 L4 与 L6 的一条硬约束：渲染与落库都走 StringFixed，不要用 String
//
// decimal 对小数位数做**规范化**（剥掉尾随零），已实测：
//
//	decimal.Parse("0.00").Scale()         = 0，String() = "0"
//	decimal.Parse("100.50").Scale()       = 1，String() = "100.5"
//	decimal.FromUnits(100000000, 8).Scale() = 0，String() = "1"
//
// 即 Scale() 返回的是「表示该数值所需的最小位数」，**不是**它被构造时声明的位数。
// 于是有两条推论：
//
//  1. 「读回值的 Scale() 应等于列位数」是一条**不成立**的性质。判断能否无损落库
//     要用 FitsScale(scale)，判断往返是否稳定要比最小单位整数或用 Equal，
//     都不能比 Scale()。TestRoundTrip 按这三条断言。
//  2. 金额与汇率对外呈现（F-3 同屏展示、F-7 导出、K-3 导出）**必须**走
//     StringFixed(位数)。用 String() 会把 100.50 输出成 "100.5"、0.00 输出成 "0"，
//     在对账场景下是显性错误。
const (
	// BaseAmountScale 是本位币金额列的小数位数，恒 2 位。
	//
	// 直接取自 decimal.BaseAmountStoreScale，不另定常量值——那会变成第二处口径。
	// 这里给它一个 store 侧的名字，是为了让 L4 的建表脚本有一个语义明确的引用点。
	BaseAmountScale = decimal.BaseAmountStoreScale

	// RateScale 是折算汇率列的小数位数，恒 8 位（T39）。
	//
	// 同样取自 decimal.RateScale。T39 明写「规整点唯一」：rule/convert 在做乘法
	// 前把入参汇率规整到 8 位，其输出即落库值。**本包不做规整**，只做「能否
	// 无损落这一列」的判定——若在此再规整一次，就出现了第二个规整点，
	// T39 想避免的「库里存 12 位、算的时候用 8 位」会以另一种形式回来。
	RateScale = decimal.RateScale
)

// 数值列口径的文案键。
//
// 两个键共用 errs.KindInvalidInput 一档（HTTP 400）：能走到这里的值都已经过了
// decimal 的入口校验，剩下的越界形态一律是「用户填的金额/汇率太大」，属于输入
// 错误而非系统故障。靠键区分身份、不另设数字业务码，与 base/errs 包注释一致。
const (
	// KeyAmountOutOfRange 是「金额超出该列可表示的范围」的文案键。
	//
	// 占位参数：Amount（原值的规范文本）、Scale（目标列位数）、
	// Currency（币种代码，仅支出金额一侧有值）。
	KeyAmountOutOfRange = "store.amount_out_of_range"

	// KeyRateOutOfRange 是「汇率超出汇率列可表示的范围」的文案键。
	//
	// 占位参数：Rate（原值的规范文本）、Scale（恒 8）。
	KeyRateOutOfRange = "store.rate_out_of_range"
)

// AmountUnits 把**原币**金额换算成该币种的整数最小单位，供 L4 落库与拼查询参数。
//
// 位数取 cur.Scale()，即 §四 表里的第一套口径。cur 为零值时返回输入错误——
// 支出币种在 entity.Expense 上是必填（终版 §四 3），零值意味着调用方漏填，
// 此时无法确定位数，猜任何一个都可能丢数。
//
// # 为什么这个函数必须存在于端口层
//
// 它是「数值列口径」这项承载的**可执行形态**。若只在注释里写「金额按币种位数
// 落库」，每个 L4 实现都要自己写一遍换算与越界判定，而下面这条边界极易被漏掉。
//
// # decimal 的入口比列容量宽，这里是唯一的拦截点
//
// decimal 的入口上限是 MaxIntDigits = 16 位整数，但 int64 最小单位列的容量随
// 位数收缩（实测）：
//
//	scale=0 → 安全整数位 16 / 入口 16
//	scale=2 → 安全整数位 16 / 入口 16
//	scale=3 → 安全整数位 15 / 入口 16   ← 入口宽于列容量
//	scale=8 → 安全整数位 10 / 入口 16   ← 入口宽于列容量
//
// 即一个 16 位整数的 KWD 金额、或一个 11 位整数的汇率，**能过 decimal 的入口，
// 到落库时才溢出**。若不在此拦下，故障形态是「用户提交一批明细 → 前 N 条入库
// 成功 → 第 N+1 条报 500」，且 D-7 是整批事务语义，整批回滚而错误信息只说
// 「存储层失败」。这与 rule/passwd 那一轮修掉的缺陷同类：**错误必须在造成它的
// 那一次请求里、以能指明原因的形态失败**。
//
// 返回的 KindInvalidInput 会被 middleware 映射成 400 并经 i18n 取词，用户看到的
// 是「金额超出可记录范围」，而不是一条系统错误。
func AmountUnits(amount decimal.Decimal, cur currency.Code) (int64, error) {
	if cur.IsZero() {
		return 0, errs.NewInvalidInput(KeyAmountOutOfRange, errs.Params{
			"Amount":   amount.String(),
			"Scale":    nil,
			"Currency": "",
		})
	}
	units, err := amount.Units(cur.Scale())
	if err != nil {
		//把 decimal 的原始错误挂在 cause 上：errs.Error 经 Unwrap 暴露它，
		//日志里能看到「需 3 位, 目标 2 位」这类精确诊断，而前端只取文案键。
		return 0, errs.Wrap(err, errs.KindInvalidInput, KeyAmountOutOfRange, errs.Params{
			"Amount":   amount.String(),
			"Scale":    cur.Scale(),
			"Currency": cur.String(),
		})
	}
	return units, nil
}

// BaseAmountUnits 把**本位币**金额换算成 2 位定点的整数最小单位。
//
// 位数恒为 BaseAmountScale，不随本位币币种变化。这不是简化：T38 保证本位币候选
// 的小数位数 ≤ 2，而 0/1/2 位的算出结果都能被 2 位列无损容纳，故该转换无损
// （decimal.BaseAmountStoreScale 的注释即如此论证）。
//
// 与 AmountUnits 分成两个函数而不是共用一个带 scale 参数的函数：两者的位数来源
// 完全不同（一个来自支出币种、一个是常量），合并后调用方要自己决定传哪个位数，
// 而传错不会报错——只会静默按错误的位数落库。分成两个函数让「这是原币还是
// 本位币」在调用点就必须表态。
func BaseAmountUnits(baseAmount decimal.Decimal) (int64, error) {
	units, err := baseAmount.Units(BaseAmountScale)
	if err != nil {
		return 0, errs.Wrap(err, errs.KindInvalidInput, KeyAmountOutOfRange, errs.Params{
			"Amount":   baseAmount.String(),
			"Scale":    int32(BaseAmountScale),
			"Currency": "",
		})
	}
	return units, nil
}

// RateUnits 把折算汇率换算成 8 位定点的整数最小单位（T39）。
//
// **本函数不做规整**，只判定「能否无损落 8 位列」。T39 定的规整点唯一在
// rule/convert：它在做乘法前把入参汇率规整到 8 位，其输出三元组中的汇率即落库
// 值。若本函数顺手调一次 Round，就出现了第二个规整点——两处规整在舍入方式一致
// 时看起来无害，直到某一处改了舍入方式，而那时的症状是「金额与汇率对不上恒等式」
// ，排错要同时怀疑两个包。
//
// 因此传进来的汇率若位数超过 8 位，是**调用方违约**（没走 rule/convert），
// 这里返回错误而不是替它规整。
func RateUnits(rate decimal.Decimal) (int64, error) {
	units, err := rate.Units(RateScale)
	if err != nil {
		return 0, errs.Wrap(err, errs.KindInvalidInput, KeyRateOutOfRange, errs.Params{
			"Rate":  rate.String(),
			"Scale": int32(RateScale),
		})
	}
	return units, nil
}

// AmountFromUnits 把该币种的整数最小单位读回 decimal，是 AmountUnits 的逆向。
//
// 供 L4 的读路径使用。与写路径成对提供，是为了让「往返无损」这条性质有一个可被
// 测试直接调用的形态（见 column_test.go 的 TestRoundTrip）——若只提供写向转换，
// 各 L4 实现的读回逻辑各写一遍，位数用错时表现为「读出来的金额差了 10 倍」。
//
// 币种为零值时返回输入错误，理由同 AmountUnits。
func AmountFromUnits(units int64, cur currency.Code) (decimal.Decimal, error) {
	if cur.IsZero() {
		return decimal.Decimal{}, errs.NewInvalidInput(KeyAmountOutOfRange, errs.Params{
			"Amount":   nil,
			"Scale":    nil,
			"Currency": "",
		})
	}
	value, err := decimal.FromUnits(units, cur.Scale())
	if err != nil {
		return decimal.Decimal{}, errs.Wrap(err, errs.KindInvalidInput, KeyAmountOutOfRange, errs.Params{
			"Amount":   nil,
			"Scale":    cur.Scale(),
			"Currency": cur.String(),
		})
	}
	return value, nil
}

// BaseAmountFromUnits 把 2 位定点的整数最小单位读回 decimal。
func BaseAmountFromUnits(units int64) (decimal.Decimal, error) {
	value, err := decimal.FromUnits(units, BaseAmountScale)
	if err != nil {
		return decimal.Decimal{}, errs.Wrap(err, errs.KindInvalidInput, KeyAmountOutOfRange, errs.Params{
			"Amount":   nil,
			"Scale":    int32(BaseAmountScale),
			"Currency": "",
		})
	}
	return value, nil
}

// RateFromUnits 把 8 位定点的整数最小单位读回 decimal。
//
// # 错误分支在当前口径下不可达，但刻意保留
//
// 实测：scale=8 时 int64 全域都在 decimal 的 16 位整数上限内
// （MaxInt64 → 92233720368.54775807，整数部分 11 位；MinInt64 同量级），
// 故 decimal.FromUnits 在此不会失败——本函数的错误分支覆盖率因此上不去
// （单包覆盖率停在 98.6%，缺口即此处）。
//
// 不删它的理由是：RateScale 是从 decimal 取的常量（见上方 TestScaleConstantsCome
// FromDecimal），一旦 T39 调整汇率位数（例如降到 4 位），MaxInt64 换算后就是 15 位
// 整数、再降就会越界，那时这个分支立刻变为可达。删掉它换来的是一行覆盖率，
// 代价是那次调整会以「读出一个错误汇率」的形态静默出错。
func RateFromUnits(units int64) (decimal.Decimal, error) {
	value, err := decimal.FromUnits(units, RateScale)
	if err != nil {
		return decimal.Decimal{}, errs.Wrap(err, errs.KindInvalidInput, KeyRateOutOfRange, errs.Params{
			"Rate":  nil,
			"Scale": int32(RateScale),
		})
	}
	return value, nil
}
