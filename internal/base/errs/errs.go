package errs

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/pkg/errors"
)

// Params 是文案键的具名占位参数。
//
// 用具名 map 而非位置参数是 K-4 的要求（见包注释）：语序是语言属性，位置参数在
// 跨语种调序时必须回改 Go 代码。键名由文案键的声明方与语言文件约定，例如 parser
// 的字段缺失错误用 {"Line": 12, "Field": "金额"}，语言文件里写 {{.Line}} 与
// {{.Field}}，各语种自行决定语序。
//
// **禁止放入任何密码值**（明文、哈希、盐）——§九 红线行。错误会同时流向日志与
// 前端两条路径，放进来等于两处同时泄露。
type Params map[string]any

// Error 是本包的错误类型，也是全仓库唯一的业务错误类型。
//
// 四个字段各有唯一职责：
//   - kind   五档之一，供 middleware 映射 HTTP 状态
//   - msgKey 文案键，供 middleware 经 i18n 取词；本类型内不含任何成品文案
//   - params 具名占位参数，供取词后填充
//   - cause  底层错误，可为 nil；经 Unwrap/Cause 暴露，使 errors.Is/As 仍能穿透
//
// 字段全部私有：错误对象一旦构造即不可变。错误会同时流向日志与渲染两条路径，
// 可变会造成一处改动另一处受影响。
type Error struct {
	kind   Kind
	msgKey string
	params Params
	cause  error
}

// newError 是全部构造函数的唯一收口，负责三件事的归一化，保证「构造出来的 Error
// 必然可用」，上层无需再判空：
//  1. 非五档的 kind 归一到 KindInternal——未知档位一律按系统错误对外呈现；
//  2. 空 msgKey 归一到该档的兜底键——middleware 必然有键可渲染；
//  3. params 深拷贝一层——调用方后续改动原 map 不会影响已产生的错误。
func newError(kind Kind, msgKey string, params Params, cause error) *Error {
	if !kind.Valid() {
		kind = KindInternal
	}
	if strings.TrimSpace(msgKey) == "" {
		msgKey = kind.DefaultMsgKey()
	}

	var cloned Params
	if len(params) > 0 {
		cloned = make(Params, len(params))
		maps.Copy(cloned, params)
	}

	return &Error{kind: kind, msgKey: msgKey, params: cloned, cause: cause}
}

// New 构造一个无底层错误的分档错误，供「自己判定出的业务错误」使用。
//
// 这类错误是预期内的（parser 判定格式不符、rule/passwd 判定密码不合策略），
// 不是故障，因此**不捕获调用栈**：栈只在 Wrap 有 cause 时才补（见 Wrap）。
// 定位依靠 base/log 每条日志注入的 caller 字段，已经足够。
//
// 返回 *Error 而非 error：本函数恒返回非 nil，不存在 Wrap 那种「类型化 nil 指针」
// 的陷阱，返回具体类型可让调用方直接链式取 Kind/MsgKey，也便于单测断言。
// New 与 Wrap 返回类型不同是刻意的，原因见 Wrap 的注释。
//
// msgKey 为空时取该档兜底键；params 可为 nil。
func New(kind Kind, msgKey string, params Params) *Error {
	return newError(kind, msgKey, params, nil)
}

// Wrap 给底层错误打上档位与文案键。
//
// 两条关键语义：
//   - **cause 为 nil 时返回 nil**。沿用 pkg/errors 的既有口径，使
//     `return errs.Wrap(err, ...)` 在 err 为 nil 时天然返回 nil，避免包出一个
//     非 nil 的空错误——那是最难查的一类 bug。
//   - **栈只补一次**。cause 链上已有栈（来自 pkg/errors 的 Wrap/WithStack）时不
//     再补，否则每层包装各捕一次栈，%+v 会打出多份几乎相同的栈，反而看不清。
//
// 返回类型是 error 而不是 *Error，这是上面第一条语义的**必要条件**，不是随意选择：
// 若返回 *Error，`return errs.Wrap(err, ...)` 在 err 为 nil 时会把一个「类型化的
// nil 指针」装进 error 接口，使调用方的 `if err != nil` 恒为真——接口值非 nil，
// 只是其内部指针为 nil。pkg/errors 的 Wrap 同样返回 error，口径一致。
// 需要拿到 *Error 时用 From，或直接用 KindOf / MsgKeyOf / ParamsOf 读三件套。
//
// 典型调用方是 store/sqlite：把驱动的唯一约束错误包成 KindConflict 档。包装后
// 底层错误仍可被 errors.As 取回、errors.Is 命中，不丢诊断信息。
func Wrap(cause error, kind Kind, msgKey string, params Params) error {
	if cause == nil {
		return nil
	}
	if !hasStack(cause) {
		cause = errors.WithStack(cause)
	}
	return newError(kind, msgKey, params, cause)
}

