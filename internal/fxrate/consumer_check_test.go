package fxrate

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/currency"
)

// 本文件是走查性质的测试，钉住几件无法用普通运行期断言表达的约定：
//   1. 依赖边界（分层 §六 L3 表 fxrate 行的依赖列为 currency、base/*）；
//   2. 端口包只有接口、类型与注册表——不含任何汇率源实现、不发 IO；
//   3. 承载不外溢（本包不做缓存、不做规整、不做 I-4 短路）；
//   4. 本包不打日志（I-7 逐笔调用，端口打日志会淹没根因）；
//   5. 下游 service/fx 的四条用法能走通。
//
// 前四条用 AST 扫描本包源码实现——注释里写「不依赖 X」是没有约束力的，
// 下一个人加一行 import 就破了。

// 本包在**非测试文件**中允许出现的 import。改这张表等于改分层的依赖列，
// 必须先改文档。
var allowedImports = map[string]string{
	//标准库
	"context": "Fetcher.Fetch 的取消与超时（I-7 可中断可续跑）",
	"errors":  "六个哨兵错误的构造",
	"fmt":     "Query.String 与错误信息的格式化",
	"sort":    "Sources 与构造校验错误列表的稳定顺序（map 遍历顺序随机）",
	//仓库内包（对应分层 §六 L3 fxrate 行依赖列「currency、base/*」）
	"github.com/cellargalaxy/jotcash/internal/currency":      "Query 的币种对类型与 RateSource 路由键",
	"github.com/cellargalaxy/jotcash/internal/base/calendar": "Query.Date 的日期类型（前提 7：不带时区的字面值）",
	"github.com/cellargalaxy/jotcash/internal/base/decimal":  "汇率的数值载体",
}

// TestDependencyBoundary 双向锁死依赖边界：
//   - 出现表外的 import 即失败（越界）；
//   - 表内的仓库内包若没有任何文件用到，也失败（依赖列虚高）。
//
// 特别地，本用例会拦下几类具体的越界：
//   - 依赖 fxrate/<源>、service、api、app（L4 及以上）：端口与实现会成环，
//     且 L5 会经本包间接依赖某一家汇率源，「换选型 = 改 app 一行」随之作废；
//   - 依赖同层的 store / parser / token / i18n（分层 §六「层内规则：同层包互不依赖」）；
//   - 依赖 base/errs：本包的错误一律为哨兵，档位由 service/fx 按链路判定
//     （同一个 ErrFetch 在 D-7 录入是用户输入错误档、在 I-7 重算是系统错误档）；
//   - 依赖 entity：一次汇率查询只需「两个币种 + 一个日期」，收 Expense 会把
//     一堆无关字段带进签名（取向同 rule/convert「为什么不收 entity.Expense」）。
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
					"分层 §六 L3 表 fxrate 的依赖列为「currency、base/*」；"+
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

// TestNoUpperLayerDependency 单独拦下「端口反向依赖实现」这一类越界。
//
// 与 TestDependencyBoundary 的白名单是互补关系：白名单拦的是「表外的一切」，
// 本用例把最要命的几个具体路径写成显式断言，让报错信息直接说清后果。
func TestNoUpperLayerDependency(t *testing.T) {
	forbidden := map[string]string{
		"github.com/cellargalaxy/jotcash/internal/fxrate/fixed": "端口不得依赖自己的实现（成环），且会让 L5 间接绑上选型",
		"github.com/cellargalaxy/jotcash/internal/service":      "L3 不得依赖 L5",
		"github.com/cellargalaxy/jotcash/internal/api":          "L3 不得依赖 L6",
		"github.com/cellargalaxy/jotcash/internal/app":          "L3 不得依赖 L7",
		"github.com/cellargalaxy/jotcash/internal/store":        "同层包互不依赖（分层 §六 层内规则）",
		"github.com/cellargalaxy/jotcash/internal/parser":       "同层包互不依赖",
		"github.com/cellargalaxy/jotcash/internal/i18n":         "同层包互不依赖",
		"github.com/cellargalaxy/jotcash/internal/token":        "同层包互不依赖",
		"github.com/cellargalaxy/jotcash/internal/entity":       "一次查询只需币种对与日期；收 Expense 会把无关字段带进签名",
		"github.com/cellargalaxy/jotcash/internal/base/errs":    "本包的错误一律为哨兵，档位由 service/fx 按链路判定",
		"github.com/cellargalaxy/jotcash/internal/rule/convert": "规整点在 convert 且它是下游，反向依赖会成环",
	}

	for _, file := range packageFiles(t, false) {
		for _, imp := range parseImports(t, file) {
			for bad, reason := range forbidden {
				if imp == bad || strings.HasPrefix(imp, bad+"/") {
					t.Errorf("%s 引入了 %q。%s", filepath.Base(file), imp, reason)
				}
			}
		}
	}
}

