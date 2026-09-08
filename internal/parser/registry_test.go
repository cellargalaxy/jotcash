package parser

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// TestNewRegistryAcceptsValidParsers 走查正常装配路径。
func TestNewRegistryAcceptsValidParsers(t *testing.T) {
	t.Parallel()

	csv := &stubParser{pt: enum.ParserTypeGenericCSV}
	reg, err := NewRegistry(csv)
	if err != nil {
		t.Fatalf("装配应成功，实际 %v", err)
	}
	if got := reg.Len(); got != 1 {
		t.Errorf("应注册 1 个，实际 %d", got)
	}
	if !reg.Has(enum.ParserTypeGenericCSV) {
		t.Error("Has 应报告 generic_csv 已注册")
	}

	got, err := reg.Get(enum.ParserTypeGenericCSV)
	if err != nil {
		t.Fatalf("Get 应成功，实际 %v", err)
	}
	//必须取回**同一个实例**：注册表不该复制或包装实现
	if got != Parser(csv) {
		t.Error("Get 应返回注册时的同一实例")
	}
}

// TestNewRegistryAllowsEmpty 走查「零个实现」是合法状态。
//
// 这正是当前阶段 4 的实际情形：enum 已声明 generic_csv，而 parser/csv 属阶段 6
// 还没写。若构造期一律拒绝空表，本包连自己的测试都构造不出一个合法实例。
func TestNewRegistryAllowsEmpty(t *testing.T) {
	t.Parallel()

	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("空表构造应成功，实际 %v", err)
	}
	if reg.Len() != 0 {
		t.Errorf("空表 Len 应为 0，实际 %d", reg.Len())
	}
	if got := reg.Types(); len(got) != 0 {
		t.Errorf("空表 Types 应为空，实际 %v", got)
	}
}

// TestNewRegistryRejectsBadInput 走查构造期自校验的三条（nil / 非法键 / 重键）。
//
// 三者都必须**响亮失败**而不是静默放行：
//   - nil 会在 Parse 时 panic，那时离装配点已经很远；
//   - 非法键在 Get 时永远取不到，症状是「选了这个解析器就报未注册」；
//   - 重键会让 map 赋值静默覆盖，行为取决于 app 里两行代码的先后顺序。
func TestNewRegistryRejectsBadInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		parsers    []Parser
		wantSubstr string
	}{
		{
			name:       "nil 实现",
			parsers:    []Parser{nil},
			wantSubstr: "为 nil",
		},
		{
			name:       "nil 夹在合法实现之后",
			parsers:    []Parser{&stubParser{pt: enum.ParserTypeGenericCSV}, nil},
			wantSubstr: "第 1 个",
		},
		{
			name:       "类型键为零值",
			parsers:    []Parser{&stubParser{pt: enum.ParserType("")}},
			wantSubstr: "类型键非法",
		},
		{
			name:       "类型键不存在",
			parsers:    []Parser{&stubParser{pt: enum.ParserType("no_such_parser")}},
			wantSubstr: "类型键非法",
		},
		{
			name: "键重复",
			parsers: []Parser{
				&stubParser{pt: enum.ParserTypeGenericCSV},
				&stubParser{pt: enum.ParserTypeGenericCSV},
			},
			wantSubstr: "重复注册",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			reg, err := NewRegistry(c.parsers...)
			if err == nil {
				t.Fatal("应报错，实际成功")
			}
			if reg != nil {
				t.Error("失败时不应返回半成品注册表")
			}
			if !strings.Contains(err.Error(), c.wantSubstr) {
				t.Errorf("错误信息应含 %q，实际 %q", c.wantSubstr, err.Error())
			}
		})
	}
}

