package token

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
	"time"

	"github.com/cellargalaxy/jotcash/internal/enum"
)

// 本文件是走查性质的测试，钉住几件无法用运行期断言表达的约定：
//   1. 依赖边界（分层 §六 L3 表 token 行的依赖列为 enum）；
//   2. 本包不打日志（A-6 每请求调 Parse，端口层打日志会按请求量刷屏）；
//   3. 本包不再依赖 golang-jwt v3（go_common 引入的 indirect 版本）；
//   4. 承载不外溢（不做基线时间比对、不做黑名单、不做续期、不读配置）；
//   5. 边界包形态：无 L4 实现子包（约定 2）；
//   6. 下游 service/auth 与 middleware 的用法能走通。
//
// 前五条用 AST 扫描本包源码实现——注释里写「不依赖 X」是没有约束力的，
// 下一个人加一行 import 就破了。

// 本包在**非测试文件**中允许出现的 import。改这张表等于改分层的依赖列，
// 必须先改文档。
var allowedImports = map[string]string{
	//标准库
	"errors":  "九个哨兵错误的构造，以及 errors.Is 判定库哨兵",
	"fmt":     "错误信息与 Payload.String 的格式化",
	"strings": "New 判定密钥是否全空白",
	"time":    "TTL、签发与过期时间、秒级截断",
	//第三方库（选型见包注释「选型」一节）
	"github.com/golang-jwt/jwt/v5": "JWT 签发解析的算法实现（9217★、MIT、零非标准库依赖）",
	//仓库内包
	"github.com/cellargalaxy/jotcash/internal/enum": "分层 §六 L3 token 行依赖列的唯一一项：Payload.Role 的角色类型与码值校验",
	//下面这项在分层的依赖列之外，理由见包注释末节，已作为待文档追认项记入本轮 answer
	"github.com/cellargalaxy/jotcash/internal/base/idgen": "用户ID 与 sub 之间的规范文本互转（String/ParseString 是全仓库该规范的唯一实现）",
}

// TestDependencyBoundary 双向锁死依赖边界：
//   - 出现表外的 import 即失败（越界）；
//   - 表内的仓库内包若没有任何文件用到，也失败（依赖列虚高）。
//
// 特别地，本用例会拦下几类具体的越界：
//   - 依赖 base/config：约定 8「配置只在 L7 读」，密钥由 app 注入（承载明写）；
//   - 依赖 base/errs：本包的错误一律为哨兵，档位随调用方而变（Parse 失败在 A-6
//     是 KindUnauthorized、Issue 失败是 KindInternal），不能在本包定死；
//   - 依赖 entity：本包不读 User，基线时间比对不在此包（承载末句）；
//   - 依赖 store：那会让 L3 反向依赖同层包，且意味着本包开始读库；
//   - 依赖同层的 fxrate / parser / i18n（分层 §六「层内规则：同层包互不依赖」）；
//   - 依赖 service / api / app（L5 及以上，会成环）。
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
					"分层 §六 L3 表 token 行的依赖列只有 enum；"+
					"若确需新增，先改文档再改这张表。", file, imp)
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
// 理由见包注释：Parse 是 A-6 全局登录态校验的判定点，**每个请求都会调一次**，
// 而「令牌过期」是最正常不过的事件。端口层在此打日志等于按请求量刷日志，
// 真正的异常反而会被淹掉。与 fxrate 同一条取向（其包注释亦写明「本包一行日志
// 都不打」，因 I-7 逐笔重算同样是高频调用）。
//
// 另有一条更硬的理由：日志行为不该由被调库决定——go_common 的 DeJwt 在失败时
// 自己打一条 ERRO（实测 codec.go:123），这也是本包不复用它的原因之一。
func TestNoLoggingDependency(t *testing.T) {
	forbidden := []string{
		"github.com/sirupsen/logrus",
		"github.com/cellargalaxy/jotcash/internal/base/log",
		"github.com/cellargalaxy/go_common/util",
		"log",
		"log/slog",
	}

	//非测试文件与测试文件一并检查：测试里打印令牌同样会落进 CI 日志，
	//而令牌等价于口令。
	for _, file := range packageFiles(t, true) {
		for _, imp := range parseImports(t, file) {
			for _, bad := range forbidden {
				if imp == bad {
					t.Errorf("%s 引入了日志包 %q。本包不打日志——"+
						"Parse 每请求调用一次，「令牌过期」是正常事件，"+
						"在此打日志会按请求量刷屏并淹掉真正的异常。", file, imp)
				}
			}
		}
	}
}

