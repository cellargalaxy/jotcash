package decimal

import (
	"fmt"
)

// RoundMode 是舍入方式，即承载中 round(值, 位数, 舍入方式) 的第三个参数。
//
// 零值不代表任何一种方式，传零值即报 ErrRoundMode：忘传参数必须响亮失败，
// 不能静默按某种方式把钱舍掉。这与 Kind 用零值表示「无错误」的取向不同——
// 那里零值有确切语义，这里没有。
type RoundMode uint8

const (
	// RoundHalfUp 四舍五入，且负数对称：1.005 舍到 2 位得 1.01，-1.005 得 -1.01。
	//
	// 本位币金额与折算汇率的舍入方式，是全仓库金额舍入的默认口径。
	RoundHalfUp RoundMode = iota + 1

	// RoundTruncate 向零截断，且负数对称：1.999 截到 2 位得 1.99，-1.999 得 -1.99。
	//
	// 供 8.2 摊分切分份额使用：先截断取每月份额，再由 rule/amortize 把尾差计入末月。
	// 若这里用四舍五入，各月份额之和可能超过原金额，尾差会变成负数。
	RoundTruncate
)

// String 返回舍入方式的可读名称，供错误信息与日志排查使用。
func (m RoundMode) String() string {
	switch m {
	case RoundHalfUp:
		return "四舍五入"
	case RoundTruncate:
		return "向零截断"
	default:
		return fmt.Sprintf("未知舍入方式(%d)", uint8(m))
	}
}

// checkScale 校验目标小数位数，只接受 [0, MaxScale]。
func checkScale(scale int32) error {
	if scale < 0 || scale > MaxScale {
		return fmt.Errorf("%w: %d, 允许范围 0~%d", ErrScaleInvalid, scale, MaxScale)
	}
	return nil
}

// Round 按指定位数与舍入方式舍入，即承载所要求的 round(值, 位数, 舍入方式)。
//
// 三处精度口径都经由本方法落地：
//   - 原币金额：scale 取该笔支出币种的小数位数（由 currency 提供）；
//   - 本位币金额：scale 取本位币的小数位数（恒 ≤ 2 位）；
//   - 折算汇率：scale 取 RateScale，即 RoundRate 的等价写法。
//
// 位数由调用方传入而不在本包写死（RateScale 除外）：币种小数位数的取值归 currency，
// 按约定 5 不得进入基础层。
func (d Decimal) Round(scale int32, mode RoundMode) (Decimal, error) {
	if err := checkScale(scale); err != nil {
		return Decimal{}, err
	}
	switch mode {
	case RoundHalfUp:
		return wrap(d.v.Round(scale)), nil
	case RoundTruncate:
		return wrap(d.v.Truncate(scale)), nil
	default:
		return Decimal{}, fmt.Errorf("%w: %s", ErrRoundMode, mode)
	}
}

// RoundRate 把汇率规整到 RateScale 位小数、四舍五入。
//
// 不接收位数参数、也不会失败：汇率精度是定死的常量，若让每个调用点各传一次位数，
// 迟早出现某处传了别的值而无人察觉。
//
// 规整的**时机**由 rule/convert 统一决定（自动获取与用户手填共用同一个规整点，
// 保证两条入口的汇率形态一致），本方法只负责规整这一步算术。
func (d Decimal) RoundRate() Decimal {
	return wrap(d.v.Round(RateScale))
}

// Scale 返回有效小数位数，即剥掉尾随零后实际需要的位数。
// 1.50 与 1.500 一律返回 1，0 与 0.00 一律返回 0。
//
// 取「有效位数」而非底层指数：指数受构造方式影响（1.50 的指数是 -2、1.5 是 -1），
// 若直接用指数判定「该值需要几位小数」，同一个数值会因来源不同得出不同结论——
// 从账单解析来的 1.50 会被判为需要 2 位，而计算得出的 1.5 被判为需要 1 位。
//
// 实现上按十进制文本一次数完尾随零，而非逐位做 big.Int 除法。两者结果完全相同，
// 但除法版本对 n 位系数要做 n 次大整数除法，整体 O(n²)：这是 Parse 的量级校验
// 必经的一步，而校验前的值尚未设界，「0.1 后接 12 万个 0」这类文本会在这里耗掉
// 约 1.7 秒——设界本身反而成了拒绝服务的入口。改为一次 String 转换后线性扫描，
// 同一输入降到约 1.2 毫秒（实测提速 226~1374 倍，输入越长差距越大）。
func (d Decimal) Scale() int32 {
	exp := d.v.Exponent()
	if exp >= 0 {
		return 0
	}
	//系数为零时不必剥零：任何位数都能无损容纳 0
	coefficient := d.v.Coefficient()
	if coefficient.Sign() == 0 {
		return 0
	}
	//十进制文本的尾随零个数即可剥掉的位数；负号只可能出现在首位，不影响从尾部计数
	text := coefficient.String()
	zeros := int32(0)
	for i := len(text) - 1; i >= 0 && text[i] == '0'; i-- {
		zeros++
	}
	//剥掉的位数可能超过小数位数（如 1e3：系数 1、指数 3 已在上面返回；
	//又如系数 100、指数 -1 表示 10，此时 zeros=2 而 -exp=1），故下取到 0
	if scale := -exp - zeros; scale > 0 {
		return scale
	}
	return 0
}

// FitsScale 报告该值能否被 scale 位小数无损容纳。
//
// 两处用途：
//   - store/sqlite 落库前判定金额与汇率是否符合列口径（金额 2 位、汇率 8 位）；
//   - 校验「按本位币小数位数舍入的结果能被 2 位定点无损存储」这一前提。
//     本位币小数位数恒 ≤ 2，故该判定必然通过——这是「算按本位币位数、存按 2 位」
//     两个口径能够共存的原因。
//
// scale 非法时返回 false 而不报错：本方法是判定用的谓词，
// 调用方拿到 false 后走的分支与「不能无损容纳」完全一致。
func (d Decimal) FitsScale(scale int32) bool {
	if checkScale(scale) != nil {
		return false
	}
	return d.Scale() <= scale
}
