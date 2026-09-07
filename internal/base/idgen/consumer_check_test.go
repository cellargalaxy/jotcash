package idgen

import (
	"encoding/json"
	"strconv"
	"testing"
)

// 本文件是「下游包会怎么用 base/idgen」的可执行走查，与 calendar 包的
// consumer_check_test.go 同构。每个用例对应分层设计里一处明确的承载，
// 目的是在 L0 阶段就把上层的用法钉死，避免写到 L5/L6 时才发现口径不合。

// TestConsumerAuditFirstThenAttach 走查不变式 4 的核心时序（分层设计 §九 不变式 4、
// 约定 9）：「先落审计取 ID → 业务数据挂载该 ID → 同事务提交」。
//
// 这条时序成立的唯一前提是「ID 在事务提交前就已确定」——本包正是为此存在。
// 若 ID 依赖数据库自增回填，同一事务内取不到值，明细与文件就无从挂载来源审计ID。
func TestConsumerAuditFirstThenAttach(t *testing.T) {
	// 模拟 service/intake 的 D-7 提交入库：一条「数据入库」审计 + N 条明细同事务
	type auditLog struct {
		AuditID    int64 // 审计ID，兼入库批次号
		UserID     int64 // 操作人 = 归属人
		ObjectType int   // 零值 = 批量操作
		ObjectID   int64 // 零值 = 无具体对象
	}
	type expense struct {
		ExpenseID     int64 // 明细ID
		OwnerUserID   int64 // 归属用户ID
		SourceAuditID int64 // 来源审计ID，批次筛选与撤销的依据
		SourceFileID  int64 // 来源文件ID，手工录入时为空
	}

	ownerID := GenUserID()

	// 第一步：先生成审计ID —— 此刻尚未落库，但 ID 已确定
	auditID := GenAuditID()
	if !IsValid(auditID) {
		t.Fatalf("审计ID %d 不合法", auditID)
	}
	// 「数据入库」审计的对象类型与对象ID 为零值（终版 8.4 第 6 类：批量）
	audit := auditLog{AuditID: auditID, UserID: ownerID}
	if audit.ObjectType != 0 {
		t.Errorf("「数据入库」审计的操作对象类型应为零值（批量），实际 %d", audit.ObjectType)
	}
	if IsValid(audit.ObjectID) {
		t.Errorf("「数据入库」审计的对象ID 应为零值（批量），实际 %d", audit.ObjectID)
	}

	// 第二步：N 条明细挂载该审计ID
	const n = 37
	fileID := GenFileID() // 文件导入链路，有来源文件
	rows := make([]expense, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, expense{
			ExpenseID:     GenExpenseID(),
			OwnerUserID:   ownerID,
			SourceAuditID: auditID,
			SourceFileID:  fileID,
		})
	}

	// 校验：每条明细的 ID 唯一、来源审计ID 全部指向同一批次
	ids := make(map[int64]struct{}, n)
	for i, r := range rows {
		if !IsValid(r.ExpenseID) {
			t.Fatalf("第 %d 条明细ID %d 不合法", i, r.ExpenseID)
		}
		if _, dup := ids[r.ExpenseID]; dup {
			t.Fatalf("第 %d 条明细ID %d 重复", i, r.ExpenseID)
		}
		ids[r.ExpenseID] = struct{}{}
		if r.SourceAuditID != auditID {
			t.Errorf("第 %d 条明细的来源审计ID = %d, 期望 %d（一次提交 = 一个批次）", i, r.SourceAuditID, auditID)
		}
		// 明细ID 不得与审计ID 撞号（同一取值空间内的唯一性）
		if r.ExpenseID == auditID {
			t.Errorf("第 %d 条明细ID 与审计ID 撞号 %d", i, auditID)
		}
	}
	if len(ids) != n {
		t.Errorf("明细ID 唯一值 = %d, 期望 %d", len(ids), n)
	}
}

