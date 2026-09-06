// Package idgen 提供 5 类实体的唯一标识生成，是**全仓库唯一的 ID 来源**。
//
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md` §六 L0 `base/idgen` 行与约定 9：
// 「`base/idgen` 是全仓库唯一 ID 来源，`store` 不生成 ID、不依赖自增回填」——
// 不变式 4 的「先落审计取 ID → 明细挂载该 ID → 同事务提交」因此不需要 DB 回传。
//
// 调用方恰为 5 个创建方（同行原文，一一对应，不存在第六个）：
//
//	service/audit    → GenAuditID     审计ID（兼「入库批次号」，见 §八 8.4）
//	service/account  → GenUserID      用户ID
//	service/category → GenCategoryID  类型ID
//	service/file     → GenFileID      文件ID
//	service/intake   → GenExpenseID   明细ID
//
// 分层约束（§六 L0 表头 + 约定 5）：本包**不 import 任何 base 兄弟包**，
// L0 层内零依赖成立；也不含任何业务语义类型（`base/` 是纯技术层）。
// 特别地，本包**不依赖 `base/pwdhash`**——这正是 §三 问题 2 修法的目的所在。
package idgen

import (
	"fmt"

	"github.com/cellargalaxy/go_common/util"
)

// gen 生成一个唯一标识，是本包唯一的 ID 产出点，5 个导出函数全部委托它。
//
// 实现直接复用 `go_common/util` 的 GenId()（本轮 prompt 指定），不自行造轮子：
// 该函数以「年份后两位 + 月日时分秒 + 微秒」拼成 18 位十进制数，并由包级互斥量
// 保证发号的微秒时刻严格递增（同一微秒内连续调用或系统时钟回拨时取上次值 +1），
// 因此**顺序与并发下均唯一、且严格递增**。严格递增对 store/sqlite 有额外收益：
// 主键顺序追加写入，不产生随机插入导致的页分裂。
//
// 关于唯一性的前提：GenId 的互斥量是**进程级**的，唯一性因此只在单进程内成立。
// 本系统为单二进制个人系统，且 store/sqlite 已定「写连接单例 + WAL」（§六 L4），
// 该前提成立；若将来改为多实例部署，需在本包换用分布式方案，5 个调用方无需改动
// ——这也是本包存在的意义：把 ID 方案收敛到一处。
//
// 返回裸 int64 而不定义命名类型（如 type ID int64），是为了让 `entity` 保持
// 与 §六 L1.1 一致的依赖列（`currency`、`enum`、`base/decimal`、`base/calendar`，
// 不含 `base/idgen`）——若出命名类型，`entity` 的 5 个 ID 字段就得凭空多一条依赖边。
func gen() int64 {
	id := util.GenId()
	// 零值兜底：0 在本仓库是**保留值**，不能作为任何实体的 ID 落库。
	// 依据 `doc/decisions/记账系统功能与模型.md` §四：多处属性以「零值 = 未设置」表意
	// （`Expense.来源文件ID` 手工录入时为空、`AuditLog.对象ID` 零值 = 批量操作无具体对象）。
	// 一旦生成 0 并落库，「未设置」与「真实指向」将无法区分，归属校验与批次追溯会静默错乱。
	//
	// 该分支在格式上不可达——GenId 的月/日字段恒 ≥ 01，最小值形如 000101000000000000，
	// 实测 20 万次亦从未出现 0——故此处只作不可达兜底。
	// 取 panic 而非返回 error 有两条理由：① 本包依赖列为 `—`，用不了 base/errs
	// （那是 L0 兄弟包，import 会破坏层内零依赖）；② 若签名带 error，5 个调用方就得
	// 为一条不可达分支各写一遍错误处理。ID 为 0 属于「宁可当场崩，不可静默写坏数据」。
	if id == 0 {
		panic(fmt.Sprintf("idgen：生成的标识为零值，禁止作为实体标识使用，id=%d", id))
	}
	return id
}

// GenAuditID 生成审计ID，供 service/audit 使用。
//
// 该 ID 同时是「入库批次号」（§八 8.4：一次提交 = 一条审计 = 一个可追溯批次），
// 是 F-4「来源审计」筛选与批次撤销的唯一依据，因此必须先于业务数据生成：
// 不变式 4 要求「先落审计取 ID → 业务数据挂载 → 同事务提交」。
func GenAuditID() int64 {
	return gen()
}

// GenUserID 生成用户ID，供 service/account 使用（A-1 系统初始化建 admin、A-7 新增账户）。
//
// 用户ID 是全部业务数据的归属键（§四 User），也是不变式 3 归属校验的比对基准。
func GenUserID() int64 {
	return gen()
}

// GenCategoryID 生成支出类型ID，供 service/category 使用（G-1 新建类型）。
func GenCategoryID() int64 {
	return gen()
}

// GenFileID 生成文件ID，供 service/file 使用（C-1 上传入库）。
func GenFileID() int64 {
	return gen()
}

// GenExpenseID 生成支出明细ID，供 service/intake 使用。
//
// 三种录入进入方式（文件导入 / 手工录入 / 复制已删除明细）在 service/intake 收敛，
// 明细ID 一律在此生成后随 D-7 提交入库（§六 L5 `service/intake` 行）。
func GenExpenseID() int64 {
	return gen()
}
