package store

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/rule/dedup"
)

// 本文件按分层 §六 与 §九 逐条走查本包，把「端口层必须成立的结构性性质」变成可执行
// 断言。目的不是覆盖率——本包绝大部分是接口声明，没有可执行逻辑——而是让**方法集合
// 与签名形状**的有害变化在本包的测试里当场失败，而不是等到 L4/L5 那一轮才发现。
//
// §十一 阶段 4 对本包的完成标志是一次走查：「方法集合须通过『前提 8 + 不变式 3 +
// 前提 3 无角色旁路 + 十条查询口径 + 两种软删除形态 + 两项唯一约束 + 数值列口径 +
// 事务句柄』走查」。本文件即那次走查的可执行形态。

// parseOwnPackage 解析本包的**非测试**源码，供依赖边界与签名形状检查使用。
func parseOwnPackage(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := goparser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, goparser.ParseComments)
	if err != nil {
		t.Fatalf("解析本包源码失败: %v", err)
	}
	files := make(map[string]*ast.File)
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			files[name] = file
		}
	}
	if len(files) == 0 {
		t.Fatal("未解析到任何源码文件")
	}
	return fset, files
}

// TestDependencyBoundary 双向锁死本包的依赖边界。
//
// 判据取自分层 §六 L3 表 store 行的依赖列（`entity`、`enum`、`currency`）与表头
// 「端口包：只有接口、类型与注册表，实现在 L4」。
//
// # 两个方向都必须查
//
//	不得依赖  database/sql 与任何驱动、任何 ORM/查询构建器、L4/L5/L6/L7 包、
//	          同层包（fxrate/parser/token/i18n）、L2 规则层
//	必须只用  entity、enum、currency + base 内的数值与日期类型
//
// 只查「不得依赖」是不够的：漏了「必须只用」这一侧，本包可以悄悄多 import 一个
// 依赖列里没有的包（例如 rule/convert），而那会让端口层开始承担规则层的职责。
func TestDependencyBoundary(t *testing.T) {
	t.Parallel()

	const modulePrefix = "github.com/cellargalaxy/jotcash/"

	//依赖列（分层 §六 L3 表 store 行）+ base 内本包确实需要的三个类型包。
	//base/* 是 L0，全仓库可用（§六 L0 表头「被所有层依赖」）。
	allowedInternal := map[string]bool{
		"internal/entity":        true, //5 实体
		"internal/enum":          true, //两套枚举 + 11 类审计类型
		"internal/currency":      true, //币种与小数位数
		"internal/base/decimal":  true, //金额与汇率的数值载体
		"internal/base/calendar": true, //日期与年月（前提 7）
		"internal/base/errs":     true, //错误档位与文案键
		//base/idgen 只允许用 IsValid（判定归属ID 形态），**不得**用任何 Gen* 生成函数。
		//依据是 idgen.IsValid 的文档明写它「供三处使用」，第一处就是「store 的按 ID
		//访问方法在下压 SQL 前做前置断言」；而 §六 L0 表 idgen 行同样明写
		//「store 不生成 ID、不依赖自增回填」。两句话合起来的意思是：可以判、不可以生成。
		//生成函数的禁止由 TestStoreDoesNotGenerateIDs 单独扫。
		"internal/base/idgen": true,
	}

	//逐个点名禁止项，附理由——理由会打进失败信息，避免后人再犯同一处
	forbidden := map[string]string{
		"internal/store/sqlite":  "端口不得依赖自己的实现（成环），且会让 L5 间接绑上选型",
		"internal/fxrate":        "同层包互不依赖（分层 §六 层内规则）",
		"internal/parser":        "同层包互不依赖",
		"internal/token":         "同层包互不依赖",
		"internal/i18n":          "同层包互不依赖；文案键由本包声明、由 i18n 落词",
		"internal/service":       "L3 不得依赖 L5",
		"internal/api":           "L3 不得依赖 L6",
		"internal/app":           "L3 不得依赖 L7",
		"internal/rule/convert":  "L3 不得依赖 L2；T39 规整点在 convert，端口层不参与规整",
		"internal/rule/amortize": "L3 不得依赖 L2；份额派生在 rule/amortize + service/stat",
		"internal/rule/dedup":    "L3 不得依赖 L2（依赖图无 L3→L2 边）；四要素键用本包的 DedupKey，结构相同可零成本互转",
		"internal/rule/passwd":   "L3 不得依赖 L2",
		"internal/base/config":   "配置由 app 注入给 L4，端口不读配置",
		"internal/base/pwdhash":  "口令哈希由 service/auth 与 rule/passwd 负责，仓储只存已算好的哈希",
	}

	//标准库与第三方的禁止项：端口层出现它们即意味着选型已被固化
	forbiddenExternal := map[string]string{
		"database/sql":                    "会把「关系型数据库」这一选型固化进端口层，且让 L5 有机会直接写 SQL",
		"database/sql/driver":             "同上；driver.Valuer 的实现归 L4 与各值类型",
		"gorm.io/gorm":                    "ORM 出现在 L3 会让 L5 的 11 个 service 全部编译期依赖它（分层 §十二）",
		"entgo.io/ent":                    "同上",
		"github.com/jmoiron/sqlx":         "同上",
		"github.com/Masterminds/squirrel": "查询构建器属 L4",
		"modernc.org/sqlite":              "驱动属 store/sqlite（L4）",
		"github.com/mattn/go-sqlite3":     "驱动属 store/sqlite（L4）",
		"net/http":                        "HTTP 属 L6",
	}

	_, files := parseOwnPackage(t)
	for name, file := range files {
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)

			if reason, bad := forbiddenExternal[path]; bad {
				t.Errorf("%s 依赖了 %s：%s", name, path, reason)
				continue
			}
			//SQL 驱动的通用形态兜底：路径里带 sqlite/postgres/mysql 的一律拒绝
			for _, kw := range []string{"sqlite", "postgres", "pgx", "mysql", "sqlserver"} {
				if strings.Contains(strings.ToLower(path), kw) {
					t.Errorf("%s 依赖了疑似数据库驱动 %s：驱动选型属 L4，端口层不得出现",
						name, path)
				}
			}

			if !strings.HasPrefix(path, modulePrefix) {
				continue //其余标准库放行（context/iter/time 等）
			}
			rel := strings.TrimPrefix(path, modulePrefix)
			if reason, bad := forbidden[rel]; bad {
				t.Errorf("%s 依赖了 %s：%s", name, rel, reason)
				continue
			}
			if !allowedInternal[rel] {
				t.Errorf("%s 依赖了依赖列之外的仓库内包 %s。"+
					"分层 §六 L3 表 store 行的依赖列为 entity/enum/currency（+ base 内类型包）；"+
					"若确需新增，先更新分层文档再改本用例", name, rel)
			}
		}
	}
}

