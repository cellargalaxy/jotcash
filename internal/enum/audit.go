package enum

import (
	"database/sql/driver"
)

// OperationType 是操作类型，承载 AuditLog.操作类型（终版 §8.4 的 11 类）。
//
// # 11 类是闭集，且与写入方一一对应
//
// 终版 B-4 定「11 类操作记为审计记录，是全系统唯一的留痕依据」，分层方案 §九 下方
// 列出 9 个 service 与这 11 类的对应关系，且明写「一一对应、无第二写入方」：
//
//	1  系统初始化       service/account   A-1 创建 admin
//	2  登录             service/auth      A-4（失败以「操作结果 = 失败」表达）
//	3  账户管理         service/account   A-7 新增 / 启停 / 重置他人密码
//	4  密码修改         service/account   A-8 自助改密、首登强制改密
//	5  文件上传         service/file      C-1 / D-2
//	6  数据入库         service/intake    D-7（含复制提交）
//	7  明细编辑与删除   service/expense   F-5 编辑、F-8 删除
//	8  本位币金额重算   service/fx        I-7
//	9  支出类型维护     service/category  G-1 新建 / 删除
//	10 用户偏好设置     service/preference K-1 本位币 / 界面语言切换
//	11 数据导出         service/export    K-3 整库导出、F-7 明细导出
//
// # 三个刻意的缺席
//
//   - **无「登出」**：A-4 明写「登出为前端丢弃令牌，服务端无动作、不记审计」。
//   - **无「明细新增」**：终版 §8.4 表下注「明细的新增不在本表单列——一律走数据
//     入库（含复制提交）」。
//   - **无「回滚」**：终版 §十二 记「D-8 一键回滚整项取消，审计枚举 12 → 11 类」，
//     批次撤销已下沉为「按来源审计筛选 + 批量删除」，走第 7 类。
//
// 另按 B-4 边界①，**除数据导出外读操作一律不记审计**，故本枚举没有任何「查询 /
// 查看 / 下载」类目——第 11 类是唯一记审计的读操作。
type OperationType string

const (
	// OperationTypeSystemInit 系统初始化（第 1 类）。A-1 创建 admin，
	// 对象为 User / admin 用户ID。写入方 service/account。
	OperationTypeSystemInit OperationType = "system_init"

	// OperationTypeLogin 登录（第 2 类）。A-4；对象类型为零值。
	//
	// 失败以 操作结果 = 失败 表达，且遵「有主则记、无主不记」：用户名存在（含密码错、
	// 账户禁用、锁定期内）则记录并归属该用户，用户名不存在则**不记审计**（无归属、
	// 无消费方）。写入方 service/auth，失败类走独立事务语义。
	OperationTypeLogin OperationType = "login"

	// OperationTypeAccountManage 账户管理（第 3 类）。A-7 新增 / 启停 / 重置他人密码，
	// 对象为 User / 目标用户ID。
	//
	// 按 B-4 边界③，管理员对他人账户的操作**归属管理员**，被操作者在 K-2 中看不到。
	OperationTypeAccountManage OperationType = "account_manage"

	// OperationTypePasswordChange 密码修改（第 4 类）。A-8 自助改密、首登强制改密，
	// 对象为 User / 自身用户ID。
	//
	// 与第 3 类的「重置他人密码」区分：那是管理员对他人，本类是用户对自己。
	// 两类的 变更内容 一律不得记任何密码值（B-4 边界②）。
	OperationTypePasswordChange OperationType = "password_change"

	// OperationTypeFileUpload 文件上传（第 5 类）。C-1 / D-2，对象为 File / 文件ID。
	// 走「先落审计取 ID → File.来源审计ID 挂载 → 同事务提交」（不变式 4）。
	OperationTypeFileUpload OperationType = "file_upload"

	// OperationTypeDataIntake 数据入库（第 6 类）。D-7 提交，对象类型为零值（批量）。
	//
	// **该审计ID 即批次号**（D-7），是 F-4 按来源审计筛选与批次撤销的唯一依据。
	// 含复制提交，摘要记「复制自明细 X」（8.5）。
	OperationTypeDataIntake OperationType = "data_intake"

	// OperationTypeExpenseModify 明细编辑与删除（第 7 类）。F-5 编辑、F-8 删除。
	//
	// 对象二态：逐笔为 Expense / 明细ID；**批量删除为零值，摘要记筛选条件与笔数**。
	// F-5 保存不生成「数据入库」审计、不改 来源审计ID 与 来源文件ID。
	OperationTypeExpenseModify OperationType = "expense_modify"

	// OperationTypeBaseAmountRecalc 本位币金额重算（第 8 类）。I-7，对象类型为零值（批量）。
	//
	// 由 service/fx 在重算收尾写一条，记成功 / 失败笔数——重算允许部分失败、可续跑，
	// 因此这条审计的 操作结果 未必是成功。K-1 切换本位币时与第 10 类各落一条，
	// 二者职责不同、不是重复记录。
	OperationTypeBaseAmountRecalc OperationType = "base_amount_recalc"

	// OperationTypeCategoryManage 支出类型维护（第 9 类）。G-1 新建 / 删除，
	// 对象为 ExpenseCategory / 类型ID。
	//
	// **摘要必须带类型名称**——类型是物理删除，删除后 对象ID 指向的记录已不存在
	// （K-2 会提示「该对象已删除」），摘要里的名称是审计仍可读的唯一保障。
	OperationTypeCategoryManage OperationType = "category_manage"

	// OperationTypePreferenceSet 用户偏好设置（第 10 类）。K-1 本位币 / 界面语言切换，
	// 对象为 User / 自身用户ID。由 service/preference 写，必成功。
	OperationTypePreferenceSet OperationType = "preference_set"

	// OperationTypeDataExport 数据导出（第 11 类）。K-3 整库导出、F-7 明细导出，
	// 对象类型为零值（批量），摘要记范围与笔数。
	//
	// **B-4 边界①的唯一例外**：它是全系统唯一记审计的读操作。
	OperationTypeDataExport OperationType = "data_export"
)

