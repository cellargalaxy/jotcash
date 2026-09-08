package passwd

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

	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// 本文件是走查性质的测试，钉住三件无法用运行期断言表达的约定：
//   1. 依赖边界（分层 §六 L2 表 rule/passwd 行的依赖列）；
//   2. 本包一行日志都不打（分层 §九 红线的日志出口已被 A-3 占满）；
//   3. 承载不外溢（本包只做 A-8 校验与 A-2 生成两件事）。
//
// 都用 AST 扫描本包源码实现——注释里写「不依赖 X」是没有约束力的，
// 下一个人加一行 import 就破了。

// 本包在**非测试文件**中允许出现的 import。改这张表等于改分层的依赖列，
// 必须先改文档。
var allowedImports = map[string]string{
	//标准库
	"context":      "向 base/pwdhash 透传 ctx（其按约定 4 打日志需要）",
	"strings":      "洗牌结果的 Builder 拼接",
	"unicode":      "A-8 三类判据 IsLetter / IsDigit / IsPunct / IsSymbol",
	"unicode/utf8": "长度按 rune 计 + 非法 UTF-8 检查",
	//仓库内包
	"github.com/cellargalaxy/jotcash/internal/base/pwdhash": "分层依赖列的唯一一项：安全随机源",
	"github.com/cellargalaxy/jotcash/internal/base/errs":    "分档错误（普遍依赖，L0）",
}

// TestDependencyBoundary 双向锁死依赖边界：
//   - 出现表外的 import 即失败（越界）；
//   - 表内的仓库内包若没有任何文件用到，也失败（依赖列虚高）。
//
// 特别地，本用例会拦下几类具体的越界：依赖 entity（本包只处理裸字符串口令，
// User 实体的密码哈希与盐归 service/account 写）、依赖 store 或 api（L2 不碰
// IO 与传输）、依赖 base/log（见下一条用例）。
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
					"分层 §六 L2 表 rule/passwd 的依赖列只有 base/pwdhash；"+
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

// TestNoLoggingDependency 断言本包**一行日志都不打**。
//
// 理由见包注释：本包每个错误分支的唯一入参就是密码明文本身，能写进日志的
// 上下文只有明文；而分层 §九 红线只给明文留了三个出口，日志那一个已被 A-3 的
// admin 初始密码打印占满。不 import logrus 把这件事变成编译期约束，
// 本用例把它变成 CI 约束。
//
// 与 base/pwdhash 的差别要说清：后者**打日志**（只记长度与版本，有
// TestNoPasswordInLog 兜底），本包连日志库都不引。
func TestNoLoggingDependency(t *testing.T) {
	forbidden := []string{
		"github.com/sirupsen/logrus",
		"github.com/cellargalaxy/jotcash/internal/base/log",
		"log",
		"log/slog",
	}

	//非测试文件与测试文件一并检查：测试里打印明文同样会落到 CI 日志里
	for _, file := range packageFiles(t, true) {
		for _, imp := range parseImports(t, file) {
			for _, bad := range forbidden {
				if imp == bad {
					t.Errorf("%s 引入了日志包 %q。本包不打日志——"+
						"每个错误分支的唯一入参就是密码明文，一旦能打日志，"+
						"「顺手把明文带上」在语法上就毫无阻力。",
						filepath.Base(file), imp)
				}
			}
		}
	}
}

// TestNoPlaintextPrintingInTests 断言测试文件里没有 fmt.Print* / t.Log*
// 直接打印生成的密码。
//
// 这不是洁癖：CI 日志通常长期保留且可被更多人读到，一条 t.Logf("%s", password)
// 等于给明文开了第四个出口。判据放宽为「测试里不出现裸的 fmt.Print 家族」，
// t.Errorf / t.Fatalf 允许（它们只在失败时输出，且用例已确保不带明文）。
func TestNoPlaintextPrintingInTests(t *testing.T) {
	for _, file := range packageFiles(t, true) {
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", file, err)
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", file, err)
		}

		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			//fmt.Print / fmt.Println / fmt.Printf 一律不允许
			if pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Print") {
				t.Errorf("%s:%d 出现 fmt.%s——测试不得向标准输出打印，"+
					"以免生成的明文密码进入 CI 日志",
					filepath.Base(file), fset.Position(call.Pos()).Line, sel.Sel.Name)
			}
			//t.Log / t.Logf 同理
			if pkg.Name == "t" && strings.HasPrefix(sel.Sel.Name, "Log") {
				t.Errorf("%s:%d 出现 t.%s——同上，请改用只在失败时输出的 t.Errorf/t.Fatalf",
					filepath.Base(file), fset.Position(call.Pos()).Line, sel.Sel.Name)
			}
			return true
		})
	}
}

