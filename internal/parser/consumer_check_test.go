package parser

import (
	"context"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// parseOwnPackage 解析本包的**非测试**源码，供依赖边界与注释语种检查使用。
func parseOwnPackage(t *testing.T) map[string]*ast.File {
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
	return files
}

// TestDependencyBoundary 双向锁死本包的依赖边界，判据取自分层 §六 L3 表
// parser 行的依赖列与承载末句。
//
// # 两个方向都必须查
//
//	不得依赖  entity（承载末句明写）、任何 L4/L5/L6 包、任何具体格式的词法库
//	必须只用  enum、currency、base/errs、base/decimal、base/calendar
//
// 只查「不得依赖」是不够的：漏了「必须只用」这一侧，本包可以悄悄多 import 一个
// 依赖列里没有的包（例如 rule/convert），而那会让端口层开始承担规则层的职责。
func TestDependencyBoundary(t *testing.T) {
	t.Parallel()

	const modulePrefix = "github.com/cellargalaxy/jotcash/"

	//依赖列的逐字副本（分层 §六 L3 表 parser 行）
	allowedInternal := map[string]bool{
		"internal/enum":          true,
		"internal/currency":      true,
		"internal/base/errs":     true,
		"internal/base/decimal":  true,
		"internal/base/calendar": true,
	}
	//标准库白名单：本包只该用到这三个
	allowedStdlib := map[string]bool{
		"context": true, //Parse 的首参
		"errors":  true, //errors.Is 判跨包哨兵
		"fmt":     true, //装配错误
		"unicode": true, //isBlank
	}

	for name, file := range parseOwnPackage(t) {
		base := filepath.Base(name)
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)

			if !strings.HasPrefix(path, modulePrefix) {
				if !allowedStdlib[path] {
					t.Errorf("%s 引入了预期外的外部包 %q：端口层应保持极小的依赖面",
						base, path)
				}
				continue
			}

			rel := strings.TrimPrefix(path, modulePrefix)

			//「不得依赖」一侧，逐条给出违反的条款
			switch {
			case rel == "internal/entity" || strings.HasPrefix(rel, "internal/entity/"):
				t.Errorf("%s 不得 import entity：承载明写「不依赖 entity——原始行到 "+
					"entity.Expense 的映射归 service/intake」", base)
			case strings.HasPrefix(rel, "internal/parser/"):
				t.Errorf("%s 不得 import 自己的 L4 子包 %q：准则 4「端口在下、实现在上」，"+
					"否则新增银行会反向影响端口层", base, rel)
			case strings.HasPrefix(rel, "internal/service/"),
				strings.HasPrefix(rel, "internal/store/"),
				strings.HasPrefix(rel, "internal/api/"),
				strings.HasPrefix(rel, "internal/app"):
				t.Errorf("%s 不得 import 上层包 %q：L3 不能依赖 L4/L5/L6", base, rel)
			case strings.HasPrefix(rel, "internal/rule/"):
				t.Errorf("%s 不得 import 规则层 %q：折算与摊分归 service/intake 编排，"+
					"端口层不参与（取舍 3）", base, rel)
			}

			//「必须只用」一侧
			if !allowedInternal[rel] {
				t.Errorf("%s 引入了依赖列之外的仓库内包 %q：分层 §六 L3 表 parser 行的"+
					"依赖列是 enum / currency / base/errs / base/decimal / base/calendar",
					base, rel)
			}
		}
	}
}

// TestNoCSVOrFormatSpecificImport 单独盯住「端口层不得固化具体格式的选型」。
//
// encoding/csv 是最容易被误加进来的一个——写 RawRow 时顺手想「解析 CSV 总要用
// 它吧」。但一旦本包 import 它，「CSV」这一选型就固化进了端口层，而 D-1 的
// 扩展方式恰恰要求端口层对格式无感（同为 PDF，各银行版式不同）。
func TestNoCSVOrFormatSpecificImport(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"encoding/csv", "encoding/xml", "archive/zip",
		"github.com/gocarina/gocsv", "github.com/xuri/excelize",
		"github.com/tealeg/xlsx", "github.com/pdfcpu/pdfcpu",
		"github.com/ledongthuc/pdf", "github.com/extrame/xls",
	}
	for name, file := range parseOwnPackage(t) {
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s 不得 import 具体格式库 %q：那会把该格式的选型固化进"+
						"端口层，违反准则 4 与 D-1", filepath.Base(name), path)
				}
			}
		}
	}
}

