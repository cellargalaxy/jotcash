package errs

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/pkg/errors"
)

// ============================================================
// 一、五档是闭集
// 承载：L0 表 base/errs 行「**五档**：用户输入错误 / 唯一冲突 / 归属越权 / 未登录 /
// 系统错误」。档位增删会直接改变 middleware 的 HTTP 状态映射，必须被用例锁死。
// ============================================================

// TestKindsIsClosedSet 锁死「恰五档」。
// 有人新增第六档而未同步 middleware 的映射表时，本用例先失败。
func TestKindsIsClosedSet(t *testing.T) {
	kinds := Kinds()
	if len(kinds) != 5 {
		t.Fatalf("档位数量应为 5，实际 %d：%v", len(kinds), kinds)
	}

	want := []Kind{KindInvalidInput, KindConflict, KindForbidden, KindUnauthorized, KindInternal}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("第 %d 档应为 %v，实际 %v", i, want[i], kinds[i])
		}
	}

	// KindUnknown 是零值、表示「无错误」，不得混入五档。
	for _, k := range kinds {
		if k == KindUnknown {
			t.Error("KindUnknown 不应出现在五档中")
		}
	}
}

// TestKindsReturnsCopy 锁死 Kinds 返回副本：调用方改动不得污染本包状态。
func TestKindsReturnsCopy(t *testing.T) {
	first := Kinds()
	first[0] = KindInternal

	if Kinds()[0] != KindInvalidInput {
		t.Error("Kinds 返回的切片被外部改动影响，应返回副本")
	}
}

// TestKindCodeAndNameUnique 锁死码与中文名两两不重复。
// 重复会让日志检索与前端判定把两档混为一谈。
func TestKindCodeAndNameUnique(t *testing.T) {
	codes := make(map[string]Kind, 5)
	names := make(map[string]Kind, 5)

	for _, k := range Kinds() {
		code := k.Code()
		if code == "" {
			t.Errorf("档位 %v 的 Code 为空", k)
		}
		if dup, ok := codes[code]; ok {
			t.Errorf("码 %q 在 %v 与 %v 上重复", code, dup, k)
		}
		codes[code] = k

		name := k.String()
		if name == "" || name == "unknown" {
			t.Errorf("档位 %v 的中文名非法：%q", k, name)
		}
		if dup, ok := names[name]; ok {
			t.Errorf("中文名 %q 在 %v 与 %v 上重复", name, dup, k)
		}
		names[name] = k
	}
}

// TestKindValidBoundary 锁死 Valid 的边界：只有五档为真。
func TestKindValidBoundary(t *testing.T) {
	if KindUnknown.Valid() {
		t.Error("KindUnknown 不应 Valid")
	}
	// 越界值：五档之外的任意数值一律非法。
	if Kind(200).Valid() {
		t.Error("越界档位不应 Valid")
	}
	for _, k := range Kinds() {
		if !k.Valid() {
			t.Errorf("档位 %v 应 Valid", k)
		}
	}
}

// TestKindZeroAndOutOfRangeFallback 锁死非五档的降级表现：
// Code 空串（「没有码」的确定表达）、String 为 unknown（不返回空串，否则日志里
// 会出现看不见的字段）、兜底键归一到 KeyInternal（不把内部状态暴露给用户）。
func TestKindZeroAndOutOfRangeFallback(t *testing.T) {
	for _, k := range []Kind{KindUnknown, Kind(200)} {
		if k.Code() != "" {
			t.Errorf("%d 的 Code 应为空串，实际 %q", k, k.Code())
		}
		if k.String() != "unknown" {
			t.Errorf("%d 的 String 应为 unknown，实际 %q", k, k.String())
		}
		if k.DefaultMsgKey() != KeyInternal {
			t.Errorf("%d 的兜底键应为 %q，实际 %q", k, KeyInternal, k.DefaultMsgKey())
		}
	}
}

