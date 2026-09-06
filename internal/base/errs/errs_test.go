package errs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pkg/errors"
)

// TestKindCodeAndName 校验五档的错误码与名称。
// 依据：`doc/decisions/仓库代码包的层级设计与划分.md` §六 L0 `base/errs` 行的五档顺序。
func TestKindCodeAndName(t *testing.T) {
	cases := []struct {
		kind Kind
		code int
		name string
	}{
		{KindInput, 1, "用户输入错误"},
		{KindConflict, 2, "唯一冲突"},
		{KindForbidden, 3, "归属越权"},
		{KindUnauthorized, 4, "未登录"},
		{KindSystem, 5, "系统错误"},
	}
	for _, item := range cases {
		if item.kind.Code() != item.code {
			t.Errorf("档位%s，错误码期望%d，实际%d", item.name, item.code, item.kind.Code())
		}
		if item.kind.String() != item.name {
			t.Errorf("档位码%d，名称期望%s，实际%s", item.code, item.name, item.kind.String())
		}
		if !item.kind.Valid() {
			t.Errorf("档位%s，Valid期望true，实际false", item.name)
		}
	}
	// 五档必须两两不同码，否则 middleware 的状态映射会撞车。
	seen := make(map[int]Kind, len(cases))
	for _, item := range cases {
		if before, ok := seen[item.code]; ok {
			t.Errorf("错误码%d重复：%s与%s", item.code, before, item.kind)
		}
		seen[item.code] = item.kind
	}
	// 未知档位不得冒充五档。
	var unknown Kind = 200
	if unknown.Valid() {
		t.Error("未知档位，Valid期望false，实际true")
	}
	if unknown.String() != "未知档位" {
		t.Errorf("未知档位，名称期望「未知档位」，实际%s", unknown.String())
	}
	// 零值档位同样不合法——KindOf(nil) 返回它，用于表达「无错误」。
	if Kind(0).Valid() {
		t.Error("零值档位，Valid期望false，实际true")
	}
}

// TestNewCarriesKeyAndArgs 校验「每个错误携带文案键与占位参数」，
// 且错误对象内不写死面向用户的文案。
func TestNewCarriesKeyAndArgs(t *testing.T) {
	err := NewConflict(KeyCategoryNameDuplicated, A(ArgName, "餐饮"))

	if err.Kind() != KindConflict {
		t.Errorf("档位期望唯一冲突，实际%s", err.Kind())
	}
	if err.Code() != KindConflict.Code() {
		t.Errorf("错误码期望%d，实际%d", KindConflict.Code(), err.Code())
	}
	if err.Key() != KeyCategoryNameDuplicated {
		t.Errorf("文案键期望%s，实际%s", KeyCategoryNameDuplicated, err.Key())
	}
	args := err.Args()
	if len(args) != 1 || args[ArgName] != "餐饮" {
		t.Errorf("占位参数期望{%s:餐饮}，实际%v", ArgName, args)
	}
}

// TestArgsReturnsCopy 校验 Args() 返回副本——调用方改返回值不得污染错误对象。
func TestArgsReturnsCopy(t *testing.T) {
	err := NewInput(KeyFieldInvalid, A(ArgField, "支出金额"))

	args := err.Args()
	args[ArgField] = "被篡改"
	args["注入"] = "非法"

	again := err.Args()
	if again[ArgField] != "支出金额" {
		t.Errorf("占位参数被外部篡改，期望「支出金额」，实际%v", again[ArgField])
	}
	if len(again) != 1 {
		t.Errorf("占位参数数量期望1，实际%d：%v", len(again), again)
	}
}