// TestNoSQLInPortLayer 验证端口层源码里不出现 SQL 语句。
//
// 端口包「只有接口、类型与注册表」（§六 L3 表头）。一旦有人在这里写了一句 SQL，
// 「唯一知道 SQL 的地方是 store/sqlite」就不再成立，而那是换存储引擎的代价能维持在
// 「新增平级子包 + app 改一行」的前提（分层 §十二）。
//
// 注释里出现 SQL 关键词是允许的（本包多处注释在说明 L4 该怎么拼条件），故只扫
// **字符串字面量**。
func TestNoSQLInPortLayer(t *testing.T) {
	t.Parallel()

	sqlKeywords := []string{
		"select ", "insert into", "update ", "delete from", "create table",
		"where ", "order by", "group by", "left join", "begin transaction",
	}

	_, files := parseOwnPackage(t)
	for name, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			lower := strings.ToLower(strings.Trim(lit.Value, "`\""))
			for _, kw := range sqlKeywords {
				if strings.Contains(lower, kw) {
					t.Errorf("%s 的字符串字面量含 SQL 关键词 %q：%s。"+
						"端口层只有接口与类型，SQL 属 store/sqlite（L4）",
						name, kw, lit.Value)
					return false
				}
			}
			return true
		})
	}
}

// repoInterfaces 是本包的 5 个仓储接口，供逐条走查复用。
//
// 用反射拿接口类型：reflect.TypeFor 对接口类型可直接取到方法集合，
// 无需实例。这比 AST 更可靠——AST 拿不到嵌入接口展开后的方法集。
func repoInterfaces() map[string]reflect.Type {
	return map[string]reflect.Type{
		"UserRepo":     reflect.TypeFor[UserRepo](),
		"ExpenseRepo":  reflect.TypeFor[ExpenseRepo](),
		"FileRepo":     reflect.TypeFor[FileRepo](),
		"AuditLogRepo": reflect.TypeFor[AuditLogRepo](),
		"CategoryRepo": reflect.TypeFor[CategoryRepo](),
	}
}

// ownedRepoInterfaces 是 4 个**归属型**仓储（承载 ①），不含 UserRepo。
func ownedRepoInterfaces() map[string]reflect.Type {
	all := repoInterfaces()
	delete(all, "UserRepo")
	return all
}