// TestNoIODependency 断言端口包零 IO。
//
// 分层 §六 L3 表头把 fxrate 归为端口包：「只有接口、类型与注册表，实现在 L4」。
// 一旦端口自己能发 HTTP，「换汇率源 = 加平级子包 + 改 app 一行」这条准则就失效了。
func TestNoIODependency(t *testing.T) {
	forbidden := map[string]string{
		"net/http":      "端口不发 HTTP，汇率源实现在 L4",
		"net":           "端口不碰网络",
		"database/sql":  "端口不查库",
		"os":            "端口不碰文件系统与环境变量",
		"io":            "端口无 IO",
		"encoding/json": "端口不解析任何汇率源的响应体",
		"github.com/cellargalaxy/jotcash/internal/base/config": "端点与密钥由 app 注入给 L4，端口不读配置",
	}

	for _, file := range packageFiles(t, false) {
		for _, imp := range parseImports(t, file) {
			if reason, bad := forbidden[imp]; bad {
				t.Errorf("%s 引入了 %q。%s", filepath.Base(file), imp, reason)
			}
		}
	}
}

// TestNoLogging 断言本包一行日志都不打。
//
// I-7 逐笔重算会以**每笔明细一次**的频率调用本包：一个汇率源故障，在端口打
// 一行日志就是 N 行几乎相同的记录，真正需要被看见的那条根因反而被淹没。
// 失败一律以错误返回，由 service/fx 在批处理收尾汇总（分层 §六 L5 5.2）。
func TestNoLogging(t *testing.T) {
	forbidden := []string{
		"github.com/sirupsen/logrus",
		"github.com/cellargalaxy/jotcash/internal/base/log",
		"log",
		"log/slog",
	}
	for _, file := range packageFiles(t, false) {
		for _, imp := range parseImports(t, file) {
			for _, bad := range forbidden {
				if imp == bad {
					t.Errorf("%s 引入了 %q。I-7 逐笔调用本包，端口打日志会产生 N 行重复记录、"+
						"淹没真正的根因；失败以错误返回，由 service/fx 汇总。", filepath.Base(file), imp)
				}
			}
		}
	}
}

// TestNoTimeDependency 断言本包不碰 time。
//
// 汇率日期是 base/calendar.Date——不带时区的日历日期，前提 7 要求支出日期
// 全程按账单字面值处理。一旦引入 time，就会出现「把 Date 转成 time.Time 再按
// 某时区取那一天」的写法，而那正是前提 7 要杜绝的：同一笔支出在不同部署时区下
// 会取到不同日期的汇率，金额随之不同，且不会有任何报错。
//
// 「日期缺省时取今天」这类便利写法也一并被拦下——ErrDateEmpty 的存在就是为了
// 不替调用方做那个决定。
func TestNoTimeDependency(t *testing.T) {
	for _, file := range packageFiles(t, false) {
		for _, imp := range parseImports(t, file) {
			if imp == "time" {
				t.Errorf("%s 引入了 time。汇率日期是不带时区的 calendar.Date（前提 7）；"+
					"转成 time.Time 后按某时区取「那一天」会让同一笔支出在不同部署时区下"+
					"取到不同日期的汇率，且不会有任何报错。", filepath.Base(file))
			}
		}
	}
}

