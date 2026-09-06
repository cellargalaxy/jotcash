package decimal

// 本文件承载 §六 L0 `base/decimal` 行的三处精度口径与 `round(值, 位数, 舍入方式)`。
// 之所以与 decimal.go 分开：那边是「数是什么、怎么算」，这边是「按什么口径规整」，
// 后者每一条都直接对应决策文档里的一条业务规则，改动理由与前者完全不同。

import (
	"github.com/pkg/errors"
)

// RoundMode 舍入方式，即 §六 L0 `round(值, 位数, 舍入方式)` 的第三个参数。
//
// 只定义业务实际用到的两种，不做通用舍入库：多一种就多一条「哪里该用哪种」的判断，
// 而金额舍入方式选错属于典型的静默出错。
type RoundMode uint8

const (
	// RoundHalfUp 四舍五入（半值远离零，负数对称：2.5→3、-2.5→-3）。
	//
	// 三处精度口径全部指定「四舍五入」，即中文语境的四舍五入，
	// **不是**银行家舍入（后者 2.5→2、3.5→4，底层库另有 RoundBank，本包不用）。
	// 负数对称是必需的：终版前提 2 允许负金额表示退款/冲正，
	// 若负数按「向上」舍，一笔退款与其对应的支出会得到不对称的结果，
	// I-7 逐笔重算后正负相消不为零。
	RoundHalfUp RoundMode = iota + 1

	// RoundTruncate 向零截断（2.9→2、-2.9→-2）。
	// 供 8.2 摊分的前 N−1 月备选，见 DivTruncate 的说明。
	RoundTruncate
)

// Valid 舍入方式是否为已定义值。
func (m RoundMode) Valid() bool {
	return m == RoundHalfUp || m == RoundTruncate
}

// String 返回中文名称，仅用于诊断信息（约定 3：注释与日志一律中文）。
func (m RoundMode) String() string {
	switch m {
	case RoundHalfUp:
		return "四舍五入"
	case RoundTruncate:
		return "向零截断"
	default:
		return "未知舍入方式"
	}
}

const (
	// RateScale 折算汇率的小数位数：8 位（T39）。
	//
	// 业务口径，非技术上限，故写死在本包、不进 `base/config`（约定 8）。
	// T39 另定「规整点唯一」——汇率的 8 位规整只发生在 `rule/convert` 一处，
	// 本包只提供算子，不代替它决定何时规整。
	RateScale int32 = 8

	// BaseAmountStoreScale 本位币金额的**存储**精度：2 位定点（T35）。
	//
	// 注意与「算」的区别（§三 问题 1）：计算按本位币的 `currency.小数位数` 舍入
	// （T38 保证 ≤ 2 位），得到 0/1/2 位的结果后由本常量无损容纳落库。
	// 该值**不得**用作舍入基准，否则本位币为 JPY（0 位）时会算出该币种不存在的面额。
	BaseAmountStoreScale int32 = 2
)

// checkScale 校验小数位数是否在允许范围内。
// 负数位数（如「舍到十位」）在本仓库没有业务场景，一律拒绝：
// 金额舍到十位是明显的口径错误，静默接受会把它变成难查的数据问题。
func checkScale(scale int32) error {
	if scale < 0 {
		return errors.Wrapf(ErrScale, "小数位数 %d 为负", scale)
	}
	if scale > MaxScale {
		return errors.Wrapf(ErrScale, "小数位数 %d 超过上限 %d", scale, MaxScale)
	}
	return nil
}

