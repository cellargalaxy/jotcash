package calendar

import "fmt"

// MonthRange 是闭区间 [起始月, 结束月] 的月区间，承载分层设计中
// 「区间运算（起始月 + N − 1、起始月 ≤ M ≤ 结束月）」这一项。
//
// 两个用途：
//   - rule/amortize 派生 Expense.摊分结束月（不变式 1b：结束月 = 起始月 + 月数 − 1）
//     与 H-4 的「覆盖月份区间」；
//   - store 的摊分区间穿刺判据（8.2：起始月 ≤ M 且 结束月 ≥ M）。
//
// 零值表示「未指定」。内部字段私有且只能经 NewMonthRange / NewMonthRangeBetween
// 构造，因此一个非零 MonthRange 必然满足 起始月 ≤ 结束月 且两端均为合法年月，
// 上层不必再校验区间方向。
//
// 注意：本类型只做纯区间运算，不含任何业务规则——摊分月数默认 1、无上限
// （H-1、前提 5）属业务语义，归 rule/amortize；本包只回答「给定起始月与月数，
// 区间是什么」。
type MonthRange struct {
	start Month
	end   Month
}

// NewMonthRange 按「起始月 + 月数」构造闭区间，即不变式 1b：
// 结束月 = 起始月 + 月数 − 1。月数为 1 时区间退化为起始月本身（不摊分）。
//
// 月数非正返回 ErrMonthCount；起始月为零值返回 ErrMonthValue；
// 结束月年份越界返回 ErrYearRange。
func NewMonthRange(start Month, count int) (MonthRange, error) {
	if start.IsZero() {
		return MonthRange{}, ErrMonthValue
	}
	if count < 1 {
		return MonthRange{}, ErrMonthCount
	}
	// 减 1 是不变式 1b 的字面实现：起始月本身即第 1 个摊分月。
	end, err := start.AddMonths(count - 1)
	if err != nil {
		return MonthRange{}, err
	}
	return MonthRange{start: start, end: end}, nil
}

// NewMonthRangeBetween 按起止月构造闭区间，供已落库的
// 摊分起始月 / 摊分结束月两字段还原为区间时使用。
//
// 任一端为零值返回 ErrMonthValue；结束月早于起始月返回 ErrMonthOrder。
func NewMonthRangeBetween(start, end Month) (MonthRange, error) {
	if start.IsZero() || end.IsZero() {
		return MonthRange{}, ErrMonthValue
	}
	if end.Before(start) {
		return MonthRange{}, ErrMonthOrder
	}
	return MonthRange{start: start, end: end}, nil
}

// Start 返回起始月；零值区间返回零值 Month。
func (r MonthRange) Start() Month { return r.start }

// End 返回结束月；零值区间返回零值 Month。
func (r MonthRange) End() Month { return r.end }

// IsZero 报告是否为零值区间。
func (r MonthRange) IsZero() bool { return r.start.IsZero() && r.end.IsZero() }

// Len 返回区间覆盖的月数，等于摊分月数；零值区间返回 0。
// 与 NewMonthRange 的 count 互为逆运算，供摊分份额均分时确定份数。
func (r MonthRange) Len() int {
	if r.IsZero() {
		return 0
	}
	return r.end.MonthsSince(r.start) + 1
}

// Contains 报告 m 是否落在闭区间内，即 8.2 的区间穿刺判据
// 「起始月 ≤ m 且 结束月 ≥ m」。两端闭合：起始月与结束月本身都算命中。
//
// 零值区间与零值 m 一律不命中——「未指定」不参与穿刺。
func (r MonthRange) Contains(m Month) bool {
	if r.IsZero() || m.IsZero() {
		return false
	}
	return !m.Before(r.start) && !m.After(r.end)
}

// Months 按时间正序返回区间覆盖的全部月份，供 H-4「覆盖月份区间 + 每月分摊金额」
// 的即时预览与 J 域摊分份额派生逐月对齐。零值区间返回 nil。
//
// 摊分月数无上限（H-1、前提 5），本方法的返回长度即等于 Len()，
// 最坏情况受年份上界约束（约 12 万），调用方如需展示应自行分页或截断。
func (r MonthRange) Months() []Month {
	if r.IsZero() {
		return nil
	}
	n := r.Len()
	months := make([]Month, 0, n)
	for i := 0; i < n; i++ {
		months = append(months, Month{seq: r.start.seq + i})
	}
	return months
}

// String 返回 "2026-01~2026-03" 形态的可读文本；零值区间返回空串。
// 仅用于日志与调试，不是落库形态——区间在库中以摊分起始月 / 结束月两列存储。
func (r MonthRange) String() string {
	if r.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s~%s", r.start, r.end)
}