// TestNewRegistryErrorsAreNotUserFacing 走查装配错误**不带** base/errs 的档位
// 与文案键（registry.go 里刻意如此）。
//
// 装配失败发生在启动阶段：没有 HTTP 请求、没有界面语言、也没有用户能看到它。
// 给它套一个面向用户的文案键，会让人误以为这是一条能回给前端的错误。
func TestNewRegistryErrorsAreNotUserFacing(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(&stubParser{pt: enum.ParserType("bad")})
	if err == nil {
		t.Fatal("应报错")
	}
	//MsgKeyOf 对未分档错误返回 base/errs 的技术兜底键，而不是本包的任何键
	if got := errs.MsgKeyOf(err); strings.HasPrefix(got, "parser.") {
		t.Errorf("装配错误不应带本包的业务文案键，实际 %q", got)
	}
}

// TestRegistryGetSplitsKinds 走查 Get 的两类失败分属两档（取舍 8），
// 并钉住「先判合法性再查表」的顺序。
func TestRegistryGetSplitsKinds(t *testing.T) {
	t.Parallel()

	reg, err := NewRegistry(&stubParser{pt: enum.ParserTypeGenericCSV})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}

	cases := []struct {
		name     string
		pt       enum.ParserType
		wantKind errs.Kind
		wantKey  string
	}{
		{"非法键", enum.ParserType("no_such"), errs.KindInvalidInput, KeyTypeUnsupported},
		{"零值键", enum.ParserType(""), errs.KindInvalidInput, KeyTypeUnsupported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p, err := reg.Get(c.pt)
			if p != nil {
				t.Error("失败时不应返回实现")
			}
			if !errs.IsKind(err, c.wantKind) {
				t.Errorf("档位应为 %v，实际 %v", c.wantKind, errs.KindOf(err))
			}
			if got := errs.MsgKeyOf(err); got != c.wantKey {
				t.Errorf("文案键应为 %q，实际 %q", c.wantKey, got)
			}
		})
	}

	//合法键但未注册 → 500。用空表构造，避免依赖某个具体的未注册键
	t.Run("合法键未注册", func(t *testing.T) {
		t.Parallel()
		empty, err := NewRegistry()
		if err != nil {
			t.Fatalf("空表构造失败: %v", err)
		}
		p, err := empty.Get(enum.ParserTypeGenericCSV)
		if p != nil {
			t.Error("失败时不应返回实现")
		}
		if !errs.IsKind(err, errs.KindInternal) {
			t.Errorf("档位应为系统错误，实际 %v", errs.KindOf(err))
		}
		if got := errs.MsgKeyOf(err); got != KeyNotRegistered {
			t.Errorf("文案键应为 %q，实际 %q", KeyNotRegistered, got)
		}
	})
}

// TestRegistryHasRejectsInvalid 走查 Has 对非法键与零值一律 false。
func TestRegistryHasRejectsInvalid(t *testing.T) {
	t.Parallel()

	reg, err := NewRegistry(&stubParser{pt: enum.ParserTypeGenericCSV})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	for _, pt := range []enum.ParserType{"", "no_such", "GENERIC_CSV"} {
		if reg.Has(pt) {
			t.Errorf("Has(%q) 应为 false", pt)
		}
	}
	if !reg.Has(enum.ParserTypeGenericCSV) {
		t.Error("Has 对已注册键应为 true")
	}
}

// TestRegistryTypesFollowsEnumOrder 走查 Types 按 enum.ParserTypes() 的声明序
// 返回，而不是 map 遍历序。
//
// 顺序必须稳定：D-2 的上传页下拉直接由它驱动。map 遍历序在 Go 里是**故意随机**
// 的，用它会让下拉每次刷新都换一次顺序。
//
// 当前枚举只有一项，无法从「一次结果」里看出顺序是否稳定，因此改用两条判据：
// 一、多次调用结果恒等；二、结果是 enum 声明序的子序列。第二条在将来枚举增加
// 到多项时自动开始有实质约束。
func TestRegistryTypesFollowsEnumOrder(t *testing.T) {
	t.Parallel()

	reg, err := NewRegistry(&stubParser{pt: enum.ParserTypeGenericCSV})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}

	first := reg.Types()
	//反复调用应恒等（若用 map 遍历序，多项时这里会抖动）
	for i := 0; i < 50; i++ {
		if got := reg.Types(); !reflect.DeepEqual(got, first) {
			t.Fatalf("Types 结果应稳定，第 %d 次得到 %v，首次为 %v", i, got, first)
		}
	}

	//必须是 enum 声明序的子序列
	declared := enum.ParserTypes()
	idx := 0
	for _, pt := range first {
		found := false
		for idx < len(declared) {
			if declared[idx] == pt {
				found = true
				idx++
				break
			}
			idx++
		}
		if !found {
			t.Errorf("Types 的顺序不是 enum.ParserTypes() 的子序列: %v vs %v", first, declared)
			break
		}
	}
}

