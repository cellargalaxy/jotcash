// Package idgen 是 jotcash 全仓库唯一的实体标识来源（L0 基础层）。
//
// # 为什么需要本包
//
// 「仓库代码包的层级设计与划分」约定 9 规定：标识由应用层生成，store 不生成 ID、
// 不依赖自增回填。这不是风格偏好，而是不变式 4「一次提交 = 一条审计 = 一个批次」
// 的实现前提——service/audit 必须「先落审计取 ID → 业务数据挂载该 ID → 同事务
// 提交」。若审计ID 依赖数据库自增回填，则同一事务内拿不到已提交的自增值，
// Expense.来源审计ID 与 File.来源审计ID 就无从挂载，批次追溯与撤销失去边界。
//
// # 五类调用方
//
// 分层设计的 base/idgen 承载写明：调用方恰为 5 个创建方，一一对应 5 个实体。
// 本包为每类各出一个具名函数（而非只出一个通用 GenID），使「谁在造哪类 ID」
// 在调用点即可见，也让越权生成在评审时一眼可查：
//
//   - GenAuditID    → service/audit    审计ID（兼入库批次号）
//   - GenUserID     → service/account  用户ID
//   - GenCategoryID → service/category 类型ID
//   - GenFileID     → service/file     文件ID
//   - GenExpenseID  → service/intake   明细ID
//
// 五者共用同一实现，因此**全仓库 ID 取值空间统一、互不重叠**（同一时刻只会发出
// 一个值），AuditLog.对象ID 这类「历史指针」跨实体存放时不会与别的实体撞号。
//
// # 依赖与层内零依赖
//
// 本包处于 L0 且层内零依赖：不 import 仓库内任何其他包。特别地，按分层设计
// §三 问题 2 的修复口径，本包的随机性**不依赖 base/pwdhash**——ID 生成的
// 随机性挂在「密码哈希包」下与约定 5「base/ 是纯技术层」的分工相悖，
// 且会多出一条 L0 层内边。
//
// 实现下沉给 github.com/cellargalaxy/go_common/util 的 GenId()（本轮任务指定），
// 本包不自行实现发号逻辑，只做一层语义收窄与边界断言，理由见 §「为什么不是薄薄
// 一层 type alias」。
//
// # ID 形态（来自对 GenId() 的实测，非推断）
//
// GenId() 返回 int64，形态为「东八区年份后两位 + 月日时分秒 + 6 位微秒」的定长
// 数字拼接，即 060102150405.000000 去掉小数点。故：
//
//   - 常规年份（2010~2099）恒为 18 位十进制正整数；
//   - 严格单调递增：同一微秒内并发调用由 go_common 内部的互斥锁 + 微秒时刻 +1
//     保证不重号，实测顺序 20 万次、并发 64×5000 次均零重复；
//   - 时钟回拨也不会重号（同样走「上次时刻 +1」分支）；
//   - 距 int64 上界有约 35 倍余量，不存在溢出风险。
//
// # 两条被上层依赖的性质
//
// 一、**恒为正整数**。零值 0 因此可被上层安全地当作「无 ID」使用，这正是终版
// §四 AuditLog 的「操作对象类型 / 对象ID 零值 = 本次操作无具体对象或为批量操作」
// 与 Expense.来源文件ID「手工录入时为空」所依赖的语义。IsValid 是这条性质的
// 可判定形式。
//
// 二、**单调递增**。store 可直接用 ID 排序近似插入顺序，Expense 的游标迭代
// （I-7 与 K-3 不全量装载）因此可用「ID > 上次游标」作稳定游标，无需额外序列列。
// 注意这只是「同进程内单调」，不构成跨进程的全局时钟保证，也不应被用作业务时间——
// 业务时间一律用实体自己的创建时间字段。
//
// # 为什么不是薄薄一层 type alias
//
// 本包在 GenId() 之上只加三件事，每件都对应一处会静默出错的真实风险：
//
//  1. **五个具名入口**：让「5 个创建方」这条约定在类型系统里留下痕迹；
//  2. **正整数断言**：GenId() 的正整数性质源于其内部时间编码，属实现细节而非
//     承诺。一旦上游改实现（或系统时钟落在 2000~2009 这类首位为 0 的年份区间，
//     实测 2000-01-01 只有 15 位）而发出非正值，「0 = 无 ID」的语义会在
//     AuditLog.对象ID 上静默失效——审计跳转会把有主记录显示成「无具体对象」。
//     故本包在返回前断言 id > 0，不成立即 panic：这是进程启动期就该暴露的
//     环境级故障，比带着错 ID 继续写库更安全；
//  3. **String / ParseString 形态**：JSON 契约用它规避前端精度丢失，见下条；
//     且 ParseString 只认规范形态，保证「一个 ID 只有一种文本写法」。
//
// # JSON 序列化必须走字符串（实测结论，交给 api/http/dto 落实）
//
// 18 位 ID 超过 IEEE-754 双精度的安全整数上界 2^53（16 位），而 JavaScript 的
// Number 即 float64。实测 260906160443337625 经 float64 往返后变成
// 260906160443337632（偏差 7），即**前端拿到的 ID 与库里的不是同一个**。
// 因此 dto 层的 ID 字段必须以字符串传输（`json:"id,string"` 或直接用 string），
// 不能裸传 int64。本包提供 String / ParseString 承接该转换，并在
// consumer_check_test.go 里把这条以可执行断言钉住。
package idgen

