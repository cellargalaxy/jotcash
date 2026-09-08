package amortize

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
)

// 本文件覆盖不变式 1b（起止月派生）与包级依赖边界。
// 8.2 均分在 shares_test.go，H-4 与 J 域在 schedule_test.go，
// 下游用法走查在 consumer_check_test.go。

// TestDeriveInvariant1b 逐条验证不变式 1b：
// 起始月 = 支出日期所属月；结束月 = 起始月 + 摊分月数 − 1。
//
// 用例覆盖分层 §九 不变式 1b 与终版 H-1/H-2 的全部要点：默认不摊分、
// 跨年、跨多年、月末日期、闰日、负金额场景（起始月不受金额影响）。
func TestDeriveInvariant1b(t *testing.T) {
	cases := []struct {
		date   string
		months int
		start  string
		end    string
		desc   string
	}{
		{"2026-01-15", 1, "2026-01", "2026-01", "默认 1 个月（不摊分，H-1）起止同月"},
		{"2026-01-15", 3, "2026-01", "2026-03", "月内 3 个月"},
		{"2026-11-30", 12, "2026-11", "2027-10", "跨年 12 个月"},
		{"2026-01-01", 600, "2026-01", "2075-12", "600 个月（无上限，前提 5）"},
		{"2026-01-31", 2, "2026-01", "2026-02", "月末日期：起始月只看年月，不看日"},
		{"2028-02-29", 1, "2028-02", "2028-02", "闰日"},
		{"2026-12-31", 1, "2026-12", "2026-12", "年末最后一天"},
		{"2026-12-31", 2, "2026-12", "2027-01", "年末跨年"},
		{"0001-01-01", 1, "0001-01", "0001-01", "年份下界"},
		{"9999-12-31", 1, "9999-12", "9999-12", "年份上界，恰好不越界"},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			r, err := Derive(calendar.MustParseDate(c.date), c.months)
			if err != nil {
				t.Fatalf("派生报错: %v", err)
			}
			//起始月 = 支出日期所属月
			if got := r.Start().String(); got != c.start {
				t.Errorf("起始月 = %s, 期望 %s", got, c.start)
			}
			//结束月 = 起始月 + 月数 − 1
			if got := r.End().String(); got != c.end {
				t.Errorf("结束月 = %s, 期望 %s", got, c.end)
			}
			//区间长度必须等于摊分月数——它同时是均分的份数，
			//两者不等会让「区间 3 个月、份额切 4 份」成为可能
			if r.Len() != c.months {
				t.Errorf("区间长度 = %d, 期望 %d", r.Len(), c.months)
			}
		})
	}
}

// TestDeriveStartMonthIgnoresAmountSign 验证终版 H-2「负金额（退款）规则完全
// 对称，从退款发生月起摊，不改写历史月份」。
//
// Derive 的签名里根本没有金额——这正是该要求得以自动满足的原因：
// 本函数无从根据符号做出不同的起始月。本用例把这条性质钉成断言，
// 防止后续有人往签名里加金额参数并据此分支。
func TestDeriveStartMonthIgnoresAmountSign(t *testing.T) {
	//同一日期、同一月数，无论这笔是支出还是退款，起止月必须完全一致
	date := calendar.MustParseDate("2026-06-15")
	r, err := Derive(date, 3)
	if err != nil {
		t.Fatalf("派生报错: %v", err)
	}
	if r.Start().String() != "2026-06" || r.End().String() != "2026-08" {
		t.Fatalf("区间 = %s, 期望 2026-06~2026-08", r)
	}
	//「不改写历史月份」：起始月不得早于支出日期所属月
	if r.Start().Before(date.ToMonth()) {
		t.Errorf("起始月 %s 早于支出日期所属月 %s，违反 H-2「不改写历史月份」",
			r.Start(), date.ToMonth())
	}
}