// TestRegistryTypesOnlyReturnsKnownKeys 钉住 Types 的实现方式：它以
// **enum.ParserTypes() 的声明序为基准过滤**，而不是遍历自己的 map。
//
// # 为什么需要这条，以及它为什么能在当前枚举只有一项时仍然有效
//
// 顺序稳定性本身在「枚举只有一项」时无法被观测——把实现改成 map 遍历序，
// TestRegistryTypesFollowsEnumOrder 照样通过（编写时实测：那条变异未被捕获）。
// 于是本用例改为钉住同一个实现选择的**另一个可观测后果**：
// Types() 只会返回 enum 已声明的键。
//
// 判据用一个**绕过 NewRegistry 直接构造**的注册表（包内测试才能做到，
// NewRegistry 会拒绝非法键）。两种实现在此分道扬镳：
//
//	按 enum 声明序过滤（当前实现）  未知键被排除 → Types 为空
//	按 map 遍历序（变异）          未知键原样返回 → Types 含 "ghost"
//
// 这条性质本身也有独立价值：D-2 的上传页下拉由 Types 驱动，它保证下拉里
// 永远不会冒出一个 enum 不认识的选项——即使将来有人放宽了注册校验。
func TestRegistryTypesOnlyReturnsKnownKeys(t *testing.T) {
	t.Parallel()

	ghost := enum.ParserType("ghost_parser")
	if ghost.Valid() {
		t.Fatal("用例前提有误：ghost_parser 不应是合法枚举键")
	}

	//绕过 NewRegistry 直接构造，模拟「注册校验被放宽」后的情形
	reg := Registry{parsers: map[enum.ParserType]Parser{
		ghost:                     &stubParser{pt: ghost},
		enum.ParserTypeGenericCSV: &stubParser{pt: enum.ParserTypeGenericCSV},
	}}

	got := reg.Types()
	for _, pt := range got {
		if !pt.Valid() {
			t.Errorf("Types 不应返回 enum 未声明的键 %q：它会成为 D-2 下拉里一个"+
				"选了必然报错的选项（说明实现改用了 map 遍历序）", pt)
		}
	}
	//合法键应仍在
	if len(got) != 1 || got[0] != enum.ParserTypeGenericCSV {
		t.Errorf("应只返回合法键 generic_csv，实际 %v", got)
	}
	//Len 与 Types 的口径差异是刻意的：Len 报「注册了几个」，Types 报「可用的有哪几个」
	if reg.Len() != 2 {
		t.Errorf("Len 应报告 map 里的实际条数 2，实际 %d", reg.Len())
	}
}

// TestRegistryTypesReturnsCopy 走查 Types 返回副本：调用方改它不影响本表。
func TestRegistryTypesReturnsCopy(t *testing.T) {
	t.Parallel()

	reg, err := NewRegistry(&stubParser{pt: enum.ParserTypeGenericCSV})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}

	got := reg.Types()
	if len(got) == 0 {
		t.Fatal("应至少有一项")
	}
	got[0] = enum.ParserType("tampered")

	//改副本不得影响表本身
	if !reg.Has(enum.ParserTypeGenericCSV) {
		t.Error("改 Types 的返回值不应影响注册表")
	}
	if again := reg.Types(); again[0] != enum.ParserTypeGenericCSV {
		t.Errorf("再次调用应仍返回原值，实际 %v", again[0])
	}
}