// TestArgsEmptyIsNil 校验「无占位参数即 nil」的一致语义，
// 避免 i18n 侧需要同时处理 nil 与空 map 两种形态。
func TestArgsEmptyIsNil(t *testing.T) {
	if args := NewInput(KeyFieldRequired).Args(); args != nil {
		t.Errorf("无占位参数时期望nil，实际%v", args)
	}
	// 空名参数无法在文案模板里索引，必须丢弃。
	if args := NewInput(KeyFieldRequired, A("", "值")).Args(); args != nil {
		t.Errorf("空名占位参数应丢弃，期望nil，实际%v", args)
	}
	// 混合场景：空名丢弃、有名保留。
	args := NewInput(KeyFieldRequired, A("", "丢弃"), A(ArgField, "支出日期")).Args()
	if len(args) != 1 || args[ArgField] != "支出日期" {
		t.Errorf("占位参数期望{%s:支出日期}，实际%v", ArgField, args)
	}
}

// TestArgsLastWins 校验同名占位参数后者覆盖前者，行为确定。
func TestArgsLastWins(t *testing.T) {
	args := NewInput(KeyFieldInvalid, A(ArgField, "旧"), A(ArgField, "新")).Args()
	if args[ArgField] != "新" {
		t.Errorf("同名占位参数期望后者覆盖，期望「新」，实际%v", args[ArgField])
	}
}

// TestNoUserFacingTextInError 校验错误对象内不写死面向用户的文案：
// Error() 只应出现档位名、文案键与占位参数值，不得出现完整的中文提示句。
// 这是 K-4「新增语种只需增加配置、无需改动代码」的前提。
func TestNoUserFacingTextInError(t *testing.T) {
	err := NewInput(KeyPasswordTooShort, A(ArgMin, 10))

	text := err.Error()
	if !strings.Contains(text, KeyPasswordTooShort) {
		t.Errorf("诊断信息应含文案键%s，实际%s", KeyPasswordTooShort, text)
	}
	// 若把用户文案写进了错误对象，这类句子就会出现在 Error() 里。
	for _, forbidden := range []string{"密码长度", "至少", "请输入"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("诊断信息不应含面向用户的文案「%s」，实际%s", forbidden, text)
		}
	}
}

// TestWrapKeepsCause 校验 Wrap 保留底层原因，且 errors.Is 可穿透到原始哨兵错误。
func TestWrapKeepsCause(t *testing.T) {
	cause := errors.New("SQLITE_CONSTRAINT_UNIQUE")
	err := Wrap(cause, KindConflict, KeyUserNameDuplicated, A(ArgName, "admin"))

	if !errors.Is(err, cause) {
		t.Error("errors.Is期望能穿透到底层原因，实际不能")
	}
	if errors.Unwrap(err) == nil {
		t.Error("Unwrap期望非nil")
	}
	if err.Kind() != KindConflict {
		t.Errorf("档位期望唯一冲突，实际%s", err.Kind())
	}
	if !strings.Contains(err.Error(), "SQLITE_CONSTRAINT_UNIQUE") {
		t.Errorf("诊断信息应含底层原因，实际%s", err.Error())
	}
}

// TestWrapNilCause 校验 cause 为 nil 时退化为无 cause 的错误，不产生空包装层。
func TestWrapNilCause(t *testing.T) {
	err := Wrap(nil, KindInput, KeyFieldRequired, A(ArgField, "支出币种"))

	if err.Unwrap() != nil {
		t.Errorf("cause为nil时Unwrap期望nil，实际%v", err.Unwrap())
	}
	if err.Kind() != KindInput || err.Key() != KeyFieldRequired {
		t.Errorf("档位与文案键期望(用户输入错误,%s)，实际(%s,%s)", KeyFieldRequired, err.Kind(), err.Key())
	}
}

// TestIsMatchesByKind 校验按档位匹配：同档位为真、异档位为假。
// 档位是 middleware 状态映射与 service 分支的唯一判据，文案键不参与匹配。
func TestIsMatchesByKind(t *testing.T) {
	err := NewForbidden(KeyAuthForbidden)

	// 同档位、不同文案键 → 匹配成功。
	if !errors.Is(err, NewForbidden("other.key")) {
		t.Error("同档位期望匹配成功，实际失败")
	}
	// 异档位 → 匹配失败。
	if errors.Is(err, NewInput(KeyAuthForbidden)) {
		t.Error("异档位期望匹配失败，实际成功")
	}
}

