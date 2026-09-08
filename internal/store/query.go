package store

import (
	"time"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// 查询条件类型（承载 ④）。
//
// 承载 ④ 逐条点名了十条查询口径，本文件定义它们的入参与出参类型：
//
//	 1  F-4 全部筛选维度 + 排序 + 分页              ExpenseFilter / ExpenseSort / Page
//	 2  F-6 筛选态聚合                              ExpenseSummary
//	 3  按归属用户分页列 AuditLog（K-2）            AuditFilter
//	 4  按归属用户分页列 File（C-1，只取元信息）    FilePage（见 file.go）
//	 5  按来源审计ID 查 File（C-3 反向）            见 file.go
//	 6  摊分区间穿刺（起始月 ≤ M 且 结束月 ≥ M）    ExpenseFilter.AmortizeCovers
//	 7  查重候选批量匹配                            见 expense.go 的 FindDedupCandidates
//	 8  按「归属用户 + 名称」查 ExpenseCategory     见 category.go
//	 9  按归属用户列全部类型（G-4）                 见 category.go
//	10  J 域月度聚合（双口径）                      MonthlyQuery / MonthlyBucket
//
// # 一个筛选类型，三处复用——这是「所删即所见」的结构保证
//
// ExpenseFilter 同时服务于 F-4 列表、F-6 聚合、F-8 按筛选批量软删除。分层 §三
// 问题 3 的裁定明写这条：批量软删除要「**复用 ④ 的 F-4 筛选条件类型**（同一套
// 口径，保证『所删即所见』与 F-6 合计对得上）」。
//
// 若三处各有一套条件结构，终版 §八 的批次撤销流程（K-2 定位 → 跳转列表页按来源
// 审计筛选 → F-6 核对笔数与合计 → 全选 → 批量删除）就没有任何东西保证「核对时
// 看到的 N 笔」与「删除时删掉的 N 笔」是同一批。
//
// # 全部字段零值即「不筛」
//
// 这是本文件所有筛选结构的统一口径：零值不参与条件拼装。于是一个零值
// ExpenseFilter 表示「该用户的全部未删除明细」——注意**不是全部明细**，
// 因为 ShowDeleted 的零值 false 即 F-4 的「默认不显示已删除」。

// ExpenseFilter 是 F-4 多维筛选的全部维度（承载 ④ 第 1 条），同时承载 F-6 聚合与
// F-8 按筛选批量软删除的条件（分层 §三 问题 3）。
//
// 维度逐条对应终版 F-4 的列举：「日期区间、金额区间、币种、对手方（模糊）、
// 备注（模糊）、支出类型（多选，空即未分类）、摊分月数（多选）、来源审计/文件
// （批次撤销的入口）、折算本位币币种（定位未按当前本位币折算的行）、银行名称/
// 卡号后四位/卡片币种（含「未识别」选项）、是否显示已删除（默认不显示）」。
//
// # 归属用户不在本结构里
//
// 刻意如此。归属用户ID 是**方法签名的参数**（红线 1），不是筛选条件。放进结构体
// 就会出现「忘了给结构体赋值」这种形态，而结构体字段漏赋值不会被编译器发现；
// 放在签名上则漏传即编译错误。这也是为什么本结构没有 OwnerUserID 字段。
type ExpenseFilter struct {
	// DateFrom、DateTo 是支出日期区间，闭区间，各自零值即该端不限。
	//
	// 用 calendar.Date（前提 7 的字面值语义，不做时区换算）。其零值语义
	// 「未指定」正是 F-4「日期区间端点可选」所需（见 calendar.Date 的注释）。
	DateFrom calendar.Date
	DateTo   calendar.Date

	// AmountFrom、AmountTo 是金额区间，闭区间。
	//
	// **必须与 AmountFromSet / AmountToSet 配对使用**：decimal.Decimal 的零值
	// 就是 0，而 0 是一个合法的筛选边界（「金额 ≥ 0」用于筛出非退款行），
	// 靠零值无法区分「不限」与「限定为 0」。
	//
	// 不用 *decimal.Decimal 指针：decimal.Decimal 刻意做成不可比较类型
	// （其注释「为什么类型不可比较」），指针会引入「两个指针指向数值相等的
	// 不同对象」这类需要额外解引用比较的形态。布尔标志更笨但不会错。
	//
	// 金额是**原币**金额，故区间比较跨币种时无意义——F-4 把币种与金额区间
	// 列为两个独立维度，用户可自行组合（先选 CNY 再限金额）。本包不阻止
	// 「不选币种只限金额」的用法，那是用户的选择。
	AmountFrom    decimal.Decimal
	AmountFromSet bool
	AmountTo      decimal.Decimal
	AmountToSet   bool

	// Currencies 是支出币种多选，空切片即不筛。
	//
	// 取值域是**全部启用币种**，不受 T38 本位币候选口径限制
	// （分层 §六 L1.0「支出币种不受此限」）。
	Currencies []currency.Code

	// Counterparty、Remark 是对手方与备注的**模糊**匹配（F-4 明写「模糊」），
	// 空串即不筛。
	//
	// 匹配语义（含大小写敏感性、通配符转义）由 L4 实现按存储引擎决定，
	// 端口层只声明「这是模糊匹配」。L4 必须转义用户输入中的通配符
	// （SQLite 的 % 与 _），否则用户搜「50%」会匹配到一切以 50 开头的备注。
	Counterparty string
	Remark       string

	// CategoryIDs 是支出类型多选（F-4 明写「多选，**空即未分类**」）。
	//
	// 与 IncludeUncategorized 配对：CategoryIDs 里放具体的类型ID，而「未分类」
	// 这一选项由 IncludeUncategorized 单独表达。
	//
	// # 为什么「未分类」不能编码成 CategoryIDs 里的一个 0
	//
	// 看似可以：Expense.CategoryID 的零值即未分类（G-2）。但那样
	// `CategoryIDs = []int64{0}` 与 `CategoryIDs = nil`（不筛）在语义上就靠
	// 「切片长度」区分，而 `CategoryIDs = []int64{}`（空切片）落在两者之间，
	// 含义暧昧。更要紧的是 L4 拼 SQL 时 `category_id IN (0, 123)` 这种写法把
	// 「IS NULL 或 = 0」与「IN 具体值」混进一个条件，正确性依赖于「未分类到底
	// 存 NULL 还是存 0」这个 L4 细节，而端口层不该知道它。
	CategoryIDs          []int64
	IncludeUncategorized bool

	// AmortizeMonths 是摊分月数多选（F-4 明写「多选」），空切片即不筛。
	//
	// 摊分月数「不设上限」（H-1），故不校验取值范围；但 ≤ 0 是非法值
	// （rule/amortize 会拒绝），L4 遇到时可原样拼条件——恒不命中，
	// 与「筛一个不存在的类型ID」同理，不必特殊处理。
	AmortizeMonths []int

	// SourceAuditID 是来源审计ID（F-4 的「来源审计」，**批次撤销的入口**），
	// 零值即不筛。
	//
	// K-2 明写「对『数据入库』类审计的跳转必须带上『来源审计 = 该ID』的筛选
	// 条件——这是批次撤销的唯一入口」，故这个字段是终版 §八 第 5 条流程能否
	// 走通的关键，不是一个可选的便利功能。
	SourceAuditID int64

	// SourceFileID 是来源文件ID（F-4 的「来源文件」），零值即不筛。
	//
	// 数据源是 C-1 的文件管理页列表（分层 §八 C 域「文件管理页与 F-4『来源
	// 文件』下拉同源」）。
	SourceFileID int64

	// BaseCurrencies 是折算本位币币种多选（F-4「**定位未按当前本位币折算的行**」），
	// 空切片即不筛。
	//
	// 这一维度的用途是不变式 2 的收尾：I-7 允许部分失败，未收敛的行「由 F-4
	// 按折算本位币币种筛选定位、由 I-5 手工兜底」（终版 I-7）。判据是
	// `折算本位币币种 ≠ User.本位币`，但本包**不提供「≠ 当前本位币」这种取反
	// 形态**——当前本位币是 User 上的一个字段，端口层不该去读它来构造条件。
	// service/fx 与 handler 知道当前本位币，把「除它之外的全部币种」列进这个
	// 切片即可（currency.All() 减去当前本位币）。
	BaseCurrencies []currency.Code

	// BankName、CardLast4 是银行名称与卡号后四位（F-4 的卡片维度），空串即不筛。
	//
	// 与 CardUnidentified 配对表达 F-4 明写的「含**未识别**选项」：
	// 卡片三属性「解析不出则留空」（D-2），故「未识别」= 该三项为空。
	BankName  string
	CardLast4 string

	// CardCurrencies 是卡片币种多选，空切片即不筛。
	//
	// 注意卡片币种与支出币种是正交两列（同一张 USD 卡可刷出 EUR 消费，
	// 见 entity.Expense.CardCurrency）。
	CardCurrencies []currency.Code

	// CardUnidentified 为真时，只返回卡片信息**未识别**的行（F-4 的「未识别」选项）。
	//
	// 判据是卡片三属性（银行名称 / 卡号后四位 / 卡片币种）全为空。与
	// BankName / CardLast4 / CardCurrencies 同时设置时语义矛盾（既要求未识别、
	// 又要求某个具体值），L4 实现应按「两条件取合」拼装——结果恒为空集，
	// 这比静默忽略其中一个条件好：空结果会让调用方发现自己传了矛盾条件。
	CardUnidentified bool

	// AmortizeCovers 是**摊分区间穿刺**的目标月（承载 ④ 第 6 条），零值即不筛。
	//
	// 判据是「起始月 ≤ M 且 结束月 ≥ M」（终版 8.2、calendar.MonthRange 的
	// 注释）。它是 J 域摊分口径取行的唯一条件，也可用于 F-4 之外的场景。
	//
	// # 为什么这个条件可以不建聚合表
	//
	// entity.Expense.AmortizeEnd 的注释已论证：「两个条件是合取且各自具备
	// 选择性——查近期月份时后者排除掉早已摊完的绝大多数支出，查久远月份时
	// 前者排除掉该月之后发生的全部支出。calendar.Month 的定长文本形态使这两个
	// 条件可走索引」。
	AmortizeCovers calendar.Month

	// ShowDeleted 为真时把已软删除的明细一并计入（F-4「是否显示已删除，
	// **默认不显示**」）。
	//
	// # 零值 false 即默认不显示，这是刻意的
	//
	// 反过来（零值 = 显示全部）会让任何忘记设置该字段的调用点默认把已删除明细
	// 混进列表与合计，而 F-6 的「列表所见 = 合计所指」正是批次撤销前核对的依据。
	//
	// # F-6/F-7 跟随筛选，J 域不受它影响
	//
	// 终版 J 域末注与 F-6 一起构成一条容易搞混的分工：
	//
	//	F-6 筛选态合计 / F-7 导出   **跟随**本字段（打开即计入）
	//	J 域月度统计                 **一律排除**已删除明细，与本字段无关
	//	I-7 本位币重算               **一律包含**已删除明细，与本字段无关
	//
	// 因此 J 域聚合走 MonthlyQuery（它没有这个字段），I-7 走游标迭代
	// （见 expense.go 的 IterateForRecalc，它自带「含已删除」语义），
	// 两者都不复用本结构，避免调用方误以为可以用本字段调整它们的口径。
	ShowDeleted bool
}

// ExpenseSortField 是 F-3 排序字段（「支持按日期、金额等排序」）。
//
// 做成具名字符串类型而非裸 string：裸 string 意味着排序字段可以由请求直接透传到
// L4 的 SQL 里，那是 ORDER BY 注入。本类型的取值是闭集，Valid 是判据。
type ExpenseSortField string

const (
	// SortBySpendDate 按支出日期排序（F-3 点名的第一个）。
	//
	// calendar.Date 的定长文本形态保证「字典序 = 时间序」，故 L4 无论按文本列
	// 还是按整数列存储都能得到正确顺序。
	SortBySpendDate ExpenseSortField = "spend_date"

	// SortByAmount 按支出金额排序（F-3 点名的第二个）。
	//
	// **必须按整数最小单位排序**，不能按定点文本——后者字典序不等于数值序
	// （见 column.go 的实测）。注意跨币种排序在业务上意义有限（100 JPY 与
	// 100 CNY 的最小单位分别是 100 与 10000），但 F-3 没有限制它，故本包不禁止。
	SortByAmount ExpenseSortField = "amount"

	// SortByBaseAmount 按本位币金额排序。
	//
	// F-3 的「等」字所指之一：F-3 要求「支出金额、折算汇率、本位币金额三者同屏
	// 展示」，三个都可见的列都应可排序。本位币金额跨币种可比，实际上比按原币
	// 金额排序更常用。
	SortByBaseAmount ExpenseSortField = "base_amount"

	// SortByCreatedAt 按创建时间排序。
	//
	// 默认排序应当以它为兜底键，理由见 ExpenseSort.Normalize。
	SortByCreatedAt ExpenseSortField = "created_at"

	// SortByID 按明细ID 排序。
	//
	// idgen 生成的 ID 含时间前缀（见 base/idgen），故它与创建时间序基本一致，
	// 且**严格唯一**——这是它作为兜底排序键的价值所在（见 Normalize）。
	SortByID ExpenseSortField = "id"
)

// expenseSortFields 是排序字段的声明序清单，Valid 与 ExpenseSortFields 共用。
var expenseSortFields = []ExpenseSortField{
	SortBySpendDate,
	SortByAmount,
	SortByBaseAmount,
	SortByCreatedAt,
	SortByID,
}

// Valid 报告该排序字段是否为闭集内的取值。
//
// L4 实现**必须**先调用它再拼 ORDER BY。这是 ORDER BY 注入的唯一防线：
// 排序字段无法用查询参数占位符（? 只能占值，不能占列名），只能靠白名单。
func (f ExpenseSortField) Valid() bool {
	for _, known := range expenseSortFields {
		if f == known {
			return true
		}
	}
	return false
}

// String 返回字段的字面值，即 L4 可安全拼进 ORDER BY 的列名。
func (f ExpenseSortField) String() string {
	return string(f)
}

// ExpenseSortFields 返回全部排序字段，按声明序。
//
// 供 L6 下发给前端做排序下拉，顺序稳定（与 enum 各类型的 Xxxs() 同例）。
func ExpenseSortFields() []ExpenseSortField {
	out := make([]ExpenseSortField, len(expenseSortFields))
	copy(out, expenseSortFields)
	return out
}

// ExpenseSort 是一次排序请求：字段 + 方向。
type ExpenseSort struct {
	// Field 排序字段。零值（空串）表示未指定，由 Normalize 补默认值。
	Field ExpenseSortField

	// Desc 为真即降序。零值 false 即升序。
	Desc bool
}

// Normalize 返回补齐后的排序键序列，是 L4 拼 ORDER BY 的唯一依据。
//
// # 为什么必须追加一个唯一兜底键
//
// 分页的正确性依赖排序的**全序**性。按支出日期排序时，同一天的多笔明细之间没有
// 确定顺序，而 SQLite 等引擎对「相等的行」不保证稳定顺序——于是第 1 页与第 2 页
// 可能包含同一条明细，另一条则从未出现。这个缺陷在数据量小的时候几乎不可见，
// 上线后表现为「某笔明细在列表里找不到，但筛选能筛出来」。
//
// 兜底键取 SortByID 而非 SortByCreatedAt：ID 由 base/idgen 生成、**严格唯一**，
// 而创建时间是 time.Time，同一批入库的 N 条明细（D-7 一次提交）完全可能落在同一
// 个时间戳上——那正是最需要稳定顺序的场景。
//
// 兜底键的方向跟随主键方向，这样「按日期降序」看到的同日多笔也是新的在前，
// 与用户预期一致。
//
// 未指定字段时默认「支出日期降序」：F-3 是记账明细列表，最近的支出应在最前。
func (s ExpenseSort) Normalize() []ExpenseSort {
	field := s.Field
	desc := s.Desc
	if !field.Valid() {
		//未指定或非法 → 默认按支出日期降序
		field = SortBySpendDate
		desc = true
	}
	if field == SortByID {
		//主键已是唯一键，无需兜底
		return []ExpenseSort{{Field: field, Desc: desc}}
	}
	return []ExpenseSort{
		{Field: field, Desc: desc},
		{Field: SortByID, Desc: desc},
	}
}

// 分页的默认与上限。
//
// 按约定 8「业务阈值一律写死在对应包」，这两个取值是业务规则而非部署差异，
// 故为编译期常量，不进 base/config。
const (
	// DefaultPageSize 是未指定页大小时的默认值。
	DefaultPageSize = 20

	// MaxPageSize 是单页最大条数。
	//
	// 上限的作用不是性能调优，而是**防止分页被当成全量导出通道**：
	// K-3 整库导出与 F-7 明细导出都有各自的审计要求（「数据导出」是 B-4 边界①
	// 的唯一例外，即唯一记审计的读操作）。若列表接口允许 PageSize=1000000，
	// 就能绕开导出接口把全部数据取走而**不留任何审计**。
	//
	// 取 200 而非更大：F-3 是逐页浏览的界面，200 行已远超一屏；真正需要大批量
	// 取数的两个场景（I-7 重算、K-3 导出）都走游标迭代，不走分页。
	MaxPageSize = 200
)

// Page 是分页请求（承载 ④ 第 1 条的「分页」）。
type Page struct {
	// Number 页码，从 1 开始。零值与负值经 Normalize 归一到 1。
	Number int

	// Size 每页条数。零值经 Normalize 归一到 DefaultPageSize，
	// 超过 MaxPageSize 时截到上限。
	Size int
}

// Normalize 返回归一化后的分页参数，L4 实现应以它的返回值为准。
//
// **不返回错误**：页码越界（如第 9999 页）是一个合法请求，结果是空列表；
// 把它做成错误会让「删到最后一页只剩 3 条」这种正常操作报错。非法取值一律
// 按最接近的合法值处理，与 HTTP 层「宽进严出」的取向一致。
func (p Page) Normalize() Page {
	out := p
	if out.Number < 1 {
		out.Number = 1
	}
	if out.Size <= 0 {
		out.Size = DefaultPageSize
	}
	if out.Size > MaxPageSize {
		out.Size = MaxPageSize
	}
	return out
}

// Offset 返回归一化后的偏移量，供 L4 拼 LIMIT/OFFSET。
func (p Page) Offset() int {
	n := p.Normalize()
	return (n.Number - 1) * n.Size
}

// Limit 返回归一化后的每页条数。
func (p Page) Limit() int {
	return p.Normalize().Size
}

// CurrencyTotal 是某个币种下的金额合计，F-6 的「按币种分组金额合计」的一项。
type CurrencyTotal struct {
	// Currency 币种。
	Currency currency.Code

	// Total 该币种下的**原币**金额合计。
	//
	// 位数即该币种的小数位数（currency.Code.Scale()）：同币种内相加不改变位数，
	// 故合计与明细的位数一致，不需要额外舍入。
	Total decimal.Decimal

	// Count 该币种下的笔数。
	Count int64
}

// ExpenseSummary 是 F-6 筛选态累计统计的结果（承载 ④ 第 2 条）。
//
// 终版 F-6：「当前筛选条件下的**笔数**、**按币种分组金额合计**、**本位币折算
// 总额**；**跟随筛选**——打开『显示已删除』时即计入，保证『列表所见 = 合计所指』
// ；批次撤销前的核对即依赖它」。三项一一对应下面三个字段。
type ExpenseSummary struct {
	// Count 是筛选结果的总笔数。
	//
	// 它同时是 F-8「删除前提示『本次将删除 N 笔明细』」里那个 N 的来源，
	// 以及终版 §八 批次撤销流程里「用 F-6 核对笔数」的那个笔数。
	//
	// 注意它与 DeleteByFilter 返回的「实际受影响笔数」**可以不相等**：后者跳过
	// 已删除行（F-8「选中的已删除行跳过、不覆盖其删除时间」），而本字段在
	// ShowDeleted 为真时把它们计入。这不是矛盾，是两个不同的量——提示语说的是
	// 「将删除 N 笔」（候选数），审计摘要记的是「实际删了 M 笔」。
	Count int64

	// ByCurrency 是按币种分组的金额合计，**按 currency.All() 的声明序**。
	//
	// 顺序必须稳定：这是一个直接渲染到界面上的列表，map 遍历序会让它每次刷新
	// 都换一次顺序。声明序而非字典序，理由与 parser.Registry.Types 相同——
	// currency 侧已按「人民币 + 港澳台 + 主要储备货币 + 亚太与欧美常用」的意图
	// 排好，字典序会把这个意图抹掉。
	ByCurrency []CurrencyTotal

	// BaseTotal 是本位币折算总额。
	//
	// # 这一项是「按数据库字面数值相加」，可能是混合口径
	//
	// 终版 J 域末注明写这条口径（「一律按数据库字面数值相加，不对『折算本位币
	// 币种 ≠ 当前本位币』的混合口径做任何提示——纠错由 I-7 与 I-5 承担，发现
	// 问题靠 F-4 筛选」）。F-6 同此：若某些行还没被 I-7 重算过，它们的
	// 本位币金额是按**旧**本位币折算的，直接相加在数值上是无意义的，但系统
	// 刻意不提示。
	//
	// 因此本字段不带币种：它不是「某个币种的合计」，而是「本位币金额这一列的
	// 字面合计」。位数恒为 BaseAmountScale（2 位），同位数相加不改变位数。
	BaseTotal decimal.Decimal
}

// AuditFilter 是 K-2 审计分页查询的条件（承载 ④ 第 3 条）。
//
// 分层 §六 L3 store ④：「按归属用户分页列 AuditLog（K-2，**支持操作类型与时间
// 区间**，兼 F-4『来源审计』下拉数据源）」。
//
// # 只读，且只能查本人
//
// K-2 明写「本页**只读，不承担任何写操作**」，service/audit 行亦写「K-2 只读，
// 本包不提供任何审计的改删入口」。归属由方法签名的 OwnerUserID 保证（红线 1）。
type AuditFilter struct {
	// Types 是操作类型多选（11 类审计，见 enum.OperationType），空切片即不筛。
	//
	// 用 enum.OperationType 而非裸 string：11 类是闭集，且 enum 侧已把「哪些
	// 操作记审计」这个口径收在一处（该类型的注释详列了 11 类与写入方的
	// 一一对应关系，以及三个刻意的缺席：无登出、无明细新增、无回滚）。
	Types []enum.OperationType

	// TimeFrom、TimeTo 是操作时间区间，闭区间，零值即该端不限。
	//
	// # 这里用 time.Time 而非 calendar.Date，不违反前提 7
	//
	// 前提 7 管的是**支出日期**（「任何包不得引入带时区的时间类型参与支出日期
	// 的解析、存储与归月」），其括号里明确豁免：「业务时间戳如创建/更新/操作
	// 时间不受此限」。AuditLog.Time 在 entity 里就是 time.Time，这里与它一致。
	TimeFrom time.Time
	TimeTo   time.Time

	// Results 是操作结果多选（成功/失败），空切片即不筛。
	//
	// 承载只点名了「操作类型与时间区间」两个维度，本字段是**额外**的一项，
	// 理由是 8.4 第 2 类登录失败以「操作结果 = 失败」表达（见 enum.OperationType
	// 的注释），而 A-8 的失败排查是 K-2 最实际的用途之一——没有这个维度，
	// 用户要在成千条成功记录里翻找几条失败。这是本包唯一一处超出承载字面的
	// 筛选维度，登记在 answer 的「设计取舍」中。
	Results []enum.OperationResult

	// ObjectTypes 是操作对象类型多选，空切片即不筛。
	//
	// 同为额外维度，供 F-4「来源审计」下拉只取「数据入库」类审计时使用。
	// 注意 enum.ObjectType 的**零值是合法取值**（「本次操作无具体对象或为批量
	// 操作」，11 类里有 5 类该落零值），故「筛零值」是有意义的请求——
	// 与其他多选维度不同，这里的零值元素不应被 L4 当成噪声过滤掉。
	ObjectTypes []enum.ObjectType
}

// MonthlyMode 是 J 域月度聚合的口径（J-1 双口径）。
//
// 终版 J-1：「支持『未摊分口径（按支出日期归月）』与『摊分口径（按份额所属月
// 归月）』双口径，一键切换并明示当前口径与筛选语义」。
type MonthlyMode uint8

const (
	// MonthlyModeUnamortized 是未摊分口径：按支出日期归月。
	//
	// 这一口径**完全下推**到存储层（分层 §六 L3 store ④ 末段：「未摊分口径按
	// 支出日期归月 + 按类型/币种分组合计**完全下推**」）：一次聚合查询直接得到
	// 每月每类型每币种的合计，不装载任何明细行。
	MonthlyModeUnamortized MonthlyMode = iota + 1

	// MonthlyModeAmortized 是摊分口径：按份额所属月归月。
	//
	// 这一口径**只下推区间穿刺取行**，份额的派生与聚合在上层（同上：「摊分口径
	// **只下推区间穿刺取行**（份额不落库，派生与聚合在 rule/amortize +
	// service/stat）」）。
	//
	// 故本口径**不走** AggregateMonthly，而走 expense.go 的 IterateAmortizeCovering
	// ——两个口径的方法形状不同，这正是「哪些计算在库里、哪些在内存里」这条
	// 分工的结构表达。若把两个口径塞进同一个方法，就必然要在 L4 实现里写
	// 「if 摊分口径 then 取行 else 聚合」，而返回类型只能取两者的并集，
	// 调用方拿到的一半字段永远是空的。
	MonthlyModeAmortized
)

// Valid 报告口径取值是否合法。零值不合法——调用方必须显式表态用哪个口径。
func (m MonthlyMode) Valid() bool {
	return m == MonthlyModeUnamortized || m == MonthlyModeAmortized
}

// String 返回口径的可读名，供日志与排错。
func (m MonthlyMode) String() string {
	switch m {
	case MonthlyModeUnamortized:
		return "unamortized"
	case MonthlyModeAmortized:
		return "amortized"
	default:
		return "unknown"
	}
}

// MonthlyGroupBy 是 J 域聚合的分组维度。
type MonthlyGroupBy uint8

const (
	// GroupByNone 只按月分组，对应 J-1 月度支出总额。
	GroupByNone MonthlyGroupBy = iota

	// GroupByCategory 按月 + 支出类型分组，对应 J-2 月度类型分布。
	//
	// 「未分类」（CategoryID 零值）自成一组，不被丢弃——G-2 明写「明细的支出
	// 类型为空即未分类，字典中不设『未分类』记录」，它是一个真实存在的分组。
	GroupByCategory

	// GroupByCurrency 按月 + 币种分组，对应 J-4 币种维度统计。
	GroupByCurrency
)

// MonthlyQuery 是 J 域月度聚合的查询条件（承载 ④ 第 10 条）。
//
// # 与 ExpenseFilter 刻意分开
//
// J 域的口径与 F-4/F-6 有三处硬性不同，混用一个结构会让这三处变成「调用方要记住
// 的注意事项」，而它们每一条都是静默出错：
//
//	           F-4 / F-6                      J 域
//	已删除明细  跟随 ShowDeleted               **一律排除**（终版 J 域末注）
//	时间维度    日期区间（任意起止）           月区间（按月归组）
//	结果形态    明细行 + 筛选态合计            按月分桶的聚合值
//
// 第一条是最要紧的：J 域末注明写「**已软删除的明细不参与 J 域任何统计**」，
// 而本结构**没有** ShowDeleted 字段，于是「统计里混进已删除明细」在本层无法表达。
type MonthlyQuery struct {
	// Mode 是口径（必填，零值非法）。
	//
	// 注意 MonthlyModeAmortized 不能用于 AggregateMonthly，见该常量的注释。
	Mode MonthlyMode

	// Range 是月区间，闭区间（J-3 多月趋势、环比同比都按月区间取数）。
	//
	// 用 calendar.MonthRange：其非零值必然满足「起始月 ≤ 结束月 且两端均为合法
	// 年月」（构造函数保证），故 L4 不必再校验区间方向。零值表示未指定，
	// 此时 L4 应返回输入错误——J 域是月度报表，没有「查全部历史月份」的用例，
	// 放行会变成一次全表聚合。
	Range calendar.MonthRange

	// GroupBy 是分组维度。零值 GroupByNone 即只按月分组（J-1）。
	GroupBy MonthlyGroupBy

	// Currencies 限定币种，空切片即不筛。
	//
	// J-4 按币种拆分走 GroupByCurrency，本字段用于「只看某几个币种的趋势」。
	Currencies []currency.Code

	// CategoryIDs 限定支出类型，空切片即不筛。与 IncludeUncategorized 配对，
	// 语义同 ExpenseFilter 的同名字段。
	CategoryIDs          []int64
	IncludeUncategorized bool
}

// MonthlyBucket 是 J 域聚合的一个分组结果。
type MonthlyBucket struct {
	// Month 是该桶所属月。
	Month calendar.Month

	// CategoryID 是分组的支出类型ID，仅 GroupByCategory 时有值；
	// 零值即「未分类」这一组（G-2）。
	CategoryID int64

	// Currency 是分组的币种，仅 GroupByCurrency 时有值。
	Currency currency.Code

	// Count 该桶的笔数。
	Count int64

	// Amount 该桶的**原币**金额合计。
	//
	// 仅在 GroupByCurrency 下有明确意义——其余分组下该桶可能混有多个币种的
	// 原币金额，相加无意义。L4 实现在非 GroupByCurrency 时应留零值，
	// 而不是给出一个跨币种的和：后者是一个看起来像数字的错误答案。
	Amount decimal.Decimal

	// BaseAmount 该桶的本位币金额合计，位数恒为 BaseAmountScale。
	//
	// 这是跨币种唯一可比的量，J-1/J-2/J-3 的主指标。同 ExpenseSummary.BaseTotal，
	// 它是「按数据库字面数值相加」，可能混有未收敛的旧本位币口径（终版 J 域末注）。
	BaseAmount decimal.Decimal
}