// operationTypeSet 是 OperationType 的枚举表。
// 声明序 = 终版 §8.4 表格的 1~11 序号，K-2 的操作类型筛选下拉按此呈现。
var operationTypeSet = newSet([]entry[OperationType]{
	{OperationTypeSystemInit, "系统初始化"},
	{OperationTypeLogin, "登录"},
	{OperationTypeAccountManage, "账户管理"},
	{OperationTypePasswordChange, "密码修改"},
	{OperationTypeFileUpload, "文件上传"},
	{OperationTypeDataIntake, "数据入库"},
	{OperationTypeExpenseModify, "明细编辑与删除"},
	{OperationTypeBaseAmountRecalc, "本位币金额重算"},
	{OperationTypeCategoryManage, "支出类型维护"},
	{OperationTypePreferenceSet, "用户偏好设置"},
	{OperationTypeDataExport, "数据导出"},
})

// OperationTypes 返回全部 11 类操作类型，保持终版 §8.4 的 1~11 声明序。
//
// 这是 K-2 操作日志页「按操作类型筛选」下拉的数据源，也让 service/audit 侧可以写出
// 穷尽表并用单测锁死：一旦本包增删类目，那侧用例立即失败，而不是漏掉一类在运行期
// 静默走默认分支（与 errs.Kinds 同一手法）。
func OperationTypes() []OperationType { return operationTypeSet.all() }

// ParseOperationType 解析操作类型码值；未知码值返回 ErrUnknownCode。
func ParseOperationType(v string) (OperationType, error) { return operationTypeSet.parse(v) }

// Valid 报告是否为 11 类之一。零值返回 false。
func (o OperationType) Valid() bool { return operationTypeSet.valid(o) }

// IsZero 报告是否为零值（未指定）。
//
// 与 ObjectType 不同，本类型的零值**不是合法业务取值**：每条审计都必有操作类型
// （终版 §四 5「9 项全部必填」）。
func (o OperationType) IsZero() bool { return o == "" }

// Code 返回落库与对外契约使用的字符串码值。
func (o OperationType) Code() string { return string(o) }

// String 返回中文名，仅供日志与排错。
//
// 注意 K-2 页面上展示给用户的类型名应走 i18n（K-4：新增语种不改代码），
// 不要直接用本方法的返回值——那是日志面文案。
func (o OperationType) String() string { return operationTypeSet.name(o) }

// MarshalJSON 实现 json.Marshaler，序列化为字符串码值；非法码值报错（与 UnmarshalJSON 对称）。
func (o OperationType) MarshalJSON() ([]byte, error) { return marshalJSON(o, operationTypeSet) }

// UnmarshalJSON 实现 json.Unmarshaler，校验码值合法性；null 与空串得零值。
func (o *OperationType) UnmarshalJSON(data []byte) error {
	return unmarshalJSON(data, o, operationTypeSet)
}

// Value 实现 driver.Valuer，落库为字符串码值；非法码值报错（与 Scan 对称，避免写进去读不回来）。
func (o OperationType) Value() (driver.Value, error) { return value(o, operationTypeSet) }

// Scan 实现 sql.Scanner，校验码值合法性；NULL 与空串得零值。
func (o *OperationType) Scan(src any) error { return scan(src, o, operationTypeSet) }

