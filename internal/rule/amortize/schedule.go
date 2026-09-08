package amortize

import (
	"fmt"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// Input 是派生一张摊分表所需的全部输入，恰为最小集合。
//
// 不收 entity.Expense 的理由见包注释「为什么不收 entity.Expense」：
// 前提 6 要求摊分起止月没有人工指定入口，把它们放进入参就等于开了一个
// 「存在但必须被无视」的口子。调用方（service/expense、service/intake、
// service/stat）从 Expense 取六个字段填进来，一行赋值。
type Input struct {
	// SpendDate 支出日期，必填。起始月 = 本字段所属月（不变式 1b）。
	//
	// 用 calendar.Date 而非 time.Time：前提 7「日期字面值、不做时区换算」
	// 的唯一实现处是该类型。若用 time.Time，同一字面值在不同时区下取到的
	// 所属月会整体偏移一天，摊分起始月就会跟着错。
	SpendDate calendar.Date

	// Months 摊分月数，必填，**默认 1**（不摊分），**不设上限**（终版 H-1）。
	//
	// 取值域是 [1, +∞)，实际上界由 base/calendar 的年份上界给出。
	Months int

	// Amount 原币金额（Expense.支出金额），**允许为负**（退款、冲正）。
	//
	// 按 Currency 的小数位数均分。负金额规则完全对称（终版 H-2）。
	Amount decimal.Decimal

	// Currency 支出币种，必填。**原币份额的均分基准**。
	//
	// 取值域是全部启用币种，不受 T38 本位币候选口径限制——KWD（3 位）等
	// 仍可作支出币种正常记账与摊分（分层 §六 L1.0 明写）。
	Currency currency.Code

	// BaseAmount 本位币金额（Expense.本位币金额）。
	//
	// 由 rule/convert 按不变式 1a 算出，本包只均分、不重算、不校验恒等式——
	// 那是 1a 的职责，在此重复校验会让不变式有两处实现。
	BaseAmount decimal.Decimal

	// BaseCurrency 折算本位币币种（Expense.折算本位币币种），必填。
	// **本位币份额的均分基准**。
	//
	// 取该行**自己的** 折算本位币币种 而非当前 User.本位币：未收敛行
	// （折算本位币币种 ≠ 当前本位币）的 BaseAmount 是按它自己那个币种算的，
	// 用当前本位币的位数去均分它就是拿 A 币种的位数切 B 币种的钱。
	// 「该行是否需要重算」的判定归 service/fx（不变式 2），不在本包。
	BaseCurrency currency.Code
}

// Row 是摊分表的一行：某个月，以及该月的原币份额与本位币份额。
//
// 承载分层 §六 L2 的「H-4 的『覆盖月份区间 + 每月分摊金额』派生输出」——
// Month 是覆盖月份区间中的一个月，两个金额即该月的分摊金额。
//
// 两个金额**各自独立均分**，位数可以不同（如 CNY 原币 2 位、JPY 本位币 0 位），
// 不得用其中一个推另一个。
type Row struct {
	// Month 该份额所属月，是 J 域摊分口径的归月依据。
	Month calendar.Month

	// Amount 该月的原币份额，按 Input.Currency 的小数位数均分。
	Amount decimal.Decimal

	// BaseAmount 该月的本位币份额，按 Input.BaseCurrency 的小数位数均分。
	BaseAmount decimal.Decimal
}

// Equal 报告两行是否相同：月份相同且两个金额数值相等。
//
// **Row 不可用 == 比较**——它含 decimal.Decimal，而该类型刻意做成不可比较
// （见 base/decimal 的类型注释），故 == 是编译错误。这不是缺陷而是保护：
// 金额相等必须走 Equal（1.50 与 1.5 数值相等但内部表示可不同），用 ==
// 会把它们判为不等。本方法是唯一正确的整行比较方式。
func (r Row) Equal(other Row) bool {
	return r.Month.Equal(other.Month) &&
		r.Amount.Equal(other.Amount) &&
		r.BaseAmount.Equal(other.BaseAmount)
}

// IsZero 报告是否为零值 Row，即 Schedule.At 未命中时的返回值。
func (r Row) IsZero() bool {
	return r.Month.IsZero() && r.Amount.IsZero() && r.BaseAmount.IsZero()
}

// Schedule 是一笔支出的完整摊分表：覆盖月份区间 + 原币与本位币两套月份额。
//
// 三处消费方（分层 §六 L2 与 §八 H 域行）：
//
//	H-4  编辑预览          Rows()：逐月展示覆盖区间与每月分摊金额
//	J-1/J-5  摊分口径统计   At(月)：取某月份额，O(1)
//	J-6  未来待摊分段       Range() 端点 + At()，「已发生/未来」的切分归 service/stat
//
// **准则 7**：L6 不得跨层直接调本包。H-4 的即时预览在前端，录入态与编辑态
// 共用 service/expense 暴露的派生入口，由它转调本包（分层 §八 H 域行）。
//
// # 份额不落库
//
// 终版 H-3 明写摊分份额「查询时实时派生，不产生额外数据行」。本类型是
// 派生产物，**不对应任何表**，也不应被缓存到库里——明细上冗余的
// 摊分起始月 / 结束月 两个字段只用于 store 的区间穿刺定位，份额本身每次现算。
//
// 零值 Schedule 不可用：Rows() 返回 nil、At() 一律不命中。必须经 NewSchedule 构造。
type Schedule struct {
	// r 覆盖月份区间，Start() 即摊分起始月、End() 即摊分结束月。
	r calendar.MonthRange

	// amount 原币的月份额切分。
	amount Shares

	// base 本位币的月份额切分，与 amount **各自独立**。
	base Shares
}

// NewSchedule 派生一笔支出的完整摊分表，是本包对外的主入口。
//
// 一次调用同时完成承载的三项：起止月派生（不变式 1b）、原币与本位币各自
// 独立均分（8.2）、以及 H-4 / J 域取数所需的结构。
//
// 纯函数：无 IO、无状态、无时间依赖，同样的入参恒得同样的出参，可并发调用。
//
// # 三步
//
//  1. **派生区间**：Derive(支出日期, 月数)，即不变式 1b。区间的 Len() 恒等于
//     月数，故它同时是两次均分的份数——份数与区间由同一个来源给出，
//     不可能出现「区间 3 个月、份额切了 4 份」。
//  2. **原币均分**：按 Input.Currency 的小数位数。
//  3. **本位币均分**：按 Input.BaseCurrency 的小数位数，与第 2 步**各自独立**。
//
// 两次均分不共用位数、不共用商、不共用尾差——这正是终版 8.2「原币金额与
// 本位币金额各自独立均分」的字面要求，也是 entity 把 Currency 与
// BaseCurrency 分成两个字段的原因。
//
// # 错误一律带上下文
//
// 两次均分的失败会被分别标注「原币」与「本位币」：两者的币种与位数不同，
// 不标注的话拿到 ErrAmountScale 也不知道该查哪一个字段。
func NewSchedule(in Input) (Schedule, error) {
	//第 1 步：不变式 1b。区间 Len() 即份数，两次均分共用它，
	//保证「区间月数」与「份额个数」恒等
	r, err := Derive(in.SpendDate, in.Months)
	if err != nil {
		return Schedule{}, err
	}

	//第 2 步：原币按支出币种的小数位数均分
	amountShares, err := NewShares(in.Amount, in.Months, in.Currency)
	if err != nil {
		return Schedule{}, fmt.Errorf("原币均分失败: %w", err)
	}

	//第 3 步：本位币按折算本位币币种的小数位数均分。
	//与第 2 步各自独立——两个币种的位数可以不同（终版 8.2）
	baseShares, err := NewShares(in.BaseAmount, in.Months, in.BaseCurrency)
	if err != nil {
		return Schedule{}, fmt.Errorf("本位币均分失败: %w", err)
	}

	return Schedule{r: r, amount: amountShares, base: baseShares}, nil
}

// Range 返回覆盖月份区间，即 H-4 的「本次修改将影响的月份范围」。
//
// 调用方取 Range().Start() 与 Range().End() 写回 Expense 的
// 摊分起始月 / 摊分结束月（终版 §四 3 的只读派生项，前提 6 规定不接受
// 上游传入值）。这两个字段也是 store 做区间穿刺的两个端点。
func (s Schedule) Range() calendar.MonthRange { return s.r }

// Months 返回摊分月数，等于区间长度，也等于份额个数。零值返回 0。
func (s Schedule) Months() int { return s.r.Len() }

// AmountShares 返回原币的月份额切分，按 Input.Currency 的小数位数。
func (s Schedule) AmountShares() Shares { return s.amount }

// BaseShares 返回本位币的月份额切分，按 Input.BaseCurrency 的小数位数。
//
// 与 AmountShares **各自独立**：两者的币种、位数、商与尾差都可能不同，
// 不得用其中一个推另一个。
func (s Schedule) BaseShares() Shares { return s.base }

// IsZero 报告是否为零值 Schedule（未经 NewSchedule 构造）。
func (s Schedule) IsZero() bool { return s.r.IsZero() }

// At 返回某个月的份额行，是 **J 域摊分口径份额的唯一来源**。
//
// 第二个返回值报告该月是否落在摊分区间内：store 的区间穿刺（起始月 ≤ M 且
// 结束月 ≥ M）取回行之后，由本方法给出「这一行在该月摊多少」。区间外返回
// 零值 Row 与 false——穿刺与派生是两件事，本方法不假设调用方一定先穿刺过。
//
// 时间复杂度 O(1)：月序号相减得下标，再走 Shares.At（同为 O(1)）。
// 摊分月数无上限（H-1），J 域按月聚合会对大量明细逐笔调用本方法，
// 故这里不能有与月数成正比的开销。
//
// 零值 Schedule 与零值 Month 一律不命中——「未指定」不参与穿刺，
// 取向同 calendar.MonthRange.Contains。
func (s Schedule) At(m calendar.Month) (Row, bool) {
	if !s.r.Contains(m) {
		return Row{}, false
	}
	//区间已判定包含，下标必然落在 [0, Months())，两次 At 不会返回错误。
	//仍然接收 err 而不丢弃：若将来 Shares 与区间脱钩，这里会当场暴露。
	index := m.MonthsSince(s.r.Start())
	amount, err := s.amount.At(index)
	if err != nil {
		return Row{}, false
	}
	base, err := s.base.At(index)
	if err != nil {
		return Row{}, false
	}
	return Row{Month: m, Amount: amount, BaseAmount: base}, true
}

// Rows 按月序返回完整摊分表，即 **H-4 的「覆盖月份区间 + 每月分摊金额」**。
//
// 长度恒等于 Months()。零值 Schedule 返回 nil。
//
// **这是本包唯一的 O(月数) 分配点**。摊分月数无上限（H-1），一笔摊 600 个月
// 的支出会得到 600 行；H-4 是编辑态的即时预览，调用方（前端经
// service/expense 的派生入口）如需展示应自行分页或截断
// （calendar.MonthRange.Months 的注释亦有同样提示）。按月取值请用 At（O(1)）。
func (s Schedule) Rows() []Row {
	if s.IsZero() {
		return nil
	}
	months := s.r.Months()
	amounts := s.amount.All()
	bases := s.base.All()
	//三者长度同源于 Input.Months，恒相等；不足则说明区间与份额脱钩，
	//与其返回一张错位的表，不如返回 nil 让调用方当场发现
	if len(amounts) != len(months) || len(bases) != len(months) {
		return nil
	}

	rows := make([]Row, len(months))
	for i := range months {
		rows[i] = Row{Month: months[i], Amount: amounts[i], BaseAmount: bases[i]}
	}
	return rows
}
