package currency

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	cldr "github.com/bojanz/currency"
)

// TestScaleMatchesCLDR 是本包最重要的一条数据校验：
// 27 个币种的小数位数逐项与 CLDR 48.2.0 比对。
//
// 小数位数是 8.1 折算舍入与 8.2 摊分均分的基准（分层 §九 不变式 1a/1b），
// 写错不会报错、只会让金额差一到两个分位。故不能凭记忆写死，必须有权威来源
// 可交叉核对；CLDR 数据升级导致的漂移也会由本用例自动暴露。
//
// bojanz/currency 在此**只作校验基准**，是 test-only 依赖：生产代码不 import
// 它，生产二进制不含它（有 go list -deps 核实）。不用它作运行时依赖的三条
// 实测理由见包注释。
func TestScaleMatchesCLDR(t *testing.T) {
	for _, c := range All() {
		t.Run(c.String(), func(t *testing.T) {
			want, ok := cldr.GetDigits(c.String())
			if !ok {
				t.Fatalf("%s 不在 CLDR %s 数据中——请核实该代码是否有效",
					c, cldr.CLDRVersion)
			}
			if int32(want) != c.Scale() {
				t.Errorf("%s 的小数位数 = %d, CLDR %s 为 %d。"+
					"该值是 8.1 折算与 8.2 摊分的基准，不一致会让金额算错",
					c, c.Scale(), cldr.CLDRVersion, want)
			}
		})
	}
}

// TestIDRUsesAccountingDigits 单独钉住一处已知的数据分歧。
//
// golang.org/x/text/currency 对 IDR 给 0 位，bojanz/currency 给 2 位。
// CLDR 原始数据 currencyData.json 为 IDR: {_digits: 2, _cashDigits: 0}——
// 即 CLDR 本身区分「账面位数」与「现金位数」，x/text 取的是其冻结版本
// （CLDR v32）的 cash 口径。ISO 4217 官方 minor unit 亦为 2。
//
// 本包取账面位数 2：终版 §一 定位「以银行账单导入为主要数据来源」，记的是
// 账单流水而非现金找零。此用例防止将来有人「顺手改成 0」。
func TestIDRUsesAccountingDigits(t *testing.T) {
	idr := MustParse("IDR")
	if idr.Scale() != 2 {
		t.Errorf("IDR 的小数位数 = %d, 期望 2（账面口径，非现金口径 0）", idr.Scale())
	}
}

// TestNumericCodesValid 借 CLDR 交叉核对全部代码的有效性。
//
// 能取到 ISO 4217 数字代码，即证明该字母代码是现行有效的 ISO 4217 代码，
// 而不是笔误或已废止的历史代码（如 DEM、FRF）。
func TestNumericCodesValid(t *testing.T) {
	for _, c := range All() {
		if _, ok := cldr.GetNumericCode(c.String()); !ok {
			t.Errorf("%s 无对应的 ISO 4217 数字代码，可能是笔误或已废止的代码", c)
		}
	}
}

// TestTableSelfConsistency 覆盖表自身的完整性。
func TestTableSelfConsistency(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("枚举表为空")
	}

	seen := make(map[string]bool, len(all))
	for _, c := range all {
		code := c.String()

		t.Run(code, func(t *testing.T) {
			if seen[code] {
				t.Errorf("代码 %q 重复", code)
			}
			seen[code] = true

			if len(code) != 3 {
				t.Errorf("代码 %q 不是 3 个字符", code)
			}
			if code != strings.ToUpper(code) {
				t.Errorf("代码 %q 应全大写", code)
			}
			if c.Name() == "" {
				t.Error("名称为空")
			}
			if c.Symbol() == "" {
				t.Error("符号为空")
			}
			if c.Scale() < 0 {
				t.Errorf("小数位数 %d 为负", c.Scale())
			}
			if c.RateSource() == "" {
				t.Error("汇率源标识为空")
			}
			if got := c.NameKey(); got != "currency.name."+code {
				t.Errorf("文案键 = %q, 期望 %q", got, "currency.name."+code)
			}
		})
	}
}

// TestScaleValueDomain 钉住取舍 7：代码按值域写，不假设「非 0 即 2」。
//
// 实测 CLDR 48.2.0 全部 165 个币种的位数分布为 0 位 17 个 / 2 位 139 个 /
// 3 位 7 个 / 4 位 2 个——当前**不存在 1 位小数的币种**。但 T38 与
// base/decimal 的注释均按「本位币位数 ∈ {0,1,2}」表述：{0,1,2} 是判据的
// 值域上界，{0,2} 只是当前数据的实际取值。
//
// 故本用例只断言值域（0 ≤ scale ≤ 3），不断言「非 0 即 2」——后者会在将来
// CLDR 出现 1 位币种时把正确的数据判为失败。
func TestScaleValueDomain(t *testing.T) {
	for _, c := range All() {
		if c.Scale() < 0 || c.Scale() > 3 {
			t.Errorf("%s 的小数位数 %d 超出本仓库清单的值域 [0,3]", c, c.Scale())
		}
	}
}

