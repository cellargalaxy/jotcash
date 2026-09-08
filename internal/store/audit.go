package store

import (
	"context"
	"iter"

	"github.com/cellargalaxy/jotcash/internal/entity"
)

// AuditLogRepo 是 AuditLog 仓储（承载 ①，归属型）。
//
// # 只有写入与读取，没有更新，也没有删除
//
// 承载 ③：「`User`/`File`/`AuditLog` **无删除方法**」。审计是仅追加的：
// K-2 明写「本页**只读，不承担任何写操作**」，分层 §六 L5 service/audit 行
// 「K-2 只读，本包不提供任何审计的改删入口」。
//
// 故本接口只有 Create* 与查询方法——**没有 Update，也没有任何删除**。
// 这是前提 8「上层想违反也没有可调的方法」在本接口的形态：审计记录一旦写入，
// 在本层就没有任何方法能改动或抹除它。TestNoDeleteMethods 与
// TestAuditLogHasNoUpdateMethod 分别断言这两条。
//
// # 归属字段名是 UserID，不是 OwnerUserID
//
// entity.AuditLog 的归属字段叫 **UserID**（9 项全部必填，见该类型）。本接口的
// 方法参数仍用 OwnerUserID 具名类型（红线 1 的统一形态），实现负责把它落到
// user_id 列。这处命名差异是 entity 侧既有事实，不在本轮改动范围。
type AuditLogRepo interface {
	// Create 写入一条审计记录（不变式 4 的「独立事务审计」语义）。
	//
	// # 两种写入语义，本方法是第一种
	//
	// 分层 §六 L5 service/audit 行：「两种写入语义均可用；**同事务方法接受外部
	// 句柄**」。对应本接口的 Create 与 CreateTx：
	//
	//	Create    独立事务写入。用于「审计必须留下，即使业务操作失败」的场景——
	//	          8.4 第 2 类登录失败（A-8）、第 9 类本位币重算的部分失败（I-7）。
	//	CreateTx  同事务写入。用于不变式 4 的「先落审计取 ID → 业务数据挂载该 ID
	//	          → 同事务提交」——D-7 数据入库、A-1 初始化等。
	//
	// 两者必须都有：只有 CreateTx 时，登录失败的审计会随着「登录失败」这个错误
	// 一起回滚掉，A-8 的失败排查就永远看不到记录；只有 Create 时，D-7 的
	// 「N 条明细 + 1 条审计」无法原子提交，不变式 4 当场失效。
	//
	// log.ID 由调用方经 base/idgen.GenAuditID 生成（约定 9）。这一点对不变式 4
	// 尤其要紧：审计ID 必须在写入**之前**就已知，业务数据才能挂载它——若依赖
	// 自增回填，就必须先提交审计再写业务数据，同事务语义无从谈起。
	Create(ctx context.Context, owner OwnerUserID, log entity.AuditLog) error

	// CreateTx 在给定事务内写入一条审计记录（不变式 4 的同事务语义）。
	//
	// 见 Create 的注释对两种语义的分工说明。
	CreateTx(ctx context.Context, tx TxHandle, owner OwnerUserID, log entity.AuditLog) error

	// GetByID 按审计ID 查一条记录。
	//
	// 供 K-2 详情与 C-3 溯源使用。取不到（不存在或归属不符）返回 KeyNotFound
	// （红线 2）。
	GetByID(ctx context.Context, owner OwnerUserID, id int64) (entity.AuditLog, error)

	// List 按归属用户分页列出审计记录（承载 ④ 第 3 条：K-2）。
	//
	// 承载原文：「**按归属用户分页列 `AuditLog`**（K-2，支持操作类型与时间区间，
	// 兼 F-4「来源审计」下拉数据源）」。筛选维度见 AuditFilter。
	//
	// 返回该页记录与筛选结果总条数。
	//
	// 顺序按操作时间降序 + 审计ID 降序兜底：K-2 是操作日志页，最近的操作应在最前。
	// 兜底键在这里格外要紧——D-7 一次提交的「数据入库」审计与同批次的其他审计
	// 完全可能落在同一时间戳上（分层 §六 L5 service/audit 的时间戳由调用方传入），
	// 无唯一兜底键则分页可能重复或漏行（理由同 ExpenseSort.Normalize）。
	List(ctx context.Context, owner OwnerUserID, filter AuditFilter, page Page) ([]entity.AuditLog, int64, error)

	// IterateAll 游标迭代该用户的全部审计记录（K-3 整库导出）。
	//
	// K-3 导出范围含 AuditLog。审计是仅追加的，其行数随使用时间线性增长且没有
	// 任何清理手段（本接口无删除方法），故它是全库最有可能变得很大的一张表——
	// 走游标形态而非一次返回切片（承载「I-7 与 K-3 不全量装载」）。
	//
	// 返回值的两层错误语义同 ExpenseRepo.IterateForRecalc。
	IterateAll(ctx context.Context, owner OwnerUserID) (iter.Seq2[entity.AuditLog, error], error)
}
