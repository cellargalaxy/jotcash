package enum

// 包内白盒单测：逐类型覆盖七类枚举的取值域、两处例外口径（ParserType 两条校验路径、
// ObjectType 零值合法），以及每类的 Value / Scan / MarshalText / UnmarshalText 往返。

import (
	"database/sql/driver"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// —— 取值域与项数：直接对着终版的点算 ——

// TestEnumItemCounts 七类枚举的项数逐一对着终版点算。
// 项数是可机械断言的口径，少一项意味着某个业务取值无处表达，多一项意味着凭空发明。
func TestEnumItemCounts(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
		src  string
	}{
		{"角色", len(roleTable.codes), 2, "终版 §四 1 `User.角色`：管理员 / 普通用户"},
		{"用户状态", len(userStatusTable.codes), 2, "终版 §四 1 `User.状态`：启用 / 禁用"},
		{"文件格式", len(fileFormatTable.codes), 3, "终版 §四 4 `File.格式`：CSV / Excel / PDF"},
		{"解析器类型", len(parserTypeTable.codes), 1, "D-1「本轮仅实现 CSV」"},
		{"操作类型", len(opTypeTable.codes), OpTypeCount, "终版 §八 8.4：11 类"},
		{"操作对象类型", len(objectTypeTable.codes), 4, "终版 §八 8.4 表出现的 4 种实体"},
		{"操作结果", len(opResultTable.codes), 2, "终版 §四 5 `AuditLog.操作结果`：成功 / 失败"},
	}
	for _, item := range cases {
		if item.got != item.want {
			t.Errorf("%s项数：期望%d，实际%d（依据：%s）", item.name, item.want, item.got, item.src)
		}
	}
}

// TestOpTypeOrderMatchesDoc 11 类操作类型的**声明顺序**严格等于终版 §八 8.4 表的 1~11 行。
// K-2 的操作类型下拉据此稳定排序（table.codes 注释）。
func TestOpTypeOrderMatchesDoc(t *testing.T) {
	want := []OpType{
		OpTypeSystemInit,        // 1 系统初始化
		OpTypeLogin,             // 2 登录
		OpTypeAccountManage,     // 3 账户管理
		OpTypePasswordChange,    // 4 密码修改
		OpTypeFileUpload,        // 5 文件上传
		OpTypeDataIntake,        // 6 数据入库
		OpTypeExpenseEditDelete, // 7 明细编辑与删除
		OpTypeBaseAmountRecalc,  // 8 本位币金额重算
		OpTypeCategoryManage,    // 9 支出类型维护
		OpTypeUserPreference,    // 10 用户偏好设置
		OpTypeDataExport,        // 11 数据导出
	}
	got := ListOpTypes()
	if len(got) != len(want) {
		t.Fatalf("操作类型数量：期望%d，实际%d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第%d类：期望%q，实际%q（顺序须与 §八 8.4 表一致）", i+1, want[i], got[i])
		}
	}
	// 名称逐条对着 §八 8.4 表的中文
	names := map[OpType]string{
		OpTypeSystemInit: "系统初始化", OpTypeLogin: "登录",
		OpTypeAccountManage: "账户管理", OpTypePasswordChange: "密码修改",
		OpTypeFileUpload: "文件上传", OpTypeDataIntake: "数据入库",
		OpTypeExpenseEditDelete: "明细编辑与删除", OpTypeBaseAmountRecalc: "本位币金额重算",
		OpTypeCategoryManage: "支出类型维护", OpTypeUserPreference: "用户偏好设置",
		OpTypeDataExport: "数据导出",
	}
	for op, wantName := range names {
		if got := op.Name(); got != wantName {
			t.Errorf("%q的名称：期望%q，实际%q", op, wantName, got)
		}
	}
}

