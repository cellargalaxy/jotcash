package dedup

import (
	"reflect"
	"testing"
	"time"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/entity"
)

// 本文件验证 Check 的行为，覆盖 8.3 的四条链路与 E-1/E-2 的每一条约束。

// withID 给一条明细补上 ID，模拟「已入库」的候选。
func withID(e entity.Expense, id int64) entity.Expense {
	e.ID = id
	return e
}

// mustAmount 解析金额，失败即终止用例。
func mustAmount(t *testing.T, text string) decimal.Decimal {
	t.Helper()
	d, err := decimal.Parse(text)
	if err != nil {
		t.Fatalf("解析金额 %q 报错: %v", text, err)
	}
	return d
}

// deletedAtSample 返回一个非零的删除时间，用于构造「已删除」的明细。
//
// 取固定时刻而非 time.Now()：本包的测试不应依赖运行时刻。
func deletedAtSample() time.Time {
	return time.Date(2026, 9, 1, 10, 30, 0, 0, time.UTC)
}

// TestCheckDetectsInStoreConflict 验证与已入库明细的重复（E-2 前半）。
func TestCheckDetectsInStoreConflict(t *testing.T) {
	row := newRow(userA, "2026-09-07", "100.50", "CNY")
	// 候选写法不同但数值相同——这是判重必须命中的核心情形
	stored := withID(newRow(userA, "2026-09-07", "100.5", "CNY"), 260907000000000101)

	result := Check(Request{
		Rows:       []entity.Expense{row},
		Candidates: []entity.Expense{stored},
	})

	if !result.HasAny() {
		t.Fatal("100.50 与已入库的 100.5 是同一笔金额，应判为重复")
	}
	conflict, hit := result.Conflicts[0]
	if !hit {
		t.Fatal("第 0 行应命中冲突")
	}
	if len(conflict.InStore) != 1 {
		t.Fatalf("库内冲突应为 1 条，实得 %d 条", len(conflict.InStore))
	}
	if conflict.InStore[0].ID != stored.ID {
		t.Errorf("库内冲突明细ID: got %d, want %d", conflict.InStore[0].ID, stored.ID)
	}
	if len(conflict.InBatch) != 0 {
		t.Errorf("本批只有 1 行，不应有内部冲突，实得 %d 条", len(conflict.InBatch))
	}
	if conflict.RowIndex != 0 {
		t.Errorf("RowIndex: got %d, want 0", conflict.RowIndex)
	}
}

// TestCheckDetectsInBatchConflict 验证本次录入内部的重复（E-2 后半）。
//
// 这一半最容易被漏实现：只查库不查批内，同一份账单里的两行重复会直接入库。
func TestCheckDetectsInBatchConflict(t *testing.T) {
	rows := []entity.Expense{
		newRow(userA, "2026-09-07", "100.50", "CNY"),
		newRow(userA, "2026-09-08", "200.00", "CNY"),  // 无冲突
		newRow(userA, "2026-09-07", "100.500", "CNY"), // 与第 0 行同一笔
	}

	result := Check(Request{Rows: rows}) // 候选为空，只查批内

	if len(result.Conflicts) != 2 {
		t.Fatalf("第 0、2 行互为重复，应有 2 行命中，实得 %d 行", len(result.Conflicts))
	}
	if _, hit := result.Conflicts[1]; hit {
		t.Error("第 1 行与其他行四要素不同，不应命中")
	}

	// 冲突必须是双向的：第 0 行指向第 2 行，第 2 行也指向第 0 行
	first := result.Conflicts[0]
	if !reflect.DeepEqual(first.InBatchIndexes, []int{2}) {
		t.Errorf("第 0 行的批内冲突下标: got %v, want [2]", first.InBatchIndexes)
	}
	third := result.Conflicts[2]
	if !reflect.DeepEqual(third.InBatchIndexes, []int{0}) {
		t.Errorf("第 2 行的批内冲突下标: got %v, want [0]", third.InBatchIndexes)
	}

	// 明细内容与下标必须一一对应
	if len(first.InBatch) != len(first.InBatchIndexes) {
		t.Fatalf("InBatch 与 InBatchIndexes 长度应相等: %d vs %d",
			len(first.InBatch), len(first.InBatchIndexes))
	}
	if !first.InBatch[0].Amount.Equal(rows[2].Amount) {
		t.Error("第 0 行的批内冲突明细应是第 2 行")
	}

	// 库内冲突为空——这是与 InStore 分开返回的意义所在
	if len(first.InStore) != 0 {
		t.Errorf("候选为空时不应有库内冲突，实得 %d 条", len(first.InStore))
	}
}