// TestConsumerManualIntakeNoSourceFile 走查 Expense.来源文件ID 的零值语义
// （终版 §四「手工录入时为空；复制时继承被复制明细的值」）。
func TestConsumerManualIntakeNoSourceFile(t *testing.T) {
	// 手工录入：无来源文件，来源文件ID 留零值
	var manualSourceFileID int64
	if IsValid(manualSourceFileID) {
		t.Error("手工录入的来源文件ID 应为零值")
	}

	// 文件导入：有来源文件
	imported := GenFileID()
	if !IsValid(imported) {
		t.Errorf("文件导入的来源文件ID %d 应合法", imported)
	}

	// 复制（F-8 / 8.5）：卡片三属性与来源文件ID 随复制继承，不重新生成
	copied := imported
	if copied != imported {
		t.Error("复制应继承来源文件ID，不重新生成")
	}
	// 但明细ID 必须是新的（复制 = 生成新明细 + 新审计）
	newExpenseID := GenExpenseID()
	newAuditID := GenAuditID()
	if newExpenseID == newAuditID {
		t.Error("新明细ID 与新审计ID 撞号")
	}
}

// TestConsumerAuditObjectPointer 走查 AuditLog.对象ID 的「历史指针」语义
// （终版 §四、§七、K-2）：11 类审计里，有的带具体对象、有的为零值批量。
func TestConsumerAuditObjectPointer(t *testing.T) {
	// 取自终版 8.4 的 11 类操作类型对照表
	cases := []struct {
		name       string
		hasObject  bool
		objectID   int64
		wantIsUsed bool
	}{
		{"1 系统初始化（User / admin 用户ID）", true, GenUserID(), true},
		{"3 账户管理（User / 目标用户ID）", true, GenUserID(), true},
		{"5 文件上传（File / 文件ID）", true, GenFileID(), true},
		{"6 数据入库（零值，批量）", false, 0, false},
		{"7 明细编辑（Expense / 明细ID）", true, GenExpenseID(), true},
		{"7 批量删除（零值，摘要记筛选条件与笔数）", false, 0, false},
		{"8 本位币金额重算（零值，批量）", false, 0, false},
		{"9 支出类型维护（ExpenseCategory / 类型ID）", true, GenCategoryID(), true},
		{"11 数据导出（零值，批量）", false, 0, false},
	}
	for _, c := range cases {
		if got := IsValid(c.objectID); got != c.wantIsUsed {
			t.Errorf("%s：IsValid(对象ID=%d) = %v, 期望 %v", c.name, c.objectID, got, c.wantIsUsed)
		}
		// K-2 跳转的判据即 IsValid：零值不提供跳转入口
		canJump := IsValid(c.objectID)
		if canJump != c.hasObject {
			t.Errorf("%s：可跳转 = %v, 期望 %v", c.name, canJump, c.hasObject)
		}
	}
}