// TestDefaultMsgKeyPerKind 锁死五档各自的兜底键，且五个键两两不同。
//
// 这五个键是本包对 i18n 那一轮的硬性契约：internal/i18n/locales/ 的语言文件
// 必须全部包含，否则未分档错误渲染不出文案。
func TestDefaultMsgKeyPerKind(t *testing.T) {
	want := map[Kind]string{
		KindInvalidInput: KeyInvalidInput,
		KindConflict:     KeyConflict,
		KindForbidden:    KeyForbidden,
		KindUnauthorized: KeyUnauthorized,
		KindInternal:     KeyInternal,
	}

	seen := make(map[string]bool, 5)
	for _, k := range Kinds() {
		got := k.DefaultMsgKey()
		if got != want[k] {
			t.Errorf("档位 %v 的兜底键应为 %q，实际 %q", k, want[k], got)
		}
		if seen[got] {
			t.Errorf("兜底键 %q 重复", got)
		}
		seen[got] = true

		// 兜底键必须带 errs. 前缀：与业务包自建的 <包>.<场景> 键区分开，
		// 一眼可辨「这是没带键的兜底渲染」。
		if !strings.HasPrefix(got, "errs.") {
			t.Errorf("兜底键 %q 应以 errs. 开头", got)
		}
	}
}

// ============================================================
// 二、五档快捷构造函数
// ============================================================

// TestNewShortcutsCarryKind 锁死五个快捷构造函数各自的档位与文案键。
func TestNewShortcutsCarryKind(t *testing.T) {
	cases := []struct {
		name string
		err  *Error
		kind Kind
	}{
		{"用户输入错误", NewInvalidInput("parser.format_mismatch", nil), KindInvalidInput},
		{"唯一冲突", NewConflict("category.name_duplicated", nil), KindConflict},
		{"归属越权", NewForbidden("expense.not_owner", nil), KindForbidden},
		{"未登录", NewUnauthorized("auth.token_expired", nil), KindUnauthorized},
		{"系统错误", NewInternal("store.query_failed", nil), KindInternal},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.err.Kind() != c.kind {
				t.Errorf("档位应为 %v，实际 %v", c.kind, c.err.Kind())
			}
			if KindOf(c.err) != c.kind {
				t.Errorf("KindOf 应为 %v，实际 %v", c.kind, KindOf(c.err))
			}
			if c.err.MsgKey() == "" {
				t.Error("文案键不应为空")
			}
			// 无 cause 的错误不带栈：这类是预期内的业务错误，不是故障（方案取舍 4）。
			if hasStack(c.err) {
				t.Error("无 cause 的错误不应捕获调用栈")
			}
		})
	}
}

// TestNewNormalizesEmptyKey 锁死空文案键归一到档位兜底键——
// middleware 必须永远有键可渲染，不能拿到空串。
func TestNewNormalizesEmptyKey(t *testing.T) {
	for _, key := range []string{"", "   ", "\t\n"} {
		err := New(KindInvalidInput, key, nil)
		if err.MsgKey() != KeyInvalidInput {
			t.Errorf("键 %q 应归一到 %q，实际 %q", key, KeyInvalidInput, err.MsgKey())
		}
	}
}

// TestNewNormalizesInvalidKind 锁死非法档位归一到 KindInternal：
// 未知档位一律按系统错误对外呈现，不泄露内部状态。
func TestNewNormalizesInvalidKind(t *testing.T) {
	for _, k := range []Kind{KindUnknown, Kind(200)} {
		err := New(k, "x.y", nil)
		if err.Kind() != KindInternal {
			t.Errorf("档位 %d 应归一到 KindInternal，实际 %v", k, err.Kind())
		}
		if err.MsgKey() != "x.y" {
			t.Errorf("归一档位不应改写已给的文案键，实际 %q", err.MsgKey())
		}
	}
}

// ============================================================
// 三、提取函数是总函数（方案取舍 5）
// 承载：middleware 从**任意** error 都要能读出「档位 + 文案键 + 占位参数」。
// ============================================================

// TestExtractFromNil 锁死 nil 输入的确定输出。
func TestExtractFromNil(t *testing.T) {
	if got := KindOf(nil); got != KindUnknown {
		t.Errorf("KindOf(nil) 应为 KindUnknown，实际 %v", got)
	}
	if got := MsgKeyOf(nil); got != "" {
		t.Errorf("MsgKeyOf(nil) 应为空串，实际 %q", got)
	}
	if got := ParamsOf(nil); got != nil {
		t.Errorf("ParamsOf(nil) 应为 nil，实际 %v", got)
	}
	if _, ok := From(nil); ok {
		t.Error("From(nil) 应返回 false")
	}
	if IsKind(nil, KindInternal) {
		t.Error("IsKind(nil, ...) 应为 false")
	}
}

