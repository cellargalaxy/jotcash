package errs

// Kind 错误档位（五档）。
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md` §六 L0 `base/errs` 行：
// 「五档：用户输入错误 / 唯一冲突 / 归属越权 / 未登录 / 系统错误」。
// 常量声明顺序与该行的文字顺序严格一致，Code 即对外稳定错误码。
//
// 档位只做「错误性质」的分类，不含任何协议语义：
// 到 HTTP 状态码的映射（唯一冲突 → 409、归属越权 → 403、未登录 → 401）
// 由 `api/http/middleware` 承担（同文件 L6 `middleware` 行），本包不感知 HTTP。
type Kind uint8

const (
	// KindInput 用户输入错误：入参不合法、格式不符、字段缺失等。
	// 典型来源：D-3 五类解析错误、A-8 密码策略校验、I-5 汇率必填未填。
	KindInput Kind = iota + 1
	// KindConflict 唯一冲突：唯一索引冲突。
	// 典型来源：`User.用户名` 全局唯一、`ExpenseCategory.(归属用户ID, 名称)` 唯一，
	// 由存储层唯一索引保证并映射到本档（同文件 L3 `store` 行 ⑤）。
	KindConflict
	// KindForbidden 归属越权：登录态有效，但访问的数据不属于当前用户，或角色不足。
	// 典型来源：不变式 3 的归属校验、A-7 管理员校验。
	KindForbidden
	// KindUnauthorized 未登录：登录态缺失、过期（A-5 的 12h 绝对过期）或被基线时间作废。
	KindUnauthorized
	// KindSystem 系统错误：非预期的内部错误，也是未分类错误的兜底档。
	KindSystem
)

// kindNames 档位的中文名称，仅用于日志与诊断（约定 3：注释与日志一律中文）。
// 注意：这里不是面向用户的提示文案——面向用户的文案一律由 i18n 按文案键渲染。
var kindNames = map[Kind]string{
	KindInput:        "用户输入错误",
	KindConflict:     "唯一冲突",
	KindForbidden:    "归属越权",
	KindUnauthorized: "未登录",
	KindSystem:       "系统错误",
}

// Code 返回档位的稳定错误码，取值 1~5，与常量声明顺序一致。
func (k Kind) Code() int {
	return int(k)
}

// String 返回档位的中文名称，供日志与诊断使用；未知档位返回「未知档位」。
func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return "未知档位"
}

// Valid 返回该档位是否为五档之一。
func (k Kind) Valid() bool {
	_, ok := kindNames[k]
	return ok
}