// TestConsumerDtoJSONPrecision 走查 api/http/dto 的 JSON 契约（分层设计 L6 dto 承载、
// T34② 前后端分离下的 JSON 契约）。
//
// 这是本包最容易被忽略的一条：18 位 ID 超过 IEEE-754 双精度的安全整数上界 2^53，
// 而 JavaScript 的 Number 即 float64。裸传 int64 会让前端拿到一个与库里不同的 ID，
// 且全程不报错——归属校验（不变式 3）会因此对着一个不存在的 ID 做判定。
func TestConsumerDtoJSONPrecision(t *testing.T) {
	id := GenExpenseID()

	// 反例：裸 int64 —— 经前端 Number（float64）往返后失真
	type badDTO struct {
		ExpenseID int64 `json:"expense_id"`
	}
	raw, err := json.Marshal(badDTO{ExpenseID: id})
	if err != nil {
		t.Fatalf("序列化报错: %v", err)
	}
	var asNumber struct {
		ExpenseID float64 `json:"expense_id"`
	}
	if err := json.Unmarshal(raw, &asNumber); err != nil {
		t.Fatalf("按 float64 解析报错: %v", err)
	}
	if int64(asNumber.ExpenseID) == id {
		// 18 位 ID 必然 > 2^53，理应失真；若这里相等，说明 ID 形态变短了，
		// 那 dto 的字符串约定虽仍安全，但本用例的前提需要重新确认。
		t.Logf("提示：ID %d 经 float64 往返未失真，请确认当前 ID 位数（%d 位）是否仍 > 2^53",
			id, len(strconv.FormatInt(id, 10)))
	} else {
		t.Logf("已确认裸 int64 会失真：%d -> %d（偏差 %d），故 dto 必须用字符串",
			id, int64(asNumber.ExpenseID), int64(asNumber.ExpenseID)-id)
	}

	// 正例一：json:",string" 标签
	type goodTagDTO struct {
		ExpenseID int64 `json:"expense_id,string"`
	}
	tagged, err := json.Marshal(goodTagDTO{ExpenseID: id})
	if err != nil {
		t.Fatalf("序列化报错: %v", err)
	}
	want := `{"expense_id":"` + String(id) + `"}`
	if string(tagged) != want {
		t.Errorf("string 标签序列化 = %s, 期望 %s", tagged, want)
	}
	var backTagged goodTagDTO
	if err := json.Unmarshal(tagged, &backTagged); err != nil {
		t.Fatalf("反序列化报错: %v", err)
	}
	if backTagged.ExpenseID != id {
		t.Errorf("string 标签往返失真: %d -> %d", id, backTagged.ExpenseID)
	}

	// 正例二：dto 侧用 string 字段 + 本包的 String / ParseString 转换
	type goodStrDTO struct {
		ExpenseID string `json:"expense_id"`
	}
	strDTO := goodStrDTO{ExpenseID: String(id)}
	body, err := json.Marshal(strDTO)
	if err != nil {
		t.Fatalf("序列化报错: %v", err)
	}
	var backStr goodStrDTO
	if err := json.Unmarshal(body, &backStr); err != nil {
		t.Fatalf("反序列化报错: %v", err)
	}
	got, ok := ParseString(backStr.ExpenseID)
	if !ok {
		t.Fatalf("ParseString(%q) 失败", backStr.ExpenseID)
	}
	if got != id {
		t.Errorf("字符串字段往返失真: %d -> %d", id, got)
	}
}

// TestConsumerHandlerParsePathParam 走查 api/http/handler 从 URL 路径参数还原 ID
// 的用法：非法输入必须能被识别为「用户输入错误」，而不是静默变成一个合法 ID。
func TestConsumerHandlerParsePathParam(t *testing.T) {
	// classify 模拟 handler 的处理：解析失败即映射到 base/errs 的「用户输入错误」档
	classify := func(param string) string {
		id, ok := ParseString(param)
		if !ok {
			return "用户输入错误"
		}
		if !IsValid(id) {
			// ParseString 已保证正数，这里是双保险；两条判据必须同结论
			return "用户输入错误"
		}
		return "ok"
	}

	legal := String(GenExpenseID())
	cases := []struct{ param, want string }{
		{legal, "ok"},
		{"", "用户输入错误"},
		{"0", "用户输入错误"},
		{"-1", "用户输入错误"},
		{"abc", "用户输入错误"},
		{"9223372036854775808", "用户输入错误"}, // 溢出：不得钳成 MaxInt64 后放行
		{"1 or 1=1", "用户输入错误"},            // 注入形态一并被形态校验拦下
	}
	for _, c := range cases {
		if got := classify(c.param); got != c.want {
			t.Errorf("classify(%q) = %q, 期望 %q", c.param, got, c.want)
		}
	}
}