// TestOwnedRepoMethodsCarryOwnerParam 验证红线 1 / 不变式 3 / 前提 3：
// 4 个归属型仓储的**每个**方法都带 OwnerUserID，且位置固定。
//
// §九 不变式 3 把本包的仓储签名列为唯一实现处，前提 3 补充「该参数对任何角色一律
// 生效、无管理员旁路」。这是本包最要紧的一条断言：漏一个方法即一处跨用户泄漏。
func TestOwnedRepoMethodsCarryOwnerParam(t *testing.T) {
	t.Parallel()

	ownerType := reflect.TypeFor[OwnerUserID]()
	txType := reflect.TypeFor[TxHandle]()
	ctxName := "Context"

	for repoName, rt := range ownedRepoInterfaces() {
		if rt.NumMethod() == 0 {
			t.Errorf("%s 没有任何方法，接口是空的？", repoName)
			continue
		}
		for i := 0; i < rt.NumMethod(); i++ {
			m := rt.Method(i)
			ft := m.Type

			//必须含 OwnerUserID
			ownerIdx := -1
			for j := 0; j < ft.NumIn(); j++ {
				if ft.In(j) == ownerType {
					ownerIdx = j
					break
				}
			}
			if ownerIdx < 0 {
				t.Errorf("%s.%s 缺少 OwnerUserID 参数：不变式 3「归属校验无死角」"+
					"在本包的唯一实现处就是签名，漏一个方法即一处跨用户泄漏",
					repoName, m.Name)
				continue
			}

			//位置固定：ctx（+ TxHandle，若有）之后的首位
			if ft.NumIn() == 0 || ft.In(0).Name() != ctxName {
				t.Errorf("%s.%s 首参不是 context.Context", repoName, m.Name)
				continue
			}
			wantIdx := 1
			if ft.NumIn() > 1 && ft.In(1) == txType {
				wantIdx = 2 //事务形态：句柄在 ctx 之后（约定 7）
			}
			if ownerIdx != wantIdx {
				t.Errorf("%s.%s 的 OwnerUserID 在第 %d 位，应在第 %d 位"+
					"（ctx[+TxHandle] 之后的首位）。位置统一是为了让漏传与传反在评审时可见",
					repoName, m.Name, ownerIdx, wantIdx)
			}
		}
	}
}

// TestNoRoleParamAnywhere 验证红线 1 的第三条：**角色不出现在本包任何签名里**。
//
// 前提 3 明写这是「不变式 3 最易被『顺手加个 if』破坏的一处」。若某个方法接受
// enum.Role，那个 if 就有了落脚点；不接受，则「管理员放宽归属过滤」在本层无法表达。
func TestNoRoleParamAnywhere(t *testing.T) {
	t.Parallel()

	all := repoInterfaces()
	all["Store"] = reflect.TypeFor[Store]()
	all["TxExecutor"] = reflect.TypeFor[TxExecutor]()

	for name, rt := range all {
		for i := 0; i < rt.NumMethod(); i++ {
			m := rt.Method(i)
			for j := 0; j < m.Type.NumIn(); j++ {
				in := m.Type.In(j)
				if in.Name() == "Role" || strings.Contains(in.String(), "enum.Role") {
					t.Errorf("%s.%s 第 %d 个参数是角色类型：前提 3 明写 4 个归属型仓储"+
						"不存在管理员旁路，角色不得进入本包签名", name, m.Name, j)
				}
			}
		}
	}

	//AST 侧再扫一遍源码文本，兜住「以别名或结构体字段形式带进来」的形态
	_, files := parseOwnPackage(t)
	for fname, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "Role" {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "enum" {
				t.Errorf("%s 引用了 enum.Role：角色判定属 service 层，本包不得出现", fname)
			}
			return true
		})
	}
}

// TestNoDeleteMethods 验证前提 8 / 承载 ③：三个仓储**连删除方法都没有**。
//
// 前提 8 的措辞是「上层想违反也没有可调的方法」——这条约束由方法集合本身承担，
// 不是注释里的君子协定。
//
//	User      无删除方法（承载 ②③）
//	File      无删除方法（承载 ③；C-3 溯源链要求长期留存）
//	AuditLog  无删除方法（承载 ③；K-2 只读）
//	Expense   只有「写删除时间」，不得有物理删除
func TestNoDeleteMethods(t *testing.T) {
	t.Parallel()

	//这三个接口连「删除」字样都不能有
	for _, repoName := range []string{"UserRepo", "FileRepo", "AuditLogRepo"} {
		rt := repoInterfaces()[repoName]
		for i := 0; i < rt.NumMethod(); i++ {
			name := rt.Method(i).Name
			if strings.Contains(name, "Delete") || strings.Contains(name, "Remove") ||
				strings.Contains(name, "Purge") || strings.Contains(name, "Drop") {
				t.Errorf("%s 出现删除方法 %s：前提 8 明写该实体无删除方法，"+
					"「上层想违反也没有可调的方法」是这条约束的落地形态", repoName, name)
			}
		}
	}

	//Expense 只能有软删除（方法名带 SoftDelete），不得有硬删除
	rt := reflect.TypeFor[ExpenseRepo]()
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		if !strings.Contains(name, "Delete") {
			continue
		}
		if !strings.HasPrefix(name, "SoftDelete") {
			t.Errorf("ExpenseRepo.%s 是删除方法但不以 SoftDelete 开头："+
				"F-8 只有「写删除时间」，物理删除不在承载内", name)
		}
	}

	//Expense 也不得有恢复方法——软删除是终态、无恢复入口（终版 F-8）
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		for _, bad := range []string{"Restore", "Undelete", "Recover", "Revive"} {
			if strings.Contains(name, bad) {
				t.Errorf("ExpenseRepo.%s 是恢复方法：终版 F-8 明写软删除是"+
					"「终态、无恢复入口」", name)
			}
		}
	}
}