// TestIsThroughWrappedChain 校验档位判定能穿透 pkg/errors 的包装层
// ——service 层用 errors.Wrap 加上下文后，middleware 仍要能取到档位。
func TestIsThroughWrappedChain(t *testing.T) {
	base := NewUnauthorized(KeyAuthSessionExpired)
	wrapped := errors.Wrap(errors.WithStack(base), "校验登录态，异常")

	if !IsUnauthorized(wrapped) {
		t.Error("包装后期望仍判定为未登录，实际不是")
	}
	if KindOf(wrapped) != KindUnauthorized {
		t.Errorf("包装后档位期望未登录，实际%s", KindOf(wrapped))
	}
	// 归一化后文案键与占位参数不得丢失，否则 i18n 取词失败。
	if From(wrapped).Key() != KeyAuthSessionExpired {
		t.Errorf("包装后文案键期望%s，实际%s", KeyAuthSessionExpired, From(wrapped).Key())
	}
}

// TestKindHelpers 校验五档快捷判定函数两两互斥，不存在一个错误同时命中两档。
func TestKindHelpers(t *testing.T) {
	cases := []struct {
		name string
		err  *Error
		hit  func(error) bool
	}{
		{"用户输入错误", NewInput(KeyFieldInvalid), IsInput},
		{"唯一冲突", NewConflict(KeyUserNameDuplicated), IsConflict},
		{"归属越权", NewForbidden(KeyAuthForbidden), IsForbidden},
		{"未登录", NewUnauthorized(KeyAuthNotLogin), IsUnauthorized},
		{"系统错误", NewSystem(KeySystemInternal), IsSystem},
	}
	all := []func(error) bool{IsInput, IsConflict, IsForbidden, IsUnauthorized, IsSystem}
	for _, item := range cases {
		if !item.hit(item.err) {
			t.Errorf("%s，自身档位判定期望true，实际false", item.name)
		}
		hits := 0
		for _, fn := range all {
			if fn(item.err) {
				hits++
			}
		}
		if hits != 1 {
			t.Errorf("%s，期望只命中1档，实际命中%d档", item.name, hits)
		}
	}
}

// TestFromNil 校验 nil 的归一化结果为 nil，不产生「无错误却有错误对象」的假阳性。
func TestFromNil(t *testing.T) {
	if got := From(nil); got != nil {
		t.Errorf("From(nil)期望nil，实际%v", got)
	}
	if kind := KindOf(nil); kind.Valid() {
		t.Errorf("KindOf(nil)期望零值档位，实际%s", kind)
	}
	for _, fn := range []func(error) bool{IsInput, IsConflict, IsForbidden, IsUnauthorized, IsSystem} {
		if fn(nil) {
			t.Error("nil错误的档位判定期望false，实际true")
		}
	}
}

// TestFromForeignError 校验非本包错误归入系统错误并挂上兜底文案键
// ——middleware 拿到的错误一定有键可取词，不会出现「无键可取词」的出口。
func TestFromForeignError(t *testing.T) {
	foreign := fmt.Errorf("第三方库异常")
	got := From(foreign)

	if got.Kind() != KindSystem {
		t.Errorf("非本包错误档位期望系统错误，实际%s", got.Kind())
	}
	if got.Key() != KeySystemInternal {
		t.Errorf("非本包错误文案键期望%s，实际%s", KeySystemInternal, got.Key())
	}
	if !errors.Is(got, foreign) {
		t.Error("归一化后期望仍能穿透到原错误，实际不能")
	}
}

