// Package errs 承载 jotcash 的统一错误类型与错误码（L0 基础层）。
//
// # 五档是闭集
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md 的 L0 表 base/errs 行，全系统
// 错误恰分五档：用户输入错误 / 唯一冲突 / 归属越权 / 未登录 / 系统错误。档位既是
// 「错误类型」也是「错误码」，不再另设一根独立的数字业务码轴——细粒度身份由文案键
// 承担（D-3 的五类解析错误靠键区分，不靠码），再加一根数字轴等于同一件事编三次码，
// 必然漂移。
//
// 档位的唯一消费方是 api/http/middleware，它按五档映射 HTTP 状态。分层方案写死了
// 其中三条：唯一冲突 → 409、归属越权 → 403、未登录 → 401（且不做服务端跳转，跳
// 登录页由前端执行）。剩余两条由 middleware 那一轮定，本包不预设。
//
// # 错误对象不含文案，只含文案键与占位参数
//
// K-4 要求「新增语种只需增加配置、无需改动代码」。因此错误对象内**不写死任何面向
// 用户的文案**，只带「文案键 + 具名占位参数」，由 middleware 经 i18n 按当前界面
// 语言渲染。本包也因此**不依赖 i18n**——i18n 在 L3，反向依赖会成环。
//
// 占位参数用具名 map 而非位置参数（%s 风格），这是 K-4 的直接要求：位置参数要求
// 所有语种的占位符顺序一致，而语序是语言属性（「第 12 行的『金额』字段缺失」与
// `field "amount" missing on line 12` 的变量顺序天然相反），一旦某语种需要调序就
// 只能回头改产错误的 Go 代码，K-4 当场失效。具名参数与顺序无关。
//
// # 业务文案键不在本包
//
// 按约定 5「base/ 是纯技术层：任何带业务语义的类型都不得放入」，D-3 的五个解析错误
// 键归 parser、G-1 重名键归 service/category、A-7 键归 service/account，由各自包
// 声明常量，命名 <包>.<场景>。本包只定义键的类型与机制，外加五个纯技术兜底键
// （KeyInvalidInput 等），用于兜住没带键或完全没分档的错误。
//
// # 提取函数是总函数
//
// middleware 拿到的 err 可能是被包装过的，也可能完全没分档（database/sql 直吐的
// 错误、第三方库转出的 error）。若无档时返回空，L6 就必须自己再造一套兜底逻辑，
// 且有把底层错误原文直接吐给用户的风险（前提 1 下应用层是唯一防线）。因此
// KindOf / MsgKeyOf / ParamsOf 对任何输入都有确定输出：nil → 零值；未分档的非 nil
// error → 系统错误档 + KeyInternal。
//
// # 密码红线
//
// §九 红线行规定密码值（明文、哈希、盐）的可见范围。占位参数是 map[string]any，
// 语法上拦不住调用方把密码塞进来，而错误会同时流向日志与前端两条路径。因此定死：
// **任何 Params 中不得出现密码明文、密码哈希与盐**。本包无编译期兜底（与分层方案
// §四 R1~R3 同类），靠评审兜底。
//
// # 依赖边界
//
// 本包属 L0 且**层内零依赖**：不 import 仓库内任何其他包（含 base/log 与 i18n）。
// 栈捕获、包装链与 Is/As/Unwrap/Cause 互操作全部委托 github.com/pkg/errors 与标准
// 库 errors，本包零行栈处理代码——该库是仓库统一的错误库家族（go_common 亦依赖
// 同一版本），全仓库因此只有一个错误库。
package errs

// Kind 是错误档位，同时是错误码。取值为下列五档之一，KindUnknown 表示「无错误」
// 而非第六档——它是零值，用于 nil error 的判定结果。
//
// 底层 uint8 便于 switch 与紧凑传递；对外另有 Code() 给出稳定字符串码。
type Kind uint8

const (
	// KindUnknown 是零值，表示「无错误 / 未判定」，不是五档之一。
	// KindOf(nil) 返回它，Valid() 对它返回 false。
	KindUnknown Kind = iota

	// KindInvalidInput 用户输入错误：请求参数、文件内容、表单取值不合业务规则。
	// 典型场景：D-3 的五类解析错误（格式不符 / 内容损坏 / 字段缺失 / 解密失败 /
	// 类型不匹配）、A-8 密码不符策略、I-5 汇率转必填后未填、K-1 本位币不在候选集。
	KindInvalidInput

	// KindConflict 唯一冲突：违反存储层唯一索引。
	// 仅两处：User.用户名 全局唯一（A-1/A-7）、ExpenseCategory.(归属用户ID, 名称)
	// 唯一（G-1）。按 store 承载⑤，唯一性由存储层唯一索引保证、冲突返回本档，
	// 先查后插只用于友好提示，不作正确性依据。
	KindConflict

	// KindForbidden 归属越权：登录态有效，但访问的不是自己的数据。
	// 不变式 3「归属校验无死角」的对外表达。前提 3 下管理员亦无业务数据特权，
	// 管理员访问他人明细/文件/类型/审计同样落本档。
	KindForbidden

	// KindUnauthorized 未登录：无令牌、令牌过期（A-5 的 12 小时绝对过期）或被
	// 基线时间作废（A-7 重置/禁用、A-8 改密推进基线时间）。
	KindUnauthorized

	// KindInternal 系统错误：非预期故障，如存储层 IO 失败、汇率源不可达、
	// 编程错误。也是一切未分档错误的归一目标。
	KindInternal
)