// TestExpenseHasBothSoftDeleteForms 验证承载 ③ 的**两种软删除形态**都在。
//
// 分层 §三 问题 3 的裁定：只有按 ID 形态时，「全选当前筛选结果全集」只能把全集 ID
// 装进内存，与同表的三处性能口径自相矛盾。故按筛选形态是必需的，不是便利功能。
//
// 两者都必须返回**实际受影响笔数**（承载 ③，供审计摘要记 N 笔）。
func TestExpenseHasBothSoftDeleteForms(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[ExpenseRepo]()
	filterType := reflect.TypeFor[ExpenseFilter]()

	var hasByIDs, hasByFilter bool
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		if !strings.HasPrefix(m.Name, "SoftDelete") {
			continue
		}

		//必须返回 (int64, error)——实际受影响笔数
		if m.Type.NumOut() != 2 || m.Type.Out(0).Kind() != reflect.Int64 {
			t.Errorf("ExpenseRepo.%s 未返回 (int64, error)：承载 ③ 要求"+
				"「返回实际受影响笔数」，供审计摘要记 N 笔", m.Name)
		}

		//区分两种形态
		takesFilter := false
		takesIDs := false
		for j := 0; j < m.Type.NumIn(); j++ {
			in := m.Type.In(j)
			if in == filterType {
				takesFilter = true
			}
			if in.Kind() == reflect.Slice && in.Elem().Kind() == reflect.Int64 {
				takesIDs = true
			}
		}
		if takesFilter {
			hasByFilter = true
		}
		if takesIDs {
			hasByIDs = true
		}
	}

	if !hasByIDs {
		t.Error("缺「按 ID 软删除」形态（承载 ③ 形态一：逐笔或多选）")
	}
	if !hasByFilter {
		t.Error("缺「按筛选条件批量软删除」形态（承载 ③ 形态二）。" +
			"分层 §三 问题 3：没有它，F-8「全选当前筛选结果全集」只能把全集 ID 装进内存")
	}
}

// TestSoftDeleteByFilterReusesExpenseFilter 验证按筛选删除**复用** F-4 的筛选类型。
//
// 分层 §三 问题 3 明写「复用 ④ 的 F-4 筛选条件类型（同一套口径，保证『所删即所见』
// 与 F-6 合计对得上）」。若它用一个独立的删除条件结构，终版 §八 的批次撤销流程
// （用 F-6 核对笔数与合计 → 全选 → 批量删除）就没有任何东西保证核对与删除同集合。
func TestSoftDeleteByFilterReusesExpenseFilter(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[ExpenseRepo]()
	filterType := reflect.TypeFor[ExpenseFilter]()

	//三个方法必须收同一个筛选类型：列表、聚合、按筛选删除
	needFilter := map[string]bool{
		"List":                 false,
		"Summarize":            false,
		"SoftDeleteByFilter":   false,
		"SoftDeleteByFilterTx": false,
	}
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		if _, want := needFilter[m.Name]; !want {
			continue
		}
		for j := 0; j < m.Type.NumIn(); j++ {
			if m.Type.In(j) == filterType {
				needFilter[m.Name] = true
			}
		}
	}
	for name, ok := range needFilter {
		if !ok {
			t.Errorf("ExpenseRepo.%s 未收 ExpenseFilter：列表、聚合与按筛选删除必须"+
				"复用同一筛选类型，否则「所删即所见」与 F-6 核对失去依据", name)
		}
	}
}

// TestTenQueryCalibersPresent 逐条走查承载 ④ 的十条查询口径。
//
// 每条都点名它所在的接口与方法，缺一条即失败。这是 §十一 阶段 4 完成标志里
// 「十条查询口径」那一项的可执行形态。
func TestTenQueryCalibersPresent(t *testing.T) {
	t.Parallel()

	type caliber struct {
		no     int
		repo   string
		method string
		desc   string
	}
	calibers := []caliber{
		{1, "ExpenseRepo", "List", "F-4 全部筛选维度 + 排序 + 分页"},
		{2, "ExpenseRepo", "Summarize", "F-6 筛选态聚合"},
		{3, "AuditLogRepo", "List", "按归属用户分页列 AuditLog（K-2）"},
		{4, "FileRepo", "ListMeta", "按归属用户分页列 File（C-1，只取元信息）"},
		{5, "FileRepo", "ListMetaBySourceAudit", "按来源审计ID 查 File（C-3 反向）"},
		{6, "ExpenseRepo", "IterateAmortizeCovering", "摊分区间穿刺"},
		{7, "ExpenseRepo", "FindDedupCandidates", "查重候选批量匹配"},
		{8, "CategoryRepo", "GetByName", "按「归属用户 + 名称」查 ExpenseCategory（G-1）"},
		{9, "CategoryRepo", "List", "按归属用户列全部类型（G-4）"},
		{10, "ExpenseRepo", "AggregateMonthly", "J 域月度聚合（未摊分口径下推）"},
	}

	repos := repoInterfaces()
	for _, c := range calibers {
		rt, ok := repos[c.repo]
		if !ok {
			t.Fatalf("第 %d 条口径指向不存在的接口 %s", c.no, c.repo)
		}
		if _, found := rt.MethodByName(c.method); !found {
			t.Errorf("承载 ④ 第 %d 条口径缺失：%s.%s（%s）",
				c.no, c.repo, c.method, c.desc)
		}
	}

	//第 6 条与第 10 条的分工：摊分口径走迭代（只下推取行），未摊分口径走聚合（完全下推）
	rt := repos["ExpenseRepo"]
	if m, ok := rt.MethodByName("AggregateMonthly"); ok {
		//聚合必须返回分桶结果，而不是明细行
		if m.Type.NumOut() > 0 {
			out := m.Type.Out(0)
			if out.Kind() == reflect.Slice && out.Elem().Name() == "Expense" {
				t.Error("AggregateMonthly 返回了明细行：未摊分口径应「完全下推」，" +
					"返回按月分桶的聚合值而非明细")
			}
		}
	}
}

