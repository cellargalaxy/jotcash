package errs

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

// Error 统一错误类型：档位 + 文案键 + 占位参数 + 底层原因。
//
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md` §六 L0 `base/errs` 行：
// 「每个错误携带文案键与占位参数，错误对象内不写死面向用户的文案」。
// 面向用户的文案由 `api/http/middleware` 经 `i18n` 按「语言 + 文案键」取词并
// 填充占位参数后渲染（同文件 L3 `i18n` 行），因此本类型内**不存放任何已渲染的用户文案**
// ——这是 K-4「新增语种只需增加配置、无需改动代码」得以成立的前提。
//
// Error() 返回的字符串是**面向开发者的诊断信息**（中文，约定 3），
// 只进日志与 `%+v`，不直接回给用户。
type Error struct {
	// kind 五档之一，决定错误性质与 middleware 的 HTTP 状态映射。
	kind Kind
	// key 文案键，i18n 取词的唯一依据；本包不解释它的取值。
	key string
	// args 占位参数，按参数名索引，供 i18n 填充文案模板；可为空。
	args map[string]any
	// cause 底层原因，可为空；用于保留错误链，供 errors.Is / errors.As 穿透。
	cause error
}

// 确保 *Error 实现 error 接口。
var _ error = (*Error)(nil)

// Kind 返回错误档位。
func (e *Error) Kind() Kind {
	if e == nil {
		return KindSystem
	}
	return e.kind
}

// Code 返回错误码，等价于 Kind().Code()。
func (e *Error) Code() int {
	return e.Kind().Code()
}

// Key 返回文案键，供 i18n 取词。
func (e *Error) Key() string {
	if e == nil {
		return ""
	}
	return e.key
}

// Args 返回占位参数的**副本**，供 i18n 填充文案模板。
// 返回副本而非内部 map，避免调用方修改错误对象内部状态。
// 无占位参数时返回 nil。
func (e *Error) Args() map[string]any {
	if e == nil || len(e.args) == 0 {
		return nil
	}
	args := make(map[string]any, len(e.args))
	for name, value := range e.args {
		args[name] = value
	}
	return args
}

// Error 返回面向开发者的诊断信息（中文），格式为
// 「<档位>[<文案键>]{<占位参数>}: <底层原因>」，后两段按需省略。
// 该字符串只进日志，不作为面向用户的提示文案。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(e.kind.String())
	if e.key != "" {
		builder.WriteString("[")
		builder.WriteString(e.key)
		builder.WriteString("]")
	}
	if len(e.args) > 0 {
		builder.WriteString(formatArgs(e.args))
	}
	if e.cause != nil {
		builder.WriteString(": ")
		builder.WriteString(e.cause.Error())
	}
	return builder.String()
}

// Unwrap 返回底层原因，使 errors.Is / errors.As 可穿透本类型。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Is 支持按「档位」匹配：errors.Is(err, errs.Input()) 为真当且仅当
// err 链上存在同档位的 *Error。文案键与占位参数不参与匹配
// ——同一档位下文案键众多，按档位匹配才是调用方真正需要的判据
// （middleware 的状态映射、service 层的分支处理都只看档位）。
func (e *Error) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	var other *Error
	if !errors.As(target, &other) || other == nil {
		return false
	}
	return e.kind == other.kind
}

// Format 实现 fmt.Formatter，使 `%+v` 打印底层原因的完整堆栈。
// 依赖 pkg/errors 的堆栈能力：cause 由 New/Wrap 用 errors.WithStack 包装，
// 因此 `logrus.WithFields(logrus.Fields{"err": err})` 与 `%+v` 均可取到堆栈，
// 与 go_common/util 既有用法一致。
func (e *Error) Format(state fmt.State, verb rune) {
	if e == nil {
		return
	}
	switch verb {
	case 'v':
		if state.Flag('+') {
			// 先打印本层诊断信息（不含 cause，避免与下方堆栈重复），再打印 cause 的完整堆栈。
			var builder strings.Builder
			builder.WriteString(e.kind.String())
			if e.key != "" {
				builder.WriteString("[")
				builder.WriteString(e.key)
				builder.WriteString("]")
			}
			if len(e.args) > 0 {
				builder.WriteString(formatArgs(e.args))
			}
			_, _ = fmt.Fprint(state, builder.String())
			if e.cause != nil {
				_, _ = fmt.Fprintf(state, ": %+v", e.cause)
			}
			return
		}
		fallthrough
	case 's':
		_, _ = fmt.Fprint(state, e.Error())
	case 'q':
		_, _ = fmt.Fprintf(state, "%q", e.Error())
	}
}

// formatArgs 把占位参数格式化为 `{名称=值 名称=值}`，参数名升序保证输出稳定
// （map 遍历无序，不排序会导致同一错误的日志文本每次不同、无法比对）。
func formatArgs(args map[string]any) string {
	names := make([]string, 0, len(args))
	for name := range args {
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	builder.WriteString("{")
	for i, name := range names {
		if i > 0 {
			builder.WriteString(" ")
		}
		_, _ = fmt.Fprintf(&builder, "%s=%v", name, args[name])
	}
	builder.WriteString("}")
	return builder.String()
}
