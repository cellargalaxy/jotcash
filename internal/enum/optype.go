package enum

// 本文件承载 §六 L1.0 `enum` 行七项承载中的第 5 项：**11 类操作类型**。

import (
	"database/sql/driver"
)

// OpType 操作类型，对应 `entity.AuditLog.操作类型`。
//
// 依据终版 §八 8.4「**操作类型（11 类）**」表，11 项与该表的 1~11 行**一一对应、
// 顺序一致**——`codes` 保持声明顺序（enum.go table.codes 注释），K-2 的操作类型下拉据此稳定排序。
//
// # 为什么是 11 类，不是 12 类
//
// 终版 §十二「相对对话13 终版的实质变化」第 ④ 项：「审计枚举 11 → 12 → **11 类**
// （增「数据导出」、删「回滚」，「明细增删改」改名「明细编辑与删除」）」。
// 即：**不存在「回滚」类型**（D-8 一键回滚整项取消，撤销下沉为「按来源审计筛选 + 批量删除」），
// 也不存在「明细新增」类型（§八 8.4 表末注：「明细的『新增』不在本表单列——一律走『数据入库』」）。
//
// # 每类恰有一个写入方
//
// 分层版 §九 下方「**11 类审计的写入方（9 个 service，一一对应、无第二写入方）**」把
// 11 类分派给 9 个 service。本包只定义枚举，不持有分派关系——那属于各 service 的职责，
// 且分层版已把它写成评审依据（R1：不变式 4 无编译期兜底）。
type OpType string

const (
	// OpTypeSystemInit 1 系统初始化。触发：A-1 创建 `admin`。
	// 对象：`User` / admin 用户ID。写入方：`service/account`。
	OpTypeSystemInit OpType = "system_init"
	// OpTypeLogin 2 登录。触发：A-4。
	// **失败以 `操作结果 = 失败` 表达**——用户名存在（含密码错、账户禁用、锁定期内）则记录
	// 并归属该用户，用户名不存在则**不记审计**（无归属、无消费方）。
	// 对象：零值。写入方：`service/auth`（失败类走 audit 的独立事务语义）。
	OpTypeLogin OpType = "login"
	// OpTypeAccountManage 3 账户管理。触发：A-7 新增 / 启停 / 重置他人密码。
	// 对象：`User` / 目标用户ID。写入方：`service/account`。
	// 注意 B-4 边界③：管理员对他人账户的操作**归属管理员**，被操作者在 K-2 中看不到。
	OpTypeAccountManage OpType = "account_manage"
	// OpTypePasswordChange 4 密码修改。触发：A-8 自助改密、首登强制改密。
	// 对象：`User` / 自身用户ID。写入方：`service/account`。
	// B-4 边界②：`变更内容` 一律不记任何密码值（明文、哈希、盐）。
	OpTypePasswordChange OpType = "password_change"
	// OpTypeFileUpload 5 文件上传。触发：C-1 / D-2。
	// 对象：`File` / 文件ID。写入方：`service/file`（**同事务**，回填 `File.来源审计ID`）。
	OpTypeFileUpload OpType = "file_upload"
	// OpTypeDataIntake 6 数据入库。触发：D-7（含复制提交；摘要记「复制自明细 X」）。
	// 对象：零值（批量）。写入方：`service/intake`。
	//
	// **这一类的审计ID 即批次号**（D-7「该审计ID即批次号，是后续按批次筛选与撤销的唯一依据」），
	// 也是 K-2 跳转必须带上「来源审计 = 该ID」筛选条件的那一类（K-2 原文）。
	OpTypeDataIntake OpType = "data_intake"
	// OpTypeExpenseEditDelete 7 明细编辑与删除。触发：F-5 编辑、F-8 删除。
	// 对象：逐笔为 `Expense` / 明细ID；**批量删除为零值，摘要记筛选条件与笔数**。
	// 写入方：`service/expense`。
	OpTypeExpenseEditDelete OpType = "expense_edit_delete"
	// OpTypeBaseAmountRecalc 8 本位币金额重算。触发：I-7。
	// 对象：零值（批量）。写入方：`service/fx`（在重算收尾写一条，记成功/失败笔数）。
	OpTypeBaseAmountRecalc OpType = "base_amount_recalc"
	// OpTypeCategoryManage 9 支出类型维护。触发：G-1 新建 / 删除。
	// 对象：`ExpenseCategory` / 类型ID。写入方：`service/category`。
	// **摘要必须带类型名称**——这是物理删除后审计仍可读的唯一保障（§八 8.5 末段）。
	OpTypeCategoryManage OpType = "category_manage"
	// OpTypeUserPreference 10 用户偏好设置。触发：K-1 本位币切换、界面语言切换。
	// 对象：`User` / 自身用户ID。写入方：`service/preference`（**必成功**）。
	// K-1 明确：切换本位币落两条审计（本条 + 第 8 类），二者职责不同、不是重复记录。
	OpTypeUserPreference OpType = "user_preference"
	// OpTypeDataExport 11 数据导出。触发：K-3 整库导出、F-7 明细导出。
	// 对象：零值（批量），摘要记范围与笔数。写入方：`service/export`。
	// **B-4 边界①的唯一例外**：除本类外，读操作一律不记审计。
	OpTypeDataExport OpType = "data_export"
)