// TestCheckSelfIsNotDuplicate 验证一行不与自己重复。
//
// 若实现时忘了在批内互查中排除自身下标，**每一行**都会自报重复，
// 预览页会全页飘红——这个错误很显眼，但正因显眼容易被「临时注释掉判重」绕过。
func TestCheckSelfIsNotDuplicate(t *testing.T) {
	rows := []entity.Expense{newRow(userA, "2026-09-07", "100.50", "CNY")}

	result := Check(Request{Rows: rows})

	if result.HasAny() {
		t.Errorf("单行输入不应有任何冲突，实得 %d 行命中", len(result.Conflicts))
	}
}

// TestCheckExcludesEditedSelf 验证 F-5 逐笔编辑时排除被编辑的这笔自身（8.3）。
//
// 8.3 原文：「排除被编辑的这笔自身，否则必然自报重复」。
func TestCheckExcludesEditedSelf(t *testing.T) {
	const editingID int64 = 260907000000000201
	// 编辑后的行，四要素未变
	row := withID(newRow(userA, "2026-09-07", "100.50", "CNY"), editingID)
	// 候选里就有它自己（store 按四要素取候选时必然把它取出来）
	itself := withID(newRow(userA, "2026-09-07", "100.50", "CNY"), editingID)
	other := withID(newRow(userA, "2026-09-07", "100.50", "CNY"), 260907000000000202)

	t.Run("排除自身后不报重复", func(t *testing.T) {
		result := Check(Request{
			Rows:       []entity.Expense{row},
			Candidates: []entity.Expense{itself},
			ExcludeID:  editingID,
		})
		if result.HasAny() {
			t.Error("编辑态应排除被编辑的这笔自身，不得自报重复")
		}
	})

	t.Run("排除自身但仍报别的重复", func(t *testing.T) {
		result := Check(Request{
			Rows:       []entity.Expense{row},
			Candidates: []entity.Expense{itself, other},
			ExcludeID:  editingID,
		})
		if !result.HasAny() {
			t.Fatal("除自身外还有一条同四要素明细，应报重复")
		}
		conflict := result.Conflicts[0]
		if len(conflict.InStore) != 1 {
			t.Fatalf("应只报另一条，实得 %d 条", len(conflict.InStore))
		}
		if conflict.InStore[0].ID != other.ID {
			t.Errorf("报出的应是另一条明细 %d，实得 %d", other.ID, conflict.InStore[0].ID)
		}
	})

	t.Run("录入态不排除任何候选", func(t *testing.T) {
		// ExcludeID 为零值时，即便候选中有 ID 为 0 的行也不该被误排除
		zeroIDCandidate := newRow(userA, "2026-09-07", "100.50", "CNY") // ID 为零值
		result := Check(Request{
			Rows:       []entity.Expense{newRow(userA, "2026-09-07", "100.50", "CNY")},
			Candidates: []entity.Expense{zeroIDCandidate},
		})
		if !result.HasAny() {
			t.Error("录入态 ExcludeID 为零值，不应排除任何候选")
		}
	})
}