// TestOpTypeNoRollbackNoExpenseCreate 确认**不存在**「回滚」与「明细新增」两类。
// 依据终版 §十二 变化 ④「审计枚举 11 → 12 → 11 类（增「数据导出」、删「回滚」）」
// 与 §八 8.4 表末注「明细的『新增』不在本表单列——一律走『数据入库』」。
// 这条是反向校验：凭空多出一类的后果是审计口径与文档脱节，且无写入方。
func TestOpTypeNoRollbackNoExpenseCreate(t *testing.T) {
	forbidden := []string{
		"rollback", "revoke", "undo", "batch_rollback", // 回滚（已删）
		"expense_create", "expense_add", "expense_insert", // 明细新增（走数据入库）
	}
	for _, code := range forbidden {
		if opTypeTable.has(code) {
			t.Errorf("操作类型中不应存在%q", code)
		}
		if _, err := ParseOpType(code); err == nil {
			t.Errorf("ParseOpType(%q)：期望报错，实际通过", code)
		}
	}
}

// —— Role ——

// TestRole 角色的取值域、判据与 CheckCreatableRole。
func TestRole(t *testing.T) {
	if !RoleAdmin.Valid() || !RoleUser.Valid() {
		t.Error("两个角色都应合法")
	}
	if !RoleAdmin.IsAdmin() {
		t.Error("RoleAdmin.IsAdmin() 应为真")
	}
	if RoleUser.IsAdmin() {
		t.Error("RoleUser.IsAdmin() 应为假")
	}
	// 零值：非法且非管理员——A-7 的管理员校验不得被零值绕过
	var zero Role
	if zero.Valid() {
		t.Error("零值角色应非法")
	}
	if !zero.IsZero() {
		t.Error("零值角色的 IsZero 应为真")
	}
	if zero.IsAdmin() {
		t.Error("零值角色不得被判为管理员")
	}
	// 三分口径
	if RoleAdmin.String() != "admin" {
		t.Errorf("String 应返回代码，实际%q", RoleAdmin.String())
	}
	if RoleAdmin.Name() != "管理员" {
		t.Errorf("Name 应返回中文名，实际%q", RoleAdmin.Name())
	}
	if RoleAdmin.TextKey() != "enum.role.admin" {
		t.Errorf("TextKey 实际%q", RoleAdmin.TextKey())
	}
	// 列举
	if roles := ListRoles(); len(roles) != 2 || roles[0] != RoleAdmin || roles[1] != RoleUser {
		t.Errorf("ListRoles 实际%v", roles)
	}
	// A-7 新增账户：只放行普通用户（终版 §四 1「新建为普通用户」）
	if err := CheckCreatableRole(RoleUser); err != nil {
		t.Errorf("普通用户应可新建，实际%v", err)
	}
	if err := CheckCreatableRole(RoleAdmin); err == nil {
		t.Error("管理员不应可由 A-7 新增（A-1 唯一管理员）")
	} else if !errs.IsInput(err) {
		t.Errorf("期望「用户输入错误」档，实际%v", errs.KindOf(err))
	}
	if err := CheckCreatableRole(""); err == nil {
		t.Error("零值角色不应可新建")
	}
	if err := CheckCreatableRole("no_such_role"); err == nil {
		t.Error("非法角色不应可新建")
	}
}

// —— UserStatus ——

// TestUserStatus 用户状态的取值域与 IsEnabled 判据。
func TestUserStatus(t *testing.T) {
	if !UserStatusEnabled.IsEnabled() {
		t.Error("启用状态的 IsEnabled 应为真")
	}
	if UserStatusDisabled.IsEnabled() {
		t.Error("禁用状态的 IsEnabled 应为假")
	}
	// 零值不得被判为启用——否则漏赋值的账户可以登录
	var zero UserStatus
	if zero.IsEnabled() {
		t.Error("零值状态不得被判为启用")
	}
	if zero.Valid() || !zero.IsZero() {
		t.Error("零值状态应非法且 IsZero 为真")
	}
	if got := ListUserStatuses(); len(got) != 2 || got[0] != UserStatusEnabled {
		t.Errorf("ListUserStatuses 实际%v（默认值应在前）", got)
	}
	if UserStatusDisabled.Name() != "禁用" {
		t.Errorf("禁用的名称实际%q", UserStatusDisabled.Name())
	}
}

