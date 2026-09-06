package enum

// 本文件承载 §六 L1.0 `enum` 行七项承载中的第 6 项：**操作对象类型**。
//
// 本枚举是七类中**唯一「零值合法」**的一类（包注释第四节），因此它是唯一在
// Value / MarshalText 上给 allowZero 传 true 的类型。

import (
	"database/sql/driver"
)

// ObjectType 操作对象类型，对应 `entity.AuditLog.操作对象类型`。
//
// 依据终版 §四 5 `AuditLog.操作对象类型`：「**零值 = 本次操作无具体对象或为批量操作**」，
// 与 `对象ID`「零值 = 同上；**是历史指针，不是外键**——指向的对象可能已被删除」配对。
//
// # 零值合法是本类型唯一的特殊之处
//
// §八 8.4 的 11 类里有 **5 类**的对象类型恒为零值：登录（2）、数据入库（6）、
// 本位币金额重算（8）、数据导出（11），以及明细编辑与删除（7）的**批量删除**分支。
// 因此零值不是「漏赋值」而是**业务上的正常取值**，Value 与 MarshalText 必须放行它。
// 另外 6 类枚举的零值一律非法（它们对应的属性全部必填），落库前一律报错。
//
// # 取值域 = 4 个实体，不含 User 之外的系统对象
//
// §八 8.4 表的「对象类型 / 对象ID」列共出现 4 种实体：`User`（1/3/4/10 类）、
// `File`（5 类）、`Expense`（7 类逐笔）、`ExpenseCategory`（9 类）。
// `AuditLog` **自身不作对象类型**——审计不记录「对审计的操作」（K-2 只读，
// 本页不承担任何写操作），故本枚举只有 4 项，而非 5 实体各一项。
type ObjectType string

const (
	// ObjectTypeUser 用户。用于 §八 8.4 的第 1 类（系统初始化 / admin 用户ID）、
	// 第 3 类（账户管理 / 目标用户ID）、第 4 类（密码修改 / 自身用户ID）、
	// 第 10 类（用户偏好设置 / 自身用户ID）。
	ObjectTypeUser ObjectType = "user"
	// ObjectTypeExpenseCategory 支出类型。用于第 9 类（支出类型维护 / 类型ID）。
	//
	// **全模型唯一会触发 K-2「该对象已删除」提示的对象类型**：`ExpenseCategory` 是
	// 唯一可物理删除的实体（前提 8），删除后审计里的 `对象ID` 成为悬空历史指针，
	// K-2 据此降级提示（§八 8.5 末段、K-2 原文）。也正因如此，第 9 类的
	// **操作摘要必须带类型名称**——那是删除后审计仍可读的唯一保障。
	ObjectTypeExpenseCategory ObjectType = "expense_category"
	// ObjectTypeExpense 支出明细。用于第 7 类的**逐笔**分支（明细ID）；
	// 批量删除分支为零值（摘要记筛选条件与笔数）。
	//
	// 注意 K-2 的跳转口径：目标为**已软删除的明细**时**正常跳转**并自动带上
	// 「显示已删除」筛选（记录仍在），不降级提示——软删除不是物理删除。
	ObjectTypeExpense ObjectType = "expense"
	// ObjectTypeFile 文件。用于第 5 类（文件上传 / 文件ID）。
	// 文件只留存不物理删除（C-4），故其历史指针恒有效。
	ObjectTypeFile ObjectType = "file"
)

// objectTypeTable 操作对象类型枚举表。
// 声明顺序与 §三 实体关系图的 `User → ExpenseCategory / Expense / File` 归属顺序一致。
var objectTypeTable = newTable("object_type", "操作对象类型",
	entry{code: string(ObjectTypeUser), name: "用户"},
	entry{code: string(ObjectTypeExpenseCategory), name: "支出类型"},
	entry{code: string(ObjectTypeExpense), name: "支出明细"},
	entry{code: string(ObjectTypeFile), name: "文件"},
)

