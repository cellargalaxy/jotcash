package currency

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/decimal"
)

// 本文件按分层设计 §六 逐个走查 currency 的下游消费方，把「下游会怎么用」
// 变成可执行断言。目的不是覆盖率，而是：本包的 API 形状一旦发生对下游有害的
// 变化，在本包的测试里就当场失败，而不是等到下游那一轮才发现。
//
// 六个消费方：
//   entity(L1.1) / rule/convert(L2) / rule/amortize(L2) /
//   store(L3) / service/preference(L5.4) / fxrate(L3)

// TestConsumerEntityFieldUsage 走查 entity（L1.1）。
//
// 分层 §六 L1.1：「币种……等字段直接用 L1.0 的枚举类型……不用裸 string」。
// 要求：Code 可直接作结构体字段，且零值可用——实体以零值创建，
// 卡片币种「解析不出则留空」。
func TestConsumerEntityFieldUsage(t *testing.T) {
	// 模拟 entity.Expense 中与币种相关的四个字段（终版 §四）
	type expense struct {
		支出币种    Code // 必填
		卡片币种    Code // 可空：解析不出则留空
		折算本位币币种 Code // 折算后写入
	}

	t.Run("零值实体可直接创建并访问", func(t *testing.T) {
		var e expense // 不经任何构造函数
		if !e.支出币种.IsZero() || !e.卡片币种.IsZero() || !e.折算本位币币种.IsZero() {
			t.Error("零值实体的币种字段应均为零值")
		}
		// 零值上访问属性不得 panic——上层不应被迫先判空
		_ = e.卡片币种.String()
		_ = e.卡片币种.Scale()
		_ = e.卡片币种.Enabled()
	})

	t.Run("赋值后可比较", func(t *testing.T) {
		// 8.3 判重的四要素含支出币种，依赖结构体可比较
		a := expense{支出币种: MustParse("USD"), 折算本位币币种: MustParse("CNY")}
		b := expense{支出币种: MustParse("USD"), 折算本位币币种: MustParse("CNY")}
		if a != b {
			t.Error("含币种字段的实体应可整体比较")
		}
	})
}

// TestConsumerConvertRounding 走查 rule/convert（L2）。
//
// 分层 §九 不变式 1a：「本位币金额 = 原币金额 × 折算汇率，
// 按**本位币的 currency.小数位数**四舍五入」。
//
// 要求：由本位币 Code 取出的 Scale() 能直接喂给 decimal.Round。
func TestConsumerConvertRounding(t *testing.T) {
	// 模拟 rule/convert 的折算：原币金额 × 汇率，按本位币位数舍入
	convert := func(amount decimal.Decimal, rate decimal.Decimal, base Code) (decimal.Decimal, error) {
		if base.IsZero() {
			// 见 Scale() 注释：零值的 0 与 JPY 的 0 无法区分，必须先判
			return decimal.Decimal{}, errors.New("本位币未指定")
		}
		return amount.Mul(rate).Round(base.Scale(), decimal.RoundHalfUp)
	}

	cases := []struct {
		base   string
		amount string
		rate   string
		want   string
	}{
		// 2 位本位币：按分舍入
		{"CNY", "100.00", "7.1235", "712.35"},
		{"USD", "88.88", "1.0000", "88.88"},
		// 0 位本位币：舍到整数——这正是位数取错就静默出错的地方
		{"JPY", "100.00", "1.5678", "157"},
		{"KRW", "10.00", "9.9999", "100"},
	}
	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			base := MustParse(tc.base)
			got, err := convert(decimal.MustParse(tc.amount), decimal.MustParse(tc.rate), base)
			if err != nil {
				t.Fatalf("折算失败: %v", err)
			}
			if !got.Equal(decimal.MustParse(tc.want)) {
				t.Errorf("折算结果 = %s, 期望 %s", got, tc.want)
			}
		})
	}

	t.Run("零值本位币被拦下而不是静默按 0 位舍入", func(t *testing.T) {
		// 若不判 IsZero，712.35 会被静默舍成 712——钱少一截且不报错
		_, err := convert(decimal.MustParse("100.00"), decimal.MustParse("7.1235"), Code{})
		if err == nil {
			t.Error("零值本位币应被拦下")
		}
	})
}