// TestFromKeepsOuterMost 校验链上多个 *Error 时取最外层
// ——上层重新定档位后，出口应以上层的判定为准。
func TestFromKeepsOuterMost(t *testing.T) {
	inner := NewConflict(KeyUserNameDuplicated, A(ArgName, "admin"))
	outer := Wrap(inner, KindInput, KeyFieldInvalid, A(ArgField, "用户名"))

	got := From(outer)
	if got.Kind() != KindInput {
		t.Errorf("档位期望取最外层的用户输入错误，实际%s", got.Kind())
	}
	if got.Key() != KeyFieldInvalid {
		t.Errorf("文案键期望取最外层的%s，实际%s", KeyFieldInvalid, got.Key())
	}
	// 内层错误仍在链上，日志可见。
	if !errors.Is(outer, inner) {
		t.Error("期望内层错误仍在链上，实际不在")
	}
}

// TestFromIdempotent 校验对本包错误归一化是幂等的，不会层层套壳。
func TestFromIdempotent(t *testing.T) {
	err := NewInput(KeyFieldRequired, A(ArgField, "摊分月数"))

	if From(err) != err {
		t.Error("对本包错误归一化期望返回原对象，实际返回了新对象")
	}
	if From(From(err)) != err {
		t.Error("归一化期望幂等，实际不幂等")
	}
}

// TestNormalizeUnknownKind 校验非五档之一的档位在入口处收敛为系统错误
// ——落到未知档位会让 middleware 的状态映射无从下手。
func TestNormalizeUnknownKind(t *testing.T) {
	if got := New(Kind(200), KeySystemInternal).Kind(); got != KindSystem {
		t.Errorf("未知档位期望收敛为系统错误，实际%s", got)
	}
	if got := New(Kind(0), KeySystemInternal).Kind(); got != KindSystem {
		t.Errorf("零值档位期望收敛为系统错误，实际%s", got)
	}
	if got := Wrap(errors.New("原因"), Kind(200), KeySystemInternal).Kind(); got != KindSystem {
		t.Errorf("Wrap未知档位期望收敛为系统错误，实际%s", got)
	}
}

// TestWithArgs 校验补充占位参数返回新对象、原对象不变（值语义）。
func TestWithArgs(t *testing.T) {
	base := NewConflict(KeyCategoryNameDuplicated)
	filled := WithArgs(base, A(ArgName, "交通"))

	if len(base.Args()) != 0 {
		t.Errorf("原错误占位参数期望为空，实际%v", base.Args())
	}
	if filled.Args()[ArgName] != "交通" {
		t.Errorf("新错误占位参数期望{%s:交通}，实际%v", ArgName, filled.Args())
	}
	if filled.Kind() != base.Kind() || filled.Key() != base.Key() {
		t.Error("补充占位参数不应改变档位与文案键")
	}
	if WithArgs(nil, A(ArgName, "x")) != nil {
		t.Error("WithArgs(nil)期望nil")
	}
}

// TestWithArgsOverrides 校验补充同名占位参数时以新值为准，且保留 cause。
func TestWithArgsOverrides(t *testing.T) {
	cause := errors.New("底层原因")
	base := Wrap(cause, KindInput, KeyFieldInvalid, A(ArgField, "旧"), A(ArgMin, 1))
	filled := WithArgs(base, A(ArgField, "新"))

	if filled.Args()[ArgField] != "新" {
		t.Errorf("同名占位参数期望被覆盖为「新」，实际%v", filled.Args()[ArgField])
	}
	if filled.Args()[ArgMin] != 1 {
		t.Errorf("未被覆盖的占位参数期望保留，实际%v", filled.Args()[ArgMin])
	}
	if !errors.Is(filled, cause) {
		t.Error("补充占位参数后期望保留底层原因，实际丢失")
	}
}