// —— FileFormat ——

// TestFileFormat 文件格式的取值域与名称。
func TestFileFormat(t *testing.T) {
	want := []FileFormat{FileFormatCSV, FileFormatExcel, FileFormatPDF}
	got := ListFileFormats()
	if len(got) != len(want) {
		t.Fatalf("文件格式数量：期望%d，实际%d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第%d项：期望%q，实际%q（顺序须与终版 §四 4 的 CSV/Excel/PDF 一致）", i, want[i], got[i])
		}
	}
	names := map[FileFormat]string{
		FileFormatCSV: "CSV", FileFormatExcel: "Excel", FileFormatPDF: "PDF",
	}
	for f, wantName := range names {
		if f.Name() != wantName {
			t.Errorf("%q的名称：期望%q，实际%q", f, wantName, f.Name())
		}
	}
	var zero FileFormat
	if zero.Valid() || !zero.IsZero() {
		t.Error("零值格式应非法且 IsZero 为真")
	}
}

// —— ParserType：两条校验路径 ——

// TestParserTypeTwoPaths 两条校验路径的口径差异（包注释第三节）。
// 本轮只有一个已启用项，故用一个临时停用项验证「读历史数据仍可读、收用户输入被拦」。
func TestParserTypeTwoPaths(t *testing.T) {
	// 已启用项：两条路径都通过
	if got, err := ParseParserType(string(ParserTypeGenericCSV)); err != nil || got != ParserTypeGenericCSV {
		t.Errorf("已启用项走用户输入路径：期望通过，实际%q/%v", got, err)
	}
	if got, err := ParseParserTypeStored(string(ParserTypeGenericCSV)); err != nil || got != ParserTypeGenericCSV {
		t.Errorf("已启用项走历史数据路径：期望通过，实际%q/%v", got, err)
	}

	// 临时注入一个已停用项，模拟「停用某解析器后存量 File 仍需可读」
	const retired ParserType = "retired_bank_pdf"
	restoreTable := parserTypeTable
	restoreAttrs := parserTypeAttrs
	parserTypeTable = newTable("parser_type", "解析器类型",
		entry{code: string(ParserTypeGenericCSV), name: "通用 · 账单 · CSV"},
		entry{code: string(retired), name: "某行 · 信用卡账单 · PDF"},
	)
	parserTypeAttrs = map[ParserType]parserTypeAttr{
		ParserTypeGenericCSV: {format: FileFormatCSV, enabled: true},
		retired:              {format: FileFormatPDF, enabled: false},
	}
	defer func() {
		parserTypeTable = restoreTable
		parserTypeAttrs = restoreAttrs
	}()

	// 读历史数据：**必须仍能读出**（C-4 文件只留存不删除 + §五 2 只能停用不能删除）
	if got, err := ParseParserTypeStored(string(retired)); err != nil || got != retired {
		t.Errorf("已停用项走历史数据路径：期望通过（否则存量 File 读不出来），实际%q/%v", got, err)
	}
	// 收用户输入：必须被拦，且用「解析器类型键不存在或未启用」这个专属文案键
	got, err := ParseParserType(string(retired))
	if err == nil {
		t.Fatal("已停用项走用户输入路径：期望被拦，实际通过（已停用的解析器可被重新选中）")
	}
	if got != "" {
		t.Errorf("被拦时应返回零值，实际%q", got)
	}
	if !errs.IsInput(err) {
		t.Errorf("期望「用户输入错误」档，实际%v", errs.KindOf(err))
	}
	if key := errs.From(err).Key(); key != errs.KeyParserTypeUnknown {
		t.Errorf("文案键期望%q，实际%q", errs.KeyParserTypeUnknown, key)
	}

	// Valid 与 Enabled 的口径分离
	if !retired.Valid() {
		t.Error("已停用项的 Valid 应为真（本包认识这个键）")
	}
	if retired.Enabled() {
		t.Error("已停用项的 Enabled 应为假")
	}
	// 列举只出已启用项
	for _, p := range ListEnabledParserTypes() {
		if p == retired {
			t.Error("已停用项不应出现在上传页下拉中")
		}
	}
	// 落库仍按 Valid 校验（Value 注释：存量记录必须读得出也写得回）
	if _, err := retired.Value(); err != nil {
		t.Errorf("已停用项落库：期望放行（否则存量记录读得出写不回），实际%v", err)
	}
	// Scan 走历史数据路径
	var scanned ParserType
	if err := scanned.Scan(string(retired)); err != nil {
		t.Errorf("已停用项 Scan：期望通过，实际%v", err)
	}
	// UnmarshalText 走用户输入路径
	var unmarshaled ParserType
	if err := unmarshaled.UnmarshalText([]byte(retired)); err == nil {
		t.Error("已停用项 UnmarshalText：期望被拦（来源是请求体，即用户输入）")
	}
}

