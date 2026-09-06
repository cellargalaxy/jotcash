package enum

// 包内白盒单测：覆盖 entry / table 的三条不变式、代码规范校验、两条校验路径的公共实现，
// 以及落库 / 序列化的统一口径。
// 外部黑盒的「对外契约是否够用」验证在 zz_consumer_check_test.go（`package enum_test`），
// 与兄弟包 `base/pwdhash` / `base/decimal` / `base/errs` 的约定同构。

import (
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// —— isCanonicalCode：代码规范校验 ——

// TestIsCanonicalCode 逐条覆盖 `[a-z][a-z0-9_]*` 的边界。
// 这条机械校验拦的是「同一枚举项出现两种写法」，而代码是落库值且永不可改（终版 §五 2）。
func TestIsCanonicalCode(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"admin", true},
		{"user", true},
		{"op_type", true},
		{"expense_edit_delete", true},
		{"generic_csv", true},
		{"a", true},
		{"a1", true},
		{"a_1_b", true},
		// 非法：空
		{"", false},
		// 非法：首字符为数字或下划线
		{"1x", false},
		{"_x", false},
		{"_", false},
		{"1", false},
		// 非法：大写（`Admin` 与 `admin` 混写正是本校验要拦的）
		{"Admin", false},
		{"aB", false},
		// 非法：连字符（`system-init` 与 `system_init` 混写）
		{"system-init", false},
		// 非法：其余字符
		{"a.b", false},
		{"a b", false},
		{"a$b", false},
		{"中文", false},
		// 非法：分隔符本身（textKeyOf 用 `.` 拼键，代码含 `.` 会让键空间产生歧义）
		{".", false},
	}
	for _, item := range cases {
		if got := isCanonicalCode(item.code); got != item.want {
			t.Errorf("isCanonicalCode(%q)：期望%v，实际%v", item.code, item.want, got)
		}
	}
}

// —— newTable：三条不变式在包初始化期 panic ——

// TestNewTablePanics 逐条确认三条不变式违反时 panic 而非静默接受。
// 依据 newTable 注释的取舍：枚举表是写死在源码里的常量数据，违反属编码错误。
func TestNewTablePanics(t *testing.T) {
	cases := []struct {
		name string
		call func()
	}{
		{"枚举类型标识不规范", func() { newTable("Role", "角色", entry{code: "a", name: "甲"}) }},
		{"枚举类型标识为空", func() { newTable("", "角色", entry{code: "a", name: "甲"}) }},
		{"枚举类型中文名为空", func() { newTable("role", "", entry{code: "a", name: "甲"}) }},
		{"枚举代码不规范", func() { newTable("role", "角色", entry{code: "A", name: "甲"}) }},
		{"枚举代码为空", func() { newTable("role", "角色", entry{code: "", name: "甲"}) }},
		{"枚举中文名称为空", func() { newTable("role", "角色", entry{code: "a", name: ""}) }},
		{"枚举代码重复", func() {
			newTable("role", "角色", entry{code: "a", name: "甲"}, entry{code: "a", name: "乙"})
		}},
	}
	for _, item := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s：期望 panic，实际未 panic", item.name)
				}
			}()
			item.call()
		}()
	}
}

// TestNewTableKeepsDeclarationOrder 声明顺序即业务顺序（table.codes 注释），
// 11 类操作类型的下拉稳定排序依赖它。
func TestNewTableKeepsDeclarationOrder(t *testing.T) {
	tbl := newTable("demo", "示例",
		entry{code: "z", name: "丙"},
		entry{code: "a", name: "甲"},
		entry{code: "m", name: "乙"},
	)
	want := []string{"z", "a", "m"}
	got := tbl.listCodes()
	if len(got) != len(want) {
		t.Fatalf("代码数量：期望%d，实际%d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第%d项：期望%q，实际%q（声明顺序被打乱）", i, want[i], got[i])
		}
	}
}

// TestListCodesReturnsCopy listCodes 必须返回副本——调用方就地排序不得污染枚举表。
// 这是「枚举表构建后只读」的实际保障：一次就地排序会静默破坏声明顺序且此后每次调用都受影响。
func TestListCodesReturnsCopy(t *testing.T) {
	first := opTypeTable.listCodes()
	if len(first) < 2 {
		t.Fatal("操作类型表项数过少，无法验证副本语义")
	}
	// 就地改写调用方拿到的切片
	first[0], first[1] = "zzz", "yyy"
	second := opTypeTable.listCodes()
	if second[0] == "zzz" || second[1] == "yyy" {
		t.Fatal("listCodes 返回的不是副本，调用方改写污染了枚举表")
	}
	if second[0] != string(OpTypeSystemInit) {
		t.Errorf("首项期望%q，实际%q", OpTypeSystemInit, second[0])
	}
}

