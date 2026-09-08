package store

import (
	"context"
	"iter"

	"github.com/cellargalaxy/jotcash/internal/entity"
)

// FileRepo 是 File 仓储（承载 ①，归属型）。
//
// # 元信息与二进制内容分接口——承载点名的两处性能口径之一
//
// 分层 §六 L3 store 行末：「两处性能口径：`File` **元信息与二进制内容分接口**、
// `Expense` 提供游标迭代（I-7 与 K-3 不全量装载）」。
//
// 依据是 C-1 与 C-2 的形态差异：C-1 文件管理页「列出全部已上传文件（文件名、格式、
// 大小、上传时间、来源审计）」——这五项都是元信息，一行都不需要文件内容；而 C-2
// 原文件下载才需要内容。entity.File.Content 是 []byte，原文入库（C-2「原文入库、
// 原样下载」，B-4 边界②）意味着它可以是几 MB，把它带进列表查询会让一次 20 行的
// 分页拉走上百 MB。
//
// 故本接口把它拆成两条路：
//
//	ListMeta / GetMeta / ListMetaBySourceAudit   只取元信息，Content 恒为 nil
//	GetContent                                    只取内容
//
// # 无删除方法（前提 8）
//
// 承载 ③：「`User`/`File`/`AuditLog` **无删除方法**」。C 域功能表里没有删除文件，
// 且 C-3 的溯源链（明细 → 来源文件）要求文件长期留存——删掉文件会让已入库明细的
// 「来源文件」指向不存在的记录。故本接口不得出现任何删除方法。
type FileRepo interface {
	// CreateTx 新增文件记录（C-1 上传，D-7 提交入库时挂 SourceAuditID），
	// 在给定事务内执行。
	//
	// # 只提供事务形态
	//
	// 文件入库属 8.4 第 5 类「文件上传」审计（分层 §六 L5 service/file 行），
	// 走不变式 4 的同事务语义：「先落审计取 ID → 业务数据挂载该 ID → 同事务提交」。
	// 故不存在事务外新增文件的合法用例。
	//
	// file.ID 由调用方经 base/idgen.GenFileID 生成（约定 9），SourceAuditID
	// 必须已挂上本次上传的审计ID。实现不得自行生成或改写这两个字段。
	//
	// # 文件密码不落库
	//
	// entity.File.Password 是**入库时的临时字段**（D-2 的解密密码），
	// C-2 明写原文入库、原样下载，密码只在解析时使用。L4 实现**不得**把它写进
	// 任何列——落库了就等于把加密文件的密码与文件本身存在一起，加密形同虚设。
	// TestFilePasswordNotPersisted 在 L4 那轮验证这条。
	CreateTx(ctx context.Context, tx TxHandle, owner OwnerUserID, file entity.File) error

	// GetMeta 按文件ID 查元信息，**不含二进制内容**（Content 恒为 nil）。
	//
	// 取不到（不存在或归属不符）返回 KeyNotFound（红线 2）。
	GetMeta(ctx context.Context, owner OwnerUserID, id int64) (entity.File, error)

	// GetContent 按文件ID 取二进制内容（C-2 原文下载）。
	//
	// 返回的是**原样字节**（C-2「原文入库、原样下载」，B-4 边界②：
	// 「上传的原文件原样留存，不做任何格式转换或压缩」）。实现不得做任何转换。
	//
	// # 为什么返回 entity.File 而不只是 []byte
	//
	// C-2 下载响应需要文件名与格式来设置 Content-Disposition 与 Content-Type，
	// 只返回字节会迫使调用方再调一次 GetMeta——两次查询之间该文件的元信息与内容
	// 理论上可以不一致（虽然本系统没有更新文件的功能）。一次返回则天然一致。
	//
	// 取不到（不存在或归属不符）返回 KeyNotFound（红线 2）。
	GetContent(ctx context.Context, owner OwnerUserID, id int64) (entity.File, error)

	// ListMeta 按归属用户分页列出文件元信息（承载 ④ 第 4 条）。
	//
	// 承载原文：「**按归属用户分页列 `File`**（C-1 管理页，兼 F-4「来源文件」下拉，
	// **只取元信息**）」。返回该页记录与总条数。
	//
	// 顺序按上传时间降序 + 文件ID 降序兜底：C-1 是文件管理页，最近上传的应在最前；
	// 兜底键的理由同 ExpenseSort.Normalize（同一批上传可能落在同一时间戳上，
	// 无唯一兜底键则分页可能重复或漏行）。
	//
	// # 一个方法同时服务管理页与下拉，这是刻意的
	//
	// 分层 §八 C 域明写「文件管理页与 F-4『来源文件』下拉**同源**」。同源的意义是
	// 下拉里能选到的文件与管理页能看到的文件恒为同一集合——两个方法就会漂移
	// （比如下拉忘了过滤归属）。这与 G-4 的「筛选框与编辑框下拉同源」是同一种处置。
	ListMeta(ctx context.Context, owner OwnerUserID, page Page) ([]entity.File, int64, error)

	// ListMetaBySourceAudit 按来源审计ID 查文件元信息（承载 ④ 第 5 条：C-3 反向）。
	//
	// 承载原文：「**按来源审计ID 查 `File`**（C-3 反向）」。
	//
	// C-3 的正向链是「明细 → 来源文件」，反向链是「审计 → 该批次的文件」：
	// K-2 审计页看到一条「数据入库」记录时，要能跳到产生它的那个文件。
	//
	// # 返回切片而非单条
	//
	// 一次上传通常对应一个文件，但 C-1 的上传接口未被限定为单文件，且「一次提交
	// 入库」（D-7）的审计可以覆盖多个文件的解析结果。返回切片使这两种形态都能
	// 表达；调用方按实际数量处理即可。空结果不是错误（该审计可能不是文件类操作）。
	ListMetaBySourceAudit(ctx context.Context, owner OwnerUserID, sourceAuditID int64) ([]entity.File, error)

	// IterateAll 游标迭代该用户的全部文件（K-3 整库导出）。
	//
	// K-3 导出范围含 File，而文件内容可以很大，故走游标形态而非一次返回切片，
	// 与 ExpenseRepo.IterateAll 同理（承载「I-7 与 K-3 不全量装载」）。
	//
	// # 含二进制内容
	//
	// 与 ListMeta 不同，本方法**含** Content——K-3 是整库导出，导出一份不含原文件
	// 的备份在恢复时是不完整的（C-2 的原文留存承诺就此落空）。这也是为什么它必须
	// 走游标：逐个文件流式写出，不同时持有全部内容。
	//
	// 返回值的两层错误语义同 ExpenseRepo.IterateForRecalc。
	IterateAll(ctx context.Context, owner OwnerUserID) (iter.Seq2[entity.File, error], error)
}