// TestRegistryHasNoMutators 锁死「无导出写入口」这条设计（取舍 1）。
//
// 本用例是本文件里最重要的一条：一旦有人加了 Register / Unregister / Set /
// Add / Remove 之类的导出方法，包级可变全局表的全部问题就回来了——任何 import
// 到本包的包都能静默改掉账单解析的路由。
func TestRegistryHasNoMutators(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"Get": true, "Has": true, "Types": true, "Len": true,
		"CheckEnabledCoverage": true,
	}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(Registry{}),
		reflect.TypeOf(&Registry{}),
	} {
		for i := 0; i < typ.NumMethod(); i++ {
			name := typ.Method(i).Name
			if !allowed[name] {
				t.Errorf("%v 出现了未预期的导出方法 %s：注册表必须构造后只读（取舍 1）",
					typ, name)
			}
		}
	}
}

// TestRegistryZeroValueIsEmptyTable 走查零值 Registry 是可用的空表，不 panic。
//
// 空表与「漏注册」是同一种故障，走同一条报错路径即可。
func TestRegistryZeroValueIsEmptyTable(t *testing.T) {
	t.Parallel()

	var reg Registry

	if reg.Len() != 0 {
		t.Errorf("零值 Len 应为 0，实际 %d", reg.Len())
	}
	if reg.Has(enum.ParserTypeGenericCSV) {
		t.Error("零值 Has 应为 false")
	}
	if got := reg.Types(); len(got) != 0 {
		t.Errorf("零值 Types 应为空，实际 %v", got)
	}
	p, err := reg.Get(enum.ParserTypeGenericCSV)
	if p != nil {
		t.Error("零值 Get 不应返回实现")
	}
	if !errs.IsKind(err, errs.KindInternal) {
		t.Errorf("零值 Get 合法键应报 500，实际 %v", errs.KindOf(err))
	}
	if _, err := reg.CheckEnabledCoverage(); err == nil {
		t.Error("零值表应报出启用项缺失")
	}
}

// TestRegistryConcurrentRead 走查「构造后只读、可并发」这条性质。
//
// 必须在 -race 下跑才有意义（CI 与本轮验证都跑了 -race）：本用例的价值在于
// 一旦将来有人加了写入口并在读的同时写，race detector 会当场报出来。
func TestRegistryConcurrentRead(t *testing.T) {
	t.Parallel()

	reg, err := NewRegistry(&stubParser{pt: enum.ParserTypeGenericCSV})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}

	const goroutines = 64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if _, err := reg.Get(enum.ParserTypeGenericCSV); err != nil {
					t.Errorf("并发 Get 失败: %v", err)
					return
				}
				_ = reg.Has(enum.ParserTypeGenericCSV)
				_ = reg.Types()
				_ = reg.Len()
				_, _ = reg.CheckEnabledCoverage()
			}
		}()
	}
	wg.Wait()
}

// TestCheckEnabledCoverage 走查启用项覆盖检查的两种结果。
func TestCheckEnabledCoverage(t *testing.T) {
	t.Parallel()

	t.Run("空表报出全部启用项缺失", func(t *testing.T) {
		t.Parallel()
		reg, err := NewRegistry()
		if err != nil {
			t.Fatalf("装配失败: %v", err)
		}
		missing, err := reg.CheckEnabledCoverage()
		enabled := enum.EnabledParserTypes()
		if len(missing) != len(enabled) {
			t.Errorf("应报出 %d 个缺失，实际 %d 个", len(enabled), len(missing))
		}
		if err == nil {
			t.Fatal("应返回错误")
		}
		//档位 500：漏注册是装配问题，用户改不了
		if !errs.IsKind(err, errs.KindInternal) {
			t.Errorf("档位应为系统错误，实际 %v", errs.KindOf(err))
		}
		if got := errs.MsgKeyOf(err); got != KeyNotRegistered {
			t.Errorf("文案键应为 %q，实际 %q", KeyNotRegistered, got)
		}
		//错误参数里应能看出缺了哪些，便于照着改
		param, _ := errs.ParamsOf(err)["ParserType"].(string)
		for _, pt := range enabled {
			if !strings.Contains(param, pt.Code()) {
				t.Errorf("错误参数应含缺失键 %q，实际 %q", pt.Code(), param)
			}
		}
	})

	t.Run("全覆盖返回双 nil", func(t *testing.T) {
		t.Parallel()
		var parsers []Parser
		for _, pt := range enum.EnabledParserTypes() {
			parsers = append(parsers, &stubParser{pt: pt})
		}
		reg, err := NewRegistry(parsers...)
		if err != nil {
			t.Fatalf("装配失败: %v", err)
		}
		missing, err := reg.CheckEnabledCoverage()
		if missing != nil {
			t.Errorf("全覆盖时应返回 nil 列表，实际 %v", missing)
		}
		if err != nil {
			t.Errorf("全覆盖时应返回 nil 错误，实际 %v", err)
		}
	})
}