// TestCodesNeverRemoved 钉住终版 I-1：「枚举项只能停用不能删除」。
//
// 下面是一份冻结的清单快照。**删除其中任何一项都会让本用例失败**——这正是
// 目的：历史明细存着这些代码，删掉枚举项会让那些明细再也读不出来。
//
// 允许的操作只有两种：新增币种（往表里加，本清单不动），或把某项的 enabled
// 置 false（本清单仍应能 Parse 成功，见 TestDisabledCurrencyStillReadable）。
func TestCodesNeverRemoved(t *testing.T) {
	frozen := []string{
		"AUD", "BRL", "CAD", "CHF", "CNY", "DKK", "EUR", "GBP", "HKD", "IDR",
		"INR", "JPY", "KRW", "KWD", "MOP", "MXN", "MYR", "NOK", "NZD", "PHP",
		"RUB", "SEK", "SGD", "THB", "TWD", "USD", "VND",
	}
	for _, code := range frozen {
		if !IsKnown(code) {
			t.Errorf("币种 %q 已从枚举表中消失。终版 I-1 规定枚举项只能停用不能删除"+
				"——历史明细存着旧代码，删除会让那些明细无法读出", code)
		}
	}
	if len(All()) < len(frozen) {
		t.Errorf("枚举表当前 %d 项，少于冻结清单的 %d 项", len(All()), len(frozen))
	}
}

// TestDisabledCurrencyStillReadable 钉住取舍 4：停用币种仍可读出。
//
// 这是终版 I-1 的行为面：停用只影响「能否新选」，不影响「能否读出」。
// 当前表中 27 项全部启用，故用一个临时构造的停用条目验证该行为——它不进
// 枚举表，只借用同一套谓词逻辑。
func TestDisabledCurrencyStillReadable(t *testing.T) {
	disabled := Code{entry: &entry{
		code: "ZZZ", name: "测试停用币种", symbol: "Z",
		scale: 2, enabled: false, rateSource: RateSourceDefault,
	}}

	// 停用币种：属性照常可读——F-4「显示已删除」查看、K-3 导出、I-7 重算都依赖这一点
	if disabled.IsZero() {
		t.Error("停用币种不应是零值")
	}
	if got := disabled.String(); got != "ZZZ" {
		t.Errorf("停用币种 String() = %q, 期望 ZZZ", got)
	}
	if got := disabled.Scale(); got != 2 {
		t.Errorf("停用币种应仍能取到小数位数（重算历史明细需要）, 实际 %d", got)
	}
	if got, err := disabled.Value(); err != nil || got != "ZZZ" {
		t.Errorf("停用币种应可落库, Value() = %v, %v", got, err)
	}

	// 但不可新选、不可作本位币
	if disabled.Enabled() {
		t.Error("停用币种 Enabled() 应为 false")
	}
	if disabled.CanBeBase() {
		t.Error("停用币种不应是本位币候选（T38 要求「已启用」）")
	}
}