// TestCheckNeverMatchesAcrossUsers 验证跨用户绝不比对（前提 3 纯个人数据隔离）。
//
// 这条错了是数据泄漏：A 用户会在预览页上看到 B 用户的明细内容。
func TestCheckNeverMatchesAcrossUsers(t *testing.T) {
	row := newRow(userA, "2026-09-07", "100.50", "CNY")
	// 除归属用户外四要素完全相同的另一用户明细
	otherUsers := withID(newRow(userB, "2026-09-07", "100.50", "CNY"), 260907000000000301)

	result := Check(Request{
		Rows:       []entity.Expense{row},
		Candidates: []entity.Expense{otherUsers},
	})

	if result.HasAny() {
		t.Error("跨用户绝不比对（前提 3）：不同归属用户的明细不应判为重复")
	}

	// 批内同理
	batch := Check(Request{Rows: []entity.Expense{
		newRow(userA, "2026-09-07", "100.50", "CNY"),
		newRow(userB, "2026-09-07", "100.50", "CNY"),
	}})
	if batch.HasAny() {
		t.Error("批内跨用户同样不应比对")
	}
}

// TestCheckReportsBothConflictKindsSeparately 验证两类冲突同时存在时分开返回。
//
// D-6 对二者的展示形态不同（库内并列已入库明细，批内指向同批另一行），
// 合并成一个集合会让前端无法区分。
func TestCheckReportsBothConflictKindsSeparately(t *testing.T) {
	rows := []entity.Expense{
		newRow(userA, "2026-09-07", "100.50", "CNY"),
		newRow(userA, "2026-09-07", "100.5", "CNY"), // 与第 0 行批内重复
	}
	stored := withID(newRow(userA, "2026-09-07", "100.500", "CNY"), 260907000000000401)

	result := Check(Request{Rows: rows, Candidates: []entity.Expense{stored}})

	if len(result.Conflicts) != 2 {
		t.Fatalf("两行都应命中，实得 %d 行", len(result.Conflicts))
	}
	for idx := range rows {
		conflict := result.Conflicts[idx]
		if len(conflict.InStore) != 1 {
			t.Errorf("第 %d 行库内冲突应为 1 条，实得 %d 条", idx, len(conflict.InStore))
		}
		if len(conflict.InBatch) != 1 {
			t.Errorf("第 %d 行批内冲突应为 1 条，实得 %d 条", idx, len(conflict.InBatch))
		}
		if !conflict.HasConflict() {
			t.Errorf("第 %d 行 HasConflict 应为真", idx)
		}
	}
}

// TestCheckMultipleConflictsKeepInputOrder 验证同一行命中多条时保持输入顺序。
//
// 顺序不稳定会让 D-6 的并列展示每次刷新都变样，人工比对时容易看错行。
func TestCheckMultipleConflictsKeepInputOrder(t *testing.T) {
	row := newRow(userA, "2026-09-07", "100.50", "CNY")
	candidates := []entity.Expense{
		withID(newRow(userA, "2026-09-07", "100.5", "CNY"), 260907000000000503),
		withID(newRow(userA, "2026-09-08", "100.5", "CNY"), 260907000000000504), // 日期不同，不命中
		withID(newRow(userA, "2026-09-07", "100.500", "CNY"), 260907000000000501),
		withID(newRow(userA, "2026-09-07", "1.005e2", "CNY"), 260907000000000502),
	}

	// 多跑几次，确认顺序不随 map 遍历而变
	for attempt := range 5 {
		result := Check(Request{Rows: []entity.Expense{row}, Candidates: candidates})
		conflict := result.Conflicts[0]
		if len(conflict.InStore) != 3 {
			t.Fatalf("第 %d 次: 应命中 3 条，实得 %d 条", attempt, len(conflict.InStore))
		}
		gotIDs := []int64{conflict.InStore[0].ID, conflict.InStore[1].ID, conflict.InStore[2].ID}
		wantIDs := []int64{260907000000000503, 260907000000000501, 260907000000000502}
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Errorf("第 %d 次: 库内冲突顺序应与 Candidates 一致\ngot  %v\nwant %v",
				attempt, gotIDs, wantIDs)
		}
	}
}

