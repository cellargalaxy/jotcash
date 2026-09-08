package store

import (
	"context"

	"github.com/cellargalaxy/jotcash/internal/entity"
)

// CategoryRepo 是 ExpenseCategory 仓储（承载 ①，归属型）。
//
// # 唯一约束是 (归属用户ID, 名称) 复合唯一，不是名称全局唯一
//
// 承载 ⑤：「`User.用户名` 全局唯一、`ExpenseCategory.(归属用户ID, 名称)` 唯一，
// **由存储层唯一索引保证**，冲突返回 `base/errs` 的「唯一冲突」档（先查后插只用于
// 友好提示，不作正确性依据）」。
//
// 复合而非全局：类型字典「按归属用户隔离」（G-1），甲用户建了「餐饮」不该妨碍乙
// 用户也建「餐饮」。若做成名称全局唯一，多用户场景下先建的人就占掉了公共词汇。
//
// # 这是唯一有物理删除的仓储，且删除必须先过引用检查
//
// 承载 ③：「`ExpenseCategory` 的物理删除签名要求先过引用检查（含已软删明细）」。
// §九 前提 8 与 service/category 行进一步定死「**与删除同事务**，否则 TOCTOU」。
//
// 为什么这里是物理删除而非软删除：类型字典不是业务记录，没有「历史上曾经存在过
// 某个类型」的留存需求；而 G-4 的引用检查已经保证「被引用的类型删不掉」，
// 于是能删掉的类型必然没有任何明细指向它，物理删除不会产生悬空引用。
type CategoryRepo interface {
	// Create 新增支出类型（G-1）。
	//
	// category.ID 由调用方经 base/idgen.GenCategoryID 生成（约定 9）。
	//
	// # 名称冲突返回 409，且这是正确性依据
	//
	// 承载 ⑤ 明写唯一性「**由存储层唯一索引保证**……**先查后插只用于友好提示，
	// 不作正确性依据**」。故 L4 实现必须建 (owner_user_id, name) 唯一索引，
	// 并把驱动的唯一约束违反映射为 KeyCategoryNameConflict（用
	// WrapCategoryNameConflictError）。
	//
	// G-1 的「提示用」查询是 GetByName，它只负责在用户还在填表时给出友好提示；
	// 两个并发请求同时通过了 GetByName 检查的情况只能靠唯一索引兜住。
	Create(ctx context.Context, owner OwnerUserID, category entity.ExpenseCategory) error

	// CreateTx 同 Create，在给定事务内执行。
	//
	// G-1/G-3/G-4 的增删改都记 8.4 第 8 类「支出类型管理」审计（分层 §六 L5
	// service/category 行），走同事务语义。
	CreateTx(ctx context.Context, tx TxHandle, owner OwnerUserID, category entity.ExpenseCategory) error

	// Rename 重命名支出类型（G-3）。
	//
	// # 重命名不影响已有明细的归类
	//
	// G-3 明写这条，依据是 entity.Expense 存的是 CategoryID 而非名称——改名只动
	// 字典行，明细的引用不变。故本方法**不**需要级联更新任何明细，
	// 也不得触碰 Expense 表。
	//
	// 名称冲突同 Create，返回 KeyCategoryNameConflict（409）。
	Rename(ctx context.Context, owner OwnerUserID, categoryID int64, name string) error

	// RenameTx 同 Rename，在给定事务内执行。
	RenameTx(ctx context.Context, tx TxHandle, owner OwnerUserID, categoryID int64, name string) error

	// DeleteTx 物理删除支出类型（G-4），**只有事务形态**。
	//
	// # 只提供事务形态，这是 G-4 正确性的必要条件
	//
	// §九 前提 8：「`ExpenseCategory` 物理删除须先过引用检查（**与删除同事务**）」。
	// service/category 行给出理由：「检查与删除同事务，否则 TOCTOU」。
	//
	// 具体的竞态是：检查时该类型无明细引用 → 另一个请求在此刻提交了一笔用该类型的
	// 明细 → 删除执行 → 那笔明细的支出类型ID 指向一个已不存在的字典行。而 F-5
	// 「仅对未删除明细开放」且已删除明细的支出类型永久不可改（§8.5），
	// 这笔明细的归类就此永久损坏、无任何修复入口。
	//
	// 若提供非事务形态的 Delete，上述竞态就有了一条合法可达的代码路径。故本接口
	// **只有** DeleteTx——调用方必须先 WithinTx，在同一事务里调
	// ExpenseRepo.CountByCategoryTx 再调本方法。
	//
	// # 实现必须在删除语句里再带一次引用检查
	//
	// 即便调用方已经在同事务里检查过，L4 实现仍应在 DELETE 的条件里带上
	// 「不存在引用该类型的明细」这一子查询，命中 0 行时返回 KeyCategoryInUse。
	// 理由是纵深防御：调用顺序是运行时约定，无法由类型系统保证；而带上子查询的
	// 代价只是一次索引查找。
	//
	// 引用检查的口径**含已软删除明细**（G-4 明写），代价是终版 §十二 已知代价③
	// （废弃类型无清理手段）——这是**登记接受**的，实现不得为了让用户能删而排除
	// 已删除明细。详见 errors.go 的 KeyCategoryInUse。
	DeleteTx(ctx context.Context, tx TxHandle, owner OwnerUserID, categoryID int64) error

	// GetByID 按类型ID 查支出类型。
	//
	// 取不到（不存在或归属不符）返回 KeyNotFound（红线 2）。
	GetByID(ctx context.Context, owner OwnerUserID, categoryID int64) (entity.ExpenseCategory, error)

	// GetByName 按「归属用户 + 名称」查支出类型（承载 ④ 第 8 条：G-1 提示用）。
	//
	// 承载原文：「**按「归属用户 + 名称」查 `ExpenseCategory`**（G-1 提示用）」。
	//
	// 「提示用」是关键限定：本方法只为在用户提交前给出「该名称已存在」的友好提示，
	// **不作为唯一性的正确性依据**（承载 ⑤：「先查后插只用于友好提示」）。
	// 正确性由 Create/Rename 撞唯一索引保证。
	//
	// 取不到时返回 KeyNotFound——调用方据此判断「名称可用」，这是本方法唯一的
	// 正常用法，故调用方**必须**用 errs.IsKind 区分「未找到」与「查询失败」，
	// 不能把任何错误都当成「名称可用」（那会在数据库故障时放行重复名称，
	// 虽然唯一索引仍会兜住，但错误提示会变成 409 而非 500，掩盖真实故障）。
	GetByName(ctx context.Context, owner OwnerUserID, name string) (entity.ExpenseCategory, error)

	// List 列出该用户的全部支出类型（承载 ④ 第 9 条：G-4）。
	//
	// 承载原文：「**按归属用户列全部类型**（G-4：**筛选框与编辑框下拉同源、恒含
	// 全部**）」。
	//
	// # 不分页、无筛选，且这两点都是刻意的
	//
	// 「恒含全部」是 G-4 的硬要求，它排除了分页与筛选：若列表可以只返回一部分，
	// F-4 的类型筛选下拉就可能缺项，用户筛不到某个类型的明细，看起来像数据丢了。
	//
	// 「同源」的意义是 F-4 筛选框与 F-5 编辑框下拉用**同一个方法**取数——两个方法
	// 必然漂移（比如筛选框忘了带上刚新建的类型）。这与 C-1 的「文件管理页与 F-4
	// 来源文件下拉同源」是同一种处置。
	//
	// 顺序按名称升序：这是一个直接渲染成下拉的列表，用户按名字找。顺序必须稳定，
	// 理由同 ExpenseSort.Normalize。
	//
	// # 「未分类」不在返回值里
	//
	// G-2 明写「明细的支出类型为空即未分类，**字典中不设「未分类」记录**」。
	// 故本方法**不**返回一个虚构的「未分类」项——那一选项由 L6 在渲染下拉时自行
	// 添加，对应 ExpenseFilter.IncludeUncategorized。若本层伪造一条记录，
	// 它就会有一个 ID，而那个 ID 不存在于任何表里，Create/Rename/Delete 都会对它
	// 报错，界面上表现为「有一个删不掉也改不了的类型」。
	List(ctx context.Context, owner OwnerUserID) ([]entity.ExpenseCategory, error)
}