// TestDeriveErrors 验证三类非法输入各自返回可判定的哨兵错误。
func TestDeriveErrors(t *testing.T) {
	t.Run("支出日期为零值报 ErrSpendDateEmpty", func(t *testing.T) {
		var zero calendar.Date
		_, err := Derive(zero, 3)
		if !errors.Is(err, ErrSpendDateEmpty) {
			t.Errorf("err = %v, 期望 ErrSpendDateEmpty", err)
		}
		//必须是本包的哨兵而不是 calendar 的 ErrMonthValue——
		//后者文案是「月份超出 1~12」，会把排错引向错误方向
		if errors.Is(err, calendar.ErrMonthValue) {
			t.Error("不应返回 calendar.ErrMonthValue：日期未填与月份数值非法是两回事")
		}
	})

	t.Run("月数非正报 ErrMonthsNotPositive", func(t *testing.T) {
		for _, months := range []int{0, -1, -12} {
			_, err := Derive(calendar.MustParseDate("2026-01-15"), months)
			if !errors.Is(err, ErrMonthsNotPositive) {
				t.Errorf("months=%d: err = %v, 期望 ErrMonthsNotPositive", months, err)
			}
			//错误信息须带上非法值，否则排错时不知道传进来的是什么
			if !strings.Contains(err.Error(), strconv.Itoa(months)) {
				t.Errorf("months=%d: 错误信息 %q 未包含非法值", months, err.Error())
			}
		}
	})

	t.Run("结束月越界透传 calendar.ErrYearRange", func(t *testing.T) {
		//9999-12 起摊 2 个月，结束月落到 10000-01
		_, err := Derive(calendar.MustParseDate("9999-12-01"), 2)
		if !errors.Is(err, calendar.ErrYearRange) {
			t.Errorf("err = %v, 期望 calendar.ErrYearRange", err)
		}
	})
}