// hasStack 报告错误链上是否已有 pkg/errors 的调用栈。
// 用 errors.As 遍历整条链，而非只看最外层——栈可能挂在链的任意一层。
func hasStack(err error) bool {
	var st interface{ StackTrace() errors.StackTrace }
	return errors.As(err, &st)
}

// 五档快捷构造函数。绝大多数调用点档位是固定的，直接写档名比传 Kind 参数更难写错，
// 也让 grep「哪些地方会产生越权错误」变得可行。
// 需要包装底层错误时用 Wrap 并显式给档位。

// NewInvalidInput 构造用户输入错误（D-3 五类解析错误、A-8 密码策略、I-5 汇率必填等）。
func NewInvalidInput(msgKey string, params Params) *Error {
	return New(KindInvalidInput, msgKey, params)
}

// NewConflict 构造唯一冲突错误（User.用户名、ExpenseCategory.(归属用户ID, 名称)）。
func NewConflict(msgKey string, params Params) *Error {
	return New(KindConflict, msgKey, params)
}

// NewForbidden 构造归属越权错误（不变式 3；前提 3 下管理员亦无业务数据特权）。
func NewForbidden(msgKey string, params Params) *Error {
	return New(KindForbidden, msgKey, params)
}

// NewUnauthorized 构造未登录错误（无令牌、A-5 的 12 小时过期、基线时间作废）。
func NewUnauthorized(msgKey string, params Params) *Error {
	return New(KindUnauthorized, msgKey, params)
}

// NewInternal 构造系统错误。有底层错误时优先用 Wrap，以保留 cause 与栈。
func NewInternal(msgKey string, params Params) *Error {
	return New(KindInternal, msgKey, params)
}

// Kind 返回档位。nil 接收者返回 KindUnknown，不 panic——错误路径上的方法
// 若自身会 panic，会把原始错误彻底盖掉。
func (e *Error) Kind() Kind {
	if e == nil {
		return KindUnknown
	}
	return e.kind
}

// MsgKey 返回文案键。nil 接收者返回空串。
func (e *Error) MsgKey() string {
	if e == nil {
		return ""
	}
	return e.msgKey
}

// Params 返回占位参数的副本。
//
// 返回副本而非内部 map：调用方（如 middleware 渲染前补字段）的改动不得影响错误
// 对象本身，否则同一个错误在日志与前端两条路径上会呈现出不同的参数。
// 无参数时返回 nil，与 len()==0 的判定一致。
func (e *Error) Params() Params {
	if e == nil || len(e.params) == 0 {
		return nil
	}
	out := make(Params, len(e.params))
	maps.Copy(out, e.params)
	return out
}

// Error 实现 error 接口，返回**面向日志与排错**的中文描述，形如：
//
//	[用户输入错误] parser.field_missing {Field=金额, Line=12}: 底层错误
//
// 这不是给用户看的文案——用户文案一律由 middleware 经 i18n 按文案键渲染。
// middleware 不得把本方法的返回值下发给用户（会泄露底层错误细节，前提 1 下应用层
// 是唯一防线）。
//
// 占位参数按键名排序输出：map 遍历顺序随机，不排序会让同一个错误每次打出不同的
// 字符串，日志检索与单测断言都无从下手。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("[")
	b.WriteString(e.kind.String())
	b.WriteString("] ")
	b.WriteString(e.msgKey)

	if len(e.params) > 0 {
		// slices.Sorted 保证键名有序：map 遍历顺序随机，不排序会让同一个错误每次
		// 打出不同字符串，日志检索与单测断言都无从下手。
		keys := slices.Sorted(maps.Keys(e.params))
		b.WriteString(" {")
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s=%v", k, e.params[k])
		}
		b.WriteString("}")
	}

	if e.cause != nil {
		b.WriteString(": ")
		b.WriteString(e.cause.Error())
	}
	return b.String()
}

// Unwrap 返回底层错误，使标准库 errors.Is / errors.As 能穿透本类型。
// store/sqlite 把驱动错误包成唯一冲突档后，上层仍可 As 出驱动的具体错误类型。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Cause 返回底层错误，使 pkg/errors.Cause 能穿透本类型。
// 与 Unwrap 并存是因为 pkg/errors 认 Cause() 而标准库认 Unwrap()，
// 而仓库内两套判定方式都会出现（go_common 的既有代码用 errors.Cause）。
func (e *Error) Cause() error {
	return e.Unwrap()
}

