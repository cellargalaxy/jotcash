package store

import (
	"context"
	"iter"
	"time"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/entity"
)

// DedupKey 是查重候选批量匹配的四要素键（承载 ④ 第 7 条）。
//
// 四个字段就是 E-1 的判重四要素：归属用户ID + 支出日期 + 金额 + 币种。
//
// # 为什么本包自己声明一遍，而不 import rule/dedup.Key
//
// 分层 §六 L3 store 行的依赖列写死为 `entity`、`enum`、`currency` 三项，而
// rule/dedup 属 L2。§六 的层内规则与依赖图（`L3 --> L1.1`、`L3 --> L1.0`，
// 无 `L3 --> L2` 边）都不允许这条边：L2 规则层的消费方是 L5（`L5 --> L2`），
// 端口层若依赖 L2，则「L4 实现 store 时会间接绑上规则层」，而规则层是纯逻辑、
// 可脱库单测的一层，被存储实现拖进来毫无必要。
//
// 这与 currency 不 import base/decimal 是同一种处置（currency/table.go：
// 「这里不 import decimal……一致性由 consumer_check 锁死」）。
//
// # 两处声明如何保证不漂移：结构相同即可零成本互转
//
// 本类型与 dedup.Key 的**字段名、类型、顺序完全一致**，故 Go 允许两者直接转换
// （已实测：`store.DedupKey(k)` 与 `dedup.Key(local)` 双向可转、往返相等，
// 无需逐字段赋值、无运行时开销）。service 层（L5，同时可依赖 L2 与 L3）在调用处
// 写一次转换即可：
//
//	keys := dedup.Keys(rows)                       // L2 侧算出四要素
//	storeKeys := make([]store.DedupKey, len(keys))
//	for i, k := range keys {
//	    storeKeys[i] = store.DedupKey(k)           // 结构相同，直接转换
//	}
//	candidates, err := repo.FindDedupCandidates(ctx, owner, storeKeys)
//
// 一旦哪一侧改了字段名或类型，这个转换**当场编译失败**——这正是所需的护栏：
// 漂移在编译期暴露，而不是等到运行时漏判重复。consumer_check_test.go 的
// TestDedupKeyConvertibleToRuleKey 另用可执行断言把这条钉死，避免有人「顺手」
// 给本类型加一个字段而 L5 那侧的转换恰好还没写。
//
// # 字段形态的依据（沿用 dedup.Key 已做过的裁定）
//
// SpendDate 与 Amount 都是**文本**而非 calendar.Date / decimal.Decimal，
// 这是 dedup.Key 为「拿来拼 SQL 参数」专门做过的设计，本包照搬：
//
//   - SpendDate 是 calendar.Date 的定长落库文本（"2006-01-02"），store 侧要把它
//     直接拼进元组条件，而它的落库形态本就是这个文本；
//   - Amount 是**规范文本**（剥掉尾随零，如 "100.5"）而非定点文本。理由是
//     8.3 的金额是**原币**金额，位数随支出币种而定——KWD 是 3 位，按本位币存储的
//     2 位会直接报错（dedup.Key.Amount 的注释）。
type DedupKey struct {
	// OwnerUserID 归属用户ID。**不得为零值**——E-1 的判重范围限于本人明细，
	// 缺归属会跨用户取候选（前提 3 数据泄漏）。
	OwnerUserID int64

	// SpendDate 支出日期，calendar.Date 的定长落库文本（"2006-01-02"）。
	SpendDate string

	// Amount 原币金额的**规范文本**（剥尾随零）。
	//
	// 实现必须按 Currency 的小数位数换算成整数最小单位再比较，不得直接比文本
	// （见 FindDedupCandidates 的注释）。
	Amount string

	// Currency 支出币种代码。
	Currency string
}

