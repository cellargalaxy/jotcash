package store

import (
	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// 本包声明的文案键。**语言文件必须包含它们全部**（与 parser、rule/passwd 同例）。
//
// # 档位分配
//
//	KeyNotFound              KindInvalidInput  400  按 ID 取不到，或归属不符
//	KeyUsernameConflict      KindConflict      409  User.用户名 全局唯一
//	KeyCategoryNameConflict  KindConflict      409  ExpenseCategory.(用户,名称) 唯一
//	KeyCategoryInUse         KindInvalidInput  400  G-4 引用检查未通过
//	KeyOwnerRequired         KindInternal      500  调用方漏传归属用户ID
//	KeyAmountOutOfRange      KindInvalidInput  400  见 column.go
//	KeyRateOutOfRange        KindInvalidInput  400  见 column.go
//
// 两个 409 键对应承载 ⑤ 的两项唯一索引；middleware 已把「唯一冲突 → 409」写进
// 五档映射（分层 §六 L6 middleware 行）。
const (
	// KeyNotFound 是「记录不存在」的文案键。
	//
	// 占位参数：Entity（实体名，如 "Expense"）。**刻意不带 ID**：错误会流向
	// 日志与前端两条路径，把对象ID 放进面向用户的文案里没有增益（用户拿不到
	// 别的 ID 去试），而日志侧的诊断信息已由 cause 承担。
	//
	// # 「不存在」与「归属不符」共用本键，这是红线 2
	//
	// 分成两个键就等于提供了一个存在性预言：拿他人的明细ID 来查，若返回
	// 「存在但不属于你」，调用方即确认了该 ID 有效。不变式 3 要防的正是这类
	// 跨归属的信息泄漏——内容没泄漏，存在性泄漏了同样是泄漏，且它足以支撑
	// 「遍历 ID 空间数出别人有多少笔明细」。
	//
	// 档位是 400 而非 404：base/errs 只有五档，没有「未找到」档
	// （见 errs.Kind 的五个取值）。归到 KindInvalidInput 的依据是
	// errs_test.go 里已有的先例——`Wrap(sql.ErrNoRows, KindInvalidInput,
	// "expense.not_found", nil)`，即「你给的 ID 不对」属于输入错误。
	// **不归 KindForbidden**：那一档的语义是「登录态有效但访问的不是自己的
	// 数据」，用它就又把归属不符和不存在区分开了，红线 2 当场失效。
	KeyNotFound = "store.not_found"

	// KeyUsernameConflict 是「用户名已存在」的文案键（承载 ⑤ 第一项）。
	//
	// 占位参数：Username（冲突的用户名）。用户名不是敏感信息（A-7 的账户列表
	// 本就展示它），带上它能让 A-7 新增账户的提示直接可用。
	//
	// # 由唯一索引保证，先查后插只用于友好提示
	//
	// 承载 ⑤ 明写这条，§六 L5 service/account 行进一步说明 A-1 系统初始化
	// 「用户名唯一由 store 唯一索引保证，**重复初始化天然幂等**」。故 L4 实现
	// 必须把驱动的唯一约束违反映射成本键，而不能依赖「先 SELECT 再 INSERT」
	// ——两个并发的 A-1 都能通过 SELECT，然后一个 INSERT 成功、另一个撞索引。
	KeyUsernameConflict = "store.username_conflict"

	// KeyCategoryNameConflict 是「同名支出类型已存在」的文案键（承载 ⑤ 第二项）。
	//
	// 占位参数：Name（冲突的类型名称）。
	//
	// 唯一键是 (归属用户ID, 名称) 而非单独的名称：G-1 明写「**同一用户下**名称
	// 唯一」，做成全局唯一会让两个用户不能各建一个「餐饮」。
	KeyCategoryNameConflict = "store.category_name_conflict"

	// KeyCategoryInUse 是「支出类型被明细引用，不可删除」的文案键（G-4）。
	//
	// 占位参数：Name（类型名称）、Count（引用笔数）。带笔数是为了让提示可行动
	// ——用户能据此判断是去改那几笔明细还是放弃删除。
	//
	// # 引用检查含已软删除明细
	//
	// G-4 明写「**已软删除的明细同样构成引用**（否则 F-4 查看与 F-8 复制会指向
	// 不存在的记录）」。这条是终版 §十二 已知代价③ 的来源：「支出类型一旦被任何
	// 明细（含已软删除）引用即永久不可删除」——已删除明细只读、其类型不可改，
	// 系统又无停用能力，故废弃类型无清理手段。这是**登记接受**的代价，不是缺陷，
	// L4 实现不得为了「让用户能删掉」而把已删除明细排除在引用检查之外。
	KeyCategoryInUse = "store.category_in_use"

	// KeyOwnerRequired 是「缺归属用户ID」的文案键。
	//
	// 占位参数：Entity（实体名）。
	//
	// # 为什么是 500 而不是 400
	//
	// 归属用户ID 不来自用户输入，而来自登录上下文（middleware 写 ctxs →
	// handler 读出并显式传 L5，见约定 6）。它为零值只有一种可能：某处代码忘了
	// 传。那是接线错误，不是用户填错了什么——报 400 会让前端把它渲染成一条
	// 表单提示，用户对着一个自己无法修正的错误反复重试。
	//
	// 这条同时是「零值不是通配符」的执行点：若在此放行，查询条件里就少了归属
	// 过滤，一次全表扫描把所有用户的数据都返回给了当前请求，即不变式 3 所说的
	// 「漏一处等于全量泄露」。
	KeyOwnerRequired = "store.owner_required"
)

// NewNotFoundError 构造「记录不存在」错误（含归属不符，见 KeyNotFound 的红线 2）。
//
// entityName 传实体名（"Expense"/"File"/"AuditLog"/"ExpenseCategory"/"User"）。
// 用字符串而非 enum.ObjectType：后者只有 4 项且刻意没有 User
// （见 enum.ObjectType 注释「四项的取值范围」），而本错误对 5 个仓储都适用。
func NewNotFoundError(entityName string) error {
	return errs.NewInvalidInput(KeyNotFound, errs.Params{"Entity": entityName})
}

// WrapNotFoundError 同 NewNotFoundError，但保留底层错误供日志诊断。
//
// 供 L4 在驱动返回「无行」时使用（如 sql.ErrNoRows）。cause 只进日志，
// 不进前端——errs.Error 把 kind/msgKey/params 与 cause 分开正是为此。
func WrapNotFoundError(cause error, entityName string) error {
	return errs.Wrap(cause, errs.KindInvalidInput, KeyNotFound, errs.Params{"Entity": entityName})
}

// NewUsernameConflictError 构造「用户名已存在」错误（409）。
func NewUsernameConflictError(username string) error {
	return errs.NewConflict(KeyUsernameConflict, errs.Params{"Username": username})
}

// WrapUsernameConflictError 同上，保留驱动的唯一约束违反错误供日志诊断。
//
// L4 实现应把驱动的唯一索引冲突映射到这里，而不是自己判断「是不是唯一冲突」
// 之后返回一个裸错误——中间件按档位映射 HTTP 状态，裸错误会落到 500。
func WrapUsernameConflictError(cause error, username string) error {
	return errs.Wrap(cause, errs.KindConflict, KeyUsernameConflict, errs.Params{"Username": username})
}

// NewCategoryNameConflictError 构造「同名支出类型已存在」错误（409）。
func NewCategoryNameConflictError(name string) error {
	return errs.NewConflict(KeyCategoryNameConflict, errs.Params{"Name": name})
}

// WrapCategoryNameConflictError 同上，保留驱动错误供日志诊断。
func WrapCategoryNameConflictError(cause error, name string) error {
	return errs.Wrap(cause, errs.KindConflict, KeyCategoryNameConflict, errs.Params{"Name": name})
}

// NewCategoryInUseError 构造「支出类型被引用，不可删除」错误（G-4，400）。
//
// count 是引用笔数，**含已软删除明细**（见 KeyCategoryInUse 的注释）。
func NewCategoryInUseError(name string, count int64) error {
	return errs.NewInvalidInput(KeyCategoryInUse, errs.Params{
		"Name":  name,
		"Count": count,
	})
}

// NewOwnerRequiredError 构造「缺归属用户ID」错误（500）。
//
// L4 各实现应在方法入口用 OwnerUserID.IsValid 判定后调用它。
func NewOwnerRequiredError(entityName string) error {
	return errs.NewInternal(KeyOwnerRequired, errs.Params{"Entity": entityName})
}