// TestNoLegacyJwtImport 断言本包不使用 golang-jwt v3。
//
// go.mod 里仍有 github.com/golang-jwt/jwt v3.2.2+incompatible，那是 go_common
// 的 indirect 依赖，本仓库代码不应有任何路径可达它。v3 有两条实测硬伤：
// **默认放行无 exp 的令牌**（err 为 nil，即永不过期的令牌），且经 go_common
// 的 EnJwt/DeJwt 使用时**哨兵链已断**（用 errors.Errorf 重造错误，
// errors.Is 命中不了任何库哨兵，「过期」与「签名错」只能靠匹配文本区分）。
//
// 本用例也拦 go_common 的 util——那是 EnJwt/DeJwt 的所在处（与上一条用例重叠
// 是刻意的：两条用例的失败原因不同，一条是「打日志了」，一条是「用了 v3 链路」）。
func TestNoLegacyJwtImport(t *testing.T) {
	forbidden := map[string]string{
		"github.com/golang-jwt/jwt": "v3.2.2 默认放行无 exp 的令牌（实测 err 为 nil），" +
			"且它只是 go_common 的 indirect 依赖，本包应用 v5",
		"github.com/cellargalaxy/go_common/util": "util.EnJwt/DeJwt 的 model.Claims 无角色字段、" +
			"错误经 errors.Errorf 重造后哨兵链已断（实测），且失败时自行打日志",
	}
	for _, file := range packageFiles(t, true) {
		for _, imp := range parseImports(t, file) {
			if reason, bad := forbidden[imp]; bad {
				t.Errorf("%s 引入了 %q：%s", file, imp, reason)
			}
		}
	}
}

// TestNoImplementationSubPackage 断言本包**没有 L4 实现子包**。
//
// 分层 约定 2 明写「边界包（token/i18n）无实现子包」，§六 L3 表头注亦写
// 「边界包实现无选型分歧，内联包内，**无 L4 子包**」。这与 store/sqlite、
// fxrate/fixed、parser/csv 的形态不同，是本包形状的定义性特征之一。
//
// 判据是「子目录里有没有 .go 文件」而不是「有没有子目录」：go_common/util 会按
// **相对工作目录**落 log/<服务名>/log.log，而 go test 的工作目录就是包目录，
// 于是跑一次测试就会生成一个 internal/token/log/（其他包同样如此，
// 见 internal/base/idgen/log 等）。那是运行期产物，不是 Go 包——.gitignore 的
// *.log 使其对 git 不可见（空目录不被跟踪），按目录存在与否判定会误报。
func TestNoImplementationSubPackage(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取包目录失败：%v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		goFiles, err := filepath.Glob(filepath.Join(e.Name(), "*.go"))
		if err != nil {
			t.Fatalf("扫描子目录 %s 失败：%v", e.Name(), err)
		}
		if len(goFiles) > 0 {
			t.Errorf("本包出现含 Go 源码的子目录 %q（%v）。分层 约定 2 明写边界包"+
				"（token/i18n）无实现子包——令牌实现无选型分歧，应内联在本包",
				e.Name(), goFiles)
		}
	}
}

// TestExportedSurface 钉住本包的导出面，防承载外溢。
//
// 分层 §六 给本包的承载恰是一件事：无状态令牌的签发与解析。导出面每多一项，
// 就多一处「本包开始承担别人的职责」的入口。
func TestExportedSurface(t *testing.T) {
	wantFuncs := map[string]bool{
		"New":    true, //构造，接收 app 注入的密钥
		"Issue":  true, //签发（方法，A-4 调用）
		"Parse":  true, //解析（方法，A-6 调用）
		"IsZero": true, //Payload 的零值判定
		"String": true, //Payload 的排错文本
	}
	wantConsts := map[string]bool{
		"TTL":             true, //A-5 的 12 小时，写死在本包（约定 8）
		"MinSecretLength": true, //密钥强度门槛，库不管，只有本包能拦
	}
	wantTypes := map[string]bool{
		"Payload": true, //载荷四元组
		"Issuer":  true, //持密钥的签发解析器
	}
	//导出变量只允许九个哨兵错误（错误是哨兵，档位归上层，见包注释）。
	wantVars := map[string]bool{
		"ErrSecretEmpty": true, "ErrSecretTooShort": true,
		"ErrUserIDInvalid": true, "ErrRoleInvalid": true,
		"ErrTokenEmpty": true, "ErrTokenMalformed": true,
		"ErrTokenSignature": true, "ErrTokenExpired": true,
		"ErrTokenClaims": true,
	}

	gotFuncs := make(map[string]bool)
	gotConsts := make(map[string]bool)
	gotTypes := make(map[string]bool)
	gotVars := make(map[string]bool)

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
								gotVars[name.Name] = true
							}
						}
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							gotTypes[s.Name.Name] = true
						}
					}
				}
			}
		}
	}

	assertSameSet(t, "导出函数与方法", wantFuncs, gotFuncs)
	assertSameSet(t, "导出常量", wantConsts, gotConsts)
	assertSameSet(t, "导出类型", wantTypes, gotTypes)
	assertSameSet(t, "导出变量", wantVars, gotVars)
}