// TestNoRoundingInPort 断言本包不做任何精度规整（T39：规整点唯一）。
//
// 用 AST 扫描而非文本匹配：包注释里正当地解释了「为什么不规整」，
// 按文本匹配会把那段解释也判为违规，逼后来人删注释来让测试通过——
// 那恰恰把这条设计决策的理由抹掉了（同 rule/dedup 对 DeletedAt 的处理）。
func TestNoRoundingInPort(t *testing.T) {
	// 这些标识符出现在**代码**里即意味着本包动了精度
	forbiddenIdents := map[string]string{
		"RoundRate":   "规整到 8 位由 rule/convert 统一完成（T39 单一规整点）",
		"RoundAmount": "端口不碰金额精度",
		"RateScale":   "引用汇率位数即已在本包建立第二个精度口径",
		"FitsScale":   "端口对返回值只判是否为正，不判位数",
		"Round":       "端口不做任何舍入",
	}

	fset := token.NewFileSet()
	for _, file := range packageFiles(t, false) {
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", file, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if reason, bad := forbiddenIdents[sel.Sel.Name]; bad {
				t.Errorf("%s 引用了 %s。%s——"+
					"在此规整不会让任何数值出错，但会让自动获取与 I-5 手填不再共用同一个规整点，"+
					"而「共用」正是 T39 要解决的问题本身。",
					filepath.Base(file), sel.Sel.Name, reason)
			}
			return true
		})
	}
}

// TestExportedSurface 锁死本包的导出面，防止承载外溢。
//
// 分层 §六 给本包的承载是四项：获取接口、路由注册表、失败语义、精度不设限。
// 多出来的导出符号（尤其是 Cache / Convert / MustFetch 这类）都意味着承载外溢：
// 缓存归 L4、折算归 rule/convert、Must 风格在有网络失败的场景下不成立。
func TestExportedSurface(t *testing.T) {
	want := map[string]bool{
		//① 获取接口与入参
		"Fetcher": true,
		"Query":   true,
		//② 路由注册表
		"Registry":    true,
		"NewRegistry": true,
		//③ 失败语义：六个哨兵
		"ErrFromCurrencyEmpty":  true,
		"ErrToCurrencyEmpty":    true,
		"ErrDateEmpty":          true,
		"ErrSourceUnregistered": true,
		"ErrFetch":              true,
		"ErrRateNotPositive":    true,
	}
	wantMethods := map[string]bool{
		"Query.Validate":   true,
		"Query.String":     true,
		"Registry.Fetch":   true,
		"Registry.Fetcher": true,
		"Registry.Sources": true,
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

// TestNoCacheAPI 断言本包不提供缓存入口。
//
// 「日汇率缓存（同日同币种对只取一次）」是 L4 的承载（分层 §六 L4 fxrate/<源> 行）。
// 放在端口会让每个源被迫接受同一套缓存口径，而各源的更新时点与限流规则并不相同：
// 有的源每日固定时刻发布、有的源按分钟滚动，同一套 TTL 对二者不可能都正确。
func TestNoCacheAPI(t *testing.T) {
	forbiddenNouns := []string{"Cache", "TTL", "Expire", "Evict", "Preload", "Warmup"}

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
			for _, noun := range forbiddenNouns {
				if strings.Contains(fn.Name.Name, noun) {
					t.Errorf("%s 导出了 %q，含缓存语义 %q。"+
						"日汇率缓存是 L4 的承载——放在端口会让每个源被迫接受同一套缓存口径，"+
						"而各源的更新时点与限流规则并不相同。",
						filepath.Base(file), fn.Name.Name, noun)
				}
			}
		}
	}
}

