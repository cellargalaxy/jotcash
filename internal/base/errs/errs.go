package errs

import (
	"github.com/pkg/errors"
)

// Arg 一个占位参数。用可变参数而非 map 字面量，保证调用点简洁且参数名不会写漏。
type Arg struct {
	Name  string
	Value any
}

// A 构造一个占位参数，供 New / Wrap 的可变参数使用。
// 例：errs.New(errs.KindConflict, "category.name.duplicated", errs.A("name", "餐饮"))
func A(name string, value any) Arg {
	return Arg{Name: name, Value: value}
}

// New 构造一个不带底层原因的错误。
// kind 非五档之一时一律归入 KindSystem——档位是 middleware 状态映射的唯一依据，
// 落到未知档位会让映射无从下手，因此在入口处收敛。
func New(kind Kind, key string, args ...Arg) *Error {
	return &Error{
		kind:  normalizeKind(kind),
		key:   key,
		args:  buildArgs(args),
		cause: nil,
	}
}

// Wrap 在保留底层原因的前提下构造错误，底层原因用 errors.WithStack 记录堆栈，
// 使 `%+v` 能打印出原始出错位置（与 go_common/util 的既有用法一致）。
// cause 为 nil 时退化为 New，返回的错误不带 cause。
func Wrap(cause error, kind Kind, key string, args ...Arg) *Error {
	return &Error{
		kind:  normalizeKind(kind),
		key:   key,
		args:  buildArgs(args),
		cause: errors.WithStack(cause), // cause 为 nil 时 WithStack 返回 nil
	}
}

// WithArgs 返回一个补充了占位参数的**新错误**，原错误不变（值语义，避免共享可变状态）。
// 用于「下层定档位与文案键、上层补充上下文参数」的场景。
// err 为 nil 时返回 nil。
func WithArgs(err *Error, args ...Arg) *Error {
	if err == nil {
		return nil
	}
	merged := make(map[string]any, len(err.args)+len(args))
	for name, value := range err.args {
		merged[name] = value
	}
	for _, arg := range args {
		if arg.Name == "" {
			continue
		}
		merged[arg.Name] = arg.Value
	}
	if len(merged) == 0 {
		merged = nil
	}
	return &Error{kind: err.kind, key: err.key, args: merged, cause: err.cause}
}

// From 把任意 error 归一化为 *Error，供 middleware 在出口处统一取档位与文案键。
//
// 三种情况：
//  1. nil                → 返回 nil；
//  2. 链上存在 *Error    → 返回链上**最外层**的那个 *Error（保留其档位、文案键与占位参数）；
//  3. 其余（非本包错误） → 归入 KindSystem 并挂上兜底文案键 KeySystemInternal，
//     原错误作为 cause 保留，日志里仍能看到原始信息。
//
// 第 3 种是「未分类错误不得泄漏给用户」的兜底：middleware 拿到的一定是有档位、
// 有文案键的错误，因此不会出现「没有文案键、无从取词」的出口。
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target
	}
	return &Error{
		kind:  KindSystem,
		key:   KeySystemInternal,
		args:  nil,
		cause: errors.WithStack(err),
	}
}

// KindOf 返回 err 的档位：链上存在 *Error 时取其档位，否则一律 KindSystem；
// err 为 nil 时返回零值 Kind（Valid() 为 false），便于调用方区分「无错误」。
func KindOf(err error) Kind {
	if err == nil {
		return 0
	}
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Kind()
	}
	return KindSystem
}

// IsKind 判断 err 链上是否存在指定档位的 *Error。
// 非本包错误一律视为 KindSystem，因此 IsKind(err, KindSystem) 对它们为真。
func IsKind(err error, kind Kind) bool {
	if err == nil {
		return false
	}
	return KindOf(err) == normalizeKind(kind)
}

// 以下五个构造函数是五档的快捷入口，避免调用点重复写档位常量。

// NewInput 构造「用户输入错误」。
func NewInput(key string, args ...Arg) *Error {
	return New(KindInput, key, args...)
}

// NewConflict 构造「唯一冲突」。
func NewConflict(key string, args ...Arg) *Error {
	return New(KindConflict, key, args...)
}

// NewForbidden 构造「归属越权」。
func NewForbidden(key string, args ...Arg) *Error {
	return New(KindForbidden, key, args...)
}

// NewUnauthorized 构造「未登录」。
func NewUnauthorized(key string, args ...Arg) *Error {
	return New(KindUnauthorized, key, args...)
}

// NewSystem 构造「系统错误」。
func NewSystem(key string, args ...Arg) *Error {
	return New(KindSystem, key, args...)
}

// 以下五个判定函数是五档的快捷判据，供 service 层与 middleware 分支使用。

// IsInput 判断是否为「用户输入错误」。
func IsInput(err error) bool { return IsKind(err, KindInput) }

// IsConflict 判断是否为「唯一冲突」。
func IsConflict(err error) bool { return IsKind(err, KindConflict) }

// IsForbidden 判断是否为「归属越权」。
func IsForbidden(err error) bool { return IsKind(err, KindForbidden) }

// IsUnauthorized 判断是否为「未登录」。
func IsUnauthorized(err error) bool { return IsKind(err, KindUnauthorized) }

// IsSystem 判断是否为「系统错误」。
func IsSystem(err error) bool { return IsKind(err, KindSystem) }

// normalizeKind 把非五档之一的档位收敛为 KindSystem。
func normalizeKind(kind Kind) Kind {
	if kind.Valid() {
		return kind
	}
	return KindSystem
}

// buildArgs 把可变参数转为 map；空名参数丢弃（无名参数无法在文案模板里索引），
// 无有效参数时返回 nil 而非空 map，保证 Args() 的「无参数即 nil」语义一致。
func buildArgs(args []Arg) map[string]any {
	if len(args) == 0 {
		return nil
	}
	built := make(map[string]any, len(args))
	for _, arg := range args {
		if arg.Name == "" {
			continue
		}
		built[arg.Name] = arg.Value
	}
	if len(built) == 0 {
		return nil
	}
	return built
}