// TestNoMutationAPI 钉住取舍 2：本包不得出现任何注册 / 修改入口。
//
// 提供 Register 等于把终版 I-1 的「新增币种须改代码」降级成「运行期任意包
// 可改」，该条当场失效——这正是否决 bojanz/currency 作运行时依赖的首要理由，
// 不能自己再造一个。
//
// 用例直接解析本包源码的 AST，扫描全部导出标识符。判据是「禁用动词的完整
// 单词形式」：精确等于该动词，或以该动词开头且下一个字符是大写（如
// RegisterCurrency、SetScale）。不能只用前缀匹配——那会把只读访问器
// Enabled() 误判成 Enable 的修改入口（已实测踩到）。
func TestNoMutationAPI(t *testing.T) {
	forbidden := []string{"Register", "Set", "Add", "Remove", "Delete", "Enable", "Disable", "Update", "Put", "Insert", "Load", "Reload"}

	isMutationName := func(name string) bool {
		for _, verb := range forbidden {
			if name == verb {
				return true
			}
			// 动词 + 大写开头的后缀，即 SetScale / RegisterCurrency 这类。
			// Enabled、Settings 这类以小写延续的词不算。
			if strings.HasPrefix(name, verb) && len(name) > len(verb) {
				next := name[len(verb)]
				if next >= 'A' && next <= 'Z' {
					return true
				}
			}
		}
		return false
	}

	// 自检判据本身：正例必须被抓、反例必须放过
	for _, bad := range []string{"Register", "RegisterCurrency", "Set", "SetScale", "AddCode", "RemoveCode", "DisableAll"} {
		if !isMutationName(bad) {
			t.Fatalf("判据失效：%q 应被判为修改入口", bad)
		}
	}
	for _, ok := range []string{"Enabled", "Settings", "Adder", "Removal"} {
		if isMutationName(ok) {
			t.Fatalf("判据失效：%q 是只读命名，不应被判为修改入口", ok)
		}
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		// 只看生产代码，测试文件里的辅助函数不受此限
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("解析包源码失败: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("未解析到任何生产代码文件——用例形同虚设")
	}

	scanned := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !fn.Name.IsExported() {
					continue
				}
				scanned++
				if isMutationName(fn.Name.Name) {
					t.Errorf("%s 中的导出函数 %q 疑似修改入口。"+
						"终版 I-1 规定新增币种须改代码、枚举项只能停用不能删除，"+
						"本包不得提供运行期修改能力", path, fn.Name.Name)
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("未扫描到任何导出函数——用例形同虚设")
	}
}

// TestExportedAPISurface 冻结本包的导出 API 清单。
//
// 与 TestNoMutationAPI 互补：后者按命名判断，本用例要求**任何**新增导出都必须
// 在此显式登记。这样「不小心导出了一个写入口」不会因为命名绕开动词表而漏网。
func TestExportedAPISurface(t *testing.T) {
	want := map[string]bool{
		// 构造与解析
		"Parse": true, "MustParse": true, "ParseBase": true, "IsKnown": true,
		// 集合
		"All": true, "Enabled": true, "BaseCandidates": true, "NameKeys": true,
		// 默认值
		"DefaultBase": true,
		// Code 的方法
		"String": true, "Name": true, "NameKey": true, "Symbol": true,
		"Scale": true, "RateSource": true, "IsZero": true,
		"CanBeBase": true, "Equal": true,
		// 编解码
		"MarshalJSON": true, "UnmarshalJSON": true, "Value": true, "Scan": true,
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("解析包源码失败: %v", err)
	}

	got := make(map[string]bool)
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !fn.Name.IsExported() {
					continue
				}
				got[fn.Name.Name] = true
			}
		}
	}

	for name := range got {
		if !want[name] {
			t.Errorf("出现未登记的导出函数 %q。若为有意新增，请在本用例的清单中登记"+
				"并确认它不是运行期修改入口（终版 I-1）", name)
		}
	}
	for name := range want {
		if !got[name] {
			t.Errorf("登记的导出函数 %q 已消失，下游可能因此编译失败", name)
		}
	}
}

// TestTableIsImmutableAcrossCalls 验证表在多次访问后不变。
//
// 无写入口 + 初始化后只读 ⇒ 并发读天然安全（go test -race 覆盖）。
func TestTableIsImmutableAcrossCalls(t *testing.T) {
	snapshot := make(map[string]struct {
		name    string
		symbol  string
		scale   int32
		enabled bool
	}, len(All()))
	for _, c := range All() {
		snapshot[c.String()] = struct {
			name    string
			symbol  string
			scale   int32
			enabled bool
		}{c.Name(), c.Symbol(), c.Scale(), c.Enabled()}
	}

	// 反复读取并尝试通过返回值改动
	for i := 0; i < 100; i++ {
		list := All()
		if len(list) > 0 {
			list[0] = Code{}
		}
		_ = Enabled()
		_ = BaseCandidates()
		_ = NameKeys()
	}

	for _, c := range All() {
		want, ok := snapshot[c.String()]
		if !ok {
			t.Errorf("出现了快照中没有的币种 %q", c)
			continue
		}
		if c.Name() != want.name || c.Symbol() != want.symbol ||
			c.Scale() != want.scale || c.Enabled() != want.enabled {
			t.Errorf("%q 的属性发生了变化", c)
		}
	}
}