// TestResultRowIndexesAreSorted 验证 RowIndexes 升序且只含命中行。
//
// 用 **16 个命中行**而非三五个：Go 的 map 遍历顺序虽然随机，但键少时「恰好
// 升序」的概率很高（实测 3 个键约 75%、5 个键约 50%、8 个键约 13%），
// 未排序的实现会大概率蒙混过关，变成一个间歇性失败的测试——那比没有测试更糟。
// 16 个键时自然升序的概率实测为 0，配合多次重跑，未排序即必然失败。
func TestResultRowIndexesAreSorted(t *testing.T) {
	// 32 行，偶数行命中、奇数行不命中，共 16 个命中行
	const total = 32
	var rows []entity.Expense
	var candidates []entity.Expense
	var want []int
	for i := range total {
		// 每行金额不同，保证不会彼此批内重复，只受候选控制
		row := newRow(userA, "2026-09-07", decimal.FromInt(int64(i+1)).String(), "CNY")
		rows = append(rows, row)
		if i%2 == 0 {
			want = append(want, i)
			candidates = append(candidates, withID(row, int64(260907000000000600+i)))
		}
	}
	// 打乱候选顺序，确保命中行的插入顺序不是天然升序
	for i, j := 0, len(candidates)-1; i < j; i, j = i+1, j-1 {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	}

	// 多跑几次：map 遍历顺序随机，未排序的实现会间歇性失败
	for attempt := range 20 {
		result := Check(Request{Rows: rows, Candidates: candidates})
		got := result.RowIndexes()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("第 %d 次: RowIndexes 应为升序的命中行\ngot  %v\nwant %v", attempt, got, want)
		}
	}

	// 无冲突时返回空集合
	empty := Check(Request{Rows: rows}).RowIndexes()
	if len(empty) != 0 {
		t.Errorf("无冲突时 RowIndexes 应为空，实得 %v", empty)
	}
}

// TestCheckHandlesEmptyInput 验证空输入不 panic、返回可用的空结果。
//
// 空输入在真实链路中会出现：F-5 编辑态若上层校验未过就没有行可查；
// 导入了一个空文件时 Rows 也为空。
func TestCheckHandlesEmptyInput(t *testing.T) {
	cases := []struct {
		name string
		req  Request
	}{
		{"全空", Request{}},
		{"只有候选没有待查行", Request{Candidates: []entity.Expense{
			withID(newRow(userA, "2026-09-07", "100.50", "CNY"), 260907000000000701)}}},
		{"只有待查行没有候选", Request{Rows: []entity.Expense{
			newRow(userA, "2026-09-07", "100.50", "CNY")}}},
		{"空切片非 nil", Request{Rows: []entity.Expense{}, Candidates: []entity.Expense{}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := Check(c.req)
			if result.HasAny() {
				t.Errorf("应无冲突，实得 %d 行命中", len(result.Conflicts))
			}
			// 映射必须可用而非 nil——调用方会直接对它取值
			if result.Conflicts == nil {
				t.Error("Conflicts 不应为 nil，调用方会直接索引它")
			}
			if _, hit := result.Conflicts[0]; hit {
				t.Error("空结果不应含任何行")
			}
			if got := result.RowIndexes(); len(got) != 0 {
				t.Errorf("RowIndexes 应为空，实得 %v", got)
			}
		})
	}
}