// ObjectType 是操作对象类型，承载 AuditLog.操作对象类型（终版 §四 5、§8.4）。
//
// # 零值是合法取值，这是本包的唯一一处
//
// 终版 §四 5 明写「零值 = 本次操作无具体对象或为批量操作」。11 类审计里有 5 类
// 就该落零值：登录（第 2 类）、数据入库（第 6 类）、批量删除（第 7 类的批量形态）、
// 本位币金额重算（第 8 类）、数据导出（第 11 类）。
//
// 因此本类型的 IsZero 为真**不是**错误状态，Value() 也据此落空串而非 NULL——
// AuditLog 的 9 项全部必填，落 NULL 会被 NOT NULL 约束拒绝掉一个合法取值。
//
// # 四项的取值范围
//
// 恰四项，与终版 §8.4 表的「对象类型 / 对象ID」列一一对应：User、File、Expense、
// ExpenseCategory。**没有 AuditLog 项**——审计不指向审计，K-2 是只读页、不承担任何
// 写操作，不存在「对某条审计做了操作」这回事。
//
// # 对象ID 是历史指针，不是外键
//
// 终版 §四 5 与 K-2 都强调这一点：指向的对象可能已被删除。ExpenseCategory 是物理
// 删除（G-4），删后 K-2 降级为「该对象已删除」提示——这是全模型唯一会触发该提示的
// 对象；Expense 是软删除，记录仍在，K-2 正常跳转并自动带上「显示已删除」筛选。
type ObjectType string

const (
	// ObjectTypeUser 用户。对应第 1 / 3 / 4 / 10 类审计（系统初始化、账户管理、
	// 密码修改、用户偏好设置）。User 不可删除（前提 8），指针恒有效。
	ObjectTypeUser ObjectType = "user"

	// ObjectTypeFile 文件。对应第 5 类（文件上传）。File 不可删除（前提 8），
	// 指针恒有效；C-3 由此实现「由某次操作可查到本次涉及哪些文件」的反向查询。
	ObjectTypeFile ObjectType = "file"

	// ObjectTypeExpense 支出明细。对应第 7 类的逐笔形态（F-5 编辑、F-8 逐笔删除）。
	//
	// 明细是软删除，记录仍在，K-2 跳转时正常展示并自动带上「显示已删除」筛选。
	// 第 7 类的批量形态不用本项，用零值。
	ObjectTypeExpense ObjectType = "expense"

	// ObjectTypeExpenseCategory 支出类型。对应第 9 类（支出类型维护）。
	//
	// **全模型唯一可被物理删除的对象**（G-4：无引用时物理删除，删即不可恢复），
	// 因此也是唯一会让 K-2 出现「该对象已删除」提示的对象——这正是第 9 类审计的
	// 摘要必须带类型名称的原因。
	ObjectTypeExpenseCategory ObjectType = "expense_category"
)

// objectTypeSet 是 ObjectType 的枚举表。
// 声明序与终版 §三 实体关系图的实体顺序一致（AuditLog 不作为对象类型出现）。
var objectTypeSet = newSet([]entry[ObjectType]{
	{ObjectTypeUser, "用户"},
	{ObjectTypeFile, "文件"},
	{ObjectTypeExpense, "支出明细"},
	{ObjectTypeExpenseCategory, "支出类型"},
})

// ObjectTypes 返回全部操作对象类型，保持声明序。**不含零值**——零值虽是合法取值，
// 但它表示「无对象」，不是一个可被选择的对象类型。
func ObjectTypes() []ObjectType { return objectTypeSet.all() }

// ParseObjectType 解析操作对象类型码值；未知码值返回 ErrUnknownCode。
//
// 空串**不在**本函数的受理范围（返回 ErrUnknownCode）：要表达「无对象」应直接用
// 零值 ObjectType("")，而不是把空串喂进解析函数。反序列化与扫描路径上的空串仍按
// 零值处理，那是形态转换，与显式解析不同。
func ParseObjectType(v string) (ObjectType, error) { return objectTypeSet.parse(v) }

// Valid 报告是否为四项之一。
//
// **零值返回 false，但零值是合法的审计取值**——两者不矛盾：Valid 判定的是「是否为
// 某个具体对象类型」，而零值恰恰表示「不是任何对象类型」。审计写入方校验时应写
// `t.IsZero() || t.Valid()`，不要只写 Valid。
func (t ObjectType) Valid() bool { return objectTypeSet.valid(t) }

// IsZero 报告是否为零值，即本次操作无具体对象或为批量操作（终版 §四 5）。
func (t ObjectType) IsZero() bool { return t == "" }