// TestExtractFromUnclassifiedError 锁死未分档错误一律归一到系统错误档。
//
// 这是安全相关的一条：若返回零值，middleware 就得自己兜底，且极易把
// err.Error()（可能含表名、SQL 片段、文件路径）当文案下发给用户。
func TestExtractFromUnclassifiedError(t *testing.T) {
	plain := errors.New("底层驱动错误：no such table: expense")

	if got := KindOf(plain); got != KindInternal {
		t.Errorf("未分档错误的档位应为 KindInternal，实际 %v", got)
	}
	if got := MsgKeyOf(plain); got != KeyInternal {
		t.Errorf("未分档错误的文案键应为 %q，实际 %q", KeyInternal, got)
	}
	if got := ParamsOf(plain); got != nil {
		t.Errorf("未分档错误的占位参数应为 nil，实际 %v", got)
	}
	if _, ok := From(plain); ok {
		t.Error("From 对未分档错误应返回 false")
	}
	// 归一到系统错误档后，IsKind 只与 KindInternal 匹配。
	if !IsKind(plain, KindInternal) {
		t.Error("未分档错误应匹配 KindInternal")
	}
	if IsKind(plain, KindInvalidInput) {
		t.Error("未分档错误不应匹配 KindInvalidInput")
	}
}

// TestExtractThroughWrapChains 锁死三件套能穿透**多层包装**读出。
//
// middleware 拿到的 err 必然被 service 层包装过若干层，两种包装写法都要能穿透：
// pkg/errors.Wrap（go_common 生态的既有写法）与 fmt.Errorf("%w")（标准库写法）。
func TestExtractThroughWrapChains(t *testing.T) {
	base := NewConflict("category.name_duplicated", Params{"Name": "餐饮"})

	chains := map[string]error{
		"pkg/errors.Wrap": errors.Wrap(base, "service/category 建类型失败"),
		"pkg/errors 两层":   errors.Wrap(errors.WithMessage(base, "内层"), "外层"),
		"fmt.Errorf %w":   fmt.Errorf("handler 处理失败: %w", base),
		"两种写法混合":          errors.Wrap(fmt.Errorf("中层: %w", base), "外层"),
	}

	for name, err := range chains {
		t.Run(name, func(t *testing.T) {
			if got := KindOf(err); got != KindConflict {
				t.Errorf("档位应为 KindConflict，实际 %v", got)
			}
			if got := MsgKeyOf(err); got != "category.name_duplicated" {
				t.Errorf("文案键读取错误，实际 %q", got)
			}
			if got := ParamsOf(err); got["Name"] != "餐饮" {
				t.Errorf("占位参数读取错误，实际 %v", got)
			}
			if !IsKind(err, KindConflict) {
				t.Error("IsKind 应命中 KindConflict")
			}
			// From 必须取回同一个实例，而不是复制品。
			if got, ok := From(err); !ok || got != base {
				t.Error("From 未取回原始 *Error 实例")
			}
		})
	}
}

// TestIsKindRejectsUnknown 锁死 KindUnknown 不是档位：
// 它表示「无错误」，拿它做匹配恒为 false，避免「无错误」被误当成一档处理。
func TestIsKindRejectsUnknown(t *testing.T) {
	err := NewForbidden("expense.not_owner", nil)
	if IsKind(err, KindUnknown) {
		t.Error("IsKind 传 KindUnknown 应恒为 false")
	}
	if IsKind(err, Kind(200)) {
		t.Error("IsKind 传越界档位应恒为 false")
	}
}

// ============================================================
// 四、Wrap 与底层错误的互操作
// 承载：store 承载⑤「冲突返回 base/errs 的唯一冲突档」；阶段 5「唯一索引冲突须能
// 映射到 base/errs 的唯一冲突档」。映射后**不得丢失**底层错误，否则排错无据。
// ============================================================

// driverErr 模拟 SQLite 驱动的唯一约束错误，用于验证 errors.As 能穿透本包。
type driverErr struct {
	code int
	msg  string
}

func (e *driverErr) Error() string { return e.msg }