// TestParserTypeFormatBinding 适用格式与 MatchFormat（D-3 第 5 类的机械判据）。
func TestParserTypeFormatBinding(t *testing.T) {
	if got := ParserTypeGenericCSV.Format(); got != FileFormatCSV {
		t.Errorf("通用 CSV 的适用格式：期望%q，实际%q", FileFormatCSV, got)
	}
	if !ParserTypeGenericCSV.MatchFormat(FileFormatCSV) {
		t.Error("通用 CSV 应匹配 CSV 格式")
	}
	// 格式对不上：无需真去解析即可判定不匹配
	for _, f := range []FileFormat{FileFormatExcel, FileFormatPDF, ""} {
		if ParserTypeGenericCSV.MatchFormat(f) {
			t.Errorf("通用 CSV 不应匹配格式%q", f)
		}
	}
	// 未定义的键：格式为零值、不匹配任何格式
	const unknown ParserType = "no_such_parser"
	if got := unknown.Format(); got != "" {
		t.Errorf("未定义键的格式：期望零值，实际%q", got)
	}
	if unknown.MatchFormat(FileFormatCSV) || unknown.Enabled() {
		t.Error("未定义键不应匹配任何格式、也不应为已启用")
	}
	// 按格式过滤：本轮 CSV 有一项，Excel / PDF 无项（正常业务状态，不是错误）
	if got := ListEnabledParserTypesByFormat(FileFormatCSV); len(got) != 1 || got[0] != ParserTypeGenericCSV {
		t.Errorf("CSV 格式下的解析器：实际%v", got)
	}
	for _, f := range []FileFormat{FileFormatExcel, FileFormatPDF} {
		if got := ListEnabledParserTypesByFormat(f); len(got) != 0 {
			t.Errorf("格式%q下本轮应无解析器，实际%v", f, got)
		}
	}
	// 格式非法：返回空切片而非 nil、不报错
	if got := ListEnabledParserTypesByFormat("no_such_format"); got == nil || len(got) != 0 {
		t.Errorf("非法格式：期望空切片，实际%v", got)
	}
}

// TestParserTypeAttrsConsistency 属性表与枚举表键集合一致（init 已校验，这里是复核）。
func TestParserTypeAttrsConsistency(t *testing.T) {
	if len(parserTypeAttrs) != len(parserTypeTable.codes) {
		t.Fatalf("属性表与枚举表项数不符：%d / %d", len(parserTypeAttrs), len(parserTypeTable.codes))
	}
	for _, code := range parserTypeTable.codes {
		attr, ok := parserTypeAttrs[ParserType(code)]
		if !ok {
			t.Errorf("键%q缺少属性表项", code)
			continue
		}
		if !attr.format.Valid() {
			t.Errorf("键%q的适用格式非法：%q", code, attr.format)
		}
	}
}