// TestNoCarriageOverreach 断言本包没有承担四件明确不属于它的事。
//
// 按标识名走查——这不是查字符串，而是查「有没有出现只可能属于那些职责的名字」。
// 四件事各有出处：
//
//	基线时间比对   承载末句「基线时间比对不在此包（需读库，属 service/auth）」
//	令牌黑名单     终版 §五 3「需令牌黑名单，本轮不建」
//	续期           终版 A-5「绝对过期、**不可续期**」
//	读配置         约定 8「配置只在 L7 读」，承载「仅密钥由 app 注入」
func TestNoCarriageOverreach(t *testing.T) {
	forbidden := map[string]string{
		"Baseline":  "基线时间比对归 service/auth（承载末句：需读库，不在此包）",
		"Blacklist": "令牌黑名单终版 §五 3 明写本轮不建",
		"Blocklist": "同上",
		"Revoke":    "作废靠 service/auth 比对基线时间，本包无撤销能力（无状态令牌）",
		"Refresh":   "A-5 明写绝对过期、不可续期",
		"Renew":     "同上",
		"Config":    "约定 8：配置只在 L7 读，本包只接收 app 注入的密钥",
	}

	for _, file := range packageFiles(t, false) {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", file, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			for bad, reason := range forbidden {
				if strings.Contains(ident.Name, bad) {
					t.Errorf("%s:%d 出现标识 %q（含 %q）——%s",
						file, fset.Position(ident.Pos()).Line, ident.Name, bad, reason)
				}
			}
			return true
		})
	}
}

// ---------- 消费方走查 ----------

// TestConsumerUsageAuthLogin 走查 service/auth 在 A-4 登录成功后的用法。
//
// 分层 §六 L5.1 service/auth 行：「A-4 登录（成功即刷新 最后登录时间）」，
// 依赖列含 token。这里按那条链路调一遍，确认签名对它好用：
// 拿到令牌串下发前端，同时拿到**与令牌一致的**签发时间用于落库。
func TestConsumerUsageAuthLogin(t *testing.T) {
	issuer, err := New(testSecret)
	if err != nil {
		t.Fatalf("app 注入密钥失败：%v", err)
	}

	//service/auth 用 time.Now()，此处用固定时刻以便断言。
	loginAt := baseTime
	tokenString, issued, err := issuer.Issue(
		Payload{UserID: testUserID, Role: enum.RoleUser}, loginAt)
	if err != nil {
		t.Fatalf("A-4 签发失败：%v", err)
	}
	if tokenString == "" {
		t.Error("A-4 需要令牌串下发前端，实际为空")
	}
	//「成功即刷新最后登录时间」：service/auth 落库的时间应取 issued.IssuedAt，
	//而不是自己再调一次 time.Now()——后者与令牌内的值会差出亚秒。
	if issued.IssuedAt.IsZero() {
		t.Error("A-4 需要签发时间落库（最后登录时间），实际为零值")
	}
}