// TestWrapPreservesCauseIdentity 锁死 store/sqlite 的核心用法：
// 驱动错误被包成唯一冲突档后，仍可 As 出驱动类型、Is 命中原实例、Cause 回到根因。
func TestWrapPreservesCauseIdentity(t *testing.T) {
	driver := &driverErr{code: 2067, msg: "UNIQUE constraint failed: expense_category.name"}
	wrapped := Wrap(driver, KindConflict, "category.name_duplicated", Params{"Name": "餐饮"})

	if KindOf(wrapped) != KindConflict {
		t.Errorf("档位应为 KindConflict，实际 %v", KindOf(wrapped))
	}

	// ① errors.As 取回驱动的具体类型——上层据此读 driver 的错误码。
	var target *driverErr
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As 未能取回底层驱动错误类型")
	}
	if target.code != 2067 {
		t.Errorf("底层错误码应为 2067，实际 %d", target.code)
	}

	// ② errors.Is 命中原实例（哨兵错误判定的基础）。
	if !errors.Is(wrapped, driver) {
		t.Error("errors.Is 未能命中底层错误实例")
	}

	// ③ pkg/errors.Cause 回到根因——go_common 生态的既有判定方式。
	if errors.Cause(wrapped) != driver {
		t.Errorf("errors.Cause 未回到根因，实际 %v", errors.Cause(wrapped))
	}

	// ④ Unwrap/Cause 两个方法都要可用：标准库认 Unwrap，pkg/errors 认 Cause。
	// Wrap 返回 error 接口（见其注释），取方法前先经 From 拿到 *Error。
	target2, ok := From(wrapped)
	if !ok {
		t.Fatal("From 未能取回 *Error")
	}
	if target2.Unwrap() == nil || target2.Cause() == nil {
		t.Error("Unwrap 与 Cause 都应返回非 nil")
	}
}

// TestWrapWithSentinelError 锁死标准库哨兵错误的穿透，
// 例如 store/sqlite 把 sql.ErrNoRows 包成业务错误后，上层仍可 Is 判定。
func TestWrapWithSentinelError(t *testing.T) {
	wrapped := Wrap(sql.ErrNoRows, KindInvalidInput, "expense.not_found", nil)

	if !errors.Is(wrapped, sql.ErrNoRows) {
		t.Error("包装后应仍能 errors.Is 命中 sql.ErrNoRows")
	}
	if KindOf(wrapped) != KindInvalidInput {
		t.Errorf("档位应为 KindInvalidInput，实际 %v", KindOf(wrapped))
	}
}

// TestWrapNilReturnsNil 锁死 Wrap(nil) 返回 nil（方案取舍 6）。
//
// 这条保证 `return errs.Wrap(err, ...)` 在 err 为 nil 时天然返回 nil。
// 第二段是关键：Wrap 的返回类型必须是 error 而非 *Error，否则会把一个「类型化的
// nil 指针」装进接口，使调用方的 `if err != nil` 恒为真——本用例正是为拦住这一点
// 而写，实测曾捕获该缺陷。
func TestWrapNilReturnsNil(t *testing.T) {
	if got := Wrap(nil, KindInternal, "x.y", nil); got != nil {
		t.Errorf("Wrap(nil) 应返回 nil，实际 %v", got)
	}

	var err error = Wrap(nil, KindInternal, "x.y", nil)
	if err != nil {
		t.Error("Wrap(nil) 赋给 error 接口后应仍为 nil")
	}

	// 回放真实调用形态：仓储层「执行成功 → 包装 → 返回」这条路径。
	// 成功时必须一路 nil 到底，否则上层会把成功当失败、进而回滚事务。
	save := func(fail bool) error {
		var cause error
		if fail {
			cause = errors.New("写库失败")
		}
		return Wrap(cause, KindInternal, "store.save_failed", nil)
	}
	if got := save(false); got != nil {
		t.Errorf("成功路径应返回 nil，实际 %v", got)
	}
	if got := save(true); got == nil {
		t.Error("失败路径应返回非 nil")
	}
}

