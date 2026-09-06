package enum

// 本文件承载 §六 L1.0 `enum` 行七项承载中的第 2 项：**用户状态**。

import (
	"database/sql/driver"
)

// UserStatus 用户状态，对应 `entity.User.状态`。
//
// 依据终版 §四 1 `User.状态`：「启用 / 禁用，默认启用；**账户不可删除，无删除标记**」。
//
// 与「删除」的关系（前提 8）：本枚举**不是**软删除标记。前提 8 明确
// 「`User` / `File` / `AuditLog` 一律不可删除」，且「业务状态位（`User.状态`、
// `User.锁定截止时间`、`User.需强制改密`）不受此约束，其启停是**设计内的可逆开关**」。
// 因此禁用与启用可以来回切（A-7「启停除自身外的任意账户」），这与 `Expense.删除时间`
// 的「终态不可逆」是两套完全不同的语义，不可混用。
type UserStatus string

const (
	// UserStatusEnabled 启用。终版 §四 1「默认启用」，A-1 与 A-7 新建账户的初始状态。
	UserStatusEnabled UserStatus = "enabled"
	// UserStatusDisabled 禁用。A-7 可启停除自身外的任意账户；
	// 禁用会推进目标用户的登录态基线时间，使其旧令牌立即失效（A-7 原文）。
	// A-4 登录时命中此状态返回 errs.KeyAuthAccountDisabled。
	UserStatusDisabled UserStatus = "disabled"
)

// userStatusTable 用户状态枚举表。声明顺序：启用在前（默认值在前）。
var userStatusTable = newTable("user_status", "用户状态",
	entry{code: string(UserStatusEnabled), name: "启用"},
	entry{code: string(UserStatusDisabled), name: "禁用"},
)

// ParseUserStatus 校验并转换用户状态代码，是用户状态的唯一校验点。
func ParseUserStatus(code string) (UserStatus, error) {
	parsed, err := userStatusTable.parse(code)
	return UserStatus(parsed), err
}

// ListUserStatuses 按声明顺序返回全部用户状态，供 A-7 账户管理页的状态下拉使用。
func ListUserStatuses() []UserStatus {
	codes := userStatusTable.listCodes()
	statuses := make([]UserStatus, 0, len(codes))
	for _, code := range codes {
		statuses = append(statuses, UserStatus(code))
	}
	return statuses
}

// Valid 是否为已定义的用户状态。零值返回 false（状态必填）。
func (s UserStatus) Valid() bool { return userStatusTable.has(string(s)) }

// IsZero 是否为零值，即「未设置」。
func (s UserStatus) IsZero() bool { return s == "" }

// IsEnabled 是否为启用状态。
//
// 与 IsAdmin 同一理由：A-4 登录校验必须写成「是否启用」而非「是否不等于禁用」
// ——后者在将来加入第三种状态（如「待激活」）时会静默放行新状态登录。
func (s UserStatus) IsEnabled() bool { return s == UserStatusEnabled }

// String 返回代码，即落库值与 JSON 值。
func (s UserStatus) String() string { return string(s) }

// Name 返回中文名称，仅供日志与诊断。
func (s UserStatus) Name() string { return userStatusTable.nameOf(string(s)) }

// TextKey 返回 i18n 文案键。
func (s UserStatus) TextKey() string { return userStatusTable.textKeyOf(string(s)) }

// Value 实现 driver.Valuer，以代码文本落库；零值与非法值报错。
func (s UserStatus) Value() (driver.Value, error) {
	return valueOf(userStatusTable, string(s), false)
}

// Scan 实现 sql.Scanner。
func (s *UserStatus) Scan(src any) error {
	code, err := scanCode(src, userStatusTable, false)
	if err != nil {
		return err
	}
	*s = UserStatus(code)
	return nil
}

// MarshalText 实现 encoding.TextMarshaler。
func (s UserStatus) MarshalText() ([]byte, error) {
	return marshalTextOf(userStatusTable, string(s), false)
}

// UnmarshalText 实现 encoding.TextUnmarshaler。
func (s *UserStatus) UnmarshalText(data []byte) error {
	parsed, err := ParseUserStatus(string(data))
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}
