package calendar

// 本文件承载 §六 L0 `base/calendar` 行四项承载中的第 3 项：**区间运算**
// ——「`起始月 + N − 1`」（EndMonth）与「`起始月 ≤ M ≤ 结束月`」（Contains）。

import (
	"github.com/pkg/errors"
)

// MonthRange 年月闭区间，由**起始月 + 月数**确定，是「摊分覆盖哪几个月」这一概念的载体。
//
// 依据 §六 L0 承载「区间运算（`起始月 + N − 1`、`起始月 ≤ M ≤ 结束月`）」，
// 两项运算分别落在 EndMonth 与 Contains。
//
// **以 (起始月, 月数) 而非 (起始月, 结束月) 表示**，这是刻意的：
// 终版 §四 把 `摊分月数` 定为用户可编辑字段（F-1 八项之一）、把 `摊分起始月`
// 与 `摊分结束月` 定为**只读派生**字段。若本类型存 (起始, 结束)，就等于承认存在
// 「起始月 > 结束月」这种可构造的非法状态；存 (起始, 月数) 并强制月数 ≥ 1，
// 则 `结束月 ≥ 起始月` 由结构保证，无从违反（§五 准则 6）。
//
// 注意本类型**不承载摊分语义**：H-2「以支出日期所属月为第 1 个摊分月」这条业务规则、
// 以及金额如何均分，都归 `rule/amortize`（§六 L2，不变式 1b 的唯一实现处）。
// 本类型只提供它所需的**纯月份算术**——约定 5「`base/` 是纯技术层」。
type MonthRange struct {
	start Month
	// count 月数，即承载里的 N。恒 ≥ 1（由 NewMonthRange 保证）。
	count int
}

// NewMonthRange 构造年月闭区间。
//
// count 即 H-1 的「摊分月数」：**下界 1、不设上限**（终版 H-1「默认 1（不摊分），
// **不设上限**」），故本包只拒绝 ≤ 0（返回 ErrMonthCountInvalid），
// 上界不做业务限制——但 `起始月 + count − 1` 越过 MaxYear 时仍会由 EndMonth 报错，
// 那是**表示能力**的边界，不是业务规则。为免构造出一个「存在但求不出末月」的区间，
// 此处即刻算一次末月，越界当场返回。
func NewMonthRange(start Month, count int) (MonthRange, error) {
	if start.IsZero() {
		return MonthRange{}, errors.WithMessage(ErrZeroValue, "构造年月区间")
	}
	if count < 1 {
		return MonthRange{}, errors.WithMessagef(ErrMonthCountInvalid,
			"月数=%d，下界=1（不设上限）", count)
	}
	// 前置校验末月可表示，避免产出「构造成功但 EndMonth 恒报错」的值
	if _, err := start.AddMonths(count - 1); err != nil {
		return MonthRange{}, errors.WithMessagef(err, "月数=%d 的区间末月不可表示", count)
	}
	return MonthRange{start: start, count: count}, nil
}

// StartMonth 起始月。
func (r MonthRange) StartMonth() Month { return r.start }

// Count 月数，即 N。
func (r MonthRange) Count() int { return r.count }

// IsZero 是否为零值。零值的 count 为 0（合法值恒 ≥ 1），故可据此判出「未设置」。
func (r MonthRange) IsZero() bool { return r.count == 0 }

// EndMonth 结束月，即承载的「**起始月 + N − 1**」。
//
// 减 1 是因为区间**闭且含起始月**：摊 1 个月即「只摊在起始月」，结束月 = 起始月
// （终版 H-1「默认 1（不摊分）」+ H-2「以支出日期所属月为第 1 个摊分月」）。
// 少减这个 1 是本条最容易出的错——摊 1 个月会变成覆盖 2 个月，
// 8.2 的均分份数与 J 域区间穿刺会同时错一位。
//
// 因 NewMonthRange 已前置校验过可表示性，本方法对合法值不会失败；
// 仍返回 error 是为零值区间留一条明确的报错路径。
func (r MonthRange) EndMonth() (Month, error) {
	if r.IsZero() {
		return Month{}, errors.WithMessage(ErrZeroValue, "取区间末月")
	}
	return r.start.AddMonths(r.count - 1)
}

// Contains 判断 m 是否落在区间内，即承载的「**起始月 ≤ M ≤ 结束月**」。
//
// 这是 8.2「月度摊分统计用 `起始月 ≤ M` 且 `结束月 ≥ M` 的**区间穿刺**」在内存侧的
// 等价判据。SQL 侧由 `store` 用起止月两列下推（§六 L3 `store` ④「摊分区间穿刺」），
// 两处必须同口径——**闭区间、两端都含**，否则内存派生的份额与库里穿刺出的行数对不上。
//
// 实现上比较月序号而非先求末月：起始月序号 ≤ m 序号 < 起始月序号 + count，
// 与「≤ 末月」等价，但不需要末月可表示，也没有额外的失败分支。
func (r MonthRange) Contains(m Month) bool {
	if r.IsZero() || m.IsZero() {
		return false
	}
	offset := m.MonthsSince(r.start)
	return offset >= 0 && offset < r.count
}

// IndexOf 返回 m 是区间内第几个月，从 **1** 开始计；不在区间内返回 0。
//
// 从 1 起计是对齐 H-2 的措辞「以支出日期所属月为**第 1 个**摊分月」——
// `rule/amortize` 判断「是否末月」（尾差归末月）时用 `IndexOf(m) == Count()`，
// 读起来与业务规则逐字对应；若从 0 起计，那行代码就得写成 `== Count()-1`，
// 与规则文字差一位，是典型的易错点。
func (r MonthRange) IndexOf(m Month) int {
	if !r.Contains(m) {
		return 0
	}
	return m.MonthsSince(r.start) + 1
}

// Months 按时间序返回区间内全部年月，供 H-4「即时展示覆盖月份区间与每月分摊金额」
// 与 J-6「未来待摊分段」逐月遍历。
//
// 不设长度上限（H-1 摊分月数不设上限），调用方须自行考虑规模：
// 极端月数下切片可能很大，但 H-4 是单笔预览、J-6 按查询月份分段，
// 都不会真去展开一个十万月的区间。
func (r MonthRange) Months() ([]Month, error) {
	if r.IsZero() {
		return nil, errors.WithMessage(ErrZeroValue, "展开区间月份")
	}
	months := make([]Month, 0, r.count)
	for i := 0; i < r.count; i++ {
		m, err := r.start.AddMonths(i)
		if err != nil {
			// 不可达：NewMonthRange 已校验末月可表示，中间月必然也可表示。
			// 留作后人绕过构造函数时的即时暴露点。
			return nil, errors.WithMessagef(err, "展开区间第 %d 个月", i+1)
		}
		months = append(months, m)
	}
	return months, nil
}

// String 返回 `2026-01~2026-03(3)` 形态，仅用于日志与 8.4 审计摘要
// （如「本次修改影响 2026-01 至 2026-03」）；落库一律用起止月两列，不存本形态。
func (r MonthRange) String() string {
	if r.IsZero() {
		return "空区间"
	}
	end, err := r.EndMonth()
	if err != nil {
		// 不可达，同 Months
		return r.start.String() + "~?"
	}
	buf := make([]byte, 0, 24)
	buf = append(buf, r.start.String()...)
	buf = append(buf, '~')
	buf = append(buf, end.String()...)
	buf = append(buf, '(')
	buf = appendPadded(buf, r.count, 1)
	buf = append(buf, ')')
	return string(buf)
}