// TestNoLiteralOneReturn 扫描源码，确认没有任何一条「返回字面量 1 作为汇率」的路径。
//
// 这是 I-5「不静默按 1:1 处理」在本包的**结构性**断言，与
// TestFetchNeverReturnsOneOnFailure 的行为断言互补：后者遍历已知失败路径，
// 本用例则拦下将来新增的、尚未被行为用例覆盖的返回点。
func TestNoLiteralOneReturn(t *testing.T) {
	fset := token.NewFileSet()
	for _, file := range packageFiles(t, false) {
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", file, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			// 拦 decimal.FromInt(1) / decimal.MustParse("1") 这类构造
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "decimal" {
				return true
			}
			switch sel.Sel.Name {
			case "FromInt", "MustParse", "FromString":
				if len(call.Args) == 0 {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok {
					return true
				}
				val := strings.Trim(lit.Value, `"`)
				if val == "1" || val == "1.0" {
					t.Errorf("%s 构造了汇率常量 1（%s.%s(%s)）。"+
						"端口包不得存在任何返回 1 的路径——I-5 明写「不静默按 1:1 处理」；"+
						"同币种取 1 归 service/fx（分层 §四 修正 ②），占位汇率归 L4 的 fxrate/fixed。",
						filepath.Base(file), pkg.Name, sel.Sel.Name, lit.Value)
				}
			}
			return true
		})
	}
}

// --- 下游 service/fx 的四条用法走查 ---

// recordingFetcher 记录收到的全部 Query，用于验证调用方的用法。
type recordingFetcher struct {
	rate decimal.Decimal
	err  error
	seen []Query
}

func (f *recordingFetcher) Fetch(_ context.Context, q Query) (decimal.Decimal, error) {
	f.seen = append(f.seen, q)
	return f.rate, f.err
}

// TestConsumerServiceFxAutoFetch 走查 I-3/I-4：录入时自动取汇率并折算。
//
// 链路是「支出币种 ≠ 本位币 → 取支出日期的日汇率 → 交给 rule/convert 折算」。
// 本用例确认端口交出的汇率**未经规整**，正好是 rule/convert 期望的入参形态。
func TestConsumerServiceFxAutoFetch(t *testing.T) {
	f := &recordingFetcher{rate: decimal.MustParse("7.123456789")} // 9 位，超出汇率列的 8 位
	r := mustRegistry(t, f)

	q := Query{
		From: currency.MustParse("USD"),
		To:   currency.MustParse("CNY"),
		Date: calendar.MustParseDate("2026-09-07"),
	}
	rate, err := r.Fetch(context.Background(), q)
	if err != nil {
		t.Fatalf("取汇率失败: %v", err)
	}

	// 端口原样交出，规整由 rule/convert 在乘法前完成（T39）
	if rate.String() != "7.123456789" {
		t.Errorf("端口返回 %s, 期望原样的 7.123456789", rate)
	}
	// service/fx 随后自行规整——确认这一步确实落在调用方手里
	rounded := rate.RoundRate()
	if !rounded.FitsScale(decimal.RateScale) {
		t.Errorf("调用方规整后 %s 仍不落在 %d 位内", rounded, decimal.RateScale)
	}

	// 端口把三个字段原样传给了实现，没有替调用方改写任何一项
	if len(f.seen) != 1 {
		t.Fatalf("实现被调用 %d 次, 期望 1 次", len(f.seen))
	}
	if f.seen[0] != q {
		t.Errorf("端口改写了 Query: 收到 %s, 期望 %s", f.seen[0], q)
	}
}

// TestConsumerServiceFxI5Fallback 走查 I-5：取汇率失败 → 该行汇率转必填。
//
// service/fx 的判据是 errors.Is(err, ErrFetch)。本用例确认这个判据可用，
// 且**没有任何一条**失败路径会让它拿到一个可用的汇率。
func TestConsumerServiceFxI5Fallback(t *testing.T) {
	srcErr := errors.New("模拟：汇率源 503")
	r := mustRegistry(t, &recordingFetcher{err: srcErr})

	rate, err := r.Fetch(context.Background(), Query{
		From: currency.MustParse("USD"),
		To:   currency.MustParse("CNY"),
		Date: calendar.MustParseDate("2026-09-07"),
	})

	// service/fx 据此把该行的「折算汇率」转为必填
	if !errors.Is(err, ErrFetch) {
		t.Fatalf("I-5 的判据 errors.Is(err, ErrFetch) 不成立: %v", err)
	}
	// 且绝不能拿到 1——那正是 I-5 禁止的静默 1:1
	if rate.Equal(decimal.FromInt(1)) {
		t.Error("取汇率失败却返回了 1，I-5 明写「不静默按 1:1 处理」")
	}
	if rate.IsPositive() {
		t.Errorf("取汇率失败却返回了可用的正数汇率 %s", rate)
	}
	// 排错时仍能拿到源的具体错误
	if !errors.Is(err, srcErr) {
		t.Error("源的原始错误未穿透，运维无法区分 503 与超时")
	}
}

