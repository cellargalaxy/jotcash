package dedup

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/entity"
)

// 本文件是走查性质的测试，钉住几件无法用普通运行期断言表达的约定：
//   1. 依赖边界（分层 §六 L2 表 rule/dedup 行的依赖列只有 entity）；
//   2. 本包零 IO、不打日志（L2 纯逻辑层，可脱库单测）；
//   3. 承载不外溢（本包只做 8.3 判重一件事，不做校验、不做处置）；
//   4. 下游三个协作方（store / service/intake / service/expense）的用法能走通。
//
// 前三条用 AST 扫描本包源码实现——注释里写「不依赖 X」是没有约束力的，
// 下一个人加一行 import 就破了。

// 本包在**非测试文件**中允许出现的 import。改这张表等于改分层的依赖列，
// 必须先改文档。
var allowedImports = map[string]string{
	//标准库
	"sort":    "RowIndexes 的升序（map 遍历顺序随机，输出顺序必须确定）",
	"strconv": "判重键中归属用户ID 的十进制文本化",
	"strings": "四要素拼接判重键的 Builder",
	//仓库内包
	"github.com/cellargalaxy/jotcash/internal/entity": "分层依赖列的唯一一项：Expense 实体",
}

// TestDependencyBoundary 双向锁死依赖边界：
//   - 出现表外的 import 即失败（越界）；
//   - 表内的仓库内包若没有任何文件用到，也失败（依赖列虚高）。
//
// 特别地，本用例会拦下几类具体的越界：
//   - 依赖 store（L2 不碰 IO；比对范围「在库且未删除」由 store 的查重候选口径
//     落实，本包只接收候选切片）；
//   - 依赖 base/errs（本包无错可返——重复不是错误，是待人工裁定的提示，
//     见 E-2「只提示不阻断」）；
//   - 依赖 currency / base/decimal / base/calendar（四要素的类型由 entity 字段
//     自带，本包只调它们已有的 String()，不需要各自的包级函数；显式 import
//     会让依赖列从 1 项虚涨到 4 项，掩盖真正的耦合面）。
func TestDependencyBoundary(t *testing.T) {
	files := packageFiles(t, false)
	if len(files) == 0 {
		t.Fatal("未找到本包的非测试源文件，用例前提不成立")
	}

	usedRepoImports := make(map[string]bool)
	for _, file := range files {
		for _, imp := range parseImports(t, file) {
			reason, ok := allowedImports[imp]
			if !ok {
				t.Errorf("%s 引入了依赖边界外的包 %q。\n"+
					"分层 §六 L2 表 rule/dedup 的依赖列只有 entity；"+
					"若确需新增，先改文档再改这张表。", filepath.Base(file), imp)
				continue
			}
			if reason == "" {
				t.Errorf("allowedImports[%q] 缺少理由说明", imp)
			}
			if strings.HasPrefix(imp, "github.com/cellargalaxy/jotcash/") {
				usedRepoImports[imp] = true
			}
		}
	}

	for imp := range allowedImports {
		if !strings.HasPrefix(imp, "github.com/cellargalaxy/jotcash/") {
			continue
		}
		if !usedRepoImports[imp] {
			t.Errorf("allowedImports 列了仓库内包 %q，但本包没有任何非测试文件用到它——"+
				"依赖列虚高，应从表中删除", imp)
		}
	}
}

// TestNoIODependency 断言本包零外部 IO，可脱库单测（分层 L2 的定义）。
//
// 判重逻辑一旦能自己查库，「比对范围为在库且未删除」就有了两处实现
// （store 的查重候选口径 + 本包），两处口径将来必然漂移。不 import 任何
// IO 相关的包，把这件事变成编译期约束。
func TestNoIODependency(t *testing.T) {
	forbidden := map[string]string{
		"database/sql": "本包不查库，候选由 store 按查重候选口径取回后传入",
		"net/http":     "L2 不碰传输层",
		"os":           "L2 不碰文件系统",
		"io":           "L2 无 IO",
		"github.com/cellargalaxy/jotcash/internal/store": "本包不 import store（依赖方向相反）",
		//日志：L2 纯逻辑层不打日志，判重结果由调用方决定是否记录
		"github.com/sirupsen/logrus":                        "L2 纯逻辑层不打日志",
		"github.com/cellargalaxy/jotcash/internal/base/log": "L2 纯逻辑层不打日志",
		"log":      "L2 纯逻辑层不打日志",
		"log/slog": "L2 纯逻辑层不打日志",
	}

	for _, file := range packageFiles(t, false) {
		for _, imp := range parseImports(t, file) {
			if reason, bad := forbidden[imp]; bad {
				t.Errorf("%s 引入了 %q。%s", filepath.Base(file), imp, reason)
			}
		}
	}
}