// TestSourceCommentsAreChinese 抽查实现文件的注释是否为中文（约定 3）。
//
// 与 enum 包的同名用例同一形态：只做弱校验（每个文件的注释里含中文字符），
// 不逐行判语种——后者会误伤代码示例、专有名词与 URL。
func TestSourceCommentsAreChinese(t *testing.T) {
	t.Parallel()

	hasCJK := func(s string) bool {
		for _, r := range s {
			if r >= 0x4e00 && r <= 0x9fff {
				return true
			}
		}
		return false
	}
	for name, file := range parseOwnPackage(t) {
		var text strings.Builder
		for _, cg := range file.Comments {
			text.WriteString(cg.Text())
		}
		if !hasCJK(text.String()) {
			t.Errorf("%s 的注释中未见中文：约定 3 要求注释与日志一律中文",
				filepath.Base(name))
		}
	}
}

// TestExportedSurfaceIsMinimal 锁死本包的导出面。
//
// 端口层的导出面就是它的契约：多导出一个东西，L4 与 L5 就多一条可以依赖的路径，
// 将来收回来就是破坏性变更。本用例以「预期清单」的形式钉住，新增导出项必须
// 同时在这里登记——那一步正好是一次「这个真该导出吗」的自问。
func TestExportedSurfaceIsMinimal(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		//类型
		"Parser": true, "Input": true, "RawRow": true, "Registry": true,
		//注册表
		"NewRegistry": true,
		//文案键
		"KeyFormatMismatch": true, "KeyContentCorrupt": true,
		"KeyFieldMissing": true, "KeyDecryptFailed": true,
		"KeyTypeMismatch": true, "KeyTypeUnsupported": true,
		"KeyNotRegistered": true,
		//错误构造与分类
		"NewFormatMismatchError": true, "WrapFormatMismatchError": true,
		"NewContentCorruptError": true, "NewFieldMissingError": true,
		"NewDecryptFailedError": true, "NewTypeMismatchError": true,
		"NewTypeUnsupportedError": true, "NewNotRegisteredError": true,
		"CheckFormat": true, "ClassifyValueError": true, "ClassifyRequired": true,
	}

	for name, file := range parseOwnPackage(t) {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				//方法不算包级导出面，由 Registry 的用例单独管
				if d.Recv != nil || !d.Name.IsExported() {
					continue
				}
				if !want[d.Name.Name] {
					t.Errorf("%s 新增了未登记的导出函数 %s：请确认是否真该进入端口契约",
						filepath.Base(name), d.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() && !want[s.Name.Name] {
							t.Errorf("%s 新增了未登记的导出类型 %s",
								filepath.Base(name), s.Name.Name)
						}
					case *ast.ValueSpec:
						for _, id := range s.Names {
							if id.IsExported() && !want[id.Name] {
								t.Errorf("%s 新增了未登记的导出标识符 %s",
									filepath.Base(name), id.Name)
							}
						}
					}
				}
			}
		}
	}
}

// ============================================================================
// 以下是下游消费方走查：以「将来那个包会怎么用本包」的形态回放调用链，
// 验证本包提供的东西足够、且形状顺手。它们不测本包的内部实现，测的是**契约**。
// ============================================================================

