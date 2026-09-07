package enum

import "testing"

// 本文件锁死**码值**与**枚举项集合**这两项对外契约。
//
// 为什么值得一整个文件：码值就是落库的值（准则 3「枚举即代码」），一旦发布，
// 存量数据行里存的就是这些字符串。改一个码值，历史行就指向了一个不存在的枚举项，
// 且这类事故在编译期、在常规单测里都毫无征兆——只有在读到旧数据那一刻才爆发。
//
// 所以这里把码值逐字写死成 golden 表：任何人改动码值都会在此处失败，并被迫回答
// 「存量数据怎么迁移」这个问题。相对地，中文名不在此锁定——它只进日志，可以自由改。
//
// 同时锁定项数与项的集合：漏掉一项（比如 11 类审计写成 10 类）同样是静默故障。

// TestRoleCodes 锁定角色的码值与项集合。
func TestRoleCodes(t *testing.T) {
	want := []struct {
		role Role
		code string
	}{
		{RoleAdmin, "admin"},
		{RoleUser, "user"},
	}
	got := Roles()
	if len(got) != len(want) {
		t.Fatalf("角色项数不符: got %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w.role {
			t.Errorf("角色声明序[%d]: got %q, want %q", i, got[i].Code(), w.role.Code())
		}
		if w.role.Code() != w.code {
			t.Errorf("角色码值变更（存量数据会失配）: got %q, want %q", w.role.Code(), w.code)
		}
	}
}

// TestUserStatusCodes 锁定用户状态的码值与项集合。
func TestUserStatusCodes(t *testing.T) {
	want := []struct {
		status UserStatus
		code   string
	}{
		{UserStatusEnabled, "enabled"},
		{UserStatusDisabled, "disabled"},
	}
	got := UserStatuses()
	if len(got) != len(want) {
		t.Fatalf("用户状态项数不符: got %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w.status {
			t.Errorf("用户状态声明序[%d]: got %q, want %q", i, got[i].Code(), w.status.Code())
		}
		if w.status.Code() != w.code {
			t.Errorf("用户状态码值变更（存量数据会失配）: got %q, want %q", w.status.Code(), w.code)
		}
	}
}

// TestUserStatusNoDeletedItem 守住前提 8：账户不可删除，故无「已删除」状态。
//
// 这条不是重复 TestUserStatusCodes——那里锁的是「现有项没改」，这里锁的是
// 「不得新增某一类项」。将来若有人为了「注销账户」加一个 deleted 状态，会在此失败，
// 并被迫回到前提 8 而不是绕过它。
func TestUserStatusNoDeletedItem(t *testing.T) {
	for _, s := range UserStatuses() {
		if s.Code() == "deleted" || s.Code() == "removed" {
			t.Errorf("用户状态出现删除态 %q，与前提 8「账户不可删除、无删除标记」冲突", s.Code())
		}
	}
}