// —— ObjectType：唯一零值合法 ——

// TestObjectTypeZeroIsLegal 零值 = 「无具体对象或为批量操作」，落库与序列化一律放行。
// 依据终版 §四 5；§八 8.4 有 5 类审计的对象类型恒为零值。
func TestObjectTypeZeroIsLegal(t *testing.T) {
	none := ObjectTypeNone
	if none != "" {
		t.Errorf("ObjectTypeNone 应为空串，实际%q", none)
	}
	if !none.IsZero() {
		t.Error("ObjectTypeNone.IsZero 应为真")
	}
	// Valid 为假（不是「一个对象类型」），ValidOrNone 为真（取值可接受）
	if none.Valid() {
		t.Error("零值的 Valid 应为假——它不指向任何实体")
	}
	if !none.ValidOrNone() {
		t.Error("零值的 ValidOrNone 应为真——它是合法业务取值")
	}
	// 落库：放行且落空串
	val, err := none.Value()
	if err != nil {
		t.Fatalf("零值落库应放行（5 类审计恒为零值），实际%v", err)
	}
	if val != driver.Value("") {
		t.Errorf("零值落库应为空串，实际%v", val)
	}
	// 序列化：放行且产出空串
	text, err := none.MarshalText()
	if err != nil {
		t.Fatalf("零值序列化应放行，实际%v", err)
	}
	if len(text) != 0 {
		t.Errorf("零值序列化应为空，实际%q", text)
	}
	// Parse 与 Unmarshal 接受空串
	if got, err := ParseObjectType(""); err != nil || got != ObjectTypeNone {
		t.Errorf("ParseObjectType(\"\")：期望零值/nil，实际%q/%v", got, err)
	}
	var target ObjectType
	if err := target.UnmarshalText([]byte("")); err != nil || target != ObjectTypeNone {
		t.Errorf("UnmarshalText(\"\")：期望零值/nil，实际%q/%v", target, err)
	}
	// Scan 接受空串但仍拒收 NULL（终版 §四 表头「以零值表达，不留空」）
	if err := target.Scan(""); err != nil {
		t.Errorf("Scan(\"\")：期望通过，实际%v", err)
	}
	if err := target.Scan(nil); err == nil {
		t.Error("Scan(nil)：期望拒收 NULL")
	}
	// Name 返回「无对象」而非「未知…」——零值是正常业务取值，不是数据异常
	if got := none.Name(); got != "无对象" {
		t.Errorf("零值的 Name：期望「无对象」，实际%q", got)
	}
	// TextKey 为空：无对象则无需取词
	if got := none.TextKey(); got != "" {
		t.Errorf("零值的 TextKey：期望空串，实际%q", got)
	}
}

// TestObjectTypeMembers 4 项取值域，且不含 AuditLog 自身。
// 依据 §八 8.4 表的「对象类型 / 对象ID」列：只出现 User / File / Expense / ExpenseCategory。
func TestObjectTypeMembers(t *testing.T) {
	want := []ObjectType{
		ObjectTypeUser, ObjectTypeExpenseCategory, ObjectTypeExpense, ObjectTypeFile,
	}
	got := ListObjectTypes()
	if len(got) != len(want) {
		t.Fatalf("对象类型数量：期望%d，实际%d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第%d项：期望%q，实际%q", i, want[i], got[i])
		}
	}
	// 列举不含零值——它是「没有对象」这一状态，出现在下拉里没有意义
	for _, item := range got {
		if item == ObjectTypeNone {
			t.Error("ListObjectTypes 不应含零值")
		}
	}
	// AuditLog 自身不作对象类型（K-2 只读，不承担任何写操作）
	for _, code := range []string{"audit_log", "auditlog", "audit"} {
		if objectTypeTable.has(code) {
			t.Errorf("对象类型中不应存在%q——审计不记录「对审计的操作」", code)
		}
	}
}