// TestConsumerUsageAuthVerifyWithBaseline 走查 A-6 校验链路：
// 本包判令牌本身 → service/auth 再比对基线时间。
//
// 本用例**在测试里模拟**那一步比对（生产代码不在本包），目的是验证本包交出的
// 签发时间足以支撑它，并把包注释记录的实测坑与正确口径固化成可执行的例子：
// 基线时间必须**向上取整到秒**，否则改密后立即重登的用户会被误杀。
func TestConsumerUsageAuthVerifyWithBaseline(t *testing.T) {
	issuer, err := New(testSecret)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}

	//场景：10:00:00.900 登录拿到旧令牌；10:00:00.950 改密（推进基线）；
	//10:00:00.990 用新密码重新登录拿到新令牌。三个时刻同处一秒内。
	oldLogin := baseTime.Add(900 * time.Millisecond)
	passwordChangedAt := baseTime.Add(950 * time.Millisecond)
	reLogin := baseTime.Add(990 * time.Millisecond)

	oldToken, _ := issueAt(t, issuer, enum.RoleUser, oldLogin)
	newToken, _ := issueAt(t, issuer, enum.RoleUser, reLogin)

	//两个令牌本身都有效（未过期、签名对、载荷合规）。
	oldPayload, err := issuer.Parse(oldToken, reLogin)
	if err != nil {
		t.Fatalf("旧令牌本身应有效（作废靠基线时间，不靠本包）：%v", err)
	}
	newPayload, err := issuer.Parse(newToken, reLogin)
	if err != nil {
		t.Fatalf("新令牌应有效：%v", err)
	}

	//service/auth 的正确口径：基线时间向上取整到秒后再比。
	baseline := passwordChangedAt.Truncate(time.Second)
	if baseline.Before(passwordChangedAt) {
		baseline = baseline.Add(time.Second)
	}
	invalidated := func(p Payload) bool { return p.IssuedAt.Before(baseline) }

	//A-8「改密推进自身基线时间，当前令牌一并失效，须以新密码重新登录」：
	//在这个口径下，同一秒内签发的新旧令牌**都**失效，重登需等到下一秒。
	if !invalidated(oldPayload) {
		t.Error("改密前签发的令牌必须失效（A-8：当前令牌一并失效）")
	}
	if !invalidated(newPayload) {
		t.Error("用例前提变了：秒级精度下同秒重登的令牌也会被这条口径拦住，" +
			"这是已知代价（最多等 1 秒），若此处不再成立说明时间精度发生了变化")
	}

	//下一秒登录即可通过——证明代价确实只有 1 秒，不是永久锁死。
	nextSecond := baseTime.Add(time.Second)
	laterToken, _ := issueAt(t, issuer, enum.RoleUser, nextSecond)
	laterPayload, err := issuer.Parse(laterToken, nextSecond)
	if err != nil {
		t.Fatalf("下一秒签发的令牌应有效：%v", err)
	}
	if invalidated(laterPayload) {
		t.Error("下一秒重新登录应当通过，否则用户被永久锁死")
	}
}

// TestConsumerUsageMiddleware 走查 middleware 的用法：
// 从 Authorization 头剥出裸令牌 → Parse → 拿到登录上下文四元组中的两项。
//
// 分层 §六 L6 middleware 行：「/api/ 组链（登录态/强制改密/管理员）」，
// 且 6.0 ctxs 是「登录上下文四元组：middleware 写、handler 读」。
// 本用例确认 Payload 能提供其中的用户ID 与角色两项（另两项是界面语言与
// 事务无关的请求态，不由本包提供），并确认**剥前缀是 middleware 的事**。
func TestConsumerUsageMiddleware(t *testing.T) {
	issuer, err := New(testSecret)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	tokenString, _ := issueAt(t, issuer, enum.RoleAdmin, baseTime)

	header := "Bearer " + tokenString
	//本包只收裸令牌：带前缀直接送进来会失败（已由 TestParseRejectsMalformed 钉住），
	//这里演示 middleware 该怎么做。
	raw := strings.TrimPrefix(header, "Bearer ")
	payload, err := issuer.Parse(raw, baseTime)
	if err != nil {
		t.Fatalf("剥掉 Bearer 前缀后应解析成功：%v", err)
	}
	if payload.UserID != testUserID {
		t.Errorf("middleware 需要用户ID 写入 ctxs，实际 %d", payload.UserID)
	}
	//「管理员」判定用 Role.IsAdmin()，而非在别处写 == RoleAdmin
	//（enum 包注释：单独提供此方法是为了让「哪些地方依赖管理员」可被 grep）。
	if !payload.Role.IsAdmin() {
		t.Error("middleware 的管理员组链需要角色判定，IsAdmin 应为真")
	}
}

// TestConsumerUsageLogoutIsClientSide 断言登出**不需要本包做任何事**。
//
// 终版 A-4：「登出为前端丢弃令牌，服务端无动作、不记审计」。
// 这条在本包的形态是「没有任何登出/失效接口」——已由 TestExportedSurface 与
// TestNoCarriageOverreach 从导出面与标识名两侧钉住。此处补一条语义断言：
// 令牌在被「登出」后仍然有效（因为服务端确实没做任何事），这不是缺陷。
func TestConsumerUsageLogoutIsClientSide(t *testing.T) {
	issuer, err := New(testSecret)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	tokenString, _ := issueAt(t, issuer, enum.RoleUser, baseTime)

	//前端「登出」= 丢弃令牌。服务端无动作，故同一个令牌此刻依然有效。
	if _, err := issuer.Parse(tokenString, baseTime.Add(time.Hour)); err != nil {
		t.Errorf("登出是前端丢弃令牌、服务端无动作（A-4），"+
			"故令牌应仍然有效，实际 %v", err)
	}
}

// ---------- 辅助 ----------

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
			t.Errorf("%s 多出 %q——承载外溢。分层 §六 给本包的承载只有"+
				"「无状态令牌签发与解析」；若确需新增，先改文档再改这张表", label, name)
		}
	}
}