// TestWrapAddsStackOnce 锁死栈只补一次（方案取舍 4）。
//
// cause 无栈时补一次，保证 %+v 能定位；cause 已有栈（来自 pkg/errors）时不再补，
// 否则每层包装各捕一次栈，%+v 打出多份几乎相同的栈反而看不清。
func TestWrapAddsStackOnce(t *testing.T) {
	// ① cause 无栈 → 补栈。
	plain := &driverErr{msg: "无栈的底层错误"}
	if hasStack(plain) {
		t.Fatal("前置条件不成立：plain 不应带栈")
	}
	wrapped := Wrap(plain, KindInternal, "store.query_failed", nil)
	if !hasStack(wrapped) {
		t.Error("cause 无栈时应补一次栈")
	}

	// ② cause 已有栈 → 不重复补：栈帧数量应与原栈一致。
	withStack := errors.WithStack(plain)
	var origin interface{ StackTrace() errors.StackTrace }
	if !errors.As(withStack, &origin) {
		t.Fatal("前置条件不成立：withStack 应带栈")
	}
	originDepth := len(origin.StackTrace())

	rewrapped := Wrap(withStack, KindInternal, "store.query_failed", nil)
	var got interface{ StackTrace() errors.StackTrace }
	if !errors.As(rewrapped, &got) {
		t.Fatal("包装后应仍能取到栈")
	}
	if len(got.StackTrace()) != originDepth {
		t.Errorf("栈被重复捕获：原深度 %d，现深度 %d", originDepth, len(got.StackTrace()))
	}
}

// ============================================================
// 五、占位参数的不可变性（方案取舍 3 的两条防御）
// 错误会同时流向日志与渲染两条路径，共享 map 会造成一处改动另一处受影响。
// ============================================================

// TestParamsClonedOnConstruct 锁死构造时深拷贝：
// 调用方后续复用/改写原 map 不得影响已产生的错误。
func TestParamsClonedOnConstruct(t *testing.T) {
	origin := Params{"Line": 12, "Field": "金额"}
	err := NewInvalidInput("parser.field_missing", origin)

	origin["Line"] = 999
	origin["Extra"] = "新增"
	delete(origin, "Field")

	got := err.Params()
	if got["Line"] != 12 {
		t.Errorf("Line 应保持 12，实际 %v", got["Line"])
	}
	if got["Field"] != "金额" {
		t.Errorf("Field 应保持 金额，实际 %v", got["Field"])
	}
	if _, ok := got["Extra"]; ok {
		t.Error("原 map 新增的键不应出现在错误中")
	}
}

// TestParamsClonedOnRead 锁死读出时也拷贝：
// middleware 渲染前往副本里补字段，不得回写进错误对象。
func TestParamsClonedOnRead(t *testing.T) {
	err := NewInvalidInput("parser.field_missing", Params{"Line": 12})

	first := err.Params()
	first["Line"] = 999
	first["Injected"] = true

	second := err.Params()
	if second["Line"] != 12 {
		t.Errorf("Line 应保持 12，实际 %v", second["Line"])
	}
	if _, ok := second["Injected"]; ok {
		t.Error("对读出副本的改动回写进了错误对象")
	}
}

// TestParamsEmptyIsNil 锁死无参数时统一返回 nil，
// 使调用方只需判 len()==0 一种形态，不必区分 nil 与空 map。
func TestParamsEmptyIsNil(t *testing.T) {
	for _, p := range []Params{nil, {}} {
		err := NewInvalidInput("parser.format_mismatch", p)
		if err.Params() != nil {
			t.Errorf("入参 %v 时 Params() 应为 nil", p)
		}
		if ParamsOf(err) != nil {
			t.Errorf("入参 %v 时 ParamsOf 应为 nil", p)
		}
	}
}

// ============================================================
// 六、Error() / Format() 的输出形态
// ============================================================

// TestErrorStringIsStable 锁死占位参数按键名排序输出。
//
// map 遍历顺序随机，不排序会让同一个错误每次打出不同字符串，日志检索与断言都
// 无从下手。多次调用必须逐字节相同，且键名有序。
func TestErrorStringIsStable(t *testing.T) {
	err := NewInvalidInput("parser.field_missing", Params{
		"Line": 12, "Field": "金额", "Amount": "12.30", "Bank": "招商银行",
	})

	first := err.Error()
	for i := 0; i < 20; i++ {
		if got := err.Error(); got != first {
			t.Fatalf("第 %d 次输出与首次不一致：\n%s\n%s", i, first, got)
		}
	}

	// 键名须按字典序：Amount < Bank < Field < Line。
	inner := first[strings.Index(first, "{")+1 : strings.LastIndex(first, "}")]
	var keys []string
	for _, kv := range strings.Split(inner, ", ") {
		keys = append(keys, strings.SplitN(kv, "=", 2)[0])
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Errorf("占位参数未按键名排序：%v", keys)
			break
		}
	}
}