// TestConsumerConvertBaseScaleFitsStorage 是本包与 base/decimal 之间
// **唯一的语义耦合点**，用可执行断言锁死。
//
// base/decimal 的包注释写明：「分层设计已把本位币限定为小数位数 ≤ 2 的币种，
// 故 0/1/2 位的舍入结果必然满足 FitsScale(2)，即按本位币位数算出的金额一定
// 能被 2 位定点列无损容纳。」
//
// 这句话成立的前提，正是本包 T38 的候选口径。若将来有人放宽 maxBaseScale，
// 本用例会立刻失败——否则后果是本位币金额落库时被静默截断。
func TestConsumerConvertBaseScaleFitsStorage(t *testing.T) {
	// 取一批容易暴露截断的金额：末位非零、进位边界
	amounts := []string{
		"0.005", "0.015", "0.125", "1.005", "9.999",
		"712.345", "88.888", "100.001", "0.001", "123456.789",
	}

	candidates := BaseCandidates()
	if len(candidates) == 0 {
		t.Fatal("本位币候选集为空")
	}

	for _, base := range candidates {
		t.Run(base.String(), func(t *testing.T) {
			// 候选口径本身
			if base.Scale() > decimal.BaseAmountStoreScale {
				t.Fatalf("候选币种 %s 的小数位数 %d 超过存储精度 %d，"+
					"按其位数算出的金额将被静默截断",
					base, base.Scale(), decimal.BaseAmountStoreScale)
			}

			// 逐个金额验证：按本位币位数舍入后，必然能被 2 位定点列无损容纳
			for _, s := range amounts {
				rounded, err := decimal.MustParse(s).Round(base.Scale(), decimal.RoundHalfUp)
				if err != nil {
					t.Fatalf("%s 按 %d 位舍入失败: %v", s, base.Scale(), err)
				}
				if !rounded.FitsScale(decimal.BaseAmountStoreScale) {
					t.Errorf("%s 按 %s 的 %d 位舍入得 %s，无法被 %d 位定点列无损容纳",
						s, base, base.Scale(), rounded, decimal.BaseAmountStoreScale)
				}
				// 能取出整数单位即证明落库无损
				if _, err := rounded.Units(decimal.BaseAmountStoreScale); err != nil {
					t.Errorf("%s 无法以 %d 位定点落库: %v", rounded, decimal.BaseAmountStoreScale, err)
				}
			}
		})
	}
}

// TestConsumerAmortizeSplit 走查 rule/amortize（L2）。
//
// 分层 §六 L2：「按币种小数位数均分（原币按支出币种、本位币按本位币）」，
// §九 不变式 1b。要求：两个不同币种各自取各自的位数，且能喂给 QuoRem。
func TestConsumerAmortizeSplit(t *testing.T) {
	// 模拟 rule/amortize 的均分：金额 ÷ 月数，按币种位数取商与余
	split := func(total decimal.Decimal, months int64, c Code) (share, remainder decimal.Decimal, err error) {
		if c.IsZero() {
			return decimal.Decimal{}, decimal.Decimal{}, errors.New("币种未指定")
		}
		return total.QuoRem(decimal.FromInt(months), c.Scale())
	}

	t.Run("原币按支出币种、本位币按本位币，各取各的位数", func(t *testing.T) {
		// 一笔以 KWD（3 位）支出、折算到 CNY（2 位）的摊分
		spend := MustParse("KWD")
		base := MustParse("CNY")
		if spend.Scale() == base.Scale() {
			t.Fatal("前置条件失败：需要两个位数不同的币种才能验证「各取各的」")
		}

		spendShare, spendRem, err := split(decimal.MustParse("100.000"), 3, spend)
		if err != nil {
			t.Fatalf("原币均分失败: %v", err)
		}
		baseShare, baseRem, err := split(decimal.MustParse("2380.00"), 3, base)
		if err != nil {
			t.Fatalf("本位币均分失败: %v", err)
		}

		// 原币按 3 位：100.000 / 3 = 33.333 余 0.001
		if !spendShare.Equal(decimal.MustParse("33.333")) {
			t.Errorf("原币份额 = %s, 期望 33.333", spendShare)
		}
		// 本位币按 2 位：2380.00 / 3 = 793.33 余 0.01
		if !baseShare.Equal(decimal.MustParse("793.33")) {
			t.Errorf("本位币份额 = %s, 期望 793.33", baseShare)
		}

		// 不变式 1b 的可加性：份额 × 月数 + 尾差 = 原额
		if got := spendShare.Mul(decimal.FromInt(3)).Add(spendRem); !got.Equal(decimal.MustParse("100.000")) {
			t.Errorf("原币份额×3+尾差 = %s, 期望 100.000", got)
		}
		if got := baseShare.Mul(decimal.FromInt(3)).Add(baseRem); !got.Equal(decimal.MustParse("2380.00")) {
			t.Errorf("本位币份额×3+尾差 = %s, 期望 2380.00", got)
		}
	})

	t.Run("0 位币种均分不产生小数份额", func(t *testing.T) {
		// JPY 摊分：份额必须是整数，否则界面会显示 33.33 日元这种不存在的金额
		share, rem, err := split(decimal.MustParse("100"), 3, MustParse("JPY"))
		if err != nil {
			t.Fatalf("均分失败: %v", err)
		}
		if !share.Equal(decimal.FromInt(33)) {
			t.Errorf("JPY 份额 = %s, 期望 33", share)
		}
		if !rem.Equal(decimal.FromInt(1)) {
			t.Errorf("JPY 尾差 = %s, 期望 1", rem)
		}
		if !share.FitsScale(0) {
			t.Errorf("JPY 份额 %s 不应有小数位", share)
		}
	})

	t.Run("3 位币种可正常摊分（承载 7）", func(t *testing.T) {
		// 「支出币种不受本位币候选限制」在摊分场景的具体表现
		share, _, err := split(decimal.MustParse("10.000"), 3, MustParse("KWD"))
		if err != nil {
			t.Fatalf("KWD 摊分失败: %v", err)
		}
		if !share.FitsScale(3) {
			t.Errorf("KWD 份额 %s 应可用 3 位表示", share)
		}
	})
}