// TestFileFormatCodes 锁定文件格式的码值与项集合（终版 C-2 恰三项）。
func TestFileFormatCodes(t *testing.T) {
	want := []struct {
		format FileFormat
		code   string
	}{
		{FileFormatCSV, "csv"},
		{FileFormatExcel, "excel"},
		{FileFormatPDF, "pdf"},
	}
	got := FileFormats()
	if len(got) != len(want) {
		t.Fatalf("文件格式项数不符: got %d, want %d（终版 C-2 恰 CSV/Excel/PDF 三项）", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w.format {
			t.Errorf("文件格式声明序[%d]: got %q, want %q", i, got[i].Code(), w.format.Code())
		}
		if w.format.Code() != w.code {
			t.Errorf("文件格式码值变更（存量数据会失配）: got %q, want %q", w.format.Code(), w.code)
		}
	}
}

// TestParserTypeCodes 锁定解析器类型的码值与元数据。
//
// 与其余枚举不同，这里连**元数据**一并锁定：Name 是 D-2 下拉的标签、Format 是
// D-3 第五类错误判定的依据，两者改动都会直接影响上传流程的行为。
func TestParserTypeCodes(t *testing.T) {
	want := []struct {
		pt      ParserType
		code    string
		name    string
		format  FileFormat
		enabled bool
	}{
		{ParserTypeGenericCSV, "generic_csv", "通用 · 逐笔交易 · CSV", FileFormatCSV, true},
	}
	got := ParserTypes()
	if len(got) != len(want) {
		t.Fatalf("解析器类型项数不符: got %d, want %d（D-1 本轮仅实现 CSV）", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w.pt {
			t.Errorf("解析器类型声明序[%d]: got %q, want %q", i, got[i].Code(), w.pt.Code())
		}
		if w.pt.Code() != w.code {
			t.Errorf("解析器类型码值变更（存量 File 行会失配）: got %q, want %q", w.pt.Code(), w.code)
		}
		meta, ok := w.pt.Meta()
		if !ok {
			t.Fatalf("解析器类型 %q 缺少元数据", w.code)
		}
		if meta.Name != w.name {
			t.Errorf("解析器类型 %q 名称: got %q, want %q", w.code, meta.Name, w.name)
		}
		if meta.Format != w.format {
			t.Errorf("解析器类型 %q 适用格式: got %q, want %q", w.code, meta.Format.Code(), w.format.Code())
		}
		if meta.Enabled != w.enabled {
			t.Errorf("解析器类型 %q 启用状态: got %v, want %v", w.code, meta.Enabled, w.enabled)
		}
	}
}

// TestParserTypeDistinctFromFileFormat 守住「格式不决定解析器」这条正交性。
//
// 终版 §四 4 明写文件格式「不决定解析器」，D-1 的理由是「同为 PDF，各银行版式不同」。
// 若某个解析器类型键与某个文件格式码值相同，两列在库里看起来一模一样，日后极易被
// 误当成同一个东西而合并掉——本用例就是拦住这一步。
func TestParserTypeDistinctFromFileFormat(t *testing.T) {
	formats := make(map[string]bool, len(FileFormats()))
	for _, f := range FileFormats() {
		formats[f.Code()] = true
	}
	for _, pt := range ParserTypes() {
		if formats[pt.Code()] {
			t.Errorf("解析器类型码值 %q 与文件格式码值重名，会模糊「格式不决定解析器」的正交性", pt.Code())
		}
	}
}

// TestOperationTypeCodes 锁定 11 类操作类型的码值、项数与声明序。
//
// 项数尤其关键：终版 §十二 记「D-8 整项取消，审计枚举 12 → 11 类」，
// 这里的 11 是定案值，不是巧合。
func TestOperationTypeCodes(t *testing.T) {
	want := []struct {
		op   OperationType
		code string
	}{
		{OperationTypeSystemInit, "system_init"},
		{OperationTypeLogin, "login"},
		{OperationTypeAccountManage, "account_manage"},
		{OperationTypePasswordChange, "password_change"},
		{OperationTypeFileUpload, "file_upload"},
		{OperationTypeDataIntake, "data_intake"},
		{OperationTypeExpenseModify, "expense_modify"},
		{OperationTypeBaseAmountRecalc, "base_amount_recalc"},
		{OperationTypeCategoryManage, "category_manage"},
		{OperationTypePreferenceSet, "preference_set"},
		{OperationTypeDataExport, "data_export"},
	}
	got := OperationTypes()
	if len(got) != 11 {
		t.Fatalf("操作类型项数不符: got %d, want 11（终版 §8.4，D-8 取消后为 11 类）", len(got))
	}
	if len(want) != 11 {
		t.Fatalf("用例期望表自身项数不符: got %d, want 11", len(want))
	}
	for i, w := range want {
		if got[i] != w.op {
			t.Errorf("操作类型声明序[%d]: got %q, want %q", i, got[i].Code(), w.op.Code())
		}
		if w.op.Code() != w.code {
			t.Errorf("操作类型码值变更（存量审计行会失配）: got %q, want %q", w.op.Code(), w.code)
		}
	}
}

// TestOperationTypeExcludedItems 守住 §8.4 的三个刻意缺席。
//
// 登出（A-4「服务端无动作、不记审计」）、明细新增（§8.4 表下注「一律走数据入库」）、
// 回滚（§十二 D-8 整项取消）。三者都曾是候选类目，都被明确排除；将来若有人凭直觉
// 补回来，会在此失败并被迫先回到决策文档。
func TestOperationTypeExcludedItems(t *testing.T) {
	excluded := map[string]string{
		"logout":          "A-4 明写登出不记审计",
		"expense_create":  "§8.4 表下注：明细新增一律走数据入库",
		"expense_add":     "§8.4 表下注：明细新增一律走数据入库",
		"rollback":        "§十二 记 D-8 一键回滚整项取消",
		"batch_rollback":  "§十二 记 D-8 一键回滚整项取消",
		"intake_rollback": "§十二 记 D-8 一键回滚整项取消",
	}
	for _, op := range OperationTypes() {
		if reason, bad := excluded[op.Code()]; bad {
			t.Errorf("操作类型出现已排除项 %q：%s", op.Code(), reason)
		}
	}
}

// TestObjectTypeCodes 锁定操作对象类型的码值与项集合（恰四项）。
func TestObjectTypeCodes(t *testing.T) {
	want := []struct {
		ot   ObjectType
		code string
	}{
		{ObjectTypeUser, "user"},
		{ObjectTypeFile, "file"},
		{ObjectTypeExpense, "expense"},
		{ObjectTypeExpenseCategory, "expense_category"},
	}
	got := ObjectTypes()
	if len(got) != len(want) {
		t.Fatalf("操作对象类型项数不符: got %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w.ot {
			t.Errorf("操作对象类型声明序[%d]: got %q, want %q", i, got[i].Code(), w.ot.Code())
		}
		if w.ot.Code() != w.code {
			t.Errorf("操作对象类型码值变更（存量审计行会失配）: got %q, want %q", w.ot.Code(), w.code)
		}
	}
}

// TestObjectTypeHasNoAuditLog 守住「审计不指向审计」。
//
// K-2 是只读页，不存在「对某条审计做了操作」，故 AuditLog 不是对象类型。
func TestObjectTypeHasNoAuditLog(t *testing.T) {
	for _, ot := range ObjectTypes() {
		if ot.Code() == "audit_log" || ot.Code() == "auditlog" {
			t.Errorf("操作对象类型出现 %q：K-2 为只读页，审计不作为操作对象", ot.Code())
		}
	}
}

// TestOperationResultCodes 锁定操作结果的码值与项集合。
func TestOperationResultCodes(t *testing.T) {
	want := []struct {
		r    OperationResult
		code string
	}{
		{OperationResultSuccess, "success"},
		{OperationResultFailure, "failure"},
	}
	got := OperationResults()
	if len(got) != len(want) {
		t.Fatalf("操作结果项数不符: got %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w.r {
			t.Errorf("操作结果声明序[%d]: got %q, want %q", i, got[i].Code(), w.r.Code())
		}
		if w.r.Code() != w.code {
			t.Errorf("操作结果码值变更（存量审计行会失配）: got %q, want %q", w.r.Code(), w.code)
		}
	}
}

// TestOperationResultNoPartial 守住「无部分成功」这一取向。
//
// I-7 的部分失败以摘要「成功 N 笔 / 失败 M 笔」表达，不引入第三态（见类型注释）。
func TestOperationResultNoPartial(t *testing.T) {
	for _, r := range OperationResults() {
		if r.Code() == "partial" || r.Code() == "partial_success" {
			t.Errorf("操作结果出现部分成功态 %q：I-7 的部分失败应由操作摘要表达", r.Code())
		}
	}
}

// TestNoCodeCollisionAcrossEnums 检查跨枚举的码值重名情况。
//
// 这不是硬性错误——不同枚举各占一列，user 同时作为角色与对象类型完全合法（事实上
// 现在就是如此）。本用例存在的意义是：一旦有人合并两个枚举、或把某列的值抄到另一列，
// 这里会给出一份重名清单供评审时对照，而不是让重名悄无声息地累积。
func TestNoCodeCollisionAcrossEnums(t *testing.T) {
	seen := make(map[string][]string)
	record := func(enumName string, codes []string) {
		for _, c := range codes {
			seen[c] = append(seen[c], enumName)
		}
	}
	record("Role", codesOf(Roles()))
	record("UserStatus", codesOf(UserStatuses()))
	record("FileFormat", codesOf(FileFormats()))
	record("ParserType", codesOf(ParserTypes()))
	record("OperationType", codesOf(OperationTypes()))
	record("ObjectType", codesOf(ObjectTypes()))
	record("OperationResult", codesOf(OperationResults()))

	// 已知且允许的重名：Role.user 与 ObjectType.user 分处两列、语义不同。
	allowed := map[string]bool{"user": true}
	for c, owners := range seen {
		if len(owners) > 1 && !allowed[c] {
			t.Logf("提示: 码值 %q 出现在多个枚举中: %v（各占一列，非错误，仅供评审对照）", c, owners)
		}
	}
}

// codesOf 把任意码值枚举切片转成字符串切片，供跨枚举核对使用。
func codesOf[T code](values []T) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, string(v))
	}
	return out
}