// —— OpResult ——

// TestOpResult 操作结果的取值域与 IsSuccess 判据。
func TestOpResult(t *testing.T) {
	if !OpResultSuccess.IsSuccess() {
		t.Error("成功的 IsSuccess 应为真")
	}
	if OpResultFailure.IsSuccess() {
		t.Error("失败的 IsSuccess 应为假")
	}
	// 零值不得被判为成功——否则漏赋值的记录会走进业务同事务，随回滚一并消失
	var zero OpResult
	if zero.IsSuccess() {
		t.Error("零值结果不得被判为成功")
	}
	if zero.Valid() || !zero.IsZero() {
		t.Error("零值结果应非法且 IsZero 为真")
	}
	if got := ListOpResults(); len(got) != 2 || got[0] != OpResultSuccess {
		t.Errorf("ListOpResults 实际%v", got)
	}
	if OpResultFailure.Name() != "失败" {
		t.Errorf("失败的名称实际%q", OpResultFailure.Name())
	}
}

// —— 跨类型统一口径：落库 / 序列化 / 往返 ——

// TestAllEnumsRoundTrip 七类枚举的 Value → Scan、MarshalText → UnmarshalText 往返一致。
// 落库与序列化必须同进同退（marshalTextOf 注释），往返不一致会产出「存进去取出来变了」的字段。
func TestAllEnumsRoundTrip(t *testing.T) {
	// 每类取一个合法值，逐类走四条出口
	type roundTrip struct {
		name string
		// value 返回落库值与序列化文本
		value func() (driver.Value, []byte, error)
		// scanBack 从库内文本与 JSON 文本各扫一次，比对是否等于原值
		scanBack func(stored string, text []byte) (equal bool, err error)
	}
	cases := []roundTrip{
		{
			name: "角色",
			value: func() (driver.Value, []byte, error) {
				v, err := RoleAdmin.Value()
				if err != nil {
					return nil, nil, err
				}
				b, err := RoleAdmin.MarshalText()
				return v, b, err
			},
			scanBack: func(stored string, text []byte) (bool, error) {
				var a, b Role
				if err := a.Scan(stored); err != nil {
					return false, err
				}
				if err := b.UnmarshalText(text); err != nil {
					return false, err
				}
				return a == RoleAdmin && b == RoleAdmin, nil
			},
		},
		{
			name: "用户状态",
			value: func() (driver.Value, []byte, error) {
				v, err := UserStatusDisabled.Value()
				if err != nil {
					return nil, nil, err
				}
				b, err := UserStatusDisabled.MarshalText()
				return v, b, err
			},
			scanBack: func(stored string, text []byte) (bool, error) {
				var a, b UserStatus
				if err := a.Scan(stored); err != nil {
					return false, err
				}
				if err := b.UnmarshalText(text); err != nil {
					return false, err
				}
				return a == UserStatusDisabled && b == UserStatusDisabled, nil
			},
		},
		{
			name: "文件格式",
			value: func() (driver.Value, []byte, error) {
				v, err := FileFormatPDF.Value()
				if err != nil {
					return nil, nil, err
				}
				b, err := FileFormatPDF.MarshalText()
				return v, b, err
			},
			scanBack: func(stored string, text []byte) (bool, error) {
				var a, b FileFormat
				if err := a.Scan(stored); err != nil {
					return false, err
				}
				if err := b.UnmarshalText(text); err != nil {
					return false, err
				}
				return a == FileFormatPDF && b == FileFormatPDF, nil
			},
		},
		{
			name: "解析器类型",
			value: func() (driver.Value, []byte, error) {
				v, err := ParserTypeGenericCSV.Value()
				if err != nil {
					return nil, nil, err
				}
				b, err := ParserTypeGenericCSV.MarshalText()
				return v, b, err
			},
			scanBack: func(stored string, text []byte) (bool, error) {
				var a, b ParserType
				if err := a.Scan(stored); err != nil {
					return false, err
				}
				if err := b.UnmarshalText(text); err != nil {
					return false, err
				}
				return a == ParserTypeGenericCSV && b == ParserTypeGenericCSV, nil
			},
		},
		{
			name: "操作类型",
			value: func() (driver.Value, []byte, error) {
				v, err := OpTypeDataIntake.Value()
				if err != nil {
					return nil, nil, err
				}
				b, err := OpTypeDataIntake.MarshalText()
				return v, b, err
			},
			scanBack: func(stored string, text []byte) (bool, error) {
				var a, b OpType
				if err := a.Scan(stored); err != nil {
					return false, err
				}
				if err := b.UnmarshalText(text); err != nil {
					return false, err
				}
				return a == OpTypeDataIntake && b == OpTypeDataIntake, nil
			},
		},
		{
			name: "操作对象类型",
			value: func() (driver.Value, []byte, error) {
				v, err := ObjectTypeExpense.Value()
				if err != nil {
					return nil, nil, err
				}
				b, err := ObjectTypeExpense.MarshalText()
				return v, b, err
			},
			scanBack: func(stored string, text []byte) (bool, error) {
				var a, b ObjectType
				if err := a.Scan(stored); err != nil {
					return false, err
				}
				if err := b.UnmarshalText(text); err != nil {
					return false, err
				}
				return a == ObjectTypeExpense && b == ObjectTypeExpense, nil
			},
		},
		{
			name: "操作结果",
			value: func() (driver.Value, []byte, error) {
				v, err := OpResultFailure.Value()
				if err != nil {
					return nil, nil, err
				}
				b, err := OpResultFailure.MarshalText()
				return v, b, err
			},
			scanBack: func(stored string, text []byte) (bool, error) {
				var a, b OpResult
				if err := a.Scan(stored); err != nil {
					return false, err
				}
				if err := b.UnmarshalText(text); err != nil {
					return false, err
				}
				return a == OpResultFailure && b == OpResultFailure, nil
			},
		},
	}
	for _, item := range cases {
		val, text, err := item.value()
		if err != nil {
			t.Errorf("%s：落库或序列化报错%v", item.name, err)
			continue
		}
		stored, ok := val.(string)
		if !ok {
			t.Errorf("%s：落库值应为字符串（枚举列一律 TEXT），实际%T", item.name, val)
			continue
		}
		// 落库值与序列化文本必须是同一个代码——否则库里与 JSON 里是两套值
		if stored != string(text) {
			t.Errorf("%s：落库值%q与序列化文本%q不一致", item.name, stored, text)
		}
		equal, err := item.scanBack(stored, text)
		if err != nil {
			t.Errorf("%s：回扫报错%v", item.name, err)
			continue
		}
		if !equal {
			t.Errorf("%s：往返后取值改变", item.name)
		}
	}
}

