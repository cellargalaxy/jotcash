package entity

import (
	"reflect"
	"testing"
	"time"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// 本文件把「5 实体 62 属性」这份契约逐字段锁成 golden 表：字段名、字段类型、
// 字段个数、以及 Expense 的三段分组边界。
//
// 为什么值得一整个文件：本包是零方法的纯定义包，它唯一可能出错的方式就是
// 「字段错了」——少一个字段、类型选错、把可空字段写成指针、把只读派生字段
// 混进可编辑组。这些错误**在本包内一律编译通过、无任何征兆**，要到 store
// 建表、rule 计算、dto 裁剪那几轮才爆发，且届时已无从判断是定义错了还是
// 消费方用错了。
//
// 断言到「类型」而非仅「存在」：分层 §六 L1.1 明写「币种、角色、状态、操作类型
// 等字段直接用 L1.0 的枚举类型，金额与汇率用 base/decimal、日期与年月用
// base/calendar，**不用裸 string**」。把 SpendDate 从 calendar.Date 改成
// time.Time 不会有任何编译错误，但前提 7 当场失效——只有类型断言拦得住。

// field 是 golden 表里的一行：字段名 + 期望类型。
type field struct {
	name string
	typ  reflect.Type
}

// 四个类型来源的反射类型，供 golden 表引用。
var (
	typString  = reflect.TypeOf("")
	typInt     = reflect.TypeOf(0)
	typInt64   = reflect.TypeOf(int64(0))
	typBool    = reflect.TypeOf(false)
	typBytes   = reflect.TypeOf([]byte(nil))
	typTime    = reflect.TypeOf(time.Time{})
	typDate    = reflect.TypeOf(calendar.Date{})
	typMonth   = reflect.TypeOf(calendar.Month{})
	typDecimal = reflect.TypeOf(decimal.Decimal{})
	typCode    = reflect.TypeOf(currency.Code{})
)

// userFields 是 User 的 15 项属性（终版 §四 1，全部必填）。
var userFields = []field{
	{"ID", typInt64},
	{"Username", typString},
	{"PasswordHash", typString},
	{"Salt", typString},
	{"Role", reflect.TypeOf(enum.Role(""))},
	{"Status", reflect.TypeOf(enum.UserStatus(""))},
	{"MustChangePassword", typBool},
	{"FailedAttempts", typInt},
	{"LockedUntil", typTime},
	{"TokenBaselineTime", typTime},
	{"BaseCurrency", typCode},
	{"Language", typString},
	{"CreatedAt", typTime},
	{"UpdatedAt", typTime},
	{"LastLoginTime", typTime},
}

// categoryFields 是 ExpenseCategory 的 5 项属性（终版 §四 2，全部必填）。
var categoryFields = []field{
	{"ID", typInt64},
	{"OwnerUserID", typInt64},
	{"Name", typString},
	{"CreatedAt", typTime},
	{"UpdatedAt", typTime},
}

// expenseEditableFields 是 Expense 的用户可编辑 8 项（终版 F-1）。
//
// 这 8 项恰是 api/http/dto 写请求的可写字段全集（分层 §九 前提 6 行：
// 「明细写请求的字段全集**恒等于** F-1 八项」）。
var expenseEditableFields = []field{
	{"SpendDate", typDate},
	{"Amount", typDecimal},
	{"Currency", typCode},
	{"Counterparty", typString},
	{"Remark", typString},
	{"CategoryID", typInt64},
	{"AmortizeMonths", typInt},
	{"Rate", typDecimal},
}

// expenseDerivedFields 是 Expense 的只读派生 4 项（终版 §四 3，全部必填）。
//
// 这 4 项在写请求 dto 里**根本不定义**（前提 6：结构缺席 > 运行时忽略）。
var expenseDerivedFields = []field{
	{"BaseAmount", typDecimal},
	{"BaseCurrency", typCode},
	{"AmortizeStart", typMonth},
	{"AmortizeEnd", typMonth},
}

// expenseSystemFields 是 Expense 的系统只读 10 项（终版 §四 3）。
var expenseSystemFields = []field{
	{"ID", typInt64},
	{"OwnerUserID", typInt64},
	{"SourceAuditID", typInt64},
	{"SourceFileID", typInt64},
	{"BankName", typString},
	{"CardLast4", typString},
	{"CardCurrency", typCode},
	{"DeletedAt", typTime},
	{"CreatedAt", typTime},
	{"UpdatedAt", typTime},
}

// fileFields 是 File 的 11 项属性（终版 §四 4，仅文件密码非必填）。
var fileFields = []field{
	{"ID", typInt64},
	{"OwnerUserID", typInt64},
	{"SourceAuditID", typInt64},
	{"Name", typString},
	{"Format", reflect.TypeOf(enum.FileFormat(""))},
	{"ParserType", reflect.TypeOf(enum.ParserType(""))},
	{"Size", typInt64},
	{"Content", typBytes},
	{"Checksum", typString},
	{"Password", typString},
	{"UploadTime", typTime},
}

// auditFields 是 AuditLog 的 9 项属性（终版 §四 5，全部必填）。
var auditFields = []field{
	{"ID", typInt64},
	{"UserID", typInt64},
	{"Type", reflect.TypeOf(enum.OperationType(""))},
	{"ObjectType", reflect.TypeOf(enum.ObjectType(""))},
	{"ObjectID", typInt64},
	{"Summary", typString},
	{"Changes", typString},
	{"Result", reflect.TypeOf(enum.OperationResult(""))},
	{"Time", typTime},
}

// expenseFields 按「可编辑 8 + 只读派生 4 + 系统只读 10」拼出 Expense 的 22 项，
// 顺序与结构体声明序一致——三段分组即字段顺序，见 Expense 的类型注释。
var expenseFields = func() []field {
	all := make([]field, 0, 22)
	all = append(all, expenseEditableFields...)
	all = append(all, expenseDerivedFields...)
	all = append(all, expenseSystemFields...)
	return all
}()

// checkFields 逐字段核对结构体的字段名、声明序与类型。
func checkFields(t *testing.T, entityName string, target any, want []field) {
	t.Helper()
	typ := reflect.TypeOf(target)
	if typ.Kind() != reflect.Struct {
		t.Fatalf("%s 应为结构体, got %s", entityName, typ.Kind())
	}
	if typ.NumField() != len(want) {
		t.Fatalf("%s 属性数不符（终版 §四 的点算依据）: got %d, want %d",
			entityName, typ.NumField(), len(want))
	}
	for i, w := range want {
		got := typ.Field(i)
		if got.Name != w.name {
			t.Errorf("%s 字段声明序[%d]: got %q, want %q", entityName, i, got.Name, w.name)
			continue
		}
		if got.Type != w.typ {
			t.Errorf("%s.%s 类型不符（分层 §六 L1.1「不用裸 string」）: got %s, want %s",
				entityName, w.name, got.Type, w.typ)
		}
		// 导出性：store 与 service 都在包外，字段必须导出才能读写。
		if !got.IsExported() {
			t.Errorf("%s.%s 未导出，包外无法读写", entityName, w.name)
		}
	}
}

// TestUserFields 锁定 User 的 15 项属性。
func TestUserFields(t *testing.T) {
	checkFields(t, "User", User{}, userFields)
}

// TestExpenseCategoryFields 锁定 ExpenseCategory 的 5 项属性。
func TestExpenseCategoryFields(t *testing.T) {
	checkFields(t, "ExpenseCategory", ExpenseCategory{}, categoryFields)
}

// TestExpenseFields 锁定 Expense 的 22 项属性及其三段分组顺序。
func TestExpenseFields(t *testing.T) {
	checkFields(t, "Expense", Expense{}, expenseFields)
}

// TestFileFields 锁定 File 的 11 项属性。
func TestFileFields(t *testing.T) {
	checkFields(t, "File", File{}, fileFields)
}

// TestAuditLogFields 锁定 AuditLog 的 9 项属性。
func TestAuditLogFields(t *testing.T) {
	checkFields(t, "AuditLog", AuditLog{}, auditFields)
}

// TestAttributeCountThreeWay 复核属性点算的三条独立路径（终版 §十二 校验方式②）。
//
// 三向而非一向：单看总数 62 不足以发现「Expense 多一项、File 少一项」这种互相
// 抵消的错误。终版给了三条互相独立的算法，此处逐条走：
//
//	① 5 实体相加            15 + 5 + 22 + 11 + 9 = 62
//	② Expense 三分         8 + 4 + 10 = 22
//	③ Expense 按 F-1/F-2   8 + 14 = 22
func TestAttributeCountThreeWay(t *testing.T) {
	counts := map[string]int{
		"User":            reflect.TypeOf(User{}).NumField(),
		"ExpenseCategory": reflect.TypeOf(ExpenseCategory{}).NumField(),
		"Expense":         reflect.TypeOf(Expense{}).NumField(),
		"File":            reflect.TypeOf(File{}).NumField(),
		"AuditLog":        reflect.TypeOf(AuditLog{}).NumField(),
	}
	want := map[string]int{
		"User":            15,
		"ExpenseCategory": 5,
		"Expense":         22,
		"File":            11,
		"AuditLog":        9,
	}

	t.Run("路径①：5 实体逐个点算后相加为 62", func(t *testing.T) {
		total := 0
		for name, wantN := range want {
			if counts[name] != wantN {
				t.Errorf("%s 属性数: got %d, want %d", name, counts[name], wantN)
			}
			total += counts[name]
		}
		if total != 62 {
			t.Errorf("5 实体属性合计: got %d, want 62（终版 §十二 规模）", total)
		}
	})

	t.Run("路径②：Expense 三分 8+4+10=22", func(t *testing.T) {
		if n := len(expenseEditableFields); n != 8 {
			t.Errorf("用户可编辑项数: got %d, want 8（终版 F-1）", n)
		}
		if n := len(expenseDerivedFields); n != 4 {
			t.Errorf("只读派生项数: got %d, want 4（终版 §四 3）", n)
		}
		if n := len(expenseSystemFields); n != 10 {
			t.Errorf("系统只读项数: got %d, want 10（终版 §四 3）", n)
		}
		sum := len(expenseEditableFields) + len(expenseDerivedFields) + len(expenseSystemFields)
		if sum != counts["Expense"] {
			t.Errorf("三分之和与结构体字段数不符: %d vs %d", sum, counts["Expense"])
		}
	})

	t.Run("路径③：Expense 按 F-1(8)+F-2(14)=22", func(t *testing.T) {
		// F-2「系统维护字段（14 项）」= 只读派生 4 + 系统只读 10（终版 F-2 原文）
		f2 := len(expenseDerivedFields) + len(expenseSystemFields)
		if f2 != 14 {
			t.Errorf("F-2 系统维护字段数: got %d, want 14", f2)
		}
		if len(expenseEditableFields)+f2 != 22 {
			t.Errorf("F-1 + F-2: got %d, want 22", len(expenseEditableFields)+f2)
		}
	})
}

// TestNoDeletedFieldExceptExpense 守住前提 8：全模型只有 Expense 有软删除标记。
//
// 前提 8 明写「User / File / AuditLog 一律不可删除」，ExpenseCategory 是物理删除
// （删了就不在库里，没有标记可写）。因此除 Expense 外任何实体出现删除标记类字段，
// 都意味着有人绕过了前提 8。
//
// 这条不是重复 golden 表——那里锁的是「现有字段没改」，这里锁的是「不得新增
// 某一类字段」。将来若有人为「注销账户」给 User 加 DeletedAt，会在此失败并被迫
// 回到前提 8 而不是绕过它。
func TestNoDeletedFieldExceptExpense(t *testing.T) {
	forbidden := []string{"DeletedAt", "DeleteTime", "DeletedTime", "IsDeleted", "Deleted"}
	cases := []struct {
		name   string
		target any
	}{
		{"User", User{}},
		{"ExpenseCategory", ExpenseCategory{}},
		{"File", File{}},
		{"AuditLog", AuditLog{}},
	}
	for _, c := range cases {
		typ := reflect.TypeOf(c.target)
		for i := 0; i < typ.NumField(); i++ {
			for _, bad := range forbidden {
				if typ.Field(i).Name == bad {
					t.Errorf("%s 出现删除标记字段 %s，与前提 8「%s 不可删除」冲突",
						c.name, bad, c.name)
				}
			}
		}
	}

	// 反向：Expense 必须有且只有 DeletedAt 一个删除标记。
	expenseTyp := reflect.TypeOf(Expense{})
	if _, ok := expenseTyp.FieldByName("DeletedAt"); !ok {
		t.Error("Expense 缺少 DeletedAt，软删除（前提 8 / 8.5）无落点")
	}
}

// TestAuditLogHasNoRollbackField 守住终版 §四 5 表下注：无「回滚审计ID」。
//
// §十二 记载这是对话13 → 终版的实质变化之一（属性 63 → 62，D-8 一键回滚整项
// 取消，审计枚举 12 → 11 类）。若此字段出现，等于把已作废的 D-8 加回来了，
// 且属性点算会变成 63。
func TestAuditLogHasNoRollbackField(t *testing.T) {
	typ := reflect.TypeOf(AuditLog{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if name == "RollbackAuditID" || name == "RollbackID" {
			t.Errorf("AuditLog 出现回滚字段 %s，与终版 §四 5「无回滚审计ID」及 §十二 D-8 整项取消冲突", name)
		}
	}
}

// TestNoPointerField 守住终版 §四 表头：「不适用」的场景以零值表达，不留空。
//
// 8 个可空属性（Expense 的 6 项 + 卡片币种 + 删除时间，以及 File.文件密码）
// 一律用零值而非指针。指针会让每个消费方都要判空，而漏写不报错，只在某条没有
// 对手方的明细上突然崩掉。
//
// []byte 是唯一豁免：File.Content 的 nil 表示「元信息与内容分接口时内容未加载」，
// 与「文件是空的」有别，见该字段注释。
func TestNoPointerField(t *testing.T) {
	cases := []struct {
		name   string
		target any
	}{
		{"User", User{}},
		{"ExpenseCategory", ExpenseCategory{}},
		{"Expense", Expense{}},
		{"File", File{}},
		{"AuditLog", AuditLog{}},
	}
	for _, c := range cases {
		typ := reflect.TypeOf(c.target)
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			switch f.Type.Kind() {
			case reflect.Pointer:
				t.Errorf("%s.%s 是指针类型，违反终版 §四「不适用的场景以零值表达」",
					c.name, f.Name)
			case reflect.Map, reflect.Chan, reflect.Func, reflect.Interface:
				t.Errorf("%s.%s 类型 %s 不适合作实体属性（零值不可用或含行为）",
					c.name, f.Name, f.Type.Kind())
			case reflect.Slice:
				// 仅 File.Content 允许，理由见该字段注释。
				if !(c.name == "File" && f.Name == "Content") {
					t.Errorf("%s.%s 是切片类型，仅 File.Content 允许", c.name, f.Name)
				}
			}
		}
	}
}