// TestErrorTextFormat 校验诊断信息的格式与占位参数的稳定排序
// ——map 遍历无序，不排序会导致同一错误的日志文本每次不同、无法比对。
func TestErrorTextFormat(t *testing.T) {
	err := NewInput(KeyParserFieldMissing, A(ArgField, "支出日期"), A(ArgCount, 3))

	want := "用户输入错误[" + KeyParserFieldMissing + "]{count=3 field=支出日期}"
	if got := err.Error(); got != want {
		t.Errorf("诊断信息期望%s，实际%s", want, got)
	}
	// 反复取值必须完全一致。
	for i := 0; i < 20; i++ {
		if got := err.Error(); got != want {
			t.Fatalf("第%d次取诊断信息不一致，期望%s，实际%s", i+1, want, got)
		}
	}
}

// TestErrorTextWithoutKeyAndArgs 校验无文案键、无占位参数时的退化格式。
func TestErrorTextWithoutKeyAndArgs(t *testing.T) {
	if got := New(KindSystem, "").Error(); got != "系统错误" {
		t.Errorf("诊断信息期望「系统错误」，实际%s", got)
	}
}

// TestFormatVerbPlusPrintsStack 校验 `%+v` 能打印底层原因的堆栈
// ——go_common/util 全库以 `%+v` 与 logrus 字段打印错误，堆栈必须可见。
func TestFormatVerbPlusPrintsStack(t *testing.T) {
	err := Wrap(errors.New("原始异常"), KindSystem, KeySystemInternal)

	verbose := fmt.Sprintf("%+v", err)
	if !strings.Contains(verbose, "原始异常") {
		t.Errorf("%%+v应含底层原因，实际%s", verbose)
	}
	// 堆栈里必然出现本测试函数名。
	if !strings.Contains(verbose, "TestFormatVerbPlusPrintsStack") {
		t.Errorf("%%+v应含调用栈，实际%s", verbose)
	}
	// %v 与 %s 是紧凑形态，不带堆栈。
	compact := fmt.Sprintf("%v", err)
	if strings.Contains(compact, "TestFormatVerbPlusPrintsStack") {
		t.Errorf("%%v不应含调用栈，实际%s", compact)
	}
	if compact != err.Error() {
		t.Errorf("%%v期望等于Error()，期望%s，实际%s", err.Error(), compact)
	}
	if got := fmt.Sprintf("%s", err); got != err.Error() {
		t.Errorf("%%s期望等于Error()，期望%s，实际%s", err.Error(), got)
	}
	if got := fmt.Sprintf("%q", err); got != `"`+err.Error()+`"` {
		t.Errorf("%%q期望为带引号的Error()，实际%s", got)
	}
}

// TestNewHasNoStackNoise 校验不带 cause 的错误在 `%+v` 下不产生堆栈噪声
// ——用户输入错误是预期内的业务分支，不是异常，打堆栈只会污染日志。
func TestNewHasNoStackNoise(t *testing.T) {
	err := NewInput(KeyFieldRequired, A(ArgField, "支出金额"))

	if got := fmt.Sprintf("%+v", err); got != err.Error() {
		t.Errorf("无cause时%%+v期望等于Error()，期望%s，实际%s", err.Error(), got)
	}
}

// TestNilErrorSafe 校验 nil 接收者不 panic——日志与 middleware 可能在错误为空时调用。
func TestNilErrorSafe(t *testing.T) {
	var err *Error

	if err.Kind() != KindSystem {
		t.Errorf("nil错误档位期望系统错误，实际%s", err.Kind())
	}
	if err.Key() != "" {
		t.Errorf("nil错误文案键期望空，实际%s", err.Key())
	}
	if err.Args() != nil {
		t.Errorf("nil错误占位参数期望nil，实际%v", err.Args())
	}
	if err.Error() != "" {
		t.Errorf("nil错误诊断信息期望空，实际%s", err.Error())
	}
	if err.Unwrap() != nil {
		t.Errorf("nil错误Unwrap期望nil，实际%v", err.Unwrap())
	}
	if !err.Is(nil) {
		t.Error("nil错误与nil比对期望true，实际false")
	}
	if err.Is(NewInput(KeyFieldInvalid)) {
		t.Error("nil错误与非nil错误比对期望false，实际true")
	}
	// nil 接收者走 Format 分支不得 panic。
	_ = fmt.Sprintf("%+v|%v|%s|%q", err, err, err, err)
}