// Code 返回落库与对外契约使用的字符串码值；零值返回空串。
func (t ObjectType) Code() string { return string(t) }

// String 返回中文名，仅供日志与排错；零值返回「未指定」。
func (t ObjectType) String() string { return objectTypeSet.name(t) }

// MarshalJSON 实现 json.Marshaler，序列化为字符串码值；零值输出空串，非法码值报错。
func (t ObjectType) MarshalJSON() ([]byte, error) { return marshalJSON(t, objectTypeSet) }

// UnmarshalJSON 实现 json.Unmarshaler，校验码值合法性；null 与空串得零值。
func (t *ObjectType) UnmarshalJSON(data []byte) error { return unmarshalJSON(data, t, objectTypeSet) }

// Value 实现 driver.Valuer，落库为字符串码值；零值落**空串而非 NULL**（见类型注释）；
// 非法码值报错（与 Scan 对称）。
func (t ObjectType) Value() (driver.Value, error) { return value(t, objectTypeSet) }

// Scan 实现 sql.Scanner，校验码值合法性；NULL 与空串得零值。
func (t *ObjectType) Scan(src any) error { return scan(src, t, objectTypeSet) }

// OperationResult 是操作结果，承载 AuditLog.操作结果（终版 §四 5）。
//
// 恰两项，无「部分成功」——这不是简化，是终版的明确取向：
//
//   - 第 8 类本位币金额重算（I-7）天然允许部分失败，但它的粒度是**单笔**
//     （不变式 2），批量结果由 操作摘要 记「成功 N 笔 / 失败 M 笔」表达，
//     不是靠一个第三态；
//   - 第 6 类数据入库是「一条审计 + N 条明细同事务」（不变式 4），要么全成要么全败，
//     不存在中间态。
//
// 加「部分成功」会让这两处的判定都变成三分支，且第三态的判据在两处并不相同。
type OperationResult string

const (
	// OperationResultSuccess 成功。
	OperationResultSuccess OperationResult = "success"

	// OperationResultFailure 失败。
	//
	// 主要来源是 8.4 第 2 类登录失败（密码错、账户禁用、锁定期内），走 service/audit
	// 的**独立事务**语义——与业务事务解耦，业务回滚不会把这条失败记录一并带走。
	// 第 8 类重算整体失败时同样落本项。
	OperationResultFailure OperationResult = "failure"
)

// operationResultSet 是 OperationResult 的枚举表。
var operationResultSet = newSet([]entry[OperationResult]{
	{OperationResultSuccess, "成功"},
	{OperationResultFailure, "失败"},
})

// OperationResults 返回全部操作结果，保持声明序。
func OperationResults() []OperationResult { return operationResultSet.all() }

// ParseOperationResult 解析操作结果码值；未知码值返回 ErrUnknownCode。
func ParseOperationResult(v string) (OperationResult, error) { return operationResultSet.parse(v) }

// Valid 报告是否为合法操作结果。零值返回 false——每条审计都必有结果（§四 5 全部必填）。
func (r OperationResult) Valid() bool { return operationResultSet.valid(r) }

// IsZero 报告是否为零值（未指定）。
func (r OperationResult) IsZero() bool { return r == "" }

// Code 返回落库与对外契约使用的字符串码值。
func (r OperationResult) Code() string { return string(r) }

// String 返回中文名，仅供日志与排错。
func (r OperationResult) String() string { return operationResultSet.name(r) }

// IsSuccess 报告是否为成功。
//
// 只有恰等于 OperationResultSuccess 才为真（默认拒绝）：零值与未知码值一律不算成功，
// 避免一条状态残缺的审计被当成成功记录读走。
func (r OperationResult) IsSuccess() bool { return r == OperationResultSuccess }

// MarshalJSON 实现 json.Marshaler，序列化为字符串码值；非法码值报错（与 UnmarshalJSON 对称）。
func (r OperationResult) MarshalJSON() ([]byte, error) { return marshalJSON(r, operationResultSet) }

// UnmarshalJSON 实现 json.Unmarshaler，校验码值合法性；null 与空串得零值。
func (r *OperationResult) UnmarshalJSON(data []byte) error {
	return unmarshalJSON(data, r, operationResultSet)
}

// Value 实现 driver.Valuer，落库为字符串码值；非法码值报错（与 Scan 对称，避免写进去读不回来）。
func (r OperationResult) Value() (driver.Value, error) { return value(r, operationResultSet) }

// Scan 实现 sql.Scanner，校验码值合法性；NULL 与空串得零值。
func (r *OperationResult) Scan(src any) error { return scan(src, r, operationResultSet) }