// TestErrorStringContainsKindAndKey 锁死日志形态含中文档位名与文案键，
// 且 cause 的信息被拼进来（排错时一行即可看到全链路）。
func TestErrorStringContainsKindAndKey(t *testing.T) {
	err := Wrap(errors.New("底层失败"), KindConflict, "category.name_duplicated", Params{"Name": "餐饮"})
	got := err.Error()

	for _, want := range []string{"唯一冲突", "category.name_duplicated", "Name=餐饮", "底层失败"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() 应含 %q，实际 %q", want, got)
		}
	}
}

// TestErrorStringWithoutCause 锁死无 cause 时不出现多余的分隔符。
func TestErrorStringWithoutCause(t *testing.T) {
	got := NewUnauthorized("auth.token_expired", nil).Error()
	want := "[未登录] auth.token_expired"
	if got != want {
		t.Errorf("应为 %q，实际 %q", want, got)
	}
}

// TestFormatVerbs 锁死 %v / %s / %q 走单行，%+v 追加 cause 的调用栈。
//
// 分工依据方案取舍 4：单行形态适合作为 logrus 字段，%+v 用于排错。
func TestFormatVerbs(t *testing.T) {
	err := Wrap(errors.New("底层失败"), KindInternal, "store.query_failed", nil)
	line := err.Error()

	if got := fmt.Sprintf("%v", err); got != line {
		t.Errorf("%%v 应为单行 %q，实际 %q", line, got)
	}
	if got := fmt.Sprintf("%s", err); got != line {
		t.Errorf("%%s 应为单行 %q，实际 %q", line, got)
	}
	if got := fmt.Sprintf("%q", err); got != fmt.Sprintf("%q", line) {
		t.Errorf("%%q 应为带引号的单行，实际 %q", got)
	}

	// %+v 必须比单行长，且含本文件名——栈已透传。
	plus := fmt.Sprintf("%+v", err)
	if len(plus) <= len(line) {
		t.Errorf("%%+v 应追加栈信息，实际 %q", plus)
	}
	if !strings.Contains(plus, "errs_test.go") {
		t.Errorf("%%+v 应含调用栈文件名，实际 %q", plus)
	}
	if !strings.HasPrefix(plus, line) {
		t.Errorf("%%+v 应以单行描述开头，实际 %q", plus)
	}
}

// TestFormatWithoutCauseHasNoStack 锁死无 cause 的错误 %+v 不打栈——
// 预期内的业务错误不该在日志里刷一屏栈。
func TestFormatWithoutCauseHasNoStack(t *testing.T) {
	err := NewInvalidInput("parser.format_mismatch", nil)
	if got := fmt.Sprintf("%+v", err); got != err.Error() {
		t.Errorf("无 cause 时 %%+v 应等于单行，实际 %q", got)
	}
}

// ============================================================
// 七、nil 接收者的边界
// 错误路径上的方法若自身 panic，会把原始错误彻底盖掉，排查时只剩 panic 栈。
// ============================================================

// TestNilReceiverSafe 锁死 nil 接收者调用全部方法都不 panic 且返回零值。
func TestNilReceiverSafe(t *testing.T) {
	var err *Error

	if got := err.Error(); got != "" {
		t.Errorf("nil.Error() 应为空串，实际 %q", got)
	}
	if got := err.Kind(); got != KindUnknown {
		t.Errorf("nil.Kind() 应为 KindUnknown，实际 %v", got)
	}
	if got := err.MsgKey(); got != "" {
		t.Errorf("nil.MsgKey() 应为空串，实际 %q", got)
	}
	if got := err.Params(); got != nil {
		t.Errorf("nil.Params() 应为 nil，实际 %v", got)
	}
	if got := err.Unwrap(); got != nil {
		t.Errorf("nil.Unwrap() 应为 nil，实际 %v", got)
	}
	if got := err.Cause(); got != nil {
		t.Errorf("nil.Cause() 应为 nil，实际 %v", got)
	}
	// Format 经 fmt 调用，不得 panic。
	if got := fmt.Sprintf("%v|%+v", err, err); got != "|" {
		t.Errorf("nil 的格式化输出应为空，实际 %q", got)
	}
}