// TestFileMetaAndContentAreSeparate 验证承载点名的性能口径之一：
// File 的元信息与二进制内容**分接口**。
//
// 依据 C-1（列表只需五项元信息）与 C-2（原文入库、原样下载）。entity.File.Content
// 是 []byte，把它带进列表查询会让一次 20 行的分页拉走上百 MB。
func TestFileMetaAndContentAreSeparate(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[FileRepo]()
	//必须有只取元信息的方法，也必须有取内容的方法
	for _, name := range []string{"GetMeta", "ListMeta", "GetContent"} {
		if _, ok := rt.MethodByName(name); !ok {
			t.Errorf("FileRepo 缺 %s：承载末句要求「File 元信息与二进制内容分接口」", name)
		}
	}
	//列表方法不得叫 List（易被误用为「含内容」），必须显式表明只取元信息
	if _, ok := rt.MethodByName("List"); ok {
		t.Error("FileRepo 不应有名为 List 的方法：应叫 ListMeta，" +
			"让「只取元信息」在调用点可见（C-1 列表不需要文件内容）")
	}
}

// TestCursorIterationPresent 验证承载点名的另一处性能口径：Expense 提供游标迭代。
//
// 承载末句：「Expense 提供游标迭代（I-7 与 K-3 不全量装载）」。两条链路各有一个
// 方法，理由见 ExpenseRepo.IterateAll 的注释（方法名要能在调用栈与慢查询日志里
// 指出是哪条链路在扫全表）。
func TestCursorIterationPresent(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[ExpenseRepo]()
	for _, name := range []string{"IterateForRecalc", "IterateAll"} {
		m, ok := rt.MethodByName(name)
		if !ok {
			t.Errorf("ExpenseRepo 缺 %s：承载末句要求「Expense 提供游标迭代」", name)
			continue
		}
		//返回值必须是序列而非切片——切片即全量装载
		if m.Type.NumOut() == 0 {
			t.Errorf("%s 无返回值", name)
			continue
		}
		out := m.Type.Out(0)
		if out.Kind() == reflect.Slice {
			t.Errorf("ExpenseRepo.%s 返回切片：那是全量装载，"+
				"承载明写 I-7 与 K-3「不全量装载」", name)
			continue
		}
		//iter.Seq2 的底层是 func(yield func(V, error) bool)：Kind 为 Func，
		//且其唯一入参也是 Func（yield）。这是「拉取式序列」的形状判据——
		//实测 String() 为 "iter.Seq2[...]"，不含 "func" 字样，故不能按字符串判。
		if out.Kind() != reflect.Func {
			t.Errorf("ExpenseRepo.%s 的返回类型 %s 不是迭代序列（应为 iter.Seq2）",
				name, out)
			continue
		}
		if out.NumIn() != 1 || out.In(0).Kind() != reflect.Func {
			t.Errorf("ExpenseRepo.%s 的返回类型 %s 不是 range-over-func 形态："+
				"应为 func(yield func(V, error) bool)", name, out)
			continue
		}
		//yield 必须收两个值（行 + 逐行错误）：单值形态无法报告迭代中途的读失败
		if yield := out.In(0); yield.NumIn() != 2 {
			t.Errorf("ExpenseRepo.%s 的 yield 收 %d 个参数，应为 2（行 + 逐行错误）；"+
				"缺错误位则迭代中途读失败无法上报", name, yield.NumIn())
		}
	}
}