// TestConsumerServiceFileUploadCheck 回放 service/file 在 C-1 上传时的校验链
// （D-2 选解析器 + D-3 第五类在解析前拦下）。
//
// 走查三步：用户选的类型键是否合法 → 是否启用（C-1 的业务规则）→ 与文件格式
// 是否匹配。第二步刻意不由本包做（见 CheckFormat 的注释），本用例因此同时确认
// 「本包提供的东西足以让 service/file 自己补上那一步」。
func TestConsumerServiceFileUploadCheck(t *testing.T) {
	t.Parallel()

	reg, err := NewRegistry(&stubParser{pt: enum.ParserTypeGenericCSV})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}

	//service/file 的校验函数会长这样
	check := func(rawType string, format enum.FileFormat) string {
		pt, err := enum.ParseParserType(rawType)
		if err != nil {
			return errs.MsgKeyOf(NewTypeUnsupportedError(rawType))
		}
		//C-1：停用项不得被新选择。用 Enabled 而非 Valid，依据 enum 的注释
		if !pt.Enabled() {
			return "file.parser_type_disabled" //由 service/file 声明的键，此处仅示意
		}
		//D-3 第五类：解析前就能拦下
		if err := CheckFormat(pt, format); err != nil {
			return errs.MsgKeyOf(err)
		}
		//能取到实现才算真的可用
		if !reg.Has(pt) {
			return errs.MsgKeyOf(NewNotRegisteredError(pt))
		}
		return ""
	}

	cases := []struct {
		name     string
		rawType  string
		format   enum.FileFormat
		wantKey  string
		wantPass bool
	}{
		{"正常放行", "generic_csv", enum.FileFormatCSV, "", true},
		{"类型不存在", "no_such", enum.FileFormatCSV, KeyTypeUnsupported, false},
		{"类型为空", "", enum.FileFormatCSV, KeyTypeUnsupported, false},
		{"CSV 解析器收到 PDF", "generic_csv", enum.FileFormatPDF, KeyTypeMismatch, false},
		{"CSV 解析器收到 Excel", "generic_csv", enum.FileFormatExcel, KeyTypeMismatch, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := check(c.rawType, c.format)
			if c.wantPass {
				if got != "" {
					t.Errorf("应放行，实际报 %q", got)
				}
				return
			}
			if got != c.wantKey {
				t.Errorf("应报 %q，实际 %q", c.wantKey, got)
			}
		})
	}
}

// TestConsumerServiceIntakeParseFlow 回放 service/intake 的解析链（D-2 → D-4）：
// 从注册表取实现 → 传文件内容与密码 → 拿到原始行 → 按档位分流错误。
//
// 本用例确认三件事：入参形状够用（含密码）、返回的行能被上层继续加工、
// 五类错误都能被 middleware 按档位映射成 400。
func TestConsumerServiceIntakeParseFlow(t *testing.T) {
	t.Parallel()

	date, err := calendar.ParseDate("2026-01-15")
	if err != nil {
		t.Fatalf("构造日期失败: %v", err)
	}
	amount, err := decimal.Parse("-88.50")
	if err != nil {
		t.Fatalf("构造金额失败: %v", err)
	}
	cny, err := currency.Parse("CNY")
	if err != nil {
		t.Fatalf("构造币种失败: %v", err)
	}

	//一个「解析出两行、其中一行是退款、卡片属性留空」的实现
	impl := &stubParser{
		pt: enum.ParserTypeGenericCSV,
		rows: []RawRow{
			{Date: date, Amount: amount, Currency: cny, Counterparty: "超市", Remark: "退货"},
			{Date: date, Currency: cny}, //卡片三属性与金额留空（D-2「解析不出则留空」）
		},
	}
	reg, err := NewRegistry(impl)
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}

	//service/intake 的解析步骤会长这样
	parse := func(ctx context.Context, pt enum.ParserType, content []byte, password string) ([]RawRow, error) {
		p, err := reg.Get(pt)
		if err != nil {
			return nil, err
		}
		return p.Parse(ctx, Input{Content: content, Password: password})
	}

	rows, err := parse(context.Background(), enum.ParserTypeGenericCSV,
		[]byte("date,amount\n"), "pw")
	if err != nil {
		t.Fatalf("解析应成功，实际 %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应解析出 2 行，实际 %d 行", len(rows))
	}

	//第一行：负金额（退款）原样保留
	if rows[0].Amount.Sign() >= 0 {
		t.Error("退款行的负号应保留")
	}
	//第二行：留空的字段确实是零值，上层据此知道「要用户在预览页补」（D-2）
	if rows[1].BankName != "" || rows[1].CardLast4 != "" || !rows[1].CardCurrency.IsZero() {
		t.Error("卡片三属性应留空")
	}
	//密码确实传到了实现
	if impl.gotInput.Password != "pw" {
		t.Errorf("文件密码应透传，实际 %q", impl.gotInput.Password)
	}
}