// TestTypedNilInInterface 锁死「类型化的 nil 指针」不会让提取函数 panic。
//
// 这是本包最容易被下游踩到的一处：构造函数返回的是 *Error，于是 service 层很自然
// 会写成 `func Svc() *Error`，而 handler 侧接的是 error 接口。成功路径返回的 nil
// 指针一旦装进接口，接口值就**非 nil**（携带了类型信息），`err != nil` 判空拦不住，
// 随后 errors.As 匹配成功并把 nil 写进 target——照直返回就会在解引用时 panic。
//
// 后果的方向和直觉相反：崩的不是出错的那条路，而是**没有错误的成功路径**，且崩在
// middleware 入口。故此处锁死两件事：一是全部提取函数都不 panic，二是 From 不把
// typed-nil 报成「找到了」。
func TestTypedNilInInterface(t *testing.T) {
	var typedNil *Error
	var err error = typedNil //装进接口后 err != nil

	if err == nil {
		t.Fatal("前提不成立：typed-nil 装进接口后应当非 nil，用例失去意义")
	}

	// 四个提取函数都不得 panic。逐个跑，panic 时能直接看出是哪一个。
	for _, c := range []struct {
		name string
		call func()
	}{
		{"KindOf", func() { KindOf(err) }},
		{"MsgKeyOf", func() { MsgKeyOf(err) }},
		{"ParamsOf", func() { ParamsOf(err) }},
		{"IsKind", func() { IsKind(err, KindInternal) }},
		{"From", func() { From(err) }},
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s 遇 typed-nil 发生 panic: %v", c.name, r)
				}
			}()
			c.call()
		}()
	}

	// From 必须按「未找到」处理：ok 为 true 时第一个返回值必然非 nil 是本包对外的承诺。
	if got, ok := From(err); ok || got != nil {
		t.Errorf("From(typed-nil) 应返回 (nil, false)，实际 (%v, %v)", got, ok)
	}

	// 不携带档位的错误一律归系统错误档，与未分档错误同一条兜底规则。
	if got := KindOf(err); got != KindInternal {
		t.Errorf("KindOf(typed-nil) 应为 KindInternal，实际 %v", got)
	}
	if got := MsgKeyOf(err); got != KeyInternal {
		t.Errorf("MsgKeyOf(typed-nil) 应为 %q，实际 %q", KeyInternal, got)
	}
	if got := ParamsOf(err); got != nil {
		t.Errorf("ParamsOf(typed-nil) 应为 nil，实际 %v", got)
	}
	if !IsKind(err, KindInternal) {
		t.Error("IsKind(typed-nil, KindInternal) 应为 true")
	}
}

// TestTypedNilConsumerShape 回放下游真实链路：service 返回 *Error，handler 收 error。
//
// 修复前，失败路径正常而**成功路径**在 middleware 入口 panic。这里把两条路都跑一遍。
func TestTypedNilConsumerShape(t *testing.T) {
	//service 层的典型签名：返回具体类型 *Error 而非 error 接口
	service := func(fail bool) *Error {
		if fail {
			return NewConflict("account.username_exists", Params{"Username": "admin"})
		}
		return nil
	}
	//handler / middleware 层：统一按 error 接口处理
	handler := func(fail bool) (kind Kind, panicked any) {
		defer func() { panicked = recover() }()
		var err error = service(fail)
		if err == nil {
			return KindUnknown, nil
		}
		return KindOf(err), nil
	}

	if kind, panicked := handler(true); panicked != nil || kind != KindConflict {
		t.Errorf("失败路径应得 KindConflict 且不 panic，实际 kind=%v panic=%v", kind, panicked)
	}
	//成功路径：修复前这里 panic
	if kind, panicked := handler(false); panicked != nil {
		t.Errorf("成功路径不应 panic，实际 %v", panicked)
	} else if kind != KindInternal {
		t.Errorf("成功路径的 typed-nil 应归 KindInternal，实际 %v", kind)
	}
}

// ============================================================
// 八、下游消费方的调用形态回放
// 用例即契约：把三个消费方的真实用法跑一遍，形状变了这里先失败。
// ============================================================