// —— table 的查表与派生 ——

// TestTableNameOfUnknown 未定义代码返回「未知<枚举中文名>」而非空串。
// 依据 nameOf 注释：日志里出现「角色=」无法区分「取不到名字」与「名字是空的」。
func TestTableNameOfUnknown(t *testing.T) {
	if got := roleTable.nameOf("no_such_code"); got != "未知角色" {
		t.Errorf("未知代码的名称：期望「未知角色」，实际%q", got)
	}
	if got := opTypeTable.nameOf(""); got != "未知操作类型" {
		t.Errorf("空代码的名称：期望「未知操作类型」，实际%q", got)
	}
}

// TestTableTextKeyOf 文案键口径为 `enum.<枚举类型>.<代码>`；未定义代码返回空串。
func TestTableTextKeyOf(t *testing.T) {
	if got := roleTable.textKeyOf(string(RoleAdmin)); got != "enum.role.admin" {
		t.Errorf("角色文案键：期望 enum.role.admin，实际%q", got)
	}
	if got := opTypeTable.textKeyOf(string(OpTypeDataIntake)); got != "enum.op_type.data_intake" {
		t.Errorf("操作类型文案键：期望 enum.op_type.data_intake，实际%q", got)
	}
	// 未定义代码：返回一个查不到词的键只会让 i18n 静默取空
	if got := roleTable.textKeyOf("no_such_code"); got != "" {
		t.Errorf("未定义代码的文案键：期望空串，实际%q", got)
	}
	if got := roleTable.textKeyOf(""); got != "" {
		t.Errorf("空代码的文案键：期望空串，实际%q", got)
	}
}

// TestTextKeyPrefixIsolation 全部七类枚举的文案键一律以 `enum.` 开头，
// 与 `base/errs` 的键空间彻底隔开——两者共用同一份语言文件（分层版 §六 L3 `i18n` 行）。
func TestTextKeyPrefixIsolation(t *testing.T) {
	keys := map[string]struct{}{}
	for _, tbl := range allTables() {
		for _, code := range tbl.codes {
			key := tbl.textKeyOf(code)
			if !strings.HasPrefix(key, textKeyPrefix) {
				t.Errorf("文案键%q未以%q开头", key, textKeyPrefix)
			}
			// 键空间内不得互相侵占
			if _, dup := keys[key]; dup {
				t.Errorf("文案键重复：%q", key)
			}
			keys[key] = struct{}{}
		}
		// 与 errs 的键不得撞车：errs 的键无 `enum.` 前缀
		if strings.HasPrefix(errs.KeySystemInternal, textKeyPrefix) {
			t.Fatal("errs 的键与 enum 前缀撞车，键空间隔离失效")
		}
	}
	if len(keys) == 0 {
		t.Fatal("未收集到任何文案键")
	}
}

// allTables 返回七张枚举表，供跨类型的统一口径校验复用。
func allTables() []*table {
	return []*table{
		roleTable, userStatusTable, fileFormatTable,
		parserTypeTable, opTypeTable, objectTypeTable, opResultTable,
	}
}

// TestAllTablesInvariants 七张表逐张核对三条不变式在**真实枚举数据**上成立
// （newTable 的 panic 只保证构建期，这里是对已构建结果的复核）。
func TestAllTablesInvariants(t *testing.T) {
	for _, tbl := range allTables() {
		if !isCanonicalCode(tbl.kind) {
			t.Errorf("枚举类型标识不规范：%q", tbl.kind)
		}
		if tbl.label == "" {
			t.Errorf("枚举类型中文名为空：kind=%q", tbl.kind)
		}
		if len(tbl.codes) == 0 {
			t.Errorf("枚举表为空：kind=%q", tbl.kind)
		}
		if len(tbl.codes) != len(tbl.entries) {
			t.Errorf("kind=%q：codes 与 entries 项数不符（%d/%d）", tbl.kind, len(tbl.codes), len(tbl.entries))
		}
		seen := map[string]struct{}{}
		for _, code := range tbl.codes {
			if !isCanonicalCode(code) {
				t.Errorf("kind=%q：代码不规范 %q", tbl.kind, code)
			}
			if _, dup := seen[code]; dup {
				t.Errorf("kind=%q：代码重复 %q", tbl.kind, code)
			}
			seen[code] = struct{}{}
			e, ok := tbl.entries[code]
			if !ok {
				t.Errorf("kind=%q：codes 中的 %q 不在 entries 里", tbl.kind, code)
				continue
			}
			if e.name == "" {
				t.Errorf("kind=%q：代码 %q 的中文名称为空", tbl.kind, code)
			}
		}
	}
}