// TestConsumerMiddlewareMapsToHTTP 回放 api/http/middleware 的分流：五类解析
// 错误一律 400，注册表的两类失败一个 400 一个 500，且每条都能取到非空文案键。
//
// 这条走查的价值在于「档位 + 键」三件套是否齐备——middleware 拿到一条错误后，
// 需要状态码去回响应、需要键去 i18n 取词。缺任何一样都会退化成一条无意义的
// 「系统错误」。
func TestConsumerMiddlewareMapsToHTTP(t *testing.T) {
	t.Parallel()

	//middleware 的映射（三条已由分层写死，用户输入错误 → 400）
	statusOf := func(kind errs.Kind) int {
		switch kind {
		case errs.KindInvalidInput:
			return 400
		case errs.KindUnauthorized:
			return 401
		case errs.KindForbidden:
			return 403
		case errs.KindConflict:
			return 409
		default:
			return 500
		}
	}

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantKey    string
	}{
		{"格式不符", NewFormatMismatchError("缺表头"), 400, KeyFormatMismatch},
		{"内容损坏", NewContentCorruptError(decimal.ErrSyntax, 3, "金额", "abc"), 400, KeyContentCorrupt},
		{"字段缺失", NewFieldMissingError(3, "金额"), 400, KeyFieldMissing},
		{"解密失败", NewDecryptFailedError(nil), 400, KeyDecryptFailed},
		{"类型不匹配", NewTypeMismatchError(enum.ParserTypeGenericCSV, enum.FileFormatPDF), 400, KeyTypeMismatch},
		{"类型键非法", NewTypeUnsupportedError("x"), 400, KeyTypeUnsupported},
		{"未注册实现", NewNotRegisteredError(enum.ParserTypeGenericCSV), 500, KeyNotRegistered},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := statusOf(errs.KindOf(c.err)); got != c.wantStatus {
				t.Errorf("HTTP 状态应为 %d，实际 %d", c.wantStatus, got)
			}
			key := errs.MsgKeyOf(c.err)
			if key != c.wantKey {
				t.Errorf("文案键应为 %q，实际 %q", c.wantKey, key)
			}
			if key == "" {
				t.Error("文案键为空，i18n 无法取词")
			}
		})
	}
}

// TestConsumerAppWiring 回放 app 的装配（约定 8）：构造注册表 → 自检覆盖率 →
// 失败则让启动失败。
//
// 同时确认「阶段 4 的当前状态是可表达的」：现在还没有任何 L4 实现，装配一个
// 空表必须成功（而不是让本包在阶段 4 无法被装配），漏配由自检报出来。
func TestConsumerAppWiring(t *testing.T) {
	t.Parallel()

	//app 的装配函数会长这样
	wire := func(parsers ...Parser) (*Registry, []enum.ParserType, error) {
		reg, err := NewRegistry(parsers...)
		if err != nil {
			return nil, nil, err //装配写错 → 启动失败
		}
		missing, err := reg.CheckEnabledCoverage()
		return reg, missing, err
	}

	t.Run("阶段4：尚无实现，空表可装配但自检报缺", func(t *testing.T) {
		t.Parallel()
		reg, missing, err := wire()
		if reg == nil {
			t.Fatal("空表应能装配成功（阶段 4 的实际状态）")
		}
		if err == nil || len(missing) == 0 {
			t.Error("自检应报出启用项尚无实现")
		}
	})

	t.Run("阶段6：CSV 实现到位后自检通过", func(t *testing.T) {
		t.Parallel()
		var parsers []Parser
		for _, pt := range enum.EnabledParserTypes() {
			parsers = append(parsers, &stubParser{pt: pt})
		}
		reg, missing, err := wire(parsers...)
		if reg == nil {
			t.Fatal("装配应成功")
		}
		if err != nil || len(missing) != 0 {
			t.Errorf("全覆盖时自检应通过，实际 missing=%v err=%v", missing, err)
		}
		//D-2 的上传页下拉由 Types 驱动，应与 enum 的启用项一致
		if got, want := len(reg.Types()), len(enum.EnabledParserTypes()); got != want {
			t.Errorf("可用解析器应有 %d 个，实际 %d 个", want, got)
		}
	})

	t.Run("装配写错则启动失败", func(t *testing.T) {
		t.Parallel()
		if _, _, err := wire(nil); err == nil {
			t.Error("nil 实现应让装配失败")
		}
	})
}