// Format 实现 fmt.Formatter，让 %+v 打出底层错误的调用栈。
//
// 分工：%v / %s 走单行 Error()，适合作为 logrus 字段；%+v 在单行后追加 cause 的
// 完整栈，用于排错。栈本身由 pkg/errors 生成，本包只做转发。
func (e *Error) Format(s fmt.State, verb rune) {
	if e == nil {
		return
	}
	switch verb {
	case 'v':
		if s.Flag('+') {
			io.WriteString(s, e.Error())
			if e.cause != nil {
				// %+v 透传给 cause，由 pkg/errors 打印栈帧。
				fmt.Fprintf(s, "\n%+v", e.cause)
			}
			return
		}
		io.WriteString(s, e.Error())
	case 's':
		io.WriteString(s, e.Error())
	case 'q':
		fmt.Fprintf(s, "%q", e.Error())
	}
}

// From 从错误链上取出本包的 *Error。
//
// 用 errors.As 遍历整条链，因此 err 被 pkg/errors.Wrap 或 fmt.Errorf("%w") 包装
// 多层后仍能取到——middleware 拿到的 err 必然是被包装过的。
// 未找到（含 err 为 nil）返回 nil, false。
//
// **ok 为 true 时第一个返回值必然非 nil**，调用方可直接解引用而无需再判空。
// 这条保证不是多余的：New 系列构造函数返回的是 *Error，于是
// `var err error = svc()`（svc 声明为返回 *Error）在成功路径上会得到一个
// 「类型化的 nil 指针」——接口值非 nil、内部指针为 nil。此时 errors.As 会**匹配
// 成功并把 nil 写进 target**，若照直返回 (nil, true)，KindOf / MsgKeyOf / IsKind
// 就会解引用 nil 而 panic。已实测：成功路径反而在 middleware 入口崩溃，
// 且崩在最不该崩的地方（没有错误的那条路）。故在此显式过滤 target 为 nil 的情形。
func From(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	var target *Error
	// target 为 nil 时按「未找到」处理：typed-nil 不携带任何档位与文案键，
	// 与「没有本包错误」在语义上完全一致，交由调用方走各自的兜底分支。
	if errors.As(err, &target) && target != nil {
		return target, true
	}
	return nil, false
}

// KindOf 取错误的档位，是 middleware 做 HTTP 状态映射的入口。
//
// 总函数，三种输入都有确定输出：
//   - nil                  → KindUnknown（无错误）
//   - 本包的分档错误       → 该档
//   - 未分档的其他 error   → KindInternal
//
// 第三条是关键：database/sql 直吐的错误、第三方库转出的 error 都不带档位，若返回
// 零值就要求 L6 自己再造一套兜底逻辑。一律按系统错误处理，既安全又不需下游重复兜底。
//
// 「类型化的 nil 指针」（`var err error = (*Error)(nil)`）归入第三条得 KindInternal：
// 接口值非 nil 而内部指针为 nil，它不携带任何档位，本质是上游把 *Error 直接赋给
// error 接口造成的编程错误。按系统错误响亮呈现，既不 panic（见 From），也不把一个
// 编程错误静默当成「无错误」放过去。
func KindOf(err error) Kind {
	if err == nil {
		return KindUnknown
	}
	if e, ok := From(err); ok {
		return e.kind
	}
	return KindInternal
}

// MsgKeyOf 取错误的文案键，是 middleware 经 i18n 取词的入口。
//
// 总函数：nil → 空串；分档错误 → 其键；未分档错误 → KeyInternal。
// 未分档错误绝不能把 err.Error() 当文案下发——那会把底层错误原文（可能含表名、
// SQL 片段、文件路径）暴露给用户。
func MsgKeyOf(err error) string {
	if err == nil {
		return ""
	}
	if e, ok := From(err); ok {
		return e.msgKey
	}
	return KeyInternal
}

// ParamsOf 取错误的占位参数副本，是 middleware 填充文案的入口。
// nil、未分档错误与无参数错误一律返回 nil。
func ParamsOf(err error) Params {
	if e, ok := From(err); ok {
		return e.Params()
	}
	return nil
}

// IsKind 报告错误是否属于给定档位。
//
// 供服务层按档位分流，例如 service/category 在 G-1 建类型时判断 store 返回的是否
// 唯一冲突档，从而给出「名称已存在」的友好提示而非系统错误。
//
// 语义与 KindOf 一致：未分档错误只与 KindInternal 匹配；kind 传 KindUnknown 时
// 恒为 false（KindUnknown 不是档位，仅表示无错误）。
func IsKind(err error, kind Kind) bool {
	if !kind.Valid() {
		return false
	}
	return KindOf(err) == kind
}