import (
	"strconv"

	"github.com/cellargalaxy/go_common/util"
)

// gen 是本包唯一的发号入口，五个具名函数共用它。
//
// 收敛为一处的目的有二：一是保证五类 ID 取值空间统一（AuditLog.对象ID 跨实体
// 存放时不会撞号）；二是让「正整数」这条断言只有一处，不会漏挂。
func gen() int64 {
	return mustPositive(util.GenId())
}

// mustPositive 断言 ID 恒为正，非正即 panic。
//
// GenId() 的正整数性质来自其内部时间编码（东八区年份后两位打头），属实现细节
// 而非承诺：若系统时钟落在 2000~2009 这类年份，首位为 0，实测位数会缩短；
// 若上游改实现，更可能出现非正值。
//
// 一旦发出 0 或负值，「零值 = 无 ID」这条被 AuditLog.对象ID（终版 §四）与
// Expense.来源文件ID 依赖的语义会静默失效：有主的审计记录会被展示成
// 「无具体对象」，且已写库的错 ID 无法回溯修正。此类故障属环境级，
// 应在进程启动期立刻暴露，而不是带着错 ID 继续写库。
//
// 单独抽成函数而非内联在 gen 里，是为了让这条断言本身可被单测覆盖——
// 否则只能靠改系统时钟或替换上游实现才能触发。
func mustPositive(id int64) int64 {
	if id <= 0 {
		panic("idgen: 生成的标识非正数，无法与「零值 = 无 ID」语义共存，id=" + strconv.FormatInt(id, 10))
	}
	return id
}

// GenAuditID 生成审计ID，兼「入库批次号」（终版 §四 AuditLog.审计ID）。
//
// 唯一调用方为 service/audit。不变式 4 要求「先落审计取 ID → 业务数据挂载该 ID
// → 同事务提交」，本函数的返回值即那个「先取到的 ID」——它在事务提交前就已确定，
// 因此不需要数据库回传（约定 9）。
func GenAuditID() int64 { return gen() }

// GenUserID 生成用户ID（终版 §四 User.用户ID），是全部业务数据的归属键。
//
// 唯一调用方为 service/account：A-1 系统初始化建 admin、A-7 管理员新增账户。
func GenUserID() int64 { return gen() }

// GenCategoryID 生成支出类型ID（终版 §四 ExpenseCategory.类型ID）。
//
// 唯一调用方为 service/category（G-1 新建）。注意该实体是全模型唯一支持物理
// 删除的实体，删除后 AuditLog.对象ID 里的历史指针会指向不存在的记录——这是
// K-2「该对象已删除」提示的来源，属设计内行为，与本包无关。
func GenCategoryID() int64 { return gen() }

// GenFileID 生成文件ID（终版 §四 File.文件ID）。
//
// 唯一调用方为 service/file（C-1 上传入库）。
func GenFileID() int64 { return gen() }