// TestConsumerL4ImplementsParser 回放一个 L4 实现（将来的 parser/csv）会怎么用
// 本包的分类工具：逐行逐列解析，把下游哨兵交给分类器，带上行号与列名。
//
// 这条走查是对「本包提供的工具是否够用」最直接的检验——如果一个真实实现写起来
// 还需要自己拼错误，那说明本包缺东西。
func TestConsumerL4ImplementsParser(t *testing.T) {
	t.Parallel()

	//一个极简的「CSV 实现」：只认 日期,金额,币种 三列，逐行解析
	impl := parserFunc(func(ctx context.Context, in Input) ([]RawRow, error) {
		lines := strings.Split(strings.TrimRight(string(in.Content), "\n"), "\n")
		if len(lines) == 0 || lines[0] != "日期,金额,币种" {
			return nil, NewFormatMismatchError("缺少表头或表头不符")
		}
		var rows []RawRow
		for i, line := range lines[1:] {
			lineNo := i + 2 //1 起且含表头行
			cells := strings.Split(line, ",")
			if len(cells) != 3 {
				return nil, NewFormatMismatchError("列数不符")
			}

			//必填判空在解析前
			if err := ClassifyRequired(lineNo, "日期", cells[0]); err != nil {
				return nil, err
			}
			date, err := calendar.ParseDate(strings.TrimSpace(cells[0]))
			if err != nil {
				return nil, ClassifyValueError(err, lineNo, "日期", cells[0])
			}
			amount, err := decimal.Parse(strings.TrimSpace(cells[1]))
			if err != nil {
				return nil, ClassifyValueError(err, lineNo, "金额", cells[1])
			}
			code, err := currency.Parse(strings.TrimSpace(cells[2]))
			if err != nil {
				return nil, ClassifyValueError(err, lineNo, "币种", cells[2])
			}
			rows = append(rows, RawRow{Date: date, Amount: amount, Currency: code})
		}
		return rows, nil
	})

	cases := []struct {
		name     string
		content  string
		wantKey  string
		wantRows int
		wantLine int
	}{
		{
			name:     "正常两行",
			content:  "日期,金额,币种\n2026-01-15,100.00,CNY\n2026-01-16,-88.50,CNY\n",
			wantRows: 2,
		},
		{
			name:    "表头不符",
			content: "date,amount,currency\n2026-01-15,100.00,CNY\n",
			wantKey: KeyFormatMismatch,
		},
		{
			name:    "列数不符",
			content: "日期,金额,币种\n2026-01-15,100.00\n",
			wantKey: KeyFormatMismatch,
		},
		{
			name:     "日期列空 → 字段缺失，行号 2",
			content:  "日期,金额,币种\n,100.00,CNY\n",
			wantKey:  KeyFieldMissing,
			wantLine: 2,
		},
		{
			name:     "金额列空 → 字段缺失，行号 3",
			content:  "日期,金额,币种\n2026-01-15,100.00,CNY\n2026-01-16,,CNY\n",
			wantKey:  KeyFieldMissing,
			wantLine: 3,
		},
		{
			name:     "金额写成 abc → 内容损坏",
			content:  "日期,金额,币种\n2026-01-15,abc,CNY\n",
			wantKey:  KeyContentCorrupt,
			wantLine: 2,
		},
		{
			name:     "日期不存在 → 内容损坏",
			content:  "日期,金额,币种\n2026-02-30,100.00,CNY\n",
			wantKey:  KeyContentCorrupt,
			wantLine: 2,
		},
		{
			name:     "币种 XXX → 内容损坏",
			content:  "日期,金额,币种\n2026-01-15,100.00,XXX\n",
			wantKey:  KeyContentCorrupt,
			wantLine: 2,
		},
		{
			name:     "全角空格填的金额 → 字段缺失（PDF/Excel 抽取常见）",
			content:  "日期,金额,币种\n2026-01-15,\u3000,CNY\n",
			wantKey:  KeyFieldMissing,
			wantLine: 2,
		},
		{
			name:     "em space 填的金额 → 字段缺失",
			content:  "日期,金额,币种\n2026-01-15,\u2003,CNY\n",
			wantKey:  KeyFieldMissing,
			wantLine: 2,
		},
		{
			name:     "只有表头 → 零行且不报错（D-4）",
			content:  "日期,金额,币种\n",
			wantRows: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rows, err := impl.Parse(context.Background(), Input{Content: []byte(c.content)})

			if c.wantKey == "" {
				if err != nil {
					t.Fatalf("应成功，实际 %v", err)
				}
				if len(rows) != c.wantRows {
					t.Errorf("应解析出 %d 行，实际 %d 行", c.wantRows, len(rows))
				}
				return
			}

			if err == nil {
				t.Fatalf("应报 %q，实际成功", c.wantKey)
			}
			if rows != nil {
				t.Error("失败时不应返回半批数据（取舍 5）")
			}
			if got := errs.MsgKeyOf(err); got != c.wantKey {
				t.Errorf("文案键应为 %q，实际 %q", c.wantKey, got)
			}
			if !errs.IsKind(err, errs.KindInvalidInput) {
				t.Errorf("应为 400 档，实际 %v", errs.KindOf(err))
			}
			if c.wantLine > 0 {
				if got := errs.ParamsOf(err)["Line"]; got != c.wantLine {
					t.Errorf("行号应为 %d，实际 %v", c.wantLine, got)
				}
			}
		})
	}
}