// TestConsumerStoreCursorIteration 走查 store 的游标迭代（分层设计 store 承载
// 「Expense 提供游标迭代（I-7 与 K-3 不全量装载）」）：用「ID > 上次游标」翻页。
//
// 该用法依赖 ID 单调递增。若 ID 无序，翻页会漏行或重复——I-7 的可续跑批处理
// 会漏算部分明细，且不会报错。
func TestConsumerStoreCursorIteration(t *testing.T) {
	// 模拟按插入顺序落库的一批明细
	const total = 500
	rows := make([]int64, 0, total)
	for i := 0; i < total; i++ {
		rows = append(rows, GenExpenseID())
	}

	// 单调性：ID 顺序与插入顺序一致，故「ID > 游标」可作稳定游标
	for i := 1; i < total; i++ {
		if rows[i] <= rows[i-1] {
			t.Fatalf("第 %d 条 ID %d 未大于前一条 %d，游标迭代不成立", i, rows[i], rows[i-1])
		}
	}

	// 按每页 37 条翻页，要求不重不漏
	const pageSize = 37
	var cursor int64 // 零值 = 从头开始
	visited := make(map[int64]int, total)
	pages := 0
	for {
		page := make([]int64, 0, pageSize)
		for _, id := range rows {
			if id > cursor {
				page = append(page, id)
				if len(page) == pageSize {
					break
				}
			}
		}
		if len(page) == 0 {
			break
		}
		pages++
		for _, id := range page {
			visited[id]++
		}
		cursor = page[len(page)-1]
		if pages > total {
			t.Fatal("翻页次数异常，疑似死循环")
		}
	}

	if len(visited) != total {
		t.Errorf("游标遍历覆盖 %d 条, 期望 %d 条（有遗漏）", len(visited), total)
	}
	for id, times := range visited {
		if times != 1 {
			t.Errorf("ID %d 被访问 %d 次, 期望恰好 1 次", id, times)
		}
	}
	if wantPages := (total + pageSize - 1) / pageSize; pages != wantPages {
		t.Errorf("翻页次数 = %d, 期望 %d", pages, wantPages)
	}
}

// TestConsumerNoIDReuseAcrossEntities 走查 AuditLog.对象ID 跨实体存放的前提：
// 五类实体的 ID 不重叠，故一个对象ID 至多对应一个实体的一条记录。
func TestConsumerNoIDReuseAcrossEntities(t *testing.T) {
	// 模拟一次典型操作序列产生的各类 ID
	userID := GenUserID()
	categoryID := GenCategoryID()
	fileID := GenFileID()
	auditID := GenAuditID()
	expenseID := GenExpenseID()

	all := map[string]int64{
		"用户ID": userID, "类型ID": categoryID, "文件ID": fileID,
		"审计ID": auditID, "明细ID": expenseID,
	}
	seen := make(map[int64]string, len(all))
	for name, id := range all {
		if !IsValid(id) {
			t.Errorf("%s = %d 不合法", name, id)
		}
		if owner, dup := seen[id]; dup {
			t.Errorf("%s 与 %s 撞号（均为 %d）", name, owner, id)
		}
		seen[id] = name
	}
	if len(seen) != len(all) {
		t.Errorf("五类 ID 唯一值 = %d, 期望 %d", len(seen), len(all))
	}
}

// TestConsumerZeroValueIsUsableSentinel 走查上层直接用零值表达「无 ID」的做法：
// entity 结构体零值可用，不需要额外的「是否已设置」标志位。
func TestConsumerZeroValueIsUsableSentinel(t *testing.T) {
	type expense struct {
		ExpenseID     int64
		SourceAuditID int64
		SourceFileID  int64 // 可空
		CategoryID    int64 // 可空：空即未分类（G-2）
	}

	var zero expense
	// 零值实体的四个 ID 字段全部为「无 ID」
	for name, id := range map[string]int64{
		"明细ID": zero.ExpenseID, "来源审计ID": zero.SourceAuditID,
		"来源文件ID": zero.SourceFileID, "支出类型ID": zero.CategoryID,
	} {
		if IsValid(id) {
			t.Errorf("零值实体的 %s 应为无 ID，实际 %d", name, id)
		}
	}

	// G-2：支出类型ID 为空即未分类，字典中不设「未分类」记录
	e := expense{ExpenseID: GenExpenseID(), SourceAuditID: GenAuditID()}
	if IsValid(e.CategoryID) {
		t.Error("未打标的明细其支出类型ID 应为零值（未分类）")
	}
	// 打标后
	e.CategoryID = GenCategoryID()
	if !IsValid(e.CategoryID) {
		t.Error("打标后的支出类型ID 应合法")
	}
}