// TestCheckDoesNotMutateInput 验证 Check 不修改入参（E-2「只标记不处置」）。
//
// 「不处置」的最低要求是不动数据。若哪天有人在 Check 里「顺手」把重复行标记掉
// 或排序，本用例会失败。
func TestCheckDoesNotMutateInput(t *testing.T) {
	rows := []entity.Expense{
		newRow(userA, "2026-09-07", "100.50", "CNY"),
		newRow(userA, "2026-09-07", "100.5", "CNY"),
	}
	candidates := []entity.Expense{
		withID(newRow(userA, "2026-09-07", "100.500", "CNY"), 260907000000000801),
	}
	// 深拷贝一份作为对照（Expense 是纯值结构体，逐元素复制即可）
	rowsBefore := append([]entity.Expense(nil), rows...)
	candidatesBefore := append([]entity.Expense(nil), candidates...)

	result := Check(Request{Rows: rows, Candidates: candidates})
	if !result.HasAny() {
		t.Fatal("用例前提：本应命中冲突")
	}

	if len(rows) != len(rowsBefore) || len(candidates) != len(candidatesBefore) {
		t.Fatal("Check 不应增删入参元素")
	}
	for i := range rows {
		if KeyOf(rows[i]) != KeyOf(rowsBefore[i]) || rows[i].ID != rowsBefore[i].ID {
			t.Errorf("Rows[%d] 被修改", i)
		}
	}
	for i := range candidates {
		if KeyOf(candidates[i]) != KeyOf(candidatesBefore[i]) ||
			candidates[i].ID != candidatesBefore[i].ID {
			t.Errorf("Candidates[%d] 被修改", i)
		}
	}

	// 结果中的切片不得与其他行、或与入参 Candidates 共享底层数组：
	// 调用方修改某一行的结果不应波及另一行，更不应改到入参。
	//
	// 用**改元素**而非 append 来检测：append 在切片已满时会重新分配，
	// 共享的实现照样能通过，这个检测方向是假的。
	first := result.Conflicts[0]
	second := result.Conflicts[1]
	if len(first.InStore) != 1 || len(second.InStore) != 1 {
		t.Fatal("用例前提：两行都应各有 1 条库内冲突")
	}
	const probeID int64 = 999
	first.InStore[0].ID = probeID
	if second.InStore[0].ID == probeID {
		t.Error("不同行的 InStore 共享底层数组：改了第 0 行的结果，第 1 行也被改到")
	}
	if candidates[0].ID == probeID {
		t.Error("InStore 与入参 Candidates 共享底层数组：改了结果，入参也被改到")
	}
}

// TestCheckCoversFourEntryPaths 走查 8.3 的四条链路共用同一套逻辑。
//
// 四条链路的差异只在 Rows 与 ExcludeID 的取值上，本用例把它们并列，
// 确认没有哪条链路需要特殊分支。
func TestCheckCoversFourEntryPaths(t *testing.T) {
	stored := withID(newRow(userA, "2026-09-07", "100.50", "CNY"), 260907000000000901)
	same := newRow(userA, "2026-09-07", "100.50", "CNY")

	cases := []struct {
		name       string
		req        Request
		wantHit    bool
		wantReason string
	}{
		{
			name:       "文件导入：多行，不排除自身",
			req:        Request{Rows: []entity.Expense{same, same}, Candidates: []entity.Expense{stored}},
			wantHit:    true,
			wantReason: "两行彼此重复且都与库内重复",
		},
		{
			name:       "手工录入：单行，不排除自身",
			req:        Request{Rows: []entity.Expense{same}, Candidates: []entity.Expense{stored}},
			wantHit:    true,
			wantReason: "与库内明细重复",
		},
		{
			name: "复制已删除明细：原明细不在候选内",
			// 8.3 注：被复制的原明细已删除、不在比对范围内，不会与自身误报
			req:        Request{Rows: []entity.Expense{same}},
			wantHit:    false,
			wantReason: "候选里没有已删除的原明细",
		},
		{
			name: "F-5 逐笔编辑：排除自身",
			req: Request{
				Rows:       []entity.Expense{withID(same, stored.ID)},
				Candidates: []entity.Expense{stored},
				ExcludeID:  stored.ID,
			},
			wantHit:    false,
			wantReason: "候选中只有自身，已排除",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := Check(c.req)
			if result.HasAny() != c.wantHit {
				t.Errorf("命中情况: got %t, want %t（%s）", result.HasAny(), c.wantHit, c.wantReason)
			}
		})
	}
}

// TestCheckMatchesNegativeAndZeroAmounts 验证负金额与零金额同样参与判重。
//
// 退款、冲正是负金额，它们重复导入的后果与正常支出一样严重；
// 零金额（如全额优惠）也须判重。若实现里有「金额 > 0 才判重」之类的隐含前提，
// 本用例会失败。
func TestCheckMatchesNegativeAndZeroAmounts(t *testing.T) {
	cases := []struct{ name, a, b string }{
		{"负金额", "-33.34", "-33.340"},
		{"零金额", "0", "0.00"},
		{"负零与零", "-0", "0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := Check(Request{Rows: []entity.Expense{
				newRow(userA, "2026-09-07", c.a, "CNY"),
				newRow(userA, "2026-09-07", c.b, "CNY"),
			}})
			if !result.HasAny() {
				t.Errorf("%s 与 %s 是同一笔金额，应判为重复", c.a, c.b)
			}
		})
	}
}