// TestConsumerStorePersistence 走查 store / store/sqlite（L3/L4）。
//
// 分层 §六 L1.0：「只存代码入库」。§六 L3 承载⑥。
// 要求：落库为代码文本、回读还原；**已停用币种必须能回读**（终版 I-1）。
func TestConsumerStorePersistence(t *testing.T) {
	// 模拟一次落库 + 回读
	roundTrip := func(c Code) (Code, error) {
		v, err := c.Value() // store 写入
		if err != nil {
			return Code{}, err
		}
		var got Code
		if err := got.Scan(v); err != nil { // store 回读
			return Code{}, err
		}
		return got, nil
	}

	t.Run("全部币种落库回读一致", func(t *testing.T) {
		for _, want := range All() {
			got, err := roundTrip(want)
			if err != nil {
				t.Fatalf("%q 往返失败: %v", want, err)
			}
			if !got.Equal(want) {
				t.Errorf("往返后 %q != %q", got, want)
			}
		}
	})

	t.Run("可空列以零值往返", func(t *testing.T) {
		// 卡片币种：解析不出则留空 → NULL
		got, err := roundTrip(Code{})
		if err != nil {
			t.Fatalf("零值往返失败: %v", err)
		}
		if !got.IsZero() {
			t.Errorf("零值往返后应仍为零值, 实际 %q", got)
		}
	})

	t.Run("历史明细引用的停用币种仍可回读", func(t *testing.T) {
		// 终版 I-1 的核心行为：停用不影响读出。
		// 若 Scan 建立在「已启用」上，F-4 显示已删除、K-3 导出、I-7 重算三处同时失效。
		disabled := Code{entry: &entry{
			code: "ZZZ", name: "测试停用币种", symbol: "Z",
			scale: 2, enabled: false, rateSource: RateSourceDefault,
		}}
		v, err := disabled.Value()
		if err != nil {
			t.Fatalf("停用币种落库失败: %v", err)
		}
		if v != "ZZZ" {
			t.Errorf("停用币种落库值 = %v, 期望 ZZZ", v)
		}
	})

	t.Run("按币种分组聚合（J-4）", func(t *testing.T) {
		// store 的币种维度统计依赖 Code 可作 map 键
		stats := map[Code]decimal.Decimal{}
		rows := []struct {
			code   string
			amount string
		}{
			{"CNY", "100.00"}, {"USD", "20.00"}, {"CNY", "50.50"},
		}
		for _, r := range rows {
			c := MustParse(r.code)
			stats[c] = stats[c].Add(decimal.MustParse(r.amount))
		}
		if len(stats) != 2 {
			t.Errorf("分组数 = %d, 期望 2", len(stats))
		}
		if got := stats[MustParse("CNY")]; !got.Equal(decimal.MustParse("150.50")) {
			t.Errorf("CNY 合计 = %s, 期望 150.50", got)
		}
	})
}