// TestAllEnumsRejectZeroExceptObjectType 除 ObjectType 外，六类枚举的零值一律
// **在落库与序列化上报错**，而不是输出空串（包注释第四节）。
// 依据：这六类对应的属性全部必填，出现零值只能是上游漏赋值，
// 静默落一个空串进库会把漏填变成一条永久无法解释的记录。
func TestAllEnumsRejectZeroExceptObjectType(t *testing.T) {
	type zeroCase struct {
		name    string
		value   func() (driver.Value, error)
		marshal func() ([]byte, error)
		scan    func() error
	}
	cases := []zeroCase{
		{"角色",
			func() (driver.Value, error) { var z Role; return z.Value() },
			func() ([]byte, error) { var z Role; return z.MarshalText() },
			func() error { var z Role; return z.Scan("") }},
		{"用户状态",
			func() (driver.Value, error) { var z UserStatus; return z.Value() },
			func() ([]byte, error) { var z UserStatus; return z.MarshalText() },
			func() error { var z UserStatus; return z.Scan("") }},
		{"文件格式",
			func() (driver.Value, error) { var z FileFormat; return z.Value() },
			func() ([]byte, error) { var z FileFormat; return z.MarshalText() },
			func() error { var z FileFormat; return z.Scan("") }},
		{"解析器类型",
			func() (driver.Value, error) { var z ParserType; return z.Value() },
			func() ([]byte, error) { var z ParserType; return z.MarshalText() },
			func() error { var z ParserType; return z.Scan("") }},
		{"操作类型",
			func() (driver.Value, error) { var z OpType; return z.Value() },
			func() ([]byte, error) { var z OpType; return z.MarshalText() },
			func() error { var z OpType; return z.Scan("") }},
		{"操作结果",
			func() (driver.Value, error) { var z OpResult; return z.Value() },
			func() ([]byte, error) { var z OpResult; return z.MarshalText() },
			func() error { var z OpResult; return z.Scan("") }},
	}
	for _, item := range cases {
		if _, err := item.value(); err == nil {
			t.Errorf("%s：零值落库期望报错，实际放行", item.name)
		} else if !errs.IsSystem(err) {
			t.Errorf("%s：零值落库错误期望「系统错误」档，实际%v", item.name, errs.KindOf(err))
		}
		if _, err := item.marshal(); err == nil {
			t.Errorf("%s：零值序列化期望报错，实际放行", item.name)
		}
		if err := item.scan(); err == nil {
			t.Errorf("%s：扫描空串期望报错，实际放行", item.name)
		}
	}
}