// TestMiddlewareConsumerShape 回放 middleware 的用法：
// 从任意 error 读出三件套 → 按五档映射 HTTP 状态 → 交 i18n 渲染。
//
// 映射表中 409/403/401 三条是分层方案原文；400/500 两条是据 HTTP 语义补的推论，
// 以 middleware 那一轮为准（见 answer §六）。
func TestMiddlewareConsumerShape(t *testing.T) {
	status := map[Kind]int{
		KindInvalidInput: 400,
		KindConflict:     409,
		KindForbidden:    403,
		KindUnauthorized: 401,
		KindInternal:     500,
	}

	// 映射表必须穷尽五档：本包增删档位时这里立刻失败。
	for _, k := range Kinds() {
		if _, ok := status[k]; !ok {
			t.Fatalf("档位 %v 未配置 HTTP 状态映射", k)
		}
	}

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantKey    string
	}{
		{"解析错误", NewInvalidInput("parser.field_missing", Params{"Line": 12}), 400, "parser.field_missing"},
		{"重名", NewConflict("category.name_duplicated", Params{"Name": "餐饮"}), 409, "category.name_duplicated"},
		{"越权", NewForbidden("expense.not_owner", nil), 403, "expense.not_owner"},
		{"令牌过期", NewUnauthorized("auth.token_expired", nil), 401, "auth.token_expired"},
		{"被包装的越权", errors.Wrap(NewForbidden("file.not_owner", nil), "handler"), 403, "file.not_owner"},
		{"未分档错误", errors.New("no such table"), 500, KeyInternal},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind := KindOf(c.err)
			if got := status[kind]; got != c.wantStatus {
				t.Errorf("HTTP 状态应为 %d，实际 %d（档位 %v）", c.wantStatus, got, kind)
			}
			if got := MsgKeyOf(c.err); got != c.wantKey {
				t.Errorf("文案键应为 %q，实际 %q", c.wantKey, got)
			}
			// 三件套齐备即可交 i18n：键非空是渲染的前提。
			if MsgKeyOf(c.err) == "" {
				t.Error("文案键为空，i18n 无法取词")
			}
		})
	}
}

// TestParserConsumerShape 回放 parser 的用法（D-3 五类解析错误）：
// 无 cause 构造带文案键的错误，五类共用同一档但键各不同。
func TestParserConsumerShape(t *testing.T) {
	// 五类错误的键由 parser 包自行声明（约定 5），此处仅模拟其形态。
	keys := []string{
		"parser.format_mismatch", // 格式不符
		"parser.content_corrupt", // 内容损坏
		"parser.field_missing",   // 字段缺失
		"parser.decrypt_failed",  // 解密失败
		"parser.type_mismatch",   // 类型不匹配
	}

	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		err := NewInvalidInput(key, Params{"Line": 3})
		if err.Kind() != KindInvalidInput {
			t.Errorf("%s 应为用户输入错误档，实际 %v", key, err.Kind())
		}
		if err.MsgKey() != key {
			t.Errorf("文案键应为 %q，实际 %q", key, err.MsgKey())
		}
		if seen[err.MsgKey()] {
			t.Errorf("文案键 %q 重复", key)
		}
		seen[err.MsgKey()] = true
	}
}

// TestServiceConsumerShape 回放 service 层按档位分流的用法：
// service/category 判断 store 返回的是否唯一冲突档，从而给出友好提示。
func TestServiceConsumerShape(t *testing.T) {
	fromStore := Wrap(
		&driverErr{code: 2067, msg: "UNIQUE constraint failed"},
		KindConflict, "category.name_duplicated", Params{"Name": "餐饮"},
	)

	if !IsKind(fromStore, KindConflict) {
		t.Fatal("应识别为唯一冲突档")
	}
	if IsKind(fromStore, KindInternal) {
		t.Error("唯一冲突不应同时匹配系统错误档")
	}

	// 服务层可继续包装并附加上下文，档位与键不变。
	wrapped := errors.Wrap(fromStore, "service/category 建类型失败")
	if !IsKind(wrapped, KindConflict) {
		t.Error("包装后应仍为唯一冲突档")
	}
	if MsgKeyOf(wrapped) != "category.name_duplicated" {
		t.Errorf("包装后文案键应不变，实际 %q", MsgKeyOf(wrapped))
	}
}

// TestErrorSatisfiesErrorInterface 锁死 *Error 实现 error 与 fmt.Formatter，
// 使其可直接作为函数返回值与 %+v 的目标。
func TestErrorSatisfiesErrorInterface(t *testing.T) {
	var _ error = (*Error)(nil)
	var _ fmt.Formatter = (*Error)(nil)

	// 作为 error 返回时可被标准库 errors.As 取回。
	produce := func() error { return NewForbidden("expense.not_owner", nil) }
	var target *Error
	if !errors.As(produce(), &target) {
		t.Fatal("*Error 作为 error 返回后应可被 errors.As 取回")
	}
	if target.Kind() != KindForbidden {
		t.Errorf("档位应为 KindForbidden，实际 %v", target.Kind())
	}
}