// TestTableKindsUnique 七类枚举的 kind 互不相同——kind 是文案键的中间段，
// 撞车会让两类枚举的文案键互相覆盖。
func TestTableKindsUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for _, tbl := range allTables() {
		if _, dup := seen[tbl.kind]; dup {
			t.Errorf("枚举类型标识重复：%q", tbl.kind)
		}
		seen[tbl.kind] = struct{}{}
	}
	if len(seen) != 7 {
		t.Errorf("枚举类型数：期望 7 类，实际%d", len(seen))
	}
}

// —— parse：收用户输入路径的错误口径 ——

// TestTableParse 命中返回代码本身，未命中返回「用户输入错误」档 + KeyFieldInvalid。
func TestTableParse(t *testing.T) {
	got, err := roleTable.parse(string(RoleAdmin))
	if err != nil {
		t.Fatalf("合法代码 parse 报错：%v", err)
	}
	if got != string(RoleAdmin) {
		t.Errorf("parse 返回：期望%q，实际%q", RoleAdmin, got)
	}

	for _, bad := range []string{"", "Admin", "no_such_code", "admin "} {
		got, err := roleTable.parse(bad)
		if err == nil {
			t.Errorf("非法代码%q：期望报错，实际通过", bad)
			continue
		}
		if got != "" {
			t.Errorf("非法代码%q：期望返回空串，实际%q", bad, got)
		}
		if !errs.IsInput(err) {
			t.Errorf("非法代码%q：期望「用户输入错误」档，实际%v", bad, errs.KindOf(err))
		}
		target := errs.From(err)
		if target.Key() != errs.KeyFieldInvalid {
			t.Errorf("非法代码%q：文案键期望%q，实际%q", bad, errs.KeyFieldInvalid, target.Key())
		}
		// 占位参数为枚举类型中文名；**非法代码本身不进占位参数**（parse 注释）
		if got := target.Args()[errs.ArgField]; got != "角色" {
			t.Errorf("非法代码%q：占位参数%s期望「角色」，实际%v", bad, errs.ArgField, got)
		}
		for _, v := range target.Args() {
			if s, ok := v.(string); ok && s == bad && bad != "" {
				t.Errorf("非法代码%q出现在占位参数中，会被回显给用户", bad)
			}
		}
	}
}

// —— codeFromSrc：读历史数据路径的类型归一化与档位 ——

// TestCodeFromSrc 三条口径：string / []byte 取值、NULL 报错、其余类型报错；
// 档位一律 KindSystem（库内数据或驱动与预期不符，不是用户输入错误）。
func TestCodeFromSrc(t *testing.T) {
	// string 与 []byte 直接取值
	if got, err := codeFromSrc("admin", "角色"); err != nil || got != "admin" {
		t.Errorf("string 入参：期望 admin/nil，实际%q/%v", got, err)
	}
	if got, err := codeFromSrc([]byte("admin"), "角色"); err != nil || got != "admin" {
		t.Errorf("[]byte 入参：期望 admin/nil，实际%q/%v", got, err)
	}
	// 空串照原样返回（零值判定交由 scanCode 按 allowZero 决定）
	if got, err := codeFromSrc("", "角色"); err != nil || got != "" {
		t.Errorf("空串入参：期望 \"\"/nil，实际%q/%v", got, err)
	}

	// NULL 与其余类型：一律 KindSystem
	for _, bad := range []any{nil, 1, int64(1), 1.5, true, struct{}{}} {
		got, err := codeFromSrc(bad, "角色")
		if err == nil {
			t.Errorf("入参%T：期望报错，实际通过", bad)
			continue
		}
		if got != "" {
			t.Errorf("入参%T：期望返回空串，实际%q", bad, got)
		}
		if !errs.IsSystem(err) {
			t.Errorf("入参%T：期望「系统错误」档，实际%v", bad, errs.KindOf(err))
		}
		if key := errs.From(err).Key(); key != errs.KeySystemInternal {
			t.Errorf("入参%T：文案键期望%q，实际%q", bad, errs.KeySystemInternal, key)
		}
	}
}