// TestAllEnumsRejectNullAndBadType 七类枚举的 Scan 一律拒收 NULL 与非文本类型。
// 依据 codeFromSrc：枚举列出现 NULL 属建表或写入路径的缺陷（终版 §四 表头「不留空」）。
func TestAllEnumsRejectNullAndBadType(t *testing.T) {
	scanners := map[string]func(any) error{
		"角色":     func(v any) error { var x Role; return x.Scan(v) },
		"用户状态":   func(v any) error { var x UserStatus; return x.Scan(v) },
		"文件格式":   func(v any) error { var x FileFormat; return x.Scan(v) },
		"解析器类型":  func(v any) error { var x ParserType; return x.Scan(v) },
		"操作类型":   func(v any) error { var x OpType; return x.Scan(v) },
		"操作对象类型": func(v any) error { var x ObjectType; return x.Scan(v) },
		"操作结果":   func(v any) error { var x OpResult; return x.Scan(v) },
	}
	// 含 ObjectType：它放行空串，但 NULL 与非文本类型一律拒收
	for name, scan := range scanners {
		for _, bad := range []any{nil, 1, int64(7), 1.5, true} {
			if err := scan(bad); err == nil {
				t.Errorf("%s：Scan(%T) 期望报错，实际放行", name, bad)
			}
		}
		// []byte 与 string 等价（store/sqlite 的枚举列一律 TEXT，驱动可能给任一形态）
		if err := scan([]byte("no_such_code")); err == nil {
			t.Errorf("%s：Scan 非法代码期望报错，实际放行", name)
		}
	}
}

// TestAllEnumsCodesAreCanonical 七类枚举的全部代码符合 `[a-z][a-z0-9_]*`。
// 代码是落库值且永不可改（终版 §五 2），大小写或分隔符写混一次就永久带着两种形态。
func TestAllEnumsCodesAreCanonical(t *testing.T) {
	for _, tbl := range allTables() {
		for _, code := range tbl.codes {
			if !isCanonicalCode(code) {
				t.Errorf("kind=%q：代码%q不符合规范形态", tbl.kind, code)
			}
		}
	}
}