// Round 即 §六 L0 承载的 `round(值, 位数, 舍入方式)`，是本包**唯一的舍入总入口**。
//
// 三处精度口径全部经由它落地（RoundAmount / RoundRate 是它的具名封装）。
// 位数非法返回 ErrScale，舍入方式非法返回 ErrRoundMode——后者不设默认分支是刻意的：
// 零值 RoundMode 若被默认成四舍五入，`rule/amortize` 忘传参数时会静默按四舍五入走，
// 而 8.2 的前 N−1 月可能恰恰要求截断。
func Round(d Decimal, scale int32, mode RoundMode) (Decimal, error) {
	if err := checkScale(scale); err != nil {
		return Decimal{}, err
	}
	switch mode {
	case RoundHalfUp:
		return wrap(d.v.Round(scale)), nil
	case RoundTruncate:
		return wrap(d.v.Truncate(scale)), nil
	default:
		return Decimal{}, errors.Wrapf(ErrRoundMode, "%d", uint8(mode))
	}
}

// RoundAmount 按给定小数位数对金额四舍五入，承载精度口径①与②的「算」。
//
//	原币金额   → scale 取**支出币种**的 `currency.小数位数`（口径①）
//	本位币金额 → scale 取**本位币**的 `currency.小数位数`（口径②「算」，T38 保证 ≤ 2 位）
//
// 本包不认识币种（约束 2），scale 一律由调用方从 `currency` 取来传入。
func RoundAmount(d Decimal, currencyScale int32) (Decimal, error) {
	return Round(d, currencyScale, RoundHalfUp)
}

// RoundRate 把折算汇率规整为 8 位小数、四舍五入，承载精度口径③（T39）。
//
// 唯一调用点应为 `rule/convert`（T39「规整点唯一」）：汇率必须**先规整再相乘**，
// 顺序颠倒会让「同一天同一对币种」的两笔折算因汇率源返回位数不同而得到不同结果，
// I-7 逐笔重算便无法复现原值。
func RoundRate(d Decimal) (Decimal, error) {
	return Round(d, RateScale, RoundHalfUp)
}

// Truncate 按给定小数位数向零截断。
func Truncate(d Decimal, scale int32) (Decimal, error) {
	return Round(d, scale, RoundTruncate)
}

// FixedString 返回**固定** scale 位小数的定点文本（补零、不省略尾零），供落库与对外展示。
//
// 承载 §六 L4 `store` 行「金额与汇率禁用 REAL，一律整数最小单位或定点文本」。
//
// 关键取舍：**无法无损表示时返回 ErrPrecisionLoss，而不是静默舍入**。
// 底层的 StringFixed 会把 1.567 直接写成 "1.57"，若落库路径依赖它，
// 一个「算的时候按 3 位、存的时候按 2 位」的口径错配就会表现为「库里的钱少了几分」，
// 且没有任何报错——正是准则 2 要求当场暴露的那类静默错误。
// 因此本函数只负责**断言 + 格式化**：真要舍入，调用方须显式先调 Round。
func FixedString(d Decimal, scale int32) (string, error) {
	if err := checkScale(scale); err != nil {
		return "", err
	}
	if d.Scale() > scale {
		return "", errors.Wrapf(ErrPrecisionLoss, "值 %s 有 %d 位小数，目标 %d 位", d.String(), d.Scale(), scale)
	}
	return d.v.StringFixed(scale), nil
}

// StoreBaseAmount 把本位币金额规整为落库形态：2 位定点文本（T35）。
//
// 入参应当**已经**按本位币的 `currency.小数位数` 舍过（口径②的「算」，即 RoundAmount）。
// 本函数不再舍一次，只断言「0/1/2 位结果无损容纳」这条 T35 边界真的成立：
// 若传进来的值有 3 位小数，说明上游漏了按本位币小数位舍入，或本位币被配成了
// 小数位 > 2 的币种（T38 明确把本位币候选限定为 ≤ 2 位），两者都必须当场暴露。
func StoreBaseAmount(d Decimal) (string, error) {
	return FixedString(d, BaseAmountStoreScale)
}

// StoreRate 把折算汇率规整为落库形态：8 位定点文本（T39）。
// 入参应当已经过 RoundRate；同理只断言无损，不代为舍入。
func StoreRate(d Decimal) (string, error) {
	return FixedString(d, RateScale)
}
