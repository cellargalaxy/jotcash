package enum

import (
	"database/sql/driver"
)

// Role 是用户角色，承载 User.角色（终版 §四 1）。
//
// 恰两项，无第三种可能：终版前提 3 把管理员特权收敛为「仅限账户管理域，看不到他人
// 的明细 / 文件 / 支出类型 / 操作审计」，A-7 的四项特权（新增账户 / 启停除自身外账户 /
// 重置他人密码 / 查看账户列表）全部只作用于 User 实体。因此不存在「只读管理员」
// 「审计员」这类中间角色的落点——真要加，动的是前提 3，不是本枚举。
//
// 无「是否启用」维度：角色不是可停用的配置项。
type Role string

const (
	// RoleAdmin 管理员。A-1 系统初始化创建的 admin 即此角色；A-7 的账户管理特权
	// 由它判定，且**仅作用于 User 实体**（前提 3：管理员无业务数据特权）。
	RoleAdmin Role = "admin"

	// RoleUser 普通用户。A-7 新建账户一律为本角色（终版 §四 1「新建为普通用户」）。
	RoleUser Role = "user"
)

// roleSet 是 Role 的枚举表，声明序即对外呈现序。
var roleSet = newSet([]entry[Role]{
	{RoleAdmin, "管理员"},
	{RoleUser, "普通用户"},
})

// Roles 返回全部角色，保持声明序。
func Roles() []Role { return roleSet.all() }

// ParseRole 解析角色码值，严格全等匹配；未知码值返回 ErrUnknownCode。
func ParseRole(v string) (Role, error) { return roleSet.parse(v) }

// Valid 报告是否为合法角色。零值返回 false。
func (r Role) Valid() bool { return roleSet.valid(r) }

// IsZero 报告是否为零值（未指定）。
func (r Role) IsZero() bool { return r == "" }

// Code 返回落库与对外契约使用的字符串码值。
func (r Role) Code() string { return string(r) }

// String 返回中文名，仅供日志与排错；落库与 JSON 一律用 Code。
func (r Role) String() string { return roleSet.name(r) }

// IsAdmin 报告是否为管理员。
//
// 单独提供此方法而不让调用方写 `role == RoleAdmin`，是为了让「哪些地方依赖管理员
// 身份」可被 grep 穷举——前提 3 要求管理员特权只出现在 service/account 且只作用于
// User 实体，这条约束无编译期兜底，只能靠可枚举的调用点配合评审守住。
func (r Role) IsAdmin() bool { return r == RoleAdmin }

// MarshalJSON 实现 json.Marshaler，序列化为字符串码值；非法码值报错（与 UnmarshalJSON 对称）。
func (r Role) MarshalJSON() ([]byte, error) { return marshalJSON(r, roleSet) }

// UnmarshalJSON 实现 json.Unmarshaler，校验码值合法性；null 与空串得零值。
func (r *Role) UnmarshalJSON(data []byte) error { return unmarshalJSON(data, r, roleSet) }

// Value 实现 driver.Valuer，落库为字符串码值；非法码值报错（与 Scan 对称，避免写进去读不回来）。
func (r Role) Value() (driver.Value, error) { return value(r, roleSet) }

// Scan 实现 sql.Scanner，校验码值合法性；NULL 与空串得零值。
func (r *Role) Scan(src any) error { return scan(src, r, roleSet) }

// UserStatus 是用户状态，承载 User.状态（终版 §四 1）。
//
// 恰两项。终版 §四 1 明写「账户不可删除，无删除标记」（前提 8：User 不可删除），
// 因此本枚举没有、也不得有「已删除」项——那会绕过前提 8。
//
// 与「锁定」的区别：A-8 的连续失败 5 次锁定 10 分钟由 User.锁定截止时间 承载，
// 是带时限的临时状态，不进本枚举；本枚举只表达 A-7 管理员显式启停的持久状态。
// 两者正交——被禁用的账户同时可以处于锁定期内。
type UserStatus string

const (
	// UserStatusEnabled 启用。终版 §四 1：状态默认启用。
	UserStatusEnabled UserStatus = "enabled"

	// UserStatusDisabled 禁用。由 A-7 管理员启停（不含自身）；禁用会推进目标用户的
	// 登录态基线时间，使其旧令牌立即失效。
	UserStatusDisabled UserStatus = "disabled"
)

// userStatusSet 是 UserStatus 的枚举表。
var userStatusSet = newSet([]entry[UserStatus]{
	{UserStatusEnabled, "启用"},
	{UserStatusDisabled, "禁用"},
})

// UserStatuses 返回全部用户状态，保持声明序。
func UserStatuses() []UserStatus { return userStatusSet.all() }

// ParseUserStatus 解析用户状态码值；未知码值返回 ErrUnknownCode。
func ParseUserStatus(v string) (UserStatus, error) { return userStatusSet.parse(v) }

// Valid 报告是否为合法用户状态。零值返回 false。
func (s UserStatus) Valid() bool { return userStatusSet.valid(s) }

// IsZero 报告是否为零值（未指定）。
func (s UserStatus) IsZero() bool { return s == "" }

// Code 返回落库与对外契约使用的字符串码值。
func (s UserStatus) Code() string { return string(s) }

// String 返回中文名，仅供日志与排错。
func (s UserStatus) String() string { return userStatusSet.name(s) }

// IsEnabled 报告账户是否处于启用状态。
//
// 只有恰等于 UserStatusEnabled 才为真：零值与未知码值一律不放行。这是「默认拒绝」
// ——A-4 登录校验会调用它，若写成 `!= UserStatusDisabled`，一个零值状态的账户就能
// 登录，而零值本不该出现在任何已落库的行上。
func (s UserStatus) IsEnabled() bool { return s == UserStatusEnabled }

// MarshalJSON 实现 json.Marshaler，序列化为字符串码值；非法码值报错（与 UnmarshalJSON 对称）。
func (s UserStatus) MarshalJSON() ([]byte, error) { return marshalJSON(s, userStatusSet) }

// UnmarshalJSON 实现 json.Unmarshaler，校验码值合法性；null 与空串得零值。
func (s *UserStatus) UnmarshalJSON(data []byte) error { return unmarshalJSON(data, s, userStatusSet) }

// Value 实现 driver.Valuer，落库为字符串码值；非法码值报错（与 Scan 对称，避免写进去读不回来）。
func (s UserStatus) Value() (driver.Value, error) { return value(s, userStatusSet) }

// Scan 实现 sql.Scanner，校验码值合法性；NULL 与空串得零值。
func (s *UserStatus) Scan(src any) error { return scan(src, s, userStatusSet) }