// TestCategoryDeleteIsTxOnly 验证 G-4 的物理删除**只有事务形态**。
//
// §九 前提 8 与 service/category 行：「检查与删除同事务，否则 TOCTOU」。
// 若提供非事务形态的 Delete，那条竞态就有了一条合法可达的代码路径：
// 检查时无引用 → 另一请求提交了用该类型的明细 → 删除执行 → 明细的类型ID 悬空，
// 而已删除明细的支出类型永久不可改（§8.5），归类就此永久损坏。
func TestCategoryDeleteIsTxOnly(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[CategoryRepo]()
	txType := reflect.TypeFor[TxHandle]()

	found := false
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		if !strings.Contains(m.Name, "Delete") {
			continue
		}
		found = true
		hasTx := false
		for j := 0; j < m.Type.NumIn(); j++ {
			if m.Type.In(j) == txType {
				hasTx = true
			}
		}
		if !hasTx {
			t.Errorf("CategoryRepo.%s 是非事务形态的删除：G-4 要求引用检查与删除"+
				"同事务（否则 TOCTOU），故删除只能有事务形态", m.Name)
		}
	}
	if !found {
		t.Error("CategoryRepo 缺物理删除方法（承载 ③：ExpenseCategory 的物理删除）")
	}

	//引用检查也必须有事务形态（在 ExpenseRepo 上）
	er := reflect.TypeFor[ExpenseRepo]()
	if _, ok := er.MethodByName("CountByCategoryTx"); !ok {
		t.Error("ExpenseRepo 缺 CountByCategoryTx：引用检查必须能在删除的同一事务内执行")
	}
}

// TestUserRepoHasNoOwnerParam 验证承载 ②：User 仓储**不带归属参数**。
//
// §九 不变式 3 行：「User 仓储是唯一签名不带归属的仓储」。A-1 建首个 admin 时根本
// 没有「当前用户」，A-4 登录时也还没有。这条在本包无编译期兜底（§四 R2 已登记为
// 残留），本用例把「形状差异」钉住：其余 4 个仓储首参是 OwnerUserID，本仓储不是。
func TestUserRepoHasNoOwnerParam(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[UserRepo]()
	ownerType := reflect.TypeFor[OwnerUserID]()

	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		for j := 0; j < m.Type.NumIn(); j++ {
			if m.Type.In(j) == ownerType {
				t.Errorf("UserRepo.%s 带 OwnerUserID：承载 ② 明写 User 仓储"+
					"「不带归属参数」（A-1/A-4/A-7 无「当前归属用户」），"+
					"越权由服务层按 §九 白名单显式校验", m.Name)
			}
		}
	}

	//承载 ② 点名的 11 个可更新字段，逐个确认有对应的更新入口
	for _, name := range []string{
		"UpdateCredential",         //密码哈希 + 盐 + 登录态基线时间
		"UpdateStatus",             //状态（+ 基线时间）
		"UpdateMustChangePassword", //需强制改密
		"UpdateLoginFailure",       //连续失败次数 + 锁定截止时间
		"UpdateLastLoginTime",      //最后登录时间
		"UpdatePreference",         //本位币 + 界面语言
	} {
		if _, ok := rt.MethodByName(name); !ok {
			t.Errorf("UserRepo 缺 %s：承载 ② 逐条点名了 11 个可更新字段", name)
		}
	}

	//承载 ② 的四个读写入口
	for _, name := range []string{"Create", "GetByID", "GetByUsername", "List"} {
		if _, ok := rt.MethodByName(name); !ok {
			t.Errorf("UserRepo 缺 %s（承载 ②：新增、按用户名查、按用户ID 查、列表）", name)
		}
	}
}

// TestAuditLogHasNoUpdateMethod 验证审计**仅追加**：无更新、无删除。
//
// K-2 明写「本页只读，不承担任何写操作」；service/audit 行「本包不提供任何审计的
// 改删入口」。审计记录一旦写入，在本层就没有任何方法能改动或抹除它。
func TestAuditLogHasNoUpdateMethod(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[AuditLogRepo]()
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		if strings.HasPrefix(name, "Update") || strings.HasPrefix(name, "Set") ||
			strings.HasPrefix(name, "Modify") || strings.HasPrefix(name, "Patch") {
			t.Errorf("AuditLogRepo 出现更新方法 %s：审计仅追加，"+
				"K-2 只读且本包不提供任何改删入口", name)
		}
	}

	//两种写入语义都必须在（见 AuditLogRepo.Create 的注释）
	for _, name := range []string{"Create", "CreateTx"} {
		if _, ok := rt.MethodByName(name); !ok {
			t.Errorf("AuditLogRepo 缺 %s：独立事务与同事务两种写入语义均须可用"+
				"（分层 §六 L5 service/audit 行）", name)
		}
	}
}