// 五档的兜底文案键。业务文案键由各业务包自行声明（约定 5），本包只提供这五个
// **纯技术**兜底键，用于两种场景：构造错误时未给键，以及完全未分档的错误。
//
// i18n 的语言文件（internal/i18n/locales/）必须包含这五个键，否则未分档错误
// 渲染不出文案——这是本包对 i18n 那一轮的硬性契约。
const (
	// KeyInvalidInput 是用户输入错误档的兜底键。
	KeyInvalidInput = "errs.invalid_input"
	// KeyConflict 是唯一冲突档的兜底键。
	KeyConflict = "errs.conflict"
	// KeyForbidden 是归属越权档的兜底键。
	KeyForbidden = "errs.forbidden"
	// KeyUnauthorized 是未登录档的兜底键。
	KeyUnauthorized = "errs.unauthorized"
	// KeyInternal 是系统错误档的兜底键，也是未分档错误的归一键。
	KeyInternal = "errs.internal"
)

// kindCodes 是档位到稳定字符串码的映射，供日志与将来的 API 契约使用。
// 这些码一经发布不得改动——它们会出现在日志与前端判定里。
var kindCodes = map[Kind]string{
	KindInvalidInput: "invalid_input",
	KindConflict:     "conflict",
	KindForbidden:    "forbidden",
	KindUnauthorized: "unauthorized",
	KindInternal:     "internal",
}

// kindNames 是档位的中文名，仅供日志与排错阅读。
// 遵约定 3「注释与日志一律中文」——这是日志面文案，不是用户面文案；
// 用户面文案一律走文案键经 i18n 渲染。
var kindNames = map[Kind]string{
	KindInvalidInput: "用户输入错误",
	KindConflict:     "唯一冲突",
	KindForbidden:    "归属越权",
	KindUnauthorized: "未登录",
	KindInternal:     "系统错误",
}

// kindKeys 是档位到兜底文案键的映射。
var kindKeys = map[Kind]string{
	KindInvalidInput: KeyInvalidInput,
	KindConflict:     KeyConflict,
	KindForbidden:    KeyForbidden,
	KindUnauthorized: KeyUnauthorized,
	KindInternal:     KeyInternal,
}

// allKinds 按声明顺序列出五档。顺序即 Kind 的数值顺序，不含 KindUnknown。
var allKinds = []Kind{
	KindInvalidInput,
	KindConflict,
	KindForbidden,
	KindUnauthorized,
	KindInternal,
}

// Kinds 返回五档的副本，供上层做穷尽性处理。
//
// middleware 的「五档 → HTTP 状态映射」可据此写出穷尽表并用单测锁死：一旦本包
// 增删档位，那侧的用例立即失败，而不是漏掉一档在运行期静默走默认分支。
//
// 返回副本而非共享切片：调用方改动不会污染本包状态。
func Kinds() []Kind {
	out := make([]Kind, len(allKinds))
	copy(out, allKinds)
	return out
}

// Valid 报告档位是否为五档之一。KindUnknown 与任何越界值返回 false。
func (k Kind) Valid() bool {
	_, ok := kindCodes[k]
	return ok
}

// Code 返回稳定的字符串错误码（如 invalid_input）。
// 非五档返回空串——空串是「没有码」的确定表达，便于调用方判定。
func (k Kind) Code() string {
	return kindCodes[k]
}

// String 返回中文档位名（如 用户输入错误），仅供日志与排错。
// 非五档返回 unknown：这里不返回空串，因为 String 会被 %v 隐式调用，
// 空串会让日志里出现看不见的字段，反而更难排查。
func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return "unknown"
}

// DefaultMsgKey 返回本档的兜底文案键。非五档返回 KeyInternal——未知档位一律
// 按系统错误对外呈现，不把内部状态暴露给用户。
func (k Kind) DefaultMsgKey() string {
	if key, ok := kindKeys[k]; ok {
		return key
	}
	return KeyInternal
}