// TestCheckEnabledCoverageIgnoresDisabled 走查覆盖检查只看**已启用**项：
// 停用项没有实现是正常状态（停用往往正是因为实现被移除了）。
//
// 当前枚举表无停用项，故本用例以「表里是否存在停用项」为条件跑，与
// TestCheckFormatAllowsDisabledType 同一处理方式。
func TestCheckEnabledCoverageIgnoresDisabled(t *testing.T) {
	t.Parallel()

	var disabled []enum.ParserType
	for _, pt := range enum.ParserTypes() {
		if !pt.Enabled() {
			disabled = append(disabled, pt)
		}
	}
	if len(disabled) == 0 {
		t.Skip("当前枚举表无停用项，本用例待出现停用项后自动生效")
	}

	//只注册全部启用项，不注册任何停用项
	var parsers []Parser
	for _, pt := range enum.EnabledParserTypes() {
		parsers = append(parsers, &stubParser{pt: pt})
	}
	reg, err := NewRegistry(parsers...)
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if missing, err := reg.CheckEnabledCoverage(); err != nil {
		t.Errorf("停用项未注册不应报缺失，实际报出 %v / %v", missing, err)
	}
}

// TestRegistryRoutesToCorrectParser 端到端走查路由：注册两个实现后，Get 到的
// 必须是对应键的那一个，且 Parse 确实走进了它。
//
// 当前枚举只有一项，故第二个实现用一个**临时借用**的合法键——本用例改为
// 只用一项时也能成立的形态：注册一项，断言它被调用、且调用次数正确。
func TestRegistryRoutesToCorrectParser(t *testing.T) {
	t.Parallel()

	csv := &stubParser{pt: enum.ParserTypeGenericCSV, rows: []RawRow{{Counterparty: "超市"}}}
	reg, err := NewRegistry(csv)
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}

	p, err := reg.Get(enum.ParserTypeGenericCSV)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	rows, err := p.Parse(context.Background(), Input{Content: []byte("x"), Password: "pw"})
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Counterparty != "超市" {
		t.Errorf("应走进注册的实现并返回其结果，实际 %+v", rows)
	}
	if csv.calls != 1 {
		t.Errorf("实现应被调用 1 次，实际 %d 次", csv.calls)
	}
	if csv.gotInput.Password != "pw" {
		t.Errorf("入参应透传到实现，实际 %q", csv.gotInput.Password)
	}
}

// TestJoinCodes 走查 joinCodes 与 strings.Join 的等价性。
//
// 它是为了让 registry.go 的 import 只有 fmt 与 enum 而自己写的三行（见其注释），
// 因此必须证明它没有偏离标准库行为：空切片得空串、单元素不加分隔符、多元素
// 用「, 」连接。
func TestJoinCodes(t *testing.T) {
	t.Parallel()

	for _, c := range [][]string{
		nil,
		{},
		{"a"},
		{"a", "b"},
		{"a", "b", "c"},
		{"", ""},
		{"generic_csv", "cmb_credit_pdf"},
	} {
		want := strings.Join(c, ", ")
		if got := joinCodes(c); got != want {
			t.Errorf("joinCodes(%v) = %q，strings.Join 得 %q", c, got, want)
		}
	}
}