// opTypeTable 操作类型枚举表。
// 声明顺序**严格等于**终版 §八 8.4 表的 1~11 行顺序，K-2 下拉据此稳定排序。
var opTypeTable = newTable("op_type", "操作类型",
	entry{code: string(OpTypeSystemInit), name: "系统初始化"},
	entry{code: string(OpTypeLogin), name: "登录"},
	entry{code: string(OpTypeAccountManage), name: "账户管理"},
	entry{code: string(OpTypePasswordChange), name: "密码修改"},
	entry{code: string(OpTypeFileUpload), name: "文件上传"},
	entry{code: string(OpTypeDataIntake), name: "数据入库"},
	entry{code: string(OpTypeExpenseEditDelete), name: "明细编辑与删除"},
	entry{code: string(OpTypeBaseAmountRecalc), name: "本位币金额重算"},
	entry{code: string(OpTypeCategoryManage), name: "支出类型维护"},
	entry{code: string(OpTypeUserPreference), name: "用户偏好设置"},
	entry{code: string(OpTypeDataExport), name: "数据导出"},
)

// OpTypeCount 操作类型的项数，恒为 11（终版 §八 8.4）。
//
// 导出这个常量是为了让「11 类」这个数字**可被单测机械断言**：
// 它在终版里出现在 §二 B-4、§八 8.4、§十二 交付说明三处，是全文反复点算的口径之一
// （§十二 校验方式 ⑦「11 类审计枚举与全部写操作双向对齐」）。
// 少一类意味着某个写操作没有留痕（不变式 4 破裂），多一类意味着凭空发明了业务动作。
const OpTypeCount = 11

// init 校验项数恒为 11。理由同 newTable 的 panic 取舍：这是常量数据的自洽性，
// 属编码错误，须在包初始化期当场暴露而非留待运行期。
func init() {
	if len(opTypeTable.codes) != OpTypeCount {
		panic("enum：操作类型项数不符，须为 11 类")
	}
}

// ParseOpType 校验并转换操作类型代码，是操作类型的唯一校验点。
// 供 K-2 的操作类型筛选参数校验使用。
func ParseOpType(code string) (OpType, error) {
	parsed, err := opTypeTable.parse(code)
	return OpType(parsed), err
}

// ListOpTypes 按终版 §八 8.4 的 1~11 顺序返回全部操作类型，供 K-2 下拉使用。
func ListOpTypes() []OpType {
	codes := opTypeTable.listCodes()
	types := make([]OpType, 0, len(codes))
	for _, code := range codes {
		types = append(types, OpType(code))
	}
	return types
}

// Valid 是否为已定义的操作类型。零值返回 false（`AuditLog.操作类型` 必填，终版 §四 5）。
func (o OpType) Valid() bool { return opTypeTable.has(string(o)) }

// IsZero 是否为零值，即「未设置」。
func (o OpType) IsZero() bool { return o == "" }

// String 返回代码，即落库值与 JSON 值。
func (o OpType) String() string { return string(o) }

// Name 返回中文名称，仅供日志与诊断。
func (o OpType) Name() string { return opTypeTable.nameOf(string(o)) }

// TextKey 返回 i18n 文案键。K-2 的操作类型列与下拉一律经它取词（K-4）。
func (o OpType) TextKey() string { return opTypeTable.textKeyOf(string(o)) }

// Value 实现 driver.Valuer，以代码文本落库；零值与非法值报错。
func (o OpType) Value() (driver.Value, error) { return valueOf(opTypeTable, string(o), false) }

// Scan 实现 sql.Scanner。
func (o *OpType) Scan(src any) error {
	code, err := scanCode(src, opTypeTable, false)
	if err != nil {
		return err
	}
	*o = OpType(code)
	return nil
}

// MarshalText 实现 encoding.TextMarshaler。
func (o OpType) MarshalText() ([]byte, error) {
	return marshalTextOf(opTypeTable, string(o), false)
}

// UnmarshalText 实现 encoding.TextUnmarshaler。
func (o *OpType) UnmarshalText(data []byte) error {
	parsed, err := ParseOpType(string(data))
	if err != nil {
		return err
	}
	*o = parsed
	return nil
}