// TestConsumerServiceFxI4ShortCircuit 走查 I-4：同币种短路在 service/fx 一侧。
//
// 本用例模拟 service/fx 的正确写法——**先判同币种、再决定是否调端口**，
// 并确认端口在被短路时确实一次都没被调用。
func TestConsumerServiceFxI4ShortCircuit(t *testing.T) {
	f := &recordingFetcher{rate: decimal.MustParse("7.1")}
	r := mustRegistry(t, f)

	base := currency.MustParse("CNY")
	date := calendar.MustParseDate("2026-09-07")

	// service/fx 的短路判据（分层 §四 修正 ②）
	fetchRate := func(from currency.Code) (decimal.Decimal, error) {
		if from == base {
			return decimal.FromInt(1), nil // 短路：不调 fxrate
		}
		return r.Fetch(context.Background(), Query{From: from, To: base, Date: date})
	}

	// 本币记账：短路，端口不被调用
	rate, err := fetchRate(base)
	if err != nil {
		t.Fatalf("本币记账不应失败: %v", err)
	}
	if !rate.Equal(decimal.FromInt(1)) {
		t.Errorf("本币记账汇率应为 1, 实得 %s", rate)
	}
	if len(f.seen) != 0 {
		t.Errorf("本币记账不应调用汇率源，实得 %d 次调用", len(f.seen))
	}

	// 外币记账：正常走端口
	if _, err := fetchRate(currency.MustParse("USD")); err != nil {
		t.Fatalf("外币记账失败: %v", err)
	}
	if len(f.seen) != 1 {
		t.Errorf("外币记账应调用汇率源 1 次，实得 %d 次", len(f.seen))
	}
}

// TestConsumerServiceFxI7Recompute 走查 I-7：逐笔重算，可中断、失败计数不打断整批。
//
// 分层 §六 L5 5.2 要求重算收尾「记成功/失败笔数」。本用例模拟那一趟：
// 部分明细取汇率失败时，批处理继续、失败计数递增，而不是整批中止。
func TestConsumerServiceFxI7Recompute(t *testing.T) {
	// 隔行失败的实现：模拟部分币种对缺数据
	flaky := &flakyFetcher{rate: decimal.MustParse("7.1")}
	r := mustRegistry(t, flaky)

	base := currency.MustParse("CNY")
	dates := []string{"2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04"}

	var ok, failed int
	for _, d := range dates {
		_, err := r.Fetch(context.Background(), Query{
			From: currency.MustParse("USD"),
			To:   base,
			Date: calendar.MustParseDate(d),
		})
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrFetch):
			// I-7：失败笔数递增，整批继续（终版 I-7「重算结果需可查看」）
			failed++
		default:
			t.Fatalf("非预期的错误类型: %v", err)
		}
	}

	if ok+failed != len(dates) {
		t.Errorf("成功 %d + 失败 %d ≠ 总数 %d，有明细既没成功也没被计失败", ok, failed, len(dates))
	}
	if failed == 0 {
		t.Error("用例前提：应有部分明细失败")
	}
	if ok == 0 {
		t.Error("用例前提：应有部分明细成功——失败不应打断整批")
	}
}

// flakyFetcher 隔次失败，模拟部分币种对缺数据。
type flakyFetcher struct {
	rate decimal.Decimal
	n    int
}

func (f *flakyFetcher) Fetch(_ context.Context, _ Query) (decimal.Decimal, error) {
	f.n++
	if f.n%2 == 0 {
		return decimal.Decimal{}, errors.New("模拟：该币种对当日无数据")
	}
	return f.rate, nil
}

// --- 测试辅助（与 rule/dedup 的同名辅助保持一致的写法）---

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
			t.Errorf("%s 多出 %q——承载外溢，分层 §六 给本包的承载是四项："+
				"获取接口、路由注册表、失败语义、精度不设限；"+
				"若确需新增，先改文档再改这张表", label, name)
		}
	}
}