// ExpenseRepo 是 Expense 仓储（承载 ①，归属型）。
//
// # 归属参数强制，无管理员旁路
//
// 承载 ①：「每个按 ID 访问的方法**签名强制带归属用户ID**，不变式 3 的编译期兜底，
// **该参数对任何角色一律生效、无管理员旁路**（前提 3）」。
//
// 本接口的每一个方法首参都是 ctx（+ TxHandle）+ OwnerUserID，无例外。**角色
// （enum.Role）不出现在本接口的任何签名里**，于是 §九 前提 3 所警惕的「顺手加个
// if 放宽归属过滤」在本层无法表达——没有 if 可加的地方。
//
// # 删除能力按前提 8 裁剪：只有「写删除时间」，且形态有二
//
// 承载 ③：「`Expense` 只有「写删除时间」，形态有二——**按 ID（逐笔或多选）** 与
// **按筛选条件批量**（复用 ④ 的 F-4 筛选类型，支撑 F-8「全选当前筛选结果全集」
// 不装载全集 ID），两者一律**已删除行跳过、不覆盖其 `删除时间`**（幂等）并
// **返回实际受影响笔数**（供审计摘要记 N 笔）」。
//
// 两种形态是分层 §三 问题 3 的裁定结果：只有按 ID 形态时，「全选当前筛选结果全集」
// 只能先把全集 ID 拉进内存，与同一张表里的游标迭代、聚合下推、查重候选批量匹配
// 三处性能取向自相矛盾。
//
// # 本接口没有任何硬删除方法，也没有「恢复」方法
//
// 软删除是**终态、无恢复入口**（终版 F-8「写删除时间，**终态、无恢复入口**」）。
// 故本接口不得出现 Restore / Undelete，也不得出现物理删除。前提 8 的措辞是
// 「上层想违反也没有可调的方法」——这是方法集合本身承担的约束，
// TestNoDeleteMethods 与 TestNoRestoreMethods 逐个接口断言。
type ExpenseRepo interface {
	// ---- 写入 ----

	// CreateTx 批量新增明细，在给定事务内执行（D-7 提交入库）。
	//
	// # 只提供事务形态，不提供非事务形态
	//
	// 明细的新增**只有一条链路**：D-7 提交入库（含 F-8 复制提交）。终版 §8.4
	// 表下注明写「明细的新增不在本表单列——一律走**数据入库**（含复制提交）」，
	// enum.OperationType 的注释亦把「无『明细新增』」列为三个刻意的缺席之一。
	//
	// 而 D-7 是「一次提交生成一条『数据入库』审计 + N 条明细**同事务**」
	// （不变式 4），故不存在「在事务外新增明细」的合法用例。不提供非事务形态，
	// 那条错误路径在本层就不存在。
	//
	// 各 row 的 ID 必须由调用方经 base/idgen.GenExpenseID 生成（约定 9），
	// SourceAuditID 必须已挂上本批次的审计ID（不变式 4：「先落审计取 ID →
	// 业务数据挂载该 ID → 同事务提交」）。实现不得自行生成或改写这两个字段。
	//
	// 允许空切片（解析出 0 条的合法情形，见 D-4），此时不做任何写入并返回 nil。
	CreateTx(ctx context.Context, tx TxHandle, owner OwnerUserID, rows []entity.Expense) error

	// UpdateTx 保存一笔明细的编辑（F-5 逐笔编辑），在给定事务内执行。
	//
	// # 仅对未删除明细开放
	//
	// F-5 明写「**仅对未删除明细开放**」，§8.5 下方亦写「已删除明细**只读不可
	// 编辑**（F-5 仅对未删除明细开放，其 `支出类型ID` 因此永久不可改）」。
	// 故实现必须在条件里带上「删除时间为空」，命中 0 行时返回 KeyNotFound。
	//
	// 这条不能只靠服务层校验：F-5 的链路是「读出 → 用户编辑 → 保存」，读与写
	// 之间用户可能花几分钟，期间该明细可能被另一个标签页删掉。若写入不带这个
	// 条件，就会把一条已删除明细改回去（删除时间还在，但内容变了），
	// 而 F-8 的软删除是终态。
	//
	// # 十项系统只读字段与来源两项不得被改写
	//
	// F-5：「保存**不生成「数据入库」审计**、**不改 `来源审计ID` 与
	// `来源文件ID`**」。实现必须只更新 F-1 八项 + 四项只读派生 + UpdatedAt，
	// 不得整行覆盖——整行覆盖会把调用方结构体里的零值 SourceAuditID 写进去，
	// 那条明细就此脱离它的批次，K-2 的批次撤销再也找不到它。
	//
	// 更新的字段集合恰是：F-1 八项（支出日期/金额/币种/对手方/备注/类型/摊分
	// 月数/折算汇率）+ 四项只读派生（本位币金额/折算本位币币种/摊分起始月/
	// 摊分结束月）+ UpdatedAt。卡片三属性亦不在其中（F-2 列为系统只读 10 项）。
	UpdateTx(ctx context.Context, tx TxHandle, owner OwnerUserID, row entity.Expense) error

	// UpdateConversionTx 更新单笔的折算三字段（I-7 逐笔重算），在给定事务内执行。
	//
	// # 为什么单独一个方法，而不复用 UpdateTx
	//
	// 不变式 2：「重算原子粒度是单笔……**单笔三字段同事务**；允许部分失败、
	// 可续跑」。I-7 只改「折算汇率 + 本位币金额 + 折算本位币币种」三者
	// （终版 I-7），不碰 F-1 的其余字段。
	//
	// 若走 UpdateTx，I-7 就必须先读出整行、改三个字段、再整行写回——那是一次
	// 读改写，与另一个正在编辑同一笔的 F-5 请求构成丢失更新（F-5 改了对手方，
	// I-7 用旧快照写回，对手方被还原）。只更新三个字段则没有这个窗口。
	//
	// # 含已软删除的明细
	//
	// 终版 I-7 明写「**含已软删除的明细**（它们可经 F-4 筛选查看，展示必须处于
	// 当前本位币口径；复制时的初值同理）」。故本方法**不**排除已删除行——
	// 这与 UpdateTx 恰好相反，是本接口里唯一一处「已删除行也要写」的方法。
	//
	// 这条与 F-5 的「仅未删除」不矛盾：F-5 改的是业务内容（已删除明细只读），
	// I-7 改的是折算口径（已删除明细的展示也必须正确）。
	UpdateConversionTx(ctx context.Context, tx TxHandle, owner OwnerUserID, row entity.Expense) error

	// ---- 软删除（承载 ③ 的两种形态）----

	// SoftDeleteByIDs 按 ID 软删除（F-8 逐笔或多选），返回**实际受影响笔数**。
	//
	// # 三条硬口径
	//
	//  1. **已删除行跳过、不覆盖其删除时间**（幂等）。终版 F-8 与 §8.5 都明写
	//     「选中的已删除行**跳过、不覆盖其 `删除时间`**」。故实现的条件里必须带
	//     「删除时间为空」。覆盖了会让「这笔是什么时候删的」变成最后一次点击的
	//     时刻，而删除是终态、该时刻是唯一的历史事实。
	//  2. **返回实际受影响笔数**，供审计摘要记「N 笔」（承载 ③）。这个数与传入
	//     的 ID 个数**可以不等**：已删除的、不属于本用户的、不存在的都不计入。
	//  3. **不要求整批原子成功**。§8.5：「不要求整批原子成功，失败重跑即可
	//     （删除幂等）」。故本方法有非事务形态。
	//
	// deletedAt 由调用方传入而非实现取 time.Now()：同一次批量删除的所有行应共享
	// 同一个删除时刻，否则「按删除时间排序」会把一批删除打散；且可测试。
	//
	// 允许空切片，此时返回 0 且无错误。
	SoftDeleteByIDs(ctx context.Context, owner OwnerUserID, ids []int64, deletedAt time.Time) (int64, error)

	// SoftDeleteByIDsTx 同 SoftDeleteByIDs，在给定事务内执行。
	//
	// F-8 删除要记一条 8.4 第 7 类「明细编辑与删除」审计。逐笔删除时对象ID 是
	// 该明细ID；批量删除时「**对象ID 零值、摘要记筛选条件与实际笔数**」
	// （分层 §六 L5 service/expense 行）。
	SoftDeleteByIDsTx(ctx context.Context, tx TxHandle, owner OwnerUserID, ids []int64, deletedAt time.Time) (int64, error)

	// SoftDeleteByFilter 按筛选条件批量软删除，返回**实际受影响笔数**。
	//
	// # 这个方法的存在理由（分层 §三 问题 3）
	//
	// F-8 明写批量删除「**也支持全选当前筛选结果全集**」，而 F-4 的筛选集可以是
	// 全库量级。§三 问题 3 的裁定：只有按 ID 形态时，service/expense「只能『先按
	// 筛选取全集 ID → 逐条/分批写删除时间』，把全集装进内存——与同一张表里刚立
	// 的三处性能口径（游标迭代、J 域聚合下推、查重候选批量匹配）取向自相矛盾」。
	//
	// # 复用 ExpenseFilter 是「所删即所见」的结构保证
	//
	// 裁定明写「**复用 ④ 的 F-4 筛选条件类型**（同一套口径，保证『所删即所见』
	// 与 F-6 合计对得上）」。终版 §八 的批次撤销流程依赖这条：K-2 定位批次 →
	// 跳转列表页（按来源审计筛选）→ **用 F-6 核对笔数与合计** → 全选 → 批量删除。
	// 若聚合与删除各用一套条件，核对就失去意义。
	//
	// # filter.ShowDeleted 的作用与「已删除行跳过」不冲突
	//
	// 两者管的是不同的事：ShowDeleted 决定**候选集**（用户在列表里看到什么），
	// 「已删除行跳过」决定**实际写入**。打开 ShowDeleted 后全选删除，已删除的行
	// 进入候选但不被改写，故返回的笔数会小于 F-6 的 Count——这正是 ExpenseSummary
	// .Count 注释里说的那两个不同的量。
	//
	// # 空筛选条件是合法的，且后果是删除该用户的全部未删除明细
	//
	// 本方法**不**因为 filter 是零值而拒绝。理由是零值 ExpenseFilter 的语义明确
	// （该用户的全部未删除明细），而 F-8 允许「全选当前筛选结果全集」——不加任何
	// 筛选就是一种合法的「当前筛选」。删除前的「本次将删除 N 笔」提示（F-8）是
	// 用户侧的防线，笔数来自 F-6 聚合。
	SoftDeleteByFilter(ctx context.Context, owner OwnerUserID, filter ExpenseFilter, deletedAt time.Time) (int64, error)

	// SoftDeleteByFilterTx 同 SoftDeleteByFilter，在给定事务内执行。
	SoftDeleteByFilterTx(ctx context.Context, tx TxHandle, owner OwnerUserID, filter ExpenseFilter, deletedAt time.Time) (int64, error)

	// ---- 查询 ----

	// GetByID 按 ID 查一笔明细。
	//
	// **不排除已删除行**：F-4 的「显示已删除」可以看到它们，K-2 跳转到已软删除
	// 明细时「正常跳转并自动带上『显示已删除』筛选（记录仍在）」（终版 K-2），
	// F-8 的复制入口也要读出已删除明细作为初值。
	//
	// 「这笔是否已删除」由调用方看 entity.Expense.DeletedAt 判断，本方法如实
	// 返回。要求「仅未删除」的场景（F-5 编辑）在写入方法上已有条件保证，
	// 不必也不该在读取处提前拦。
	//
	// 取不到（不存在或归属不符）返回 KeyNotFound（红线 2）。
	GetByID(ctx context.Context, owner OwnerUserID, id int64) (entity.Expense, error)

	// List 按筛选条件分页查询（承载 ④ 第 1 条：F-4 全部筛选维度 + 排序 + 分页）。
	//
	// 返回该页的明细行与**筛选结果总笔数**。总数与 ExpenseSummary.Count 同源，
	// 供前端渲染分页器。
	//
	// sort 经 ExpenseSort.Normalize 补齐后使用（含唯一兜底键，理由见该方法），
	// page 经 Page.Normalize 归一。实现必须调用这两个方法，不得直接用原值。
	List(ctx context.Context, owner OwnerUserID, filter ExpenseFilter, sort ExpenseSort, page Page) ([]entity.Expense, int64, error)

	// Summarize 按筛选条件聚合（承载 ④ 第 2 条：F-6 筛选态聚合）。
	//
	// 三项结果（笔数、按币种分组金额合计、本位币折算总额）见 ExpenseSummary。
	// filter 必须与 List 同一个值才能保证「列表所见 = 合计所指」（F-6）。
	//
	// **完全在存储层聚合**，不装载明细行——这是与 SoftDeleteByFilter 同源的
	// 性能取向（§三 问题 3）。
	Summarize(ctx context.Context, owner OwnerUserID, filter ExpenseFilter) (ExpenseSummary, error)

	// FindDedupCandidates 按四要素元组集合批量取回查重候选（承载 ④ 第 7 条）。
	//
	// 承载原文：「**查重候选批量匹配**（按「(日期, 金额, 币种)」元组集合一次取回
	// **在库未删除**候选，供 8.3 四条链路）」。
	//
	// 键类型是本包的 DedupKey，它与 rule/dedup.Key **结构完全一致、可零成本互转**
	// ——本包不 import L2，理由与转换方式见 DedupKey 的注释。
	//
	// # 金额是规范文本，实现必须换算回最小单位再比较
	//
	// DedupKey.Amount 是 "100.5" 这样的规范文本（剥掉尾随零），而金额列存的是
	// 整数最小单位（见 column.go）。实现必须按该键的币种小数位数把它换算过去
	// （用 AmountUnits），**不得直接用文本比较**——"100.5" 与 "100.50" 是同一个
	// 数值但文本不等，直接比文本会漏判重复，而 8.3 的判重口径是**数值等值**。
	// 已实测三种位数的换算：100.5/100.50 → 10050（CNY）、12.345 → 12345（KWD）、
	// 1000 → 1000（JPY）。
	//
	// # 「在库未删除」由本方法过滤，rule/dedup 不再过滤
	//
	// E-1 的比对范围是「在库且未删除的明细」，这条**在本层落实**。
	// rule/dedup 刻意不引用 DeletedAt 字段，其 TestCandidatesRangeIsCallerDuty
	// 用 AST 扫描钉住了这条分工，并在注释里说明理由：「两处过滤等于两处口径，
	// 将来必然漂移」。
	//
	// # 排除自身由调用方做
	//
	// F-5 编辑态要「**排除被编辑的这笔自身**」（E-1）。这条在 rule/dedup.Check
	// 内完成（其 Request 带自身 ID），不在本方法——本方法只按四要素取回候选，
	// 不知道调用方正在编辑哪一笔。
	//
	// keys 允许为空切片，此时返回空结果且无错误。
	FindDedupCandidates(ctx context.Context, owner OwnerUserID, keys []DedupKey) ([]entity.Expense, error)

	// AggregateMonthly 是 J 域月度聚合的**未摊分口径**（承载 ④ 第 10 条）。
	//
	// 承载原文：「**J 域月度聚合**——未摊分口径按支出日期归月 + 按类型/币种分组
	// 合计**完全下推**，摊分口径**只下推区间穿刺取行**（份额不落库，派生与聚合在
	// rule/amortize + service/stat），两口径均排除已软删除明细」。
	//
	// # 本方法只接受未摊分口径
	//
	// query.Mode 必须是 MonthlyModeUnamortized；传 MonthlyModeAmortized 时返回
	// 输入错误。摊分口径走 IterateAmortizeCovering，理由见 MonthlyModeAmortized
	// 的注释（两个口径的方法形状不同，正是「哪些计算在库里」这条分工的表达）。
	//
	// # 排除已软删除明细，无开关
	//
	// 终版 J 域末注：「**已软删除的明细不参与 J 域任何统计**」。MonthlyQuery 刻意
	// 没有 ShowDeleted 字段，故这条在类型层面就无法被绕过。
	AggregateMonthly(ctx context.Context, owner OwnerUserID, query MonthlyQuery) ([]MonthlyBucket, error)

	// IterateAmortizeCovering 是 J 域月度聚合的**摊分口径**取行部分：
	// 迭代摊分区间覆盖给定月的明细（承载 ④ 第 6 条 + 第 10 条后半）。
	//
	// 判据是「起始月 ≤ M 且 结束月 ≥ M」（终版 8.2）。份额的派生与聚合由
	// service/stat 调 rule/amortize 完成——**份额不落库**（H-3「派生不落库：
	// 摊分份额查询时实时派生，不产生额外数据行」）。
	//
	// # 为什么是迭代而不是一次返回切片
	//
	// 摊分口径下，一个月的构成明细可能来自很多个月之前（摊分月数不设上限，H-1），
	// 且 J-3 要看多月趋势——逐月调用本方法时，每次都可能取回大量行。迭代形态让
	// service/stat 边取边聚合，不同时持有全部行。
	//
	// **排除已软删除明细**（J 域末注），与 AggregateMonthly 同口径。
	//
	// 返回的 iter.Seq2 第二个元素是逐行错误：迭代中途读失败时 yield 一个非 nil
	// 错误，调用方 break 即可。这个形状的理由见 IterateForRecalc 的注释。
	IterateAmortizeCovering(ctx context.Context, owner OwnerUserID, month calendar.Month) (iter.Seq2[entity.Expense, error], error)

	// IterateForRecalc 游标迭代该用户的全部明细，供 I-7 本位币金额重算。
	//
	// # 游标迭代是承载点名的两处性能口径之一
	//
	// 分层 §六 L3 store 行末：「两处性能口径：`File` 元信息与二进制内容**分接口**、
	// `Expense` 提供**游标迭代**（I-7 与 K-3 不全量装载）」。
	//
	// # 含已软删除明细，且这一点没有开关
	//
	// 终版 I-7：「**含已软删除的明细**（它们可经 F-4 筛选查看，展示必须处于当前
	// 本位币口径；复制时的初值同理）」。service/fx 行亦重申这条。故本方法**恒
	// 包含**已删除明细，不提供排除选项——提供了就会有人为了「少算一些」而打开它，
	// 而后果是已删除明细停留在旧本位币口径，F-4 查看时显示的本位币金额是错的。
	//
	// 这与 J 域恰好相反（J 域一律排除），两者的分工见 ExpenseFilter.ShowDeleted
	// 的注释表格。
	//
	// # 「折算本位币币种 = 当前本位币」的行由调用方跳过
	//
	// 终版 I-7：「`折算本位币币种 = 当前本位币` 的行跳过」。判据是
	// `BaseCurrency == User.BaseCurrency`，而**本包不读 User**（service/fx 行：
	// 「本位币由本包按用户ID 从 store 读，不走请求上下文」——读的是 service/fx，
	// 不是本方法）。故本方法返回全部行，跳过在 service/fx。
	//
	// 也可以让调用方传一个「排除该本位币」的参数来下推这个过滤，本包不这样做：
	// 那会让「跳过」的判据出现两处（store 一处、service/fx 一处），而不变式 2
	// 明写「**『未收敛行的识别』与『重算时的跳过』是同一个判据**」——两处实现
	// 就是两个判据。
	//
	// # 为什么用 iter.Seq2 而不是自定义 Cursor 接口
	//
	// 标准库自 Go 1.23 起有 iter 包（本仓库 go 1.27.0）。调用方写
	// `for row, err := range seq { ... }`，break 时实现的清理逻辑（关闭底层游标、
	// 归还连接）由 range-over-func 自动触发。
	//
	// 自定义 Next()/Close() 接口需要调用方自觉 `defer Close()`，漏写就泄漏——而
	// store/sqlite 是**写连接单例**（分层 §六 L4），泄漏一个读游标虽不阻塞写，
	// 但 I-7 是可中断可续跑的批处理，中断路径正是最容易漏掉 Close 的地方。
	//
	// 返回值有两层错误：外层 error 是「开不出游标」（连接失败、参数非法），
	// 内层 yield 的 error 是「迭代中途某一行读失败」。分两层是因为前者发生时
	// 根本没有序列可迭代，把它塞进序列里会迫使调用方为「一次都没成功过」也走
	// 一遍循环。
	IterateForRecalc(ctx context.Context, owner OwnerUserID) (iter.Seq2[entity.Expense, error], error)

	// IterateAll 游标迭代该用户的全部明细，供 K-3 整库导出。
	//
	// 与 IterateForRecalc 的差别只在意图，行为一致（都含已删除明细——K-3 明写
	// 「`Expense`（**含已删除**）」）。分成两个方法而不共用一个：
	//
	//   - 两者的「含已删除」各有独立依据（I-7 的依据是重算口径，K-3 的依据是
	//     导出完整性），将来若其中一条改了，另一条不该被牵连；
	//   - 方法名出现在调用栈与慢查询日志里，`IterateForRecalc` 与 `IterateAll`
	//     能直接指出是哪条链路在扫全表，而一个共用的 `Iterate` 不能。
	//
	// K-3 记一条 8.4 第 11 类「数据导出」审计——这是 B-4 边界①「除数据导出外
	// 读操作一律不记审计」的唯一例外，审计由 service/export 写，不在本包。
	IterateAll(ctx context.Context, owner OwnerUserID) (iter.Seq2[entity.Expense, error], error)

	// CountByCategory 统计引用某支出类型的明细笔数（G-4 引用检查）。
	//
	// # 含已软删除明细，这是 G-4 的硬要求
	//
	// G-4：「被明细引用的类型不可删除，**已软删除的明细同样构成引用**（否则 F-4
	// 查看与 F-8 复制会指向不存在的记录）」。故本方法**不**排除已删除行。
	//
	// 这条是终版 §十二 已知代价③ 的直接来源（「支出类型一旦被任何明细（含已软
	// 删除）引用即永久不可删除……废弃类型无清理手段」），属**登记接受**的代价。
	// 实现不得为了「让用户能删掉」而排除已删除明细。
	//
	// # 必须有事务形态，否则 TOCTOU
	//
	// §九 前提 8：「ExpenseCategory 物理删除须先过引用检查（**与删除同事务**）」，
	// service/category 行给出理由：「检查与删除同事务，否则 TOCTOU」。故本方法
	// 的事务形态（CountByCategoryTx）是 G-4 正确性的必要条件，不是便利功能。
	CountByCategory(ctx context.Context, owner OwnerUserID, categoryID int64) (int64, error)

	// CountByCategoryTx 同 CountByCategory，在给定事务内执行（G-4 检查与删除同事务）。
	CountByCategoryTx(ctx context.Context, tx TxHandle, owner OwnerUserID, categoryID int64) (int64, error)
}