// TestTxHandleIsSealed 验证事务句柄**不透明**：本包外无法实现，也无法取出数据库对象。
//
// 若句柄暴露 *sql.Tx，「唯一知道 SQL 的地方是 store/sqlite」当场失效，且手写 SQL 里
// 没人强制 WHERE owner_user_id = ?（不变式 3 的兜底随之失效）。
func TestTxHandleIsSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[TxHandle]()
	if rt.Kind() != reflect.Interface {
		t.Fatalf("TxHandle 应是接口，实际 %s", rt.Kind())
	}

	// # 判据是「无导出方法」，而 reflect 会把未导出方法一并列出
	//
	// 实测：reflect 对接口类型的 NumMethod() **包含**未导出方法
	// （interface{ isX() } 的 NumMethod() = 1，Method(0).Name = "isX"）。
	// 故不能用 NumMethod()==0 判 sealed，必须逐个方法看是否导出。
	//
	// 两条性质都要：① 至少一个未导出方法（包外无法实现 → 只有 L4 能构造）；
	// ② 没有任何导出方法（L5 拿到它无法取出底层数据库对象）。
	var unexported, exported []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		if m.IsExported() {
			exported = append(exported, m.Name)
		} else {
			unexported = append(unexported, m.Name)
		}
	}
	if len(exported) > 0 {
		t.Errorf("TxHandle 有导出方法 %v：句柄必须不透明，"+
			"L5 拿到它只能原样透传、无法取出任何数据库对象。"+
			"若暴露 *sql.Tx，「唯一知道 SQL 的地方是 store/sqlite」当场失效", exported)
	}
	if len(unexported) == 0 {
		t.Error("TxHandle 没有未导出方法：包外可自行实现它，" +
			"「只有 L4 能构造事务句柄」这条保证失效")
	}

	//AST 侧交叉确认（与上面的反射判定互为独立证据）
	_, files := parseOwnPackage(t)
	sealed := false
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "TxHandle" {
				return true
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				return true
			}
			for _, m := range it.Methods.List {
				for _, nm := range m.Names {
					if !nm.IsExported() {
						sealed = true
					}
				}
			}
			return false
		})
	}
	if !sealed {
		t.Error("TxHandle 没有未导出方法：包外可自行实现它，" +
			"「只有 L4 能构造事务句柄」这条保证失效")
	}
}

// TestTxExecutorShape 验证事务执行器的形状（承载 ⑦）。
//
// 回调形态而非 Begin/Commit/Rollback 三件套：后者要求调用方自觉配对，漏一次
// Rollback 就悬挂一个事务，而 store/sqlite 是写连接单例——一次悬挂让全库写入永久阻塞。
func TestTxExecutorShape(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[TxExecutor]()
	m, ok := rt.MethodByName("WithinTx")
	if !ok {
		t.Fatal("TxExecutor 缺 WithinTx（承载 ⑦ 事务执行器）")
	}
	//不得暴露 Begin/Commit/Rollback
	for _, bad := range []string{"Begin", "Commit", "Rollback"} {
		if _, found := rt.MethodByName(bad); found {
			t.Errorf("TxExecutor 暴露了 %s：三件套要求调用方自觉配对，"+
				"漏一次 Rollback 会让写连接单例永久阻塞。应只提供回调形态", bad)
		}
	}
	//回调参数必须收 TxHandle
	foundCallback := false
	for i := 0; i < m.Type.NumIn(); i++ {
		in := m.Type.In(i)
		if in.Kind() != reflect.Func {
			continue
		}
		for j := 0; j < in.NumIn(); j++ {
			if in.In(j) == reflect.TypeFor[TxHandle]() {
				foundCallback = true
			}
		}
	}
	if !foundCallback {
		t.Error("WithinTx 的回调未收 TxHandle：跨仓储同事务需要句柄透传（约定 7）")
	}
	//必须只返回 error
	if m.Type.NumOut() != 1 || m.Type.Out(0).Name() != "error" {
		t.Error("WithinTx 应只返回 error（原样返回 fn 的错误，包一层会丢档位与文案键）")
	}
}

// TestStoreAggregatesAllRepos 验证 Store 聚合接口覆盖 5 个仓储且嵌入事务执行器。
//
// 也验证它**不含 Close**：生命周期归 app（L7），L5 拿到 Store 不该能关掉它。
func TestStoreAggregatesAllRepos(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[Store]()
	for _, name := range []string{"Users", "Expenses", "Files", "AuditLogs", "Categories"} {
		if _, ok := rt.MethodByName(name); !ok {
			t.Errorf("Store 缺访问器 %s", name)
		}
	}
	if _, ok := rt.MethodByName("WithinTx"); !ok {
		t.Error("Store 未嵌入 TxExecutor：跨仓储同事务是承载 ⑦")
	}
	for _, bad := range []string{"Close", "Shutdown", "Stop"} {
		if _, ok := rt.MethodByName(bad); ok {
			t.Errorf("Store 含 %s：连接生命周期归 app（L7），"+
				"L5 不该能关闭存储", bad)
		}
	}
}

// TestOwnerUserIDIsNamedType 验证归属参数是**具名类型**而非裸 int64。
//
// 裸 int64 的问题是传反不会报错：用明细ID 当归属用户去查，恒查不到，表现为
// 「数据莫名消失」而不是一条错误。具名类型让传反成为编译错误。
func TestOwnerUserIDIsNamedType(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeFor[OwnerUserID]()
	if rt.Name() != "OwnerUserID" {
		t.Fatalf("OwnerUserID 应是具名类型，实际 %s", rt)
	}
	if rt.Kind() != reflect.Int64 {
		t.Fatalf("OwnerUserID 底层应为 int64（与 idgen 的 ID 一致），实际 %s", rt.Kind())
	}

	//零值不是通配符：IsValid 必须对零值返回 false
	var zero OwnerUserID
	if zero.IsValid() {
		t.Error("OwnerUserID 零值不得视为合法：零值若被当成「全部用户」，" +
			"一次漏传就是全量泄漏（前提 3）")
	}
	if OwnerUserID(-1).IsValid() {
		t.Error("负值不应合法")
	}

	//裸 int64 不能隐式当作 OwnerUserID（编译期保证，这里确认类型确实不同）
	if reflect.TypeFor[int64]() == rt {
		t.Error("OwnerUserID 与 int64 是同一类型，传反不会报错")
	}
}