// TestConsumerIntakeMapsRawRowToExpenseFields 回放 service/intake 做映射时会
// 读到的字段，确认 RawRow 提供的信息足以填 entity.Expense 的对应项，
// 且**不提供**那四个必须由别处派生的字段。
//
// 本用例不 import entity（本包的测试同样受依赖边界约束），而是以「映射函数会
// 读哪些字段」的形态回放：它逐个读 8 个字段，编译通过即证明它们都在。
func TestConsumerIntakeMapsRawRowToExpenseFields(t *testing.T) {
	t.Parallel()

	date, err := calendar.ParseDate("2026-03-05")
	if err != nil {
		t.Fatalf("构造日期失败: %v", err)
	}
	amount, err := decimal.Parse("712.35")
	if err != nil {
		t.Fatalf("构造金额失败: %v", err)
	}
	usd, err := currency.Parse("USD")
	if err != nil {
		t.Fatalf("构造币种失败: %v", err)
	}

	row := RawRow{
		Date: date, Amount: amount, Currency: usd,
		Counterparty: "Amazon", Remark: "书",
		BankName: "招商银行", CardLast4: "0042", CardCurrency: usd,
	}

	//service/intake 的映射会读这 8 项（此处以一个只读快照代替 entity.Expense）
	snapshot := struct {
		date                               calendar.Date
		amount                             decimal.Decimal
		code                               currency.Code
		counterparty, remark               string
		bankName, cardLast4                string
		cardCurrency                       currency.Code
		ownerUserID, sourceFileID, auditID int64
		amortizeMonths                     int
	}{
		date: row.Date, amount: row.Amount, code: row.Currency,
		counterparty: row.Counterparty, remark: row.Remark,
		bankName: row.BankName, cardLast4: row.CardLast4,
		cardCurrency: row.CardCurrency,
		//以下四项**不来自 RawRow**，由 service/intake 注入或按规则派生
		ownerUserID: 1, sourceFileID: 2, auditID: 3,
		amortizeMonths: 1, //H-1 默认 1
	}

	if snapshot.date.String() != "2026-03-05" {
		t.Errorf("日期映射有误: %q", snapshot.date.String())
	}
	if !snapshot.amount.Equal(amount) {
		t.Error("金额映射有误")
	}
	if !snapshot.code.Equal(usd) {
		t.Error("币种映射有误")
	}
	if snapshot.cardLast4 != "0042" {
		t.Errorf("卡号后四位应保留前导零，实际 %q", snapshot.cardLast4)
	}
	if snapshot.amortizeMonths != 1 {
		t.Error("摊分月数应由上层按 H-1 默认为 1，而不是取自 RawRow")
	}
}