// TestKeysUniqueAndNamespaced 校验文案键两两不同且符合 `<域>.<对象>.<问题>` 命名口径。
// 键重复的后果是两类错误共用一条文案，且编译期无法发现。
func TestKeysUniqueAndNamespaced(t *testing.T) {
	keys := []string{
		KeySystemInternal,
		KeyAuthNotLogin, KeyAuthSessionExpired, KeyAuthSessionRevoked,
		KeyAuthForbidden, KeyAuthAdminRequired, KeyAuthPasswordChangeRequired,
		KeyPasswordTooShort, KeyPasswordCharClassInsufficient,
		KeyAuthCredentialInvalid, KeyAuthAccountDisabled, KeyAuthAccountLocked,
		KeyParserFormatMismatch, KeyParserContentCorrupted, KeyParserFieldMissing,
		KeyParserDecryptFailed, KeyParserTypeMismatch, KeyParserTypeUnknown,
		KeyUserNameDuplicated, KeyCategoryNameDuplicated,
		KeyFxRateRequired, KeyFxRateFetchFailed,
		KeyCurrencyInvalid, KeyCurrencyNotBaseCandidate, KeyLanguageInvalid,
		KeyRecordNotFound, KeyFieldRequired, KeyFieldInvalid,
		KeyCategoryInUse, KeyExpenseDeletedReadonly,
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if key == "" {
			t.Error("文案键不得为空")
			continue
		}
		if seen[key] {
			t.Errorf("文案键重复：%s", key)
		}
		seen[key] = true

		if strings.ContainsAny(key, " \t") || key != strings.ToLower(key) {
			t.Errorf("文案键%s不符合「全小写、无空格」口径", key)
		}
		if !strings.Contains(key, ".") {
			t.Errorf("文案键%s不符合「点分命名」口径", key)
		}
	}
}

// TestArgNamesUnique 校验占位参数名两两不同——重名会让文案模板取到错误的值。
func TestArgNamesUnique(t *testing.T) {
	names := []string{
		ArgName, ArgField, ArgMin, ArgMax, ArgCount,
		ArgCurrency, ArgDate, ArgLanguage, ArgUntil, ArgType,
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" {
			t.Error("占位参数名不得为空")
			continue
		}
		if seen[name] {
			t.Errorf("占位参数名重复：%s", name)
		}
		seen[name] = true
	}
}

// TestD3FiveParserKeys 校验 D-3 五类解析错误各有独立文案键。
// 依据：终版 §二 D-3「格式不符、内容损坏、字段缺失、解密失败、所选解析器类型与文件不匹配」。
func TestD3FiveParserKeys(t *testing.T) {
	five := map[string]string{
		"格式不符":  KeyParserFormatMismatch,
		"内容损坏":  KeyParserContentCorrupted,
		"字段缺失":  KeyParserFieldMissing,
		"解密失败":  KeyParserDecryptFailed,
		"类型不匹配": KeyParserTypeMismatch,
	}
	if len(five) != 5 {
		t.Fatalf("D-3期望5类解析错误，实际%d类", len(five))
	}
	seen := make(map[string]string, len(five))
	for name, key := range five {
		if before, ok := seen[key]; ok {
			t.Errorf("解析错误文案键%s被「%s」与「%s」共用", key, before, name)
		}
		seen[key] = name
		// 五类解析错误都属于用户输入错误档。
		if err := NewInput(key); err.Kind() != KindInput {
			t.Errorf("解析错误「%s」档位期望用户输入错误，实际%s", name, err.Kind())
		}
	}
}