// ObjectTypeNone 无具体对象或批量操作，即零值（终版 §四 5）。
//
// 给零值一个**具名常量**而非让调用方写 `""`：§八 8.4 有 5 类审计的对象类型恒为零值，
// 写 `enum.ObjectTypeNone` 表达的是「本次操作确实没有具体对象」这一业务事实，
// 写 `""` 则与「忘了赋值」在代码上完全无法区分——而本类型恰恰是唯一放行零值落库的枚举，
// 区分不了就意味着漏赋值会被静默落库。
const ObjectTypeNone ObjectType = ""

// ParseObjectType 校验并转换操作对象类型代码。
//
// **接受空串**（返回 ObjectTypeNone）：这是本类型与另外 6 类校验路径唯一的差别，
// 依据终版 §四 5「零值 = 本次操作无具体对象或为批量操作」。
func ParseObjectType(code string) (ObjectType, error) {
	if code == "" {
		return ObjectTypeNone, nil
	}
	parsed, err := objectTypeTable.parse(code)
	return ObjectType(parsed), err
}

// ListObjectTypes 按声明顺序返回全部**非零值**的操作对象类型。
//
// 不含 ObjectTypeNone：它不是一个「可选的对象类型」，而是「没有对象」这一状态，
// 出现在下拉里没有意义。
func ListObjectTypes() []ObjectType {
	codes := objectTypeTable.listCodes()
	types := make([]ObjectType, 0, len(codes))
	for _, code := range codes {
		types = append(types, ObjectType(code))
	}
	return types
}

// Valid 是否为已定义的对象类型。**零值返回 false**——零值合法但不是「一个对象类型」。
//
// 判「取值是否可接受」应当用 ValidOrNone，判「是否指向某类实体」才用 Valid。
// 两者分开是刻意的：`service/audit` 的跳转对象存在性探测（K-2）只该对 Valid 为真的项
// 去查库，对零值直接跳过。
func (t ObjectType) Valid() bool { return objectTypeTable.has(string(t)) }

// ValidOrNone 取值是否可接受：已定义的对象类型**或**零值（无具体对象 / 批量操作）。
func (t ObjectType) ValidOrNone() bool { return t == ObjectTypeNone || t.Valid() }

// IsZero 是否为零值，即「无具体对象或为批量操作」。
//
// 语义与另外 6 类枚举的 IsZero **不同**：那 6 类的零值是「未设置」（非法），
// 本类型的零值是一个明确的业务取值（合法）。方法名保持一致以便统一书写，
// 语义差异由本注释与包注释第四节界定。
func (t ObjectType) IsZero() bool { return t == ObjectTypeNone }

// String 返回代码，即落库值与 JSON 值。零值返回空串。
func (t ObjectType) String() string { return string(t) }

// Name 返回中文名称，仅供日志与诊断。
//
// **零值返回「无对象」**而不是「未知操作对象类型」：零值是合法业务取值，
// 日志里写「未知」会把一条正常的批量操作审计说成数据异常。
func (t ObjectType) Name() string {
	if t == ObjectTypeNone {
		return "无对象"
	}
	return objectTypeTable.nameOf(string(t))
}

// TextKey 返回 i18n 文案键；零值返回空串（无对象则无需取词）。
func (t ObjectType) TextKey() string { return objectTypeTable.textKeyOf(string(t)) }

// Value 实现 driver.Valuer，以代码文本落库；**零值落空串**（allowZero = true）。
func (t ObjectType) Value() (driver.Value, error) {
	return valueOf(objectTypeTable, string(t), true)
}

// Scan 实现 sql.Scanner；**接受空串**（零值合法）。
//
// 仍拒收 NULL（codeFromSrc 的口径）：终版 §四 表头「业务上『不适用』的场景以零值表达，
// **不留空**」——零值在库里是空串，不是 NULL；出现 NULL 属建表或写入路径的缺陷。
func (t *ObjectType) Scan(src any) error {
	code, err := scanCode(src, objectTypeTable, true)
	if err != nil {
		return err
	}
	*t = ObjectType(code)
	return nil
}

// MarshalText 实现 encoding.TextMarshaler；零值产出空串。
func (t ObjectType) MarshalText() ([]byte, error) {
	return marshalTextOf(objectTypeTable, string(t), true)
}

// UnmarshalText 实现 encoding.TextUnmarshaler；接受空串（零值合法）。
func (t *ObjectType) UnmarshalText(data []byte) error {
	parsed, err := ParseObjectType(string(data))
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}