// TestDeriveUpperBound 验证 H-1「摊分月数不设上限」的实际边界：
// 本包不设月数上界，实际边界由 base/calendar 的年份上界给出。
//
// 这条用例的价值在于「无上限」不等于「无边界」——把真实边界写成断言，
// 后续若有人误加月数上限常量，这里会当场失败。
func TestDeriveUpperBound(t *testing.T) {
	start := calendar.MustParseDate("2026-01-01")

	//二分找出不越界的最大月数
	lo, hi := 1, 200000
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if _, err := Derive(start, mid); err == nil {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	//实测：2026-01 起最多 95688 个月，结束月恰为 9999-12
	const wantMax = 95688
	if lo != wantMax {
		t.Errorf("2026-01 起的最大摊分月数 = %d, 期望 %d", lo, wantMax)
	}
	r, err := Derive(start, lo)
	if err != nil {
		t.Fatalf("最大月数派生报错: %v", err)
	}
	if r.End().String() != "9999-12" {
		t.Errorf("最大月数的结束月 = %s, 期望 9999-12", r.End())
	}
	//再多一个月必须报错，而不是静默回绕
	if _, err := Derive(start, lo+1); !errors.Is(err, calendar.ErrYearRange) {
		t.Errorf("超出一个月应报 ErrYearRange, 实际 %v", err)
	}
}

// TestDeriveIsPure 验证 Derive 是纯函数：同样的入参恒得同样的出参。
//
// 准则 2 要求 L2 规则层「纯逻辑、无外部 IO、可脱库单测」。摊分若依赖当前
// 时间（例如「从本月起摊」），同一笔明细在不同月份查询会得到不同的区间，
// J 域的历史统计就会随时间漂移。
func TestDeriveIsPure(t *testing.T) {
	date := calendar.MustParseDate("2026-03-20")
	first, err := Derive(date, 7)
	if err != nil {
		t.Fatalf("派生报错: %v", err)
	}
	for i := 0; i < 100; i++ {
		got, err := Derive(date, 7)
		if err != nil {
			t.Fatalf("第 %d 次派生报错: %v", i, err)
		}
		if got != first {
			t.Fatalf("第 %d 次派生结果 %s ≠ 首次 %s，Derive 不是纯函数", i, got, first)
		}
	}
}

// TestNoUpwardDependency 以 AST 扫描锁死依赖边界：本包处于 L2，
// 依赖恰为 currency（L1.0）、base/calendar 与 base/decimal（L0）。
//
// 分层 §六 L2 表要求「纯逻辑、无外部 IO」，§九 要求错误由调用方分档
// （故不得 import base/errs）。这条约束靠人工评审守不住——一次
// 「顺手 import 一下 store」就会让 L2 变成可读库的层，且编译完全通过。
func TestNoUpwardDependency(t *testing.T) {
	//白名单：本包允许出现的仓库内 import
	allowed := map[string]bool{
		"github.com/cellargalaxy/jotcash/internal/currency":      true,
		"github.com/cellargalaxy/jotcash/internal/base/calendar": true,
		"github.com/cellargalaxy/jotcash/internal/base/decimal":  true,
	}
	//显式禁止：这几个是最容易被误引且后果最重的
	forbidden := map[string]string{
		"github.com/cellargalaxy/jotcash/internal/base/errs": "错误须由调用方分档（包注释「错误是哨兵」）",
		"github.com/cellargalaxy/jotcash/internal/base/log":  "L2 是纯逻辑，不打日志",
		"github.com/cellargalaxy/jotcash/internal/entity":    "见包注释「为什么不收 entity.Expense」",
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析本包源码失败: %v", err)
	}

	const repo = "github.com/cellargalaxy/jotcash/"
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			//测试文件可以多引一些（如 go/ast），只查生产代码
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatalf("%s: import 路径无法解析: %v", filepath.Base(name), err)
				}
				//标准库与第三方不在本用例的判定范围
				if !strings.HasPrefix(path, repo) {
					continue
				}
				if reason, bad := forbidden[path]; bad {
					t.Errorf("%s 不得 import %s：%s", filepath.Base(name), path, reason)
					continue
				}
				if !allowed[path] {
					t.Errorf("%s import 了白名单外的仓库内包 %s（L2 不得依赖 L3 及以上）",
						filepath.Base(name), path)
				}
			}
		}
	}
}

// TestNoTimeDependency 验证生产代码不 import time：本包无时间依赖。
//
// J-6 的「已发生（≤ 当前月）/ 未来待摊」分段需要读系统时间，但那归
// service/stat（见包注释「不做什么」）。若本包引入 time，摊分结果就会
// 随查询时刻变化，Derive 的纯函数性质随之失效。
func TestNoTimeDependency(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析本包源码失败: %v", err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			for _, spec := range file.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				if path == "time" {
					t.Errorf("%s import 了 time：本包无时间依赖，J-6 分段归 service/stat",
						filepath.Base(name))
				}
			}
		}
	}
}

// TestExportedSymbolsHaveDoc 验证全部导出符号都有文档注释（约定 3：注释一律中文）。
//
// 本包是不变式 1b 的唯一实现处，下游三个 service 包都要读它的注释确定用法；
// 漏注释的导出符号会让「唯一实现处」失去可读性这一半价值。
func TestExportedSymbolsHaveDoc(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("解析本包源码失败: %v", err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if !d.Name.IsExported() {
						continue
					}
					if d.Doc == nil || len(d.Doc.List) == 0 {
						t.Errorf("%s: 导出函数/方法 %s 缺少文档注释", filepath.Base(name), d.Name.Name)
					}
				case *ast.GenDecl:
					if d.Tok != token.TYPE {
						continue
					}
					for _, spec := range d.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok || !ts.Name.IsExported() {
							continue
						}
						//类型注释可能挂在 GenDecl 上（单类型声明块）
						if ts.Doc == nil && d.Doc == nil {
							t.Errorf("%s: 导出类型 %s 缺少文档注释", filepath.Base(name), ts.Name.Name)
						}
					}
				}
			}
		}
	}
}