// TestConcurrentRead 在 -race 下验证并发读安全。
//
// 这正是 bojanz/currency 失败的场景（Register 与读并发即 DATA RACE）。
// 本包无写入口，故只有读，必然安全——用例是为了防止将来有人加入写入口。
func TestConcurrentRead(t *testing.T) {
	const goroutines = 50
	done := make(chan struct{}, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				c, err := Parse("CNY")
				if err != nil {
					t.Error(err)
					return
				}
				_ = c.Scale()
				_ = c.CanBeBase()
				_ = All()
				_ = BaseCandidates()
				_ = DefaultBase()
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// TestBuildTableRejectsBadTable 覆盖表构建的各条自检分支。
//
// 这些分支在正常运行中不会触发（表是对的），它们是「表被改坏时进程启动即炸」
// 的保险。保险必须被验证过——否则等于没有保险。这也是把构建逻辑从 init 里
// 抽成纯函数 buildTable 的原因：直接写在 init 里，这些分支无法被测试构造。
func TestBuildTableRejectsBadTable(t *testing.T) {
	good := []entry{
		{"AAA", "甲", "A", 2, true, RateSourceDefault},
		{"BBB", "乙", "B", 0, true, RateSourceDefault},
	}

	t.Run("合法表构建成功", func(t *testing.T) {
		b, err := buildTable(good, "AAA")
		if err != nil {
			t.Fatalf("合法表被拒: %v", err)
		}
		if len(b.allCodes) != 2 {
			t.Errorf("allCodes 数量 = %d, 期望 2", len(b.allCodes))
		}
		if b.defaultBase.String() != "AAA" {
			t.Errorf("defaultBase = %q, 期望 AAA", b.defaultBase)
		}
		// 字典序
		if b.allCodes[0].String() != "AAA" || b.allCodes[1].String() != "BBB" {
			t.Error("allCodes 未按字典序排列")
		}
	})

	cases := []struct {
		name        string
		entries     []entry
		defaultCode string
		wantSubstr  string
	}{
		{
			name: "条目字段非法",
			entries: []entry{
				{"AAA", "", "A", 2, true, RateSourceDefault}, // 名称为空
			},
			defaultCode: "AAA",
			wantSubstr:  "枚举表条目非法",
		},
		{
			name: "代码重复",
			entries: []entry{
				{"AAA", "甲", "A", 2, true, RateSourceDefault},
				{"AAA", "甲二", "A", 2, true, RateSourceDefault},
			},
			defaultCode: "AAA",
			wantSubstr:  "重复",
		},
		{
			name:        "默认本位币不在表内",
			entries:     good,
			defaultCode: "ZZZ",
			wantSubstr:  "不在枚举表内",
		},
		{
			name: "默认本位币不满足候选口径（3 位小数）",
			entries: []entry{
				{"AAA", "甲", "A", 3, true, RateSourceDefault},
			},
			defaultCode: "AAA",
			wantSubstr:  "不满足本位币候选口径",
		},
		{
			name: "默认本位币不满足候选口径（已停用）",
			entries: []entry{
				{"AAA", "甲", "A", 2, false, RateSourceDefault},
			},
			defaultCode: "AAA",
			wantSubstr:  "不满足本位币候选口径",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 每个用例用自己的副本，避免 buildTable 取地址后互相影响
			entries := make([]entry, len(tc.entries))
			copy(entries, tc.entries)

			_, err := buildTable(entries, tc.defaultCode)
			if err == nil {
				t.Fatal("坏表未被拒绝")
			}
			if !contains(err.Error(), tc.wantSubstr) {
				t.Errorf("错误信息 %q 未包含 %q", err, tc.wantSubstr)
			}
		})
	}
}

// TestCheckEntryRejectsBadData 覆盖表自检的各个拒绝分支。
//
// 这些分支在正常运行中不会触发（表是对的），但它们是「表被改坏时当场炸掉」
// 的保险，必须确认真的会炸。
func TestCheckEntryRejectsBadData(t *testing.T) {
	valid := entry{code: "AAA", name: "测试", symbol: "A", scale: 2, enabled: true, rateSource: RateSourceDefault}

	cases := []struct {
		name   string
		mutate func(e *entry)
	}{
		{"代码为空", func(e *entry) { e.code = "" }},
		{"代码长度不是 3", func(e *entry) { e.code = "AB" }},
		{"代码含小写", func(e *entry) { e.code = "aAA" }},
		{"代码含数字", func(e *entry) { e.code = "A1A" }},
		{"名称为空", func(e *entry) { e.name = "" }},
		{"符号为空", func(e *entry) { e.symbol = "" }},
		{"小数位数为负", func(e *entry) { e.scale = -1 }},
		{"小数位数越界", func(e *entry) { e.scale = 99 }},
		{"汇率源为空", func(e *entry) { e.rateSource = "" }},
	}

	// 前置：合法条目应通过
	if err := checkEntry(&valid); err != nil {
		t.Fatalf("合法条目被拒: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := valid
			tc.mutate(&e)
			if err := checkEntry(&e); err == nil {
				t.Error("非法条目未被拒绝")
			}
		})
	}
}

// TestSentinelErrorsAreDistinct 确认三个哨兵错误互不混淆。
//
// 分档的意义在于上层能给不同文案；若 errors.Is 互相命中，分档就是假的。
func TestSentinelErrorsAreDistinct(t *testing.T) {
	all := []error{ErrEmptyCode, ErrUnknownCode, ErrNotBaseCandidate, ErrScanType}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("哨兵错误 %v 与 %v 互相命中，分档失效", a, b)
			}
		}
	}
}
