package amortize

import (
	"fmt"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// Shares 是一笔金额按 8.2 均分后的全部月份额，即「金额 ÷ 月数，尾差归末月」
// 这一次切分的结果。
//
// # 只存两个值，不展开数组
//
// 摊分的数学结构决定了 N 个月份额里只有**两个**不同的值：前 N−1 月都是商，
// 末月是商 + 尾差。故本类型只存月数、商、尾差、总额与币种五个字段，
// At(i) 是一次比较加一次可能的加法，与月数无关。
//
// 这条取舍由终版 H-1「摊分月数无上限」倒逼：一笔摊 600 个月的支出，
// 若每次 J 域穿刺都展开 600 个 decimal.Decimal 只为取其中 1 个月，
// 代价与月数成正比且全部浪费。Rows()（H-4 逐月展示）仍然提供，
// 但只在真的要逐月列出时才付出那份分配。
//
// # 零值不可用
//
// 零值 Shares 的月数为 0，At 一律返回 ErrShareIndex、Rows 返回 nil。
// 必须经 NewShares 构造——那里才有校验。这与 calendar.MonthRange
// 「私有字段 + 构造函数」的取向一致：一个非零 Shares 必然是校验过的。
type Shares struct {
	// months 摊分月数，恒 ≥ 1（由 NewShares 保证）。
	months int

	// quotient 每月份额（商），即前 months−1 个月各自的金额。
	quotient decimal.Decimal

	// tail 尾差（余数），恒与 total 同号或为零，**只加在末月**。
	tail decimal.Decimal

	// total 被均分的原金额，供 Total() 回溯与可加性自检。
	total decimal.Decimal

	// code 均分基准币种，其 Scale() 即本次切分用的小数位数。
	code currency.Code
}

// NewShares 按 8.2 把金额均分到指定月数，尾差固定计入末月。
//
// 这是本包 8.2 那一半的**唯一实现**。原币与本位币各调用一次、各自独立均分
// （见包注释「均分基准是币种小数位数」），本函数不知道自己算的是哪一种——
// 它只认「金额 + 月数 + 币种」。
//
// # 算法：一次带余除法
//
//	商, 余 = decimal.QuoRem(金额, 月数, 币种小数位数)
//	第 1..N−1 月 = 商
//	第 N 月       = 商 + 余
//
// 因 QuoRem 恒满足「商 × 除数 + 余 = 被除数」，各月份额之和**精确等于**
// 原金额，不会因舍入多出或丢失分位（该恒等式由 base/decimal 的
// TestQuoRemIdentity 与本包的 TestSharesSumEqualsTotal 双向锁死）。
//
// # 负金额天然对称，无额外分支
//
// QuoRem 的商向零截断、余数与被除数同号，故 -100.00 摊 3 个月直接得到
// -33.33/-33.33/-33.34，与正金额镜像。终版 H-2「负金额（退款）规则完全
// 对称」因此不需要任何符号判断——**刻意不写**符号分支：写了反而会在
// 「金额为负、尾差为零」这类边界上引入不对称。
//
// # 三项校验，每一项放行都会静默出错
//
//	月数 < 1     → QuoRem 除零 / 切片下标越界
//	币种为零值    → Scale() 返回 0 与 JPY 的 0 位无法区分，712.35 被切成整数份额
//	金额位数超币种 → 末月份额突破币种位数（100.005 按 2 位切 3 份，末月得 33.345）
//
// 三条的详细后果见各哨兵错误的注释。反之，金额本身不校验正负、不校验零：
// 负金额是退款、零金额终版未禁止，且 0 均分到任何月数都是 0，无歧义。
func NewShares(total decimal.Decimal, months int, code currency.Code) (Shares, error) {
	if months < 1 {
		return Shares{}, fmt.Errorf("%w: months=%d", ErrMonthsNotPositive, months)
	}
	//必须先判零值币种：Scale() 对零值返回 0，与 JPY 的 0 位无法区分
	if code.IsZero() {
		return Shares{}, ErrCurrencyEmpty
	}

	scale := code.Scale()
	//金额位数必须能被该币种无损容纳，否则末月份额会突破币种位数。
	//这是本包拦下的一条静默出错路径，见 ErrAmountScale 的注释。
	if !total.FitsScale(scale) {
		return Shares{}, fmt.Errorf("%w: amount=%s(%d 位), currency=%s(%d 位)",
			ErrAmountScale, total, total.Scale(), code, scale)
	}

	//按币种位数取商与余：商即每月份额，余即尾差
	quotient, tail, err := total.QuoRem(decimal.FromInt(int64(months)), scale)
	if err != nil {
		//月数已判正、位数来自 currency 表（恒在 0~4），正常走不到这里。
		//仍然透传而不吞掉：走到了就说明上面两条前置判断出现了盲区。
		return Shares{}, fmt.Errorf("amortize: 按 %s 的 %d 位均分 %s 到 %d 个月失败: %w",
			code, scale, total, months, err)
	}

	return Shares{
		months:   months,
		quotient: quotient,
		tail:     tail,
		total:    total,
		code:     code,
	}, nil
}

// Months 返回摊分月数，即份额个数。零值 Shares 返回 0。
func (s Shares) Months() int { return s.months }

// Total 返回被均分的原金额。
//
// 供调用方自检可加性（各月份额之和必须精确等于它），也供 J-5 下钻时
// 展示「份额金额 + 来源支出」中的后者。
func (s Shares) Total() decimal.Decimal { return s.total }

// Currency 返回本次均分的基准币种。
//
// Schedule 同时持有原币与本位币两个 Shares，本方法用于在调用点确认
// 「手上这个是哪一种」——两者位数可以不同，取错就是金额错。
func (s Shares) Currency() currency.Code { return s.code }

// Quotient 返回每月份额（商），即前 Months()−1 个月各自的金额。
//
// 末月不等于它（还要加尾差），取末月请用 At(Months()-1)。
func (s Shares) Quotient() decimal.Decimal { return s.quotient }

// Tail 返回尾差（余数），即末月比其余各月多出的部分。
//
// 恒与 Total() 同号或为零：正金额的尾差非负，负金额（退款）的尾差非正。
// 整除时为零，此时各月份额完全相同。
func (s Shares) Tail() decimal.Decimal { return s.tail }

// IsZero 报告是否为零值 Shares（未经 NewShares 构造）。
func (s Shares) IsZero() bool { return s.months == 0 }

// At 返回第 index 个摊分月的份额，index 从 0 开始计。
//
// 尾差固定计入**末月**（index == Months()-1），其余各月一律是商。
// 这是 8.2「尾差归末月」在代码里的字面形态，也是本类型只需存两个值的原因。
//
// index 越界返回 ErrShareIndex 而非 panic：下标可能来自 J 域按月定位的
// 计算结果（如 目标月.MonthsSince(起始月)），一次算错不应崩掉整个统计请求。
//
// 时间复杂度 O(1)，与月数无关。
func (s Shares) At(index int) (decimal.Decimal, error) {
	if index < 0 || index >= s.months {
		return decimal.Decimal{}, fmt.Errorf("%w: index=%d, months=%d",
			ErrShareIndex, index, s.months)
	}
	//末月扛尾差，其余各月取商
	if index == s.months-1 {
		return s.quotient.Add(s.tail), nil
	}
	return s.quotient, nil
}

// All 按月序返回全部份额，长度恒等于 Months()。零值 Shares 返回 nil。
//
// 供 H-4 逐月展示与「份额之和 = 原额」的整体自检。**这是本类型唯一的
// O(月数) 分配点**——摊分月数无上限（H-1），调用方应当确认自己真的需要
// 逐月列出，按月取值请用 At（O(1)）。
func (s Shares) All() []decimal.Decimal {
	if s.IsZero() {
		return nil
	}
	out := make([]decimal.Decimal, s.months)
	for i := range out {
		out[i] = s.quotient
	}
	//尾差归末月：只此一处修改，与 At 的分支同源
	out[s.months-1] = s.quotient.Add(s.tail)
	return out
}