// TestConsumerPreferenceBaseSwitch 走查 service/preference（L5.4）。
//
// 分层 §六 L5：「本位币走 currency 的「本位币候选」校验（T38，非候选一律拒绝）」。
// 要求：有**单值**校验入口，不必取回集合再自行遍历。
func TestConsumerPreferenceBaseSwitch(t *testing.T) {
	// 模拟 service/preference 的本位币切换（K-1）
	switchBase := func(input string) (Code, error) { return ParseBase(input) }

	t.Run("候选币种切换成功", func(t *testing.T) {
		got, err := switchBase("USD")
		if err != nil {
			t.Fatalf("切换失败: %v", err)
		}
		if got.String() != "USD" {
			t.Errorf("切换结果 = %q", got)
		}
	})

	t.Run("非候选一律拒绝且可分档给文案", func(t *testing.T) {
		cases := []struct {
			input string
			want  error
		}{
			{"", ErrEmptyCode},           // 没填
			{"XXX", ErrUnknownCode},      // 不支持
			{"KWD", ErrNotBaseCandidate}, // 支持，但不能作本位币
		}
		for _, tc := range cases {
			_, err := switchBase(tc.input)
			if !errors.Is(err, tc.want) {
				t.Errorf("switchBase(%q) 错误 = %v, 期望 %v", tc.input, err, tc.want)
			}
		}
	})

	t.Run("下拉候选集与单值校验口径一致", func(t *testing.T) {
		// K-1 下拉列出的每一项，都必须能通过单值校验；否则用户选了下拉里的东西却被拒
		for _, c := range BaseCandidates() {
			if _, err := switchBase(c.String()); err != nil {
				t.Errorf("下拉候选 %q 未能通过单值校验: %v", c, err)
			}
		}
		// 反向：不在候选集里的已知币种，必须被拒
		inCandidates := make(map[Code]bool)
		for _, c := range BaseCandidates() {
			inCandidates[c] = true
		}
		for _, c := range All() {
			if inCandidates[c] {
				continue
			}
			if _, err := switchBase(c.String()); err == nil {
				t.Errorf("非候选币种 %q 却通过了单值校验", c)
			}
		}
	})
}

// TestConsumerFxRateRouting 走查 fxrate（L3）。
//
// 分层 §六 准则 3：枚举项只存标识，注册表在 L3、实现体在 L4。
// 要求：标识可作 map 键，且每个币种都取得到。
func TestConsumerFxRateRouting(t *testing.T) {
	// 模拟 fxrate 的注册表：标识 → 实现
	type fetcher struct{ name string }
	registry := map[RateSource]*fetcher{
		RateSourceDefault: {name: "默认汇率源"},
	}

	t.Run("每个币种都能路由到实现", func(t *testing.T) {
		for _, c := range All() {
			src := c.RateSource()
			if src == "" {
				t.Errorf("%q 的汇率源标识为空，无法路由", c)
				continue
			}
			if _, ok := registry[src]; !ok {
				t.Errorf("%q 的汇率源标识 %q 在注册表中无对应实现", c, src)
			}
		}
	})

	t.Run("零值取不到标识，路由前须判空", func(t *testing.T) {
		if got := (Code{}).RateSource(); got != "" {
			t.Errorf("零值 RateSource() = %q, 期望空串", got)
		}
		if _, ok := registry[(Code{}).RateSource()]; ok {
			t.Error("零值不应命中任何实现")
		}
	})
}

// TestConsumerI18nNameKeys 走查 i18n（L3）与 L6 渲染。
//
// 终版 K-4：界面文案走语言配置、加语种不改代码。
// 要求：本包出文案键，且键集合可供穷尽性校验。
func TestConsumerI18nNameKeys(t *testing.T) {
	t.Run("文案键唯一且与币种一一对应", func(t *testing.T) {
		keys := NameKeys()
		seen := make(map[string]bool, len(keys))
		for _, k := range keys {
			if seen[k] {
				t.Errorf("文案键 %q 重复", k)
			}
			seen[k] = true
		}
		if len(keys) != len(All()) {
			t.Errorf("文案键数 %d 与币种数 %d 不一致", len(keys), len(All()))
		}
	})

	t.Run("模拟语言文件的穷尽性校验", func(t *testing.T) {
		// i18n 那一轮应当做的检查：语言文件必须覆盖每一个键
		langFile := map[string]string{}
		for _, k := range NameKeys() {
			langFile[k] = "（译文）"
		}
		// 故意漏一个，确认检查真的能发现
		delete(langFile, "currency.name.CNY")

		missing := 0
		for _, k := range NameKeys() {
			if _, ok := langFile[k]; !ok {
				missing++
			}
		}
		if missing != 1 {
			t.Errorf("穷尽性校验应发现 1 个缺失的键, 实际 %d", missing)
		}
	})

	t.Run("Name 只供日志，不等同于文案键", func(t *testing.T) {
		// 防止有人把 Name() 直接下发给界面
		c := MustParse("CNY")
		if c.Name() == c.NameKey() {
			t.Error("中文名与文案键不应相同——前者仅供日志与排错")
		}
	})
}