// TestNoTimeDependency 断言本包不碰 time。
//
// 判重是**纯函数**：同样的入参在任何时刻都得到同样的结论。若引入 time
// （比如「只比对最近 N 天」这类想法），判重结果会随运行时刻而变，同一份账单
// 今天导入报重复、明天导入不报，且没有任何报错。
//
// 「在库且未删除」这条比对范围也不由本包按时间判定——DeletedAt 的过滤在
// store 侧完成，见 TestCandidatesRangeIsCallerDuty。
func TestNoTimeDependency(t *testing.T) {
	for _, file := range packageFiles(t, false) {
		for _, imp := range parseImports(t, file) {
			if imp == "time" {
				t.Errorf("%s 引入了 time。判重必须是纯函数——"+
					"结论随运行时刻而变会让同一份账单今天报重复、明天不报，且不会有任何报错。",
					filepath.Base(file))
			}
		}
	}
}

// TestExportedSurface 锁死本包的导出面，防止承载外溢。
//
// 分层 §六 给本包的承载只有 8.3 判重一件事。多出来的导出符号（尤其是
// 「自动去重」「合并重复」这类）都意味着承载外溢——E-2 明写一律人工裁定。
func TestExportedSurface(t *testing.T) {
	want := map[string]bool{
		//判重入口
		"Check":   true,
		"Request": true,
		"Result":  true,
		//冲突记录
		"Conflict": true,
		//四要素键（供 store 拼查重候选的查询条件，避免两处口径漂移）
		"Key":   true,
		"KeyOf": true,
		"Keys":  true,
	}
	//方法不计入包级导出面，但要确认它们挂在预期的类型上
	wantMethods := map[string]bool{
		"Conflict.HasConflict": true,
		"Result.HasAny":        true,
		"Result.RowIndexes":    true,
		"Key.String":           true,
	}

	got := make(map[string]bool)
	gotMethods := make(map[string]bool)
	fset := token.NewFileSet()
	for _, file := range packageFiles(t, false) {
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", file, err)
		}
		for _, decl := range parsed.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					continue
				}
				if d.Recv == nil {
					got[d.Name.Name] = true
					continue
				}
				gotMethods[receiverName(d)+"."+d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							got[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if name.IsExported() {
								got[name.Name] = true
							}
						}
					}
				}
			}
		}
	}

	assertSameSet(t, "包级导出符号", want, got)
	assertSameSet(t, "导出方法", wantMethods, gotMethods)
}

// TestNoDispositionAPI 断言本包不提供任何「处置」入口（E-2「只标记不处置」）。
//
// 用命名扫描而非人工review：Remove / Filter / Merge / Dedupe / Drop 这类动词
// 一旦出现在导出面上，说明有人把「自动去重」做进了规则层，而 E-2 要求
// 「是否真重复一律人工判断，只提示不阻断，不做任何自动处置」。
func TestNoDispositionAPI(t *testing.T) {
	forbiddenVerbs := []string{
		"Remove", "Delete", "Drop", "Filter", "Merge",
		"Dedupe", "Deduplicate", "Resolve", "Apply", "Block", "Reject",
	}

	fset := token.NewFileSet()
	for _, file := range packageFiles(t, false) {
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", file, err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			for _, verb := range forbiddenVerbs {
				if strings.Contains(fn.Name.Name, verb) {
					t.Errorf("%s 导出了 %q，含处置动词 %q。"+
						"E-2 规定「只标记疑似重复，是否真重复一律人工判断，"+
						"只提示不阻断，不做任何自动处置」——本包不得提供处置入口。",
						filepath.Base(file), fn.Name.Name, verb)
				}
			}
		}
	}
}