// TestDedupKeyConvertibleToRuleKey 验证本包的 DedupKey 与 rule/dedup.Key
// **结构一致、可零成本互转**。
//
// 本包不 import L2（分层 §六 L3 依赖列 + 依赖图无 L3→L2 边），故四要素键在两处
// 各声明一遍。这个用例是两处不漂移的护栏：字段名、类型或顺序一旦有一侧改动，
// 转换即失败，本用例当场报错——而不是等到运行时漏判重复。
//
// 注意本文件是测试文件，import L2 不违反依赖边界：TestDependencyBoundary 只扫
// **非测试**源码（parseOwnPackage 过滤了 _test.go）。这与 currency 的
// TestConsumerConvertBaseScaleFitsStorage 在测试里 import base/decimal 同例。
func TestDedupKeyConvertibleToRuleKey(t *testing.T) {
	t.Parallel()

	ruleType := reflect.TypeFor[dedup.Key]()
	storeType := reflect.TypeFor[DedupKey]()

	if !storeType.ConvertibleTo(ruleType) || !ruleType.ConvertibleTo(storeType) {
		t.Fatalf("store.DedupKey 与 dedup.Key 不可互转（%s vs %s）："+
			"两处四要素口径已漂移，service 层的转换会编译失败", storeType, ruleType)
	}

	if storeType.NumField() != ruleType.NumField() {
		t.Fatalf("字段数不一致: store.DedupKey %d 项, dedup.Key %d 项",
			storeType.NumField(), ruleType.NumField())
	}
	for i := 0; i < storeType.NumField(); i++ {
		sf, rf := storeType.Field(i), ruleType.Field(i)
		if sf.Name != rf.Name {
			t.Errorf("第 %d 个字段名不一致: %s vs %s", i, sf.Name, rf.Name)
		}
		if sf.Type != rf.Type {
			t.Errorf("字段 %s 类型不一致: %s vs %s", sf.Name, sf.Type, rf.Type)
		}
	}

	//往返转换必须保值
	orig := dedup.Key{OwnerUserID: 7, SpendDate: "2026-09-07", Amount: "100.5", Currency: "CNY"}
	back := dedup.Key(DedupKey(orig))
	if back != orig {
		t.Fatalf("往返转换不保值: %+v → %+v", orig, back)
	}
}

// TestStoreDoesNotGenerateIDs 验证本包**只判定** ID 形态、**不生成** ID。
//
// 分层 §六 L0 表 idgen 行明写：「`store` 不生成 ID、不依赖自增回填——不变式 4 的
// 「先落审计取 ID → 明细挂载」因此不需要 DB 回传」。
//
// 这条对不变式 4 是硬前提：审计ID 必须在写入**之前**就已知，业务数据才能挂载它。
// 若 store 自行生成（或依赖自增回填），就必须先提交审计再写业务数据，同事务语义
// 无从谈起。
//
// 允许 import idgen 只为用 IsValid（其文档第一条用途就是「store 的按 ID 访问方法
// 在下压 SQL 前做前置断言」），故这里精确扫 Gen* 函数的调用。
func TestStoreDoesNotGenerateIDs(t *testing.T) {
	t.Parallel()

	_, files := parseOwnPackage(t)
	for name, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Name != "idgen" {
				return true
			}
			if strings.HasPrefix(sel.Sel.Name, "Gen") {
				t.Errorf("%s 调用了 idgen.%s：分层 §六 L0 表明写「store 不生成 ID」，"+
					"ID 由 5 个创建方生成后传入（约定 9）。"+
					"若 store 生成 ID，不变式 4 的「先落审计取 ID → 业务数据挂载」失效",
					name, sel.Sel.Name)
			}
			return true
		})
	}
}

// TestCommentsAreChinese 验证注释为中文（与既有风格一致，prompt 的约束）。
//
// 抽样判据：非测试文件的文档注释里必须出现中日韩字符。
func TestCommentsAreChinese(t *testing.T) {
	t.Parallel()

	_, files := parseOwnPackage(t)
	for name, file := range files {
		hasCJK := false
		for _, cg := range file.Comments {
			for _, r := range cg.Text() {
				if r >= 0x4E00 && r <= 0x9FFF {
					hasCJK = true
					break
				}
			}
			if hasCJK {
				break
			}
		}
		if !hasCJK {
			t.Errorf("%s 的注释未见中文：本仓库注释与日志一律中文", name)
		}
	}
}
