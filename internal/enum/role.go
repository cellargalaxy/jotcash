package enum

// 本文件承载 §六 L1.0 `enum` 行七项承载中的第 1 项：**角色**。
// 一类枚举一个文件：七类枚举的改动理由互不相干（加一个操作类型与加一个文件格式毫无关系），
// 与 `base/calendar` 把 Date / Month / MonthRange 分三个文件同一取向。

import (
	"database/sql/driver"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// Role 角色，对应 `entity.User.角色`。
//
// 依据终版（`doc/decisions/记账系统功能与模型.md`）§四 1 `User.角色`：
// 「管理员 / 普通用户；新建为普通用户」。**只有两个角色，不设第三档**。
//
// 定义为具名 string 类型而非裸 string：`User.角色` 与 `User.状态` 都是字符串枚举，
// 裸 string 下两者可互相赋值而编译器不会有任何反应——把状态赋给角色字段，
// 结果是一个恒不等于 RoleAdmin 的「角色」，A-7 的管理员校验从此永远为假。
type Role string

const (
	// RoleAdmin 管理员。特权**仅限账户管理域**（前提 3
	// 「管理员特权仅限账户管理域，**看不到他人的明细 / 文件 / 支出类型 / 操作审计**」），
	// 落到分层版 §九 前提 3 行：4 个归属型仓储不存在管理员旁路。
	RoleAdmin Role = "admin"
	// RoleUser 普通用户。终版 §四 1「新建为普通用户」，即 A-7 新增账户的固定角色。
	RoleUser Role = "user"
)

// roleTable 角色枚举表。声明顺序 = 业务顺序（管理员在前，与 A-1 初始化建 admin 的时序一致）。
var roleTable = newTable("role", "角色",
	entry{code: string(RoleAdmin), name: "管理员"},
	entry{code: string(RoleUser), name: "普通用户"},
)

// ParseRole 校验并转换角色代码，是角色的**唯一校验点**（终版 §五 1 口径）。
// 非法代码返回「用户输入错误」档、文案键 errs.KeyFieldInvalid 的错误。
func ParseRole(code string) (Role, error) {
	parsed, err := roleTable.parse(code)
	return Role(parsed), err
}

// ListRoles 按声明顺序返回全部角色，供 A-7 账户管理页的角色下拉使用。
func ListRoles() []Role {
	codes := roleTable.listCodes()
	roles := make([]Role, 0, len(codes))
	for _, code := range codes {
		roles = append(roles, Role(code))
	}
	return roles
}

// Valid 是否为已定义的角色。零值返回 false（角色必填，见包注释第四节）。
func (r Role) Valid() bool { return roleTable.has(string(r)) }

// IsZero 是否为零值，即「未设置」。
func (r Role) IsZero() bool { return r == "" }

// IsAdmin 是否为管理员。
//
// 提供具名判据而非让调用方写 `role == RoleAdmin`：A-7 的管理员校验散落在
// `service/account` 的多个方法上，具名方法让「哪里在判管理员」可被一次检索干净，
// 也避免出现 `role != RoleUser` 这种「非普通用户即管理员」的等价写法
// ——它在将来加入第三个角色时会静默把新角色当成管理员。
func (r Role) IsAdmin() bool { return r == RoleAdmin }

// String 返回代码，即落库值与 JSON 值（包注释第五节）。
func (r Role) String() string { return string(r) }

// Name 返回中文名称，**仅供日志与诊断**（约定 3），不是面向用户的文案。
func (r Role) Name() string { return roleTable.nameOf(string(r)) }

// TextKey 返回 i18n 文案键，面向用户展示时由 `i18n` 按「语言 + 文案键」取词。
func (r Role) TextKey() string { return roleTable.textKeyOf(string(r)) }

// Value 实现 driver.Valuer，以代码文本落库。零值与非法值一律报错（包注释第四节）。
func (r Role) Value() (driver.Value, error) { return valueOf(roleTable, string(r), false) }

// Scan 实现 sql.Scanner，只接受文本形态；NULL 与非法代码报错。
func (r *Role) Scan(src any) error {
	code, err := scanCode(src, roleTable, false)
	if err != nil {
		return err
	}
	*r = Role(code)
	return nil
}

// MarshalText 实现 encoding.TextMarshaler，产出代码。
// `encoding/json` 会自动经它把本类型序列化为 JSON 字符串，无需另写 MarshalJSON。
func (r Role) MarshalText() ([]byte, error) { return marshalTextOf(roleTable, string(r), false) }

// UnmarshalText 实现 encoding.TextUnmarshaler，接受代码；非法代码返回 ParseRole 的错误，
// 因此 `dto` 反序列化失败时拿到的已经是带档位与文案键的 `base/errs`。
func (r *Role) UnmarshalText(data []byte) error {
	parsed, err := ParseRole(string(data))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// CheckCreatableRole 校验角色是否可用于 A-7 新增账户。
//
// 依据终版 §二 A-1「首次启动创建唯一管理员账户，用户名固定 `admin`」与 §四 1
// 「新建为普通用户」：管理员账户**唯一**且只由 A-1 产生，A-7 新增账户一律为普通用户。
// 若放行新增管理员，「唯一管理员」这条口径就在 A-7 上被绕开了。
//
// 放在本包而非 `service/account`：这是**枚举取值域**的约束（哪些取值可用于新建），
// 与 `currency` 的「本位币候选」（T38）同构——那一条也是由枚举包而非 service 持有。
func CheckCreatableRole(r Role) error {
	if r == RoleUser {
		return nil
	}
	if !r.Valid() {
		return roleTable.invalidErr(string(r))
	}
	return errs.New(errs.KindInput, errs.KeyFieldInvalid, errs.A(errs.ArgField, roleTable.label))
}
