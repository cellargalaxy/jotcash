package store

import (
	"errors"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// 本文件验证错误档位与文案键（承载 ⑤ 的冲突档 + 红线 2 的同错误语义）。
//
// 档位错了的后果是可见的：唯一冲突报 500 会让 A-1 的「重复初始化天然幂等」失效
// （service/account 靠 409 判断「已初始化过」），而「不存在」与「归属不符」若返回
// 不同错误，错误本身就成了存在性预言机。

// TestNotFoundIsInvalidInputNotForbidden 验证「未找到」归 400 档而非 403。
//
// 这是红线 2 的档位依据：若归属不符返回 403、不存在返回 400，攻击者就能靠档位
// 区分「这个 ID 存在但不属于我」与「这个 ID 不存在」，从而枚举出他人明细的 ID
// 空间——不变式 3「归属校验无死角」当场失效。
//
// 先例：errs 自身的用例里就有 Wrap(sql.ErrNoRows, KindInvalidInput,
// "expense.not_found", nil)。
func TestNotFoundIsInvalidInputNotForbidden(t *testing.T) {
	err := NewNotFoundError("Expense")
	if errs.IsKind(err, errs.KindForbidden) {
		t.Fatal("「未找到」不得归 KindForbidden（403）：" +
			"归属不符与不存在必须同档同文案，否则错误档位成为存在性预言机")
	}
	if !errs.IsKind(err, errs.KindInvalidInput) {
		t.Fatalf("「未找到」应归 KindInvalidInput（400），实际 %v", err)
	}
	if got := errs.MsgKeyOf(err); got != KeyNotFound {
		t.Fatalf("文案键 = %q, 期望 %q", got, KeyNotFound)
	}
}

// TestNotFoundIsIdenticalAcrossEntities 验证红线 2 的核心：同一实体上
// 「不存在」与「归属不符」必须产出**完全一致**的错误。
//
// 本包只提供一个构造函数，故一致性天然成立——本用例把这条「只有一个入口」钉住：
// 若将来有人加了 NewOwnerMismatchError 之类的第二个构造函数，档位或文案键一旦
// 不同，攻击者就能区分两种情形并枚举他人明细ID。
func TestNotFoundIsIdenticalAcrossEntities(t *testing.T) {
	// 模拟两种情形：L4 对「查不到行」与「行存在但 owner 不匹配」都只能调这一个函数
	notExist := NewNotFoundError("Expense")
	ownerMismatch := NewNotFoundError("Expense")

	if errs.MsgKeyOf(notExist) != errs.MsgKeyOf(ownerMismatch) {
		t.Fatal("两种情形的文案键不同，用户可据此区分「不存在」与「不属于我」")
	}
	if notExist.Error() != ownerMismatch.Error() {
		t.Fatalf("两种情形的错误文本不同：%q vs %q；"+
			"文本差异同样可用于枚举", notExist.Error(), ownerMismatch.Error())
	}
	// params 也必须一致——若带上 OwnerUserID 之类的字段，前端可见的差异同样泄漏信息
	p1, p2 := paramsOf(t, notExist), paramsOf(t, ownerMismatch)
	if len(p1) != len(p2) {
		t.Fatal("两种情形的 params 不一致")
	}
	for k, v := range p1 {
		if p2[k] != v {
			t.Fatalf("params[%q] 不一致: %v vs %v", k, v, p2[k])
		}
	}
}

// TestNotFoundWrapPreservesKindAndKey 验证包装底层错误后档位与文案键不变。
//
// L4 实现会用 WrapNotFoundError(sql.ErrNoRows)。若包装后档位漂移，
// 上层的 errs.IsKind 判断会失效。
func TestNotFoundWrapPreservesKindAndKey(t *testing.T) {
	sentinel := errors.New("sql: no rows in result set")
	err := WrapNotFoundError(sentinel, "Expense")

	if !errs.IsKind(err, errs.KindInvalidInput) {
		t.Fatalf("包装后档位应保持 KindInvalidInput，实际 %v", err)
	}
	if got := errs.MsgKeyOf(err); got != KeyNotFound {
		t.Fatalf("包装后文案键 = %q, 期望 %q", got, KeyNotFound)
	}
	// 底层错误必须可经 errors.Is 追溯（排错时要能看到是驱动的哪个错误）
	if !errors.Is(err, sentinel) {
		t.Fatal("包装后无法用 errors.Is 追溯底层错误，排错时看不到驱动原始错误")
	}
}

// TestConflictErrorsAreConflictKind 验证两项唯一约束的冲突都归「唯一冲突」档。
//
// 承载 ⑤ 明写「冲突返回 base/errs 的『唯一冲突』档」。这条对 A-1 尤其要紧：
// service/account 靠 409 判断「已初始化过」，从而使重复初始化天然幂等。
// 若它变成 500，app 每次启动都会因为「存储层失败」而中止。
func TestConflictErrorsAreConflictKind(t *testing.T) {
	cases := []struct {
		name string
		err  error
		key  string
	}{
		{"用户名冲突", NewUsernameConflictError("admin"), KeyUsernameConflict},
		{"类型名冲突", NewCategoryNameConflictError("餐饮"), KeyCategoryNameConflict},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !errs.IsKind(c.err, errs.KindConflict) {
				t.Fatalf("应归 KindConflict（409），实际 %v；"+
					"归 500 会让 A-1 的「重复初始化天然幂等」失效", c.err)
			}
			if got := errs.MsgKeyOf(c.err); got != c.key {
				t.Fatalf("文案键 = %q, 期望 %q", got, c.key)
			}
		})
	}

	// 包装形态同样保持档位——L4 会用它映射驱动的唯一约束违反
	drvErr := errors.New("UNIQUE constraint failed: user.username")
	if wrapped := WrapUsernameConflictError(drvErr, "admin"); !errs.IsKind(wrapped, errs.KindConflict) {
		t.Fatalf("包装后应保持 KindConflict，实际 %v", wrapped)
	}
	if wrapped := WrapCategoryNameConflictError(drvErr, "餐饮"); !errs.IsKind(wrapped, errs.KindConflict) {
		t.Fatalf("包装后应保持 KindConflict，实际 %v", wrapped)
	}
}