// GenExpenseID 生成支出明细ID（终版 §四 Expense.明细ID）。
//
// 唯一调用方为 service/intake：D-7 提交入库时为每条原始交易行生成，
// F-8 复制走的也是同一条入库链路（复制 = 生成新明细 + 新审计）。
// F-5 逐笔编辑不生成新 ID。
func GenExpenseID() int64 { return gen() }

// String 把 ID 转为十进制字符串形态。
//
// 存在理由是 JSON 精度：18 位 ID 超过 float64 的安全整数上界 2^53，而前端的
// Number 即 float64。实测 260906160443337625 经 float64 往返后变为
// 260906160443337632——前端拿到的 ID 与库里的不是同一个，且不会报错。
// 故 api/http/dto 的 ID 字段一律以字符串出入，本函数是那一层的转换入口。
//
// 零值返回 "0"，与「无 ID」的零值语义一致，由调用方按各自场景处理
// （例如 dto 对可空的来源文件ID 输出 null 而非 "0"）。
func String(id int64) string { return strconv.FormatInt(id, 10) }

// ParseString 解析 String 的逆向操作，用于从 JSON 字符串或 URL 路径参数还原 ID。
//
// 只接受**规范形态**，即恰好等于 String 输出的那一种写法：十进制、无符号前缀、
// 无前导零、无空白、恒为正。空串、" 123"、"+123"、"007"、"12.3"、"0x10"、
// 非数字、溢出 int64 一律返回 ok = false，且此时 id 恒为 0（不返回钳制值）。
// 于是 String 与 ParseString 严格互逆，一个 ID 只有一种文本写法。
//
// 为何连 "+123"、"007" 这类能被 strconv 接受的写法也拒绝：ID 来自机器生成的
// JSON 与 URL，非规范写法即代表调用方拼装有误或是探测行为。放行它们会让同一条
// 记录拥有多种文本形态，缓存键、幂等判定与审计摘要里的 ID 就不再能按字符串
// 比对。这与同层 calendar 包拒绝 "2026-1-2" 这类非定长日期写法是同一条取向。
//
// 上层可「一次判定、两处可用」——ok 为假即按用户输入错误处理（base/errs 的
// 「用户输入错误」档），不必再额外判一次是否为正。
//
// 注意与 go_common 的 util.String2Int 的差异：后者在解析失败或溢出时返回
// 钳制值（0 / MaxInt64 / MinInt64）且吞掉 error，非法输入会静默变成一个
// 看似合法的 ID。归属校验（不变式 3）的入口不能建立在这种语义上，
// 故本包自行用 strconv.ParseInt 并把失败显式暴露出来。
func ParseString(s string) (int64, bool) {
	// 不做 TrimSpace：ID 来自机器生成的 JSON 与 URL，出现空白即代表调用方
	// 拼装有误，应当作非法输入暴露，而不是替它猜测意图后放行。
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	if id <= 0 {
		return 0, false
	}
	// 规范形态校验。strconv.ParseInt 会接受 "+123" 与 "007"，二者的数值虽合法，
	// 文本却不是 String 的输出。以「回写后是否逐字节相同」作判据，一次覆盖
	// 正号前缀、前导零及其他所有非规范写法，无需逐条枚举。
	if strconv.FormatInt(id, 10) != s {
		return 0, false
	}
	return id, true
}

// IsValid 判定 id 是否为一个「有主」的标识，即本包发出过的形态（恒为正）。
//
// 它是「零值 = 无 ID」这条语义的可判定形式，供三处使用：
//   - store 的按 ID 访问方法在下压 SQL 前做前置断言；
//   - service 层区分 AuditLog 的「有具体对象」与「零值 = 批量操作」
//     （终版 §四 操作对象类型 / 对象ID）；
//   - service 层区分 Expense.来源文件ID 的「有来源文件」与「手工录入时为空」。
//
// 本函数只判形态、不判存在性：一个 IsValid 为真的 ID 完全可能在库里查不到
// （AuditLog.对象ID 是历史指针，不是外键）。
func IsValid(id int64) bool { return id > 0 }