// TestScanCodeRejectsUnknownStoredCode scanCode 对「本版本不认识的库内代码」报系统错误。
// 与 parse 的档位差异是刻意的（scanCode 注释）：一条被改写过的库内数据若被当成用户输入错误，
// 排查方向完全走偏。
func TestScanCodeRejectsUnknownStoredCode(t *testing.T) {
	// 零值不允许时报错
	if _, err := scanCode("", roleTable, false); err == nil {
		t.Error("零值 + allowZero=false：期望报错")
	} else if !errs.IsSystem(err) {
		t.Errorf("零值：期望「系统错误」档，实际%v", errs.KindOf(err))
	}
	// 零值允许时放行
	if got, err := scanCode("", objectTypeTable, true); err != nil || got != "" {
		t.Errorf("零值 + allowZero=true：期望 \"\"/nil，实际%q/%v", got, err)
	}
	// 未定义代码一律系统错误
	if _, err := scanCode("no_such_code", roleTable, false); err == nil {
		t.Error("未定义代码：期望报错")
	} else if !errs.IsSystem(err) {
		t.Errorf("未定义代码：期望「系统错误」档，实际%v", errs.KindOf(err))
	}
	// allowZero=true 也不放行非零的非法值
	if _, err := scanCode("no_such_code", objectTypeTable, true); err == nil {
		t.Error("未定义代码 + allowZero=true：期望仍报错")
	}
}

// —— valueOf / marshalTextOf：落库与序列化必须同进同退 ——

// TestValueOfAndMarshalTextOfAgree 同一取值在两条出口上的成败必须一致。
// 依据 marshalTextOf 注释：否则会出现「存得下但发不出去」的字段。
func TestValueOfAndMarshalTextOfAgree(t *testing.T) {
	cases := []struct {
		tbl       *table
		code      string
		allowZero bool
	}{
		{roleTable, string(RoleAdmin), false},
		{roleTable, "", false},
		{roleTable, "no_such_code", false},
		{objectTypeTable, string(ObjectTypeUser), true},
		{objectTypeTable, "", true},
		{objectTypeTable, "no_such_code", true},
		{opTypeTable, string(OpTypeDataExport), false},
		{opResultTable, string(OpResultFailure), false},
	}
	for _, item := range cases {
		_, valErr := valueOf(item.tbl, item.code, item.allowZero)
		_, txtErr := marshalTextOf(item.tbl, item.code, item.allowZero)
		if (valErr == nil) != (txtErr == nil) {
			t.Errorf("kind=%q code=%q：落库与序列化口径不一致（%v / %v）",
				item.tbl.kind, item.code, valErr, txtErr)
		}
		if valErr != nil && !errs.IsSystem(valErr) {
			t.Errorf("kind=%q code=%q：落库错误期望「系统错误」档，实际%v",
				item.tbl.kind, item.code, errs.KindOf(valErr))
		}
	}
}

// TestZeroValueErrAndInvalidForOutputErrDiffer 零值与非法值必须是两条可区分的错误。
// 依据 invalidForOutputErr 注释：零值是「漏赋值」，非法值是「赋了个本版本不认识的值」，
// 两者排查方向不同。
func TestZeroValueErrAndInvalidForOutputErrDiffer(t *testing.T) {
	zeroErr := zeroValueErr("角色", "不可落库")
	invalidErr := invalidForOutputErr("角色", "no_such_code", "不可落库")
	if zeroErr.Error() == invalidErr.Error() {
		t.Fatal("零值错误与非法值错误的诊断信息完全相同，无法区分")
	}
	if !strings.Contains(zeroErr.Error(), "零值") {
		t.Errorf("零值错误未提及「零值」：%v", zeroErr)
	}
	if !strings.Contains(invalidErr.Error(), "no_such_code") {
		t.Errorf("非法值错误未带上非法代码：%v", invalidErr)
	}
	// 两者均为系统错误档（上游程序缺陷，不是用户输入问题）
	if !errs.IsSystem(zeroErr) || !errs.IsSystem(invalidErr) {
		t.Error("期望两者均为「系统错误」档")
	}
}