// TestCandidatesRangeIsCallerDuty 把「比对范围」的职责分工写成断言。
//
// E-1 的比对范围是「在库且未删除的明细」。这条由 store 的查重候选口径落实
// （分层 §六 L3 store ④「按『(日期, 金额, 币种)』元组集合一次取回在库未删除
// 候选」），**本包不再过滤 DeletedAt**——两处过滤等于两处口径，将来必然漂移。
//
// 本用例正面确认这个分工：传进来一条已删除的明细，本包照样比对。
// 这不是缺陷，而是分工的直接后果；调用方若传了已删除明细，是调用方违约。
func TestCandidatesRangeIsCallerDuty(t *testing.T) {
	row := newRow(userA, "2026-09-07", "100.50", "CNY")
	deleted := withID(newRow(userA, "2026-09-07", "100.50", "CNY"), 260907000000001001)
	deleted.DeletedAt = deletedAtSample()

	result := Check(Request{Rows: []entity.Expense{row}, Candidates: []entity.Expense{deleted}})

	if !result.HasAny() {
		t.Error("本包不过滤 DeletedAt——「在库且未删除」由 store 的查重候选口径落实。\n" +
			"若此处开始过滤，比对范围就有了两处实现，两处口径将来必然漂移。")
	}

	// 反向确认：源码里确实没有**引用** DeletedAt 字段。
	//
	// 扫 AST 而非原文匹配：包注释里正当地解释了「为什么不过滤 DeletedAt」，
	// 按文本匹配会把这段解释也判为违规，逼着后来人删掉注释来让测试通过——
	// 那恰恰把这条设计决策的理由抹掉了。
	fset := token.NewFileSet()
	for _, file := range packageFiles(t, false) {
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", file, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "DeletedAt" {
				t.Errorf("%s 引用了 DeletedAt 字段。比对范围「在库且未删除」由 store 落实，"+
					"本包不得重复过滤（见分层 §六 L3 store ④）。", filepath.Base(file))
			}
			return true
		})
	}
}

// TestConsumerStoreCandidateQuery 走查协作方 store：用 Keys 拼查重候选的查询条件。
//
// 对应分层 §六 L3 store ④「按『(日期, 金额, 币种)』元组集合一次取回在库未删除
// 候选，供 8.3 四条链路」。这里模拟 store 侧的用法，确认 Key 的四个字段拿来
// 直接拼 SQL 参数是够用的（都是可比较的标量，且形态与落库形态一致）。
func TestConsumerStoreCandidateQuery(t *testing.T) {
	// 一次文件导入解析出的若干行，其中两行四要素相同
	rows := []entity.Expense{
		newRow(userA, "2026-09-07", "100.50", "CNY"),
		newRow(userA, "2026-09-08", "88.00", "CNY"),
		newRow(userA, "2026-09-07", "100.5", "CNY"), // 与第 0 行同键
		newRow(userA, "2026-09-09", "12.345", "KWD"),
	}

	keys := Keys(rows)
	if len(keys) != 3 {
		t.Fatalf("4 行含 3 个唯一键，实得 %d 个——store 会多查一次", len(keys))
	}

	// store 侧把键摊成 SQL 参数：每个键 4 个占位符
	type sqlTuple struct {
		ownerUserID int64
		spendDate   string
		amount      string
		currency    string
	}
	tuples := make([]sqlTuple, 0, len(keys))
	for _, k := range keys {
		tuples = append(tuples, sqlTuple{k.OwnerUserID, k.SpendDate, k.Amount, k.Currency})
	}

	// 归属用户必须在查询条件里——否则会跨用户取候选（前提 3 数据泄漏）
	for i, tuple := range tuples {
		if tuple.ownerUserID == 0 {
			t.Errorf("第 %d 个元组缺归属用户，跨用户取候选会泄漏他人明细", i)
		}
		if tuple.spendDate == "" || tuple.currency == "" {
			t.Errorf("第 %d 个元组有空要素：date=%q currency=%q", i, tuple.spendDate, tuple.currency)
		}
	}

	// 金额形态：规范文本，对 3 位小数币种同样成立
	var kwdFound bool
	for _, tuple := range tuples {
		if tuple.currency == "KWD" {
			kwdFound = true
			if tuple.amount != "12.345" {
				t.Errorf("KWD 金额键: got %q, want %q", tuple.amount, "12.345")
			}
		}
	}
	if !kwdFound {
		t.Error("用例前提：应有一条 KWD 明细")
	}

	// store 取回候选后原样喂给 Check——这是完整的一趟
	candidates := []entity.Expense{
		withID(newRow(userA, "2026-09-07", "100.500", "CNY"), 260907000000001101),
	}
	result := Check(Request{Rows: rows, Candidates: candidates})
	if len(result.Conflicts) != 2 {
		t.Errorf("第 0、2 行既彼此重复又与库内重复，应有 2 行命中，实得 %d 行", len(result.Conflicts))
	}
}

