package errs_test

// 外部消费者验证（黑盒）：模拟 `api/http/middleware` 的真实用法
// ——「`base/errs` 五档 → HTTP 状态映射」+「经 `i18n` 按文案键与占位参数渲染」。
//
// 与 errs_test.go 的区别：本文件在**包外**（`errs_test` 包），只能触达导出 API，
// 因此它验证的是「对外契约是否真的够用」，而不是内部实现细节。
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md` §六 L6 `middleware` 行 ①：
// 「`base/errs` 五档 → HTTP 状态映射（唯一冲突 → 409、归属越权 → 403、未登录 → 401）
// + 经 `i18n` 渲染文案键」。

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/pkg/errors"
)

// mapStatus 即 middleware 的映射表。default 分支覆盖「系统错误 + 非本包错误」，
// 保证任何错误都有确定状态码，不会漏到 200。
func mapStatus(err error) int {
	switch errs.KindOf(err) {
	case errs.KindInput:
		return http.StatusBadRequest
	case errs.KindConflict:
		return http.StatusConflict
	case errs.KindForbidden:
		return http.StatusForbidden
	case errs.KindUnauthorized:
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// renderStub 即 i18n 取词的桩：只验证「键与占位参数在出口处可取到」，不做真实取词。
func renderStub(err error) string {
	target := errs.From(err)
	if target == nil {
		return ""
	}
	return fmt.Sprintf("key=%s args=%v", target.Key(), target.Args())
}

// TestMiddlewareMapping 逐档走查五档到 HTTP 状态的映射，并确认出口处一定有可取词的文案键。
func TestMiddlewareMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"唯一冲突", errs.NewConflict(errs.KeyCategoryNameDuplicated, errs.A(errs.ArgName, "餐饮")), http.StatusConflict},
		{"归属越权", errs.NewForbidden(errs.KeyAuthForbidden), http.StatusForbidden},
		{"未登录", errs.NewUnauthorized(errs.KeyAuthSessionExpired), http.StatusUnauthorized},
		{"用户输入错误", errs.NewInput(errs.KeyFxRateRequired), http.StatusBadRequest},
		{"系统错误", errs.NewSystem(errs.KeySystemInternal), http.StatusInternalServerError},
		// service 层按项目风格用 errors.Wrap 加中文上下文后，middleware 仍要能取到档位
		{"包装后的唯一冲突", errors.Wrap(errs.NewConflict(errs.KeyUserNameDuplicated), "新增账户，异常"), http.StatusConflict},
		// 第三方错误（如 SQLite 驱动）必须兜底为 500，且有可取词的键
		{"第三方错误", fmt.Errorf("driver: bad connection"), http.StatusInternalServerError},
	}
	for _, item := range cases {
		if got := mapStatus(item.err); got != item.status {
			t.Errorf("%s：状态码期望%d，实际%d", item.name, item.status, got)
		}
		if target := errs.From(item.err); target.Key() == "" {
			t.Errorf("%s：出口处文案键为空，i18n 无从取词", item.name)
		}
		t.Logf("%s -> %d | %s", item.name, mapStatus(item.err), renderStub(item.err))
	}
}

// TestServiceBranchByKind 模拟 service 层按档位分支：
// 例如 G-1 建类型时，唯一冲突要转成友好提示，其余错误直接上抛。
func TestServiceBranchByKind(t *testing.T) {
	// 存储层唯一索引冲突（依据 §六 L3 `store` 行 ⑤）
	storeErr := errs.Wrap(fmt.Errorf("UNIQUE constraint failed: expense_category.name"),
		errs.KindConflict, errs.KeyCategoryNameDuplicated, errs.A(errs.ArgName, "餐饮"))

	if !errs.IsConflict(storeErr) {
		t.Fatal("期望判定为唯一冲突，实际不是")
	}
	// 占位参数要能被 i18n 取到，用于渲染「类型『餐饮』已存在」这类文案
	if got := storeErr.Args()[errs.ArgName]; got != "餐饮" {
		t.Errorf("占位参数%s期望「餐饮」，实际%v", errs.ArgName, got)
	}
	// 原始驱动错误仍在链上，日志可查
	if !errors.Is(storeErr, errors.Cause(storeErr)) {
		t.Error("期望底层原因仍在链上")
	}
	// 其余档位不得误命中
	if errs.IsInput(storeErr) || errs.IsForbidden(storeErr) || errs.IsUnauthorized(storeErr) || errs.IsSystem(storeErr) {
		t.Error("唯一冲突错误命中了其他档位")
	}
}