// TestConsumerNoUpwardDependency 确认本包的生产代码零仓库内依赖。
//
// 本包处于 L1.0：若 import 了 enum / entity / i18n / fxrate / store 等同层或
// 上层包，即构成依赖环。承载表的依赖列写的是 base/*（普遍依赖口径），而本包
// 实际实现下来连 base/* 都不需要——小数位数只是个 int32 常量、错误用哨兵表达。
//
// 用例扫描全部生产代码文件的 import，出现任何 jotcash 内部包即失败。
// 本测试文件自身 import base/decimal 只用于上面的消费方走查，不进生产依赖
// （测试文件被排除在扫描之外）。
func TestConsumerNoUpwardDependency(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析包源码失败: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("未解析到任何生产代码文件——用例形同虚设")
	}

	const modulePrefix = `"github.com/cellargalaxy/jotcash/`
	scanned := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			scanned++
			for _, imp := range file.Imports {
				if strings.HasPrefix(imp.Path.Value, modulePrefix) {
					t.Errorf("%s import 了仓库内部包 %s。本包处于 L1.0，"+
						"实际实现为零仓库内依赖；引入内部依赖前须先确认不构成依赖环",
						path, imp.Path.Value)
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("未扫描到任何生产代码文件——用例形同虚设")
	}
}

// TestSentinelCarriesNoInitStack 锁死「哨兵错误不得自带包初始化时的调用栈」。
//
// 这条用例防的是一个具体且极难察觉的错误：用 github.com/pkg/errors.New 构造
// 包级哨兵。它在**变量初始化那一刻**抓栈，而包级变量的初始化发生在 init 阶段，
// 于是哨兵永久携带一份指向 currency.init 的栈。后果在本包内完全看不出来
// （errors.Is 照常成立、错误文案照常正确），要到 L5 + middleware 才暴露：
//
//	base/errs 的 hasStack（errs.go:107）见「链上已有栈」就跳过补栈，
//	middleware 打 %+v 得到的栈顶是 currency.init，真正的出错调用点不在栈里。
//
// 排错时会被引向完全错误的方向，而这种事故只在线上排查某个具体请求时才被发现。
// 同层 decimal / calendar / enum 的哨兵都用标准库 errors.New 构造，本包与之对齐。
//
// 断言方式是 AST 扫描而非行为断言：行为上「栈里有没有 init」依赖 errs 的实现细节，
// 而「哨兵是怎么造出来的」才是根因，扫 import 能一次盖住将来新增的哨兵。
func TestSentinelCarriesNoInitStack(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析包源码失败: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("未解析到任何生产代码文件——用例形同虚设")
	}

	scanned := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			scanned++
			for _, imp := range file.Imports {
				if imp.Path.Value == `"github.com/pkg/errors"` {
					t.Errorf("%s import 了 github.com/pkg/errors。本包的哨兵与错误包装"+
						"一律用标准库 errors.New + fmt.Errorf(\"%%w: …\")：pkg/errors.New "+
						"会在 init 阶段给哨兵抓一份栈，使 base/errs 跳过补栈，"+
						"middleware 打 %%+v 时栈顶变成 currency.init 而非真实出错点", path)
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("未扫描到任何生产代码文件——用例形同虚设")
	}

	// 行为侧兜一道：四个哨兵都不得实现 StackTrace()。
	// 上面扫 import 防的是「怎么造」，这里防的是「换个库照样带栈」。
	type stackTracer interface{ StackTrace() []uintptr }
	for name, sentinel := range map[string]error{
		"ErrEmptyCode":        ErrEmptyCode,
		"ErrUnknownCode":      ErrUnknownCode,
		"ErrNotBaseCandidate": ErrNotBaseCandidate,
		"ErrScanType":         ErrScanType,
	} {
		var st stackTracer
		if errors.As(sentinel, &st) {
			t.Errorf("%s 自带调用栈，会让 base/errs 跳过补栈", name)
		}
	}
}