// TestCheckMatchesThreeDecimalCurrencyEndToEnd 端到端验证 3 位小数币种能走通判重。
//
// 与 keyer_test.go 中的键级用例互补：那里验证键能算出来，这里验证整条 Check
// 路径不会在中途因位数问题失败或漏判。
func TestCheckMatchesThreeDecimalCurrencyEndToEnd(t *testing.T) {
	rows := []entity.Expense{
		newRow(userA, "2026-09-07", "12.345", "KWD"),
		newRow(userA, "2026-09-07", "12.3450", "KWD"), // 同一笔
		newRow(userA, "2026-09-07", "12.346", "KWD"),  // 差一个最小分位，不是重复
	}

	result := Check(Request{Rows: rows})

	if len(result.Conflicts) != 2 {
		t.Fatalf("第 0、1 行互为重复，应有 2 行命中，实得 %d 行", len(result.Conflicts))
	}
	if _, hit := result.Conflicts[2]; hit {
		t.Error("12.346 与 12.345 相差一个最小分位，不应判为重复")
	}
}

// TestCheckTreatsZeroValueFieldsAsOwnGroup 验证零值字段不被特殊处理。
//
// 本包不做合法性校验（那是 dto 与 service 的职责，准则 1 要求单一校验入口）。
// 零值日期、零值币种的行照常参与分组：两行同为零值即为同键，与非零值行不同键。
func TestCheckTreatsZeroValueFieldsAsOwnGroup(t *testing.T) {
	var zeroDate calendar.Date
	var zeroCode currency.Code
	zeroRow := entity.Expense{
		OwnerUserID: userA, SpendDate: zeroDate,
		Amount: decimal.MustParse("100.50"), Currency: zeroCode,
	}
	normalRow := newRow(userA, "2026-09-07", "100.50", "CNY")

	t.Run("两个零值行互为重复", func(t *testing.T) {
		result := Check(Request{Rows: []entity.Expense{zeroRow, zeroRow}})
		if !result.HasAny() {
			t.Error("四要素完全相同（含零值）的两行应判为重复")
		}
	})

	t.Run("零值行不与正常行撞键", func(t *testing.T) {
		result := Check(Request{Rows: []entity.Expense{zeroRow, normalRow}})
		if result.HasAny() {
			t.Error("零值日期与币种的行不应与正常行判为重复")
		}
	})
}

// TestCheckScalesLinearly 用一批较大的输入确认实现不是平方级。
//
// 不测绝对耗时（受机器影响不稳定），而是确认在数百行规模下能正常返回且结论正确。
// 若实现退化成两两比较，这个规模仍会通过——本用例的价值在于同时验证
// 大批量下的分组正确性：每 10 行一组共 50 组，每组内 10 行互为重复。
func TestCheckScalesLinearly(t *testing.T) {
	const groups, perGroup = 50, 10
	rows := make([]entity.Expense, 0, groups*perGroup)
	for g := range groups {
		// 每组用不同金额区分，组内 10 行金额相同
		amount := decimal.FromInt(int64(g + 1)).String()
		for range perGroup {
			rows = append(rows, newRow(userA, "2026-09-07", amount, "CNY"))
		}
	}

	result := Check(Request{Rows: rows})

	if len(result.Conflicts) != groups*perGroup {
		t.Fatalf("每组 %d 行互为重复，应全部命中 %d 行，实得 %d 行",
			perGroup, groups*perGroup, len(result.Conflicts))
	}
	// 每行的批内冲突数应为组内其余行数
	for idx, conflict := range result.Conflicts {
		if len(conflict.InBatch) != perGroup-1 {
			t.Fatalf("第 %d 行的批内冲突应为 %d 条，实得 %d 条",
				idx, perGroup-1, len(conflict.InBatch))
		}
	}
}