// TestConsumerIntakePreview 走查协作方 service/intake：D-4/D-6 预览页的用法。
//
// 链路是「解析 → 判重 → 预览页高亮 → 人工勾选 → 入库」。本用例确认
// Result 的形态够渲染 D-6：能拿到命中行的行号（高亮），能拿到并列展示的明细。
func TestConsumerIntakePreview(t *testing.T) {
	rows := []entity.Expense{
		newRow(userA, "2026-09-01", "10.00", "CNY"), // 与库内重复
		newRow(userA, "2026-09-02", "20.00", "CNY"), // 无冲突
		newRow(userA, "2026-09-03", "30.00", "CNY"), // 与第 3 行批内重复
		newRow(userA, "2026-09-03", "30.000", "CNY"),
	}
	candidates := []entity.Expense{
		withID(newRow(userA, "2026-09-01", "10.00", "CNY"), 260907000000001201),
	}

	result := Check(Request{Rows: rows, Candidates: candidates})

	// D-6：按行号顺序渲染高亮，顺序必须稳定
	hitRows := result.RowIndexes()
	if len(hitRows) != 3 {
		t.Fatalf("第 0、2、3 行应命中，实得 %d 行: %v", len(hitRows), hitRows)
	}
	for i := 1; i < len(hitRows); i++ {
		if hitRows[i] <= hitRows[i-1] {
			t.Fatalf("行号应升序，实得 %v", hitRows)
		}
	}

	// D-6：库内冲突要并列展示已入库明细的完整内容供人工比对
	first := result.Conflicts[0]
	if len(first.InStore) != 1 {
		t.Fatalf("第 0 行应有 1 条库内冲突，实得 %d 条", len(first.InStore))
	}
	if first.InStore[0].ID == 0 {
		t.Error("并列展示的已入库明细必须带 ID，否则前端无法跳转到那条明细")
	}

	// 批内冲突要能定位到「第几行」，否则预览页高亮不了
	third := result.Conflicts[2]
	if len(third.InBatchIndexes) != 1 || third.InBatchIndexes[0] != 3 {
		t.Errorf("第 2 行的批内冲突应指向第 3 行，实得 %v", third.InBatchIndexes)
	}

	// 未命中的行不出现在映射里——调用方遍历 Conflicts 即是遍历待高亮行
	if _, hit := result.Conflicts[1]; hit {
		t.Error("第 1 行无冲突，不应出现在结果映射中")
	}

	// E-2「只提示不阻断」：判重结果不影响能否入库，全部行都仍可被勾选入库
	if len(rows) != 4 {
		t.Error("判重不得增删待入库的行")
	}
}