// TestExportedSurface 钉住本包的导出面，防承载外溢。
//
// 分层 §六 给本包的承载恰是两件：A-8 校验与 A-2 生成。导出面每多一个函数，
// 就多一处「别的包把规则再实现一遍」的入口——例如若导出了 countCategories，
// 调用方就能绕过长度检查只看类别数（准则 1「一条不变式只有一处实现」）。
//
// 常量属例外：MinLength / MinCategories / 四个 Key 确有包外消费方
// （service/account 与 i18n 的语言文件）。
func TestExportedSurface(t *testing.T) {
	wantFuncs := map[string]bool{"Check": true, "Generate": true}
	wantConsts := map[string]bool{
		"MinLength": true, "MinCategories": true,
		"KeyEmpty": true, "KeyInvalidEncoding": true,
		"KeyTooShort": true, "KeyTooFewCategories": true,
		"KeyGenerateFailed": true,
	}

	gotFuncs := make(map[string]bool)
	gotConsts := make(map[string]bool)
	var gotTypes, gotVars []string

	for _, file := range packageFiles(t, false) {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", file, err)
		}
		for _, decl := range parsed.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.IsExported() {
					gotFuncs[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if !name.IsExported() {
								continue
							}
							if d.Tok == token.CONST {
								gotConsts[name.Name] = true
							} else {
								gotVars = append(gotVars, name.Name)
							}
						}
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							gotTypes = append(gotTypes, s.Name.Name)
						}
					}
				}
			}
		}
	}

	assertSameSet(t, "导出函数", wantFuncs, gotFuncs)
	assertSameSet(t, "导出常量", wantConsts, gotConsts)

	//导出类型：本包处理裸字符串，不该定义 Policy / Options 之类的类型——
	//A-8 是写死的业务规则，可配置的 Policy 结构体等于给「把下限调成 1」开了门
	if len(gotTypes) > 0 {
		sort.Strings(gotTypes)
		t.Errorf("本包不应有导出类型，实际有 %v。A-8 是写死的业务阈值，"+
			"引入可配置的 Policy/Options 类型等于开一个能把长度下限调低的口子", gotTypes)
	}
	//导出变量：会成为可被外部改写的全局状态，与包注释的「无任何包级可变状态」冲突
	if len(gotVars) > 0 {
		sort.Strings(gotVars)
		t.Errorf("本包不应有导出变量，实际有 %v", gotVars)
	}
}

// TestConsumerUsage 走查本包的两个消费方式，确认签名对 service/account 好用。
//
// 分层 §六 5.1 写明 service/account 是「rule/passwd 的唯一调用方」，
// 三条链路：A-2 初始密码生成、A-7 重置他人密码、A-8 自助改密。这里按那三条
// 链路的形状各走一遍，检查签名不别扭、错误档位对得上接入层的处理。
func TestConsumerUsage(t *testing.T) {
	ctx := t.Context()

	//A-2 / A-7：生成 → 校验 → 哈希落库。生成的密码必须能直接进 Check，
	//不需要调用方做任何预处理（若需要，说明签名设计有问题）
	t.Run("A2和A7_生成初始密码", func(t *testing.T) {
		password, err := Generate(ctx)
		if err != nil {
			t.Fatalf("生成失败：%v", err)
		}
		if err := Check(password); err != nil {
			t.Fatalf("生成的密码未通过校验：%v", err)
		}
	})

	//A-8：用户自助改密，新密码来自请求入参，校验失败必须是 KindInvalidInput，
	//这样 middleware 才会回 400 而不是 500（分层 §九 的档位映射）
	t.Run("A8_自助改密", func(t *testing.T) {
		if err := Check("MyNewPass123!"); err != nil {
			t.Fatalf("合规的新密码被拒：%v", err)
		}

		err := Check("weak")
		if err == nil {
			t.Fatal("弱密码应被拒")
		}
		//调用方只需把错误原样往上抛，接入层按 Kind 分档 + 按 MsgKey 渲染文案，
		//不需要在 service 层写任何 if 判断——这是用分档错误而非哨兵的收益
		if !isUserFacing(err) {
			t.Errorf("A-8 校验失败应可直接回给用户，实际档位不对：%v", err)
		}
	})
}

// isUserFacing 复刻接入层的判断：只有 KindInvalidInput 档的错误才把文案键
// 渲染给用户看。放在测试里而非生产代码里，是因为真正的映射归 middleware。
func isUserFacing(err error) bool {
	return errs.IsKind(err, errs.KindInvalidInput)
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
			t.Errorf("%s 多出 %q——承载外溢，分层 §六 给本包的承载只有 A-8 校验与 A-2 生成；"+
				"若确需新增，先改文档再改这张表", label, name)
		}
	}
}