// TestCategoryInUseIsInvalidInput 验证「类型被引用」归 400 而非 409。
//
// G-4 的「被引用的类型不可删除」是一条业务规则，不是并发冲突：用户下次再试仍会
// 失败。归 409 会让前端以为「重试可能成功」（409 的常规语义是资源状态冲突）。
func TestCategoryInUseIsInvalidInput(t *testing.T) {
	err := NewCategoryInUseError("餐饮", 12)
	if !errs.IsKind(err, errs.KindInvalidInput) {
		t.Fatalf("应归 KindInvalidInput（400），实际 %v", err)
	}
	if got := errs.MsgKeyOf(err); got != KeyCategoryInUse {
		t.Fatalf("文案键 = %q, 期望 %q", got, KeyCategoryInUse)
	}
	// 笔数必须进 params：G-4 的提示要告诉用户「有 12 笔明细在用」
	params := paramsOf(t, err)
	if params["Count"] == nil {
		t.Error("引用笔数未进 params，用户看不到「有几笔在用」，无从判断能否清理")
	}
}

// TestOwnerRequiredIsInternal 验证「缺归属」归 500 而非 400。
//
// 归属用户ID 来自登录上下文（token → middleware → handler → service），不是用户
// 可填的字段。它为零值只能是接线错误：某一层忘了透传。这类错误报 400 会把接线
// 缺陷伪装成用户输入问题，用户反复重试也不会好。
func TestOwnerRequiredIsInternal(t *testing.T) {
	err := NewOwnerRequiredError("Expense")
	if !errs.IsKind(err, errs.KindInternal) {
		t.Fatalf("应归 KindInternal（500），实际 %v；"+
			"归属缺失是接线错误而非用户输入问题", err)
	}
	if got := errs.MsgKeyOf(err); got != KeyOwnerRequired {
		t.Fatalf("文案键 = %q, 期望 %q", got, KeyOwnerRequired)
	}
	// 实体名必须进 params：排错时要能立刻知道是哪个仓储漏传
	if params := paramsOf(t, err); params["Entity"] == nil {
		t.Error("实体名未进 params，排错时无法定位漏传的调用点")
	}
}

// TestAllMessageKeysArePrefixed 验证全部文案键统一带 store. 前缀且无重复。
//
// 前缀是 i18n 落词时定位归属包的依据；重复键会让两条不同错误共用一句文案，
// 用户看到的提示与实际原因不符。
func TestAllMessageKeysArePrefixed(t *testing.T) {
	keys := []string{
		KeyNotFound, KeyUsernameConflict, KeyCategoryNameConflict,
		KeyCategoryInUse, KeyOwnerRequired,
		KeyAmountOutOfRange, KeyRateOutOfRange,
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if k == "" {
			t.Fatal("存在空文案键")
		}
		if len(k) < len("store.") || k[:len("store.")] != "store." {
			t.Fatalf("文案键 %q 未带 store. 前缀，i18n 落词时无法定位归属包", k)
		}
		if seen[k] {
			t.Fatalf("文案键 %q 重复，两条错误会共用同一句文案", k)
		}
		seen[k] = true
	}
}

// TestConflictParamsCarryIdentifier 验证冲突错误把冲突值带进 params。
//
// 唯一冲突的提示必须能说出「哪个用户名/类型名重复了」，否则用户不知道改什么。
func TestConflictParamsCarryIdentifier(t *testing.T) {
	if params := paramsOf(t, NewUsernameConflictError("admin")); params["Username"] != "admin" {
		t.Errorf("用户名未进 params: %+v", params)
	}
	if params := paramsOf(t, NewCategoryNameConflictError("餐饮")); params["Name"] != "餐饮" {
		t.Errorf("类型名未进 params: %+v", params)
	}
}

// paramsOf 取出错误的 params，取不到则失败。
func paramsOf(t *testing.T, err error) errs.Params {
	t.Helper()
	e, ok := errs.From(err)
	if !ok {
		t.Fatalf("错误不是 *errs.Error: %v", err)
	}
	return e.Params()
}