// TestConsumerExpenseEdit 走查协作方 service/expense：F-5 逐笔编辑的用法。
//
// 编辑链路的关键是排除自身（8.3），且编辑**没有改动**四要素时不应报重复。
func TestConsumerExpenseEdit(t *testing.T) {
	const editingID int64 = 260907000000001301
	stored := withID(newRow(userA, "2026-09-07", "100.50", "CNY"), editingID)

	t.Run("只改了非四要素字段", func(t *testing.T) {
		// 用户只改了备注，四要素未动——最常见的编辑，绝不能自报重复
		edited := stored
		edited.Remark = "改了备注"

		result := Check(Request{
			Rows:       []entity.Expense{edited},
			Candidates: []entity.Expense{stored},
			ExcludeID:  editingID,
		})
		if result.HasAny() {
			t.Error("只改备注不应触发重复提示（8.3：编辑态排除被编辑的这笔自身）")
		}
	})

	t.Run("改了金额，撞上另一条明细", func(t *testing.T) {
		// 用户把金额改成了与另一条明细相同的值——这是编辑态真正要提示的情形
		edited := stored
		edited.Amount = mustAmount(t, "88.00")

		another := withID(newRow(userA, "2026-09-07", "88.00", "CNY"), 260907000000001302)
		result := Check(Request{
			Rows:       []entity.Expense{edited},
			Candidates: []entity.Expense{stored, another},
			ExcludeID:  editingID,
		})
		if !result.HasAny() {
			t.Fatal("改后的四要素与另一条明细相同，应提示重复")
		}
		conflict := result.Conflicts[0]
		if len(conflict.InStore) != 1 || conflict.InStore[0].ID != another.ID {
			t.Errorf("应只提示另一条明细 %d，实得 %+v", another.ID,
				conflictIDs(conflict.InStore))
		}
	})

	t.Run("编辑态是单行，不存在批内冲突", func(t *testing.T) {
		result := Check(Request{
			Rows:       []entity.Expense{stored},
			Candidates: []entity.Expense{stored},
			ExcludeID:  editingID,
		})
		for _, conflict := range result.Conflicts {
			if len(conflict.InBatch) != 0 {
				t.Errorf("编辑态只有一行，不应有批内冲突，实得 %d 条", len(conflict.InBatch))
			}
		}
	})
}

// TestConsumerCopyDeletedExpense 走查 8.5 复制已删除明细的链路。
//
// 8.5 的注写明：被复制的原明细**已删除、不在比对范围内**，因此复制出来的新行
// 不会与自身误报；但库中若另有同四要素的未删除明细，仍正常提示。
func TestConsumerCopyDeletedExpense(t *testing.T) {
	// 被复制的原明细已删除，store 按「在库且未删除」取候选时不会取到它
	copied := newRow(userA, "2026-09-07", "100.50", "CNY")

	t.Run("库中无其他同四要素明细", func(t *testing.T) {
		result := Check(Request{Rows: []entity.Expense{copied}, Candidates: nil})
		if result.HasAny() {
			t.Error("原明细已删除、不在比对范围内，复制出的新行不应与自身误报（8.5）")
		}
	})

	t.Run("库中另有同四要素的未删除明细", func(t *testing.T) {
		another := withID(newRow(userA, "2026-09-07", "100.5", "CNY"), 260907000000001401)
		result := Check(Request{
			Rows:       []entity.Expense{copied},
			Candidates: []entity.Expense{another},
		})
		if !result.HasAny() {
			t.Error("库中另有同四要素的未删除明细时应正常提示（8.5）")
		}
	})
}

// conflictIDs 提取一组明细的 ID，用于报错信息。
func conflictIDs(list []entity.Expense) []int64 {
	out := make([]int64, 0, len(list))
	for _, e := range list {
		out = append(out, e.ID)
	}
	return out
}

// receiverName 取方法接收者的类型名（去掉指针）。
func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch expr := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		if ident, ok := expr.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

// packageFiles 列出本包目录下的 .go 文件。includeTests 为 false 时排除 _test.go。
func packageFiles(t *testing.T, includeTests bool) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取包目录失败：%v", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if !includeTests && strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	return files
}

// parseImports 解析出一个文件的 import 路径列表。
func parseImports(t *testing.T, file string) []string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析 %s 失败：%v", file, err)
	}
	imports := make([]string, 0, len(parsed.Imports))
	for _, imp := range parsed.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("%s 的 import 路径 %s 无法解析：%v", file, imp.Path.Value, err)
		}
		imports = append(imports, path)
	}
	return imports
}

// assertSameSet 比对两个集合，缺失与多余分别报错。
func assertSameSet(t *testing.T, label string, want, got map[string]bool) {
	t.Helper()
	for name := range want {
		if !got[name] {
			t.Errorf("%s 缺少 %q——承载被削减了？", label, name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("%s 多出 %q——承载外溢，分层 §六 给本包的承载只有 8.3 判重；"+
				"若确需新增，先改文档再改这张表", label, name)
		}
	}
}
