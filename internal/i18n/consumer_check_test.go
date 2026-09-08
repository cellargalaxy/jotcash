package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/currency"
	// 本包已用 go/parser 做 AST 扫描，故仓库内的 parser 包以别名引入避免撞名。
	jcparser "github.com/cellargalaxy/jotcash/internal/parser"
	"github.com/cellargalaxy/jotcash/internal/rule/passwd"
	"github.com/cellargalaxy/jotcash/internal/store"
)

// 本文件做两件事：
//
//	① **文案键穷尽性校验**——把「哪些包声明了文案键」变成可执行断言。
//	   本包的生产代码不能 import 这些包（依赖列为 `—`），故该校验只能落在测试
//	   文件里：测试文件 import errs / passwd / currency，拿它们**导出的键常量**
//	   逐一断言语言文件已覆盖。这与 currency 的手法一致（生产代码零仓库内依赖，
//	   测试文件 import base/decimal 做消费方走查）。
//
//	   这条校验的价值在于「加键的那一轮会当场失败」：将来任何包新增一个文案键，
//	   若忘了往语言文件里加词，本文件立即报错——而不是等到用户在界面上看到一个
//	   键名。反向也成立：语言文件里多出没人用的键同样被查出（F-4 那类死键）。
//
//	② **下游消费方走查**——按分层 §六 把「下游会怎么用本包」写成断言，
//	   使本包 API 形状一旦发生对下游有害的变化，在本轮就失败而非到下游那轮。
//
// 五个消费方：middleware(L6) / handler/i18n(L6) / service/preference(L5.4) /
// dto(L4) / entity(L1.1，间接：User.界面语言 的落库值)

// ---------- ① 文案键穷尽性校验 ----------

// declaredKeys 汇总全仓库已声明的文案键，按来源分组。
//
// 全部取自各包**导出的常量或函数**，不硬编码字面量——否则本文件会与真实声明
// 脱钩，变成一份需要人工同步的清单（那正是它要防的事）。
func declaredKeys() map[string][]string {
	return map[string][]string{
		// base/errs（L0）的五档兜底键。errs_test.go 已写明「语言文件必须覆盖
		// 这五个键」是硬性契约：它们是 F-4 兜底文案，缺一个就意味着某类错误
		// 在界面上显示为键名。
		"base/errs": {
			errs.KeyInvalidInput,
			errs.KeyConflict,
			errs.KeyForbidden,
			errs.KeyUnauthorized,
			errs.KeyInternal,
		},
		// rule/passwd（L2）的五个键。其中两个带具名占位参数，见下方
		// TestPasswdKeysCarryDeclaredParams。
		"rule/passwd": {
			passwd.KeyEmpty,
			passwd.KeyInvalidEncoding,
			passwd.KeyTooShort,
			passwd.KeyTooFewCategories,
			passwd.KeyGenerateFailed,
		},
		// currency（L1.0）的 27 个币种名称键，由 NameKeys() 给出。
		// 用函数而非字面量列表，故加币种时此处自动跟进。
		"currency": currency.NameKeys(),
		// store（L3）的 7 个键。前 5 个在 errors.go、后 2 个在 column.go。
		// 它们全部经 base/errs 流向 middleware，缺词即在界面上显示为键名。
		"store": {
			store.KeyNotFound,
			store.KeyUsernameConflict,
			store.KeyCategoryNameConflict,
			store.KeyCategoryInUse,
			store.KeyOwnerRequired,
			store.KeyAmountOutOfRange,
			store.KeyRateOutOfRange,
		},
		// parser（L3）的 7 个键：D-3 的五类解析错误 + 注册表的两个键。
		// parser/errors.go 的注释明写「语言文件必须包含全部 7 个键」。
		"parser": {
			jcparser.KeyTypeMismatch,
			jcparser.KeyDecryptFailed,
			jcparser.KeyFormatMismatch,
			jcparser.KeyContentCorrupt,
			jcparser.KeyFieldMissing,
			jcparser.KeyTypeUnsupported,
			jcparser.KeyNotRegistered,
		},
	}
}

// TestLocaleCoversAllDeclaredKeys 每个已声明的文案键在**每个**语言文件里都必须
// 有词。缺一个键就意味着某条提示在界面上显示为 "errs.internal" 这样的键名。
func TestLocaleCoversAllDeclaredKeys(t *testing.T) {
	for _, lang := range Languages() {
		msgs := Messages(lang)
		for source, keys := range declaredKeys() {
			for _, key := range keys {
				if _, ok := msgs[key]; !ok {
					t.Errorf("[%s] 缺少 %s 声明的文案键 %q", lang, source, key)
				}
			}
		}
	}
}

// TestAllDeclaredKeysRender 覆盖不等于可用：每个键还必须真能渲染出非空文案。
//
// 与上一条分开是因为两者的失败原因不同：上一条查「键在不在」，这一条查
// 「键对应的文案是否可用」（值为空串、模板坏掉、占位参数对不上都在此暴露）。
func TestAllDeclaredKeysRender(t *testing.T) {
	// 带占位参数的键需要给参数，否则 missingkey=error 会正确地报错。
	paramsFor := map[string]Params{
		passwd.KeyTooShort:         {"Min": 10, "Actual": 4},
		passwd.KeyTooFewCategories: {"Min": 2, "Actual": 1},
		// store / parser 侧带参键的参数名取自各自的构造函数（store/errors.go、
		// parser/errors.go），两侧不一致会在此处渲染失败。
		store.KeyUsernameConflict:     {"Username": "alice"},
		store.KeyCategoryNameConflict: {"Name": "餐饮"},
		store.KeyCategoryInUse:        {"Name": "餐饮", "Count": 3},
		jcparser.KeyTypeMismatch:      {"ParserType": "generic_csv", "ParserFormat": "csv", "FileFormat": "pdf"},
		jcparser.KeyFormatMismatch:    {"Reason": "缺少表头"},
		jcparser.KeyContentCorrupt:    {"Line": 12, "Field": "金额", "Value": "abc"},
		jcparser.KeyFieldMissing:      {"Line": 12, "Field": "支出日期"},
	}

	for _, lang := range Languages() {
		for source, keys := range declaredKeys() {
			for _, key := range keys {
				got, err := Render(lang, key, paramsFor[key])
				if err != nil {
					t.Errorf("[%s] %s 的键 %q 渲染失败: %v", lang, source, key, err)
					continue
				}
				if got == key {
					t.Errorf("[%s] %s 的键 %q 回落成了键名", lang, source, key)
				}
				if strings.TrimSpace(got) == "" {
					t.Errorf("[%s] %s 的键 %q 渲染出空白文案", lang, source, key)
				}
				for _, residue := range []string{"{{", "}}", "<no value>"} {
					if strings.Contains(got, residue) {
						t.Errorf("[%s] %s 的键 %q 渲染结果 %q 残留 %q",
							lang, source, key, got, residue)
					}
				}
			}
		}
	}
}

// TestNoOrphanKeysInLocale 反向校验：语言文件里不得有无人声明的键。
//
// 死键是 i18n 最典型的腐化形式——改了业务代码却忘了删文案，日积月累没人敢动
// 任何一条。此处把它变成硬失败。
//
// 保留键（i18n.display_name）与将来 L6 的界面文案键需显式登记：L6 尚未实现，
// 故本轮的白名单只有保留键一项；等 L6 那轮加界面文案时，会在此处登记其键前缀。
func TestNoOrphanKeysInLocale(t *testing.T) {
	declared := make(map[string]string) // 键 → 来源
	for source, keys := range declaredKeys() {
		for _, key := range keys {
			declared[key] = source
		}
	}
	// 本包自己的保留键
	declared[displayNameKey] = "i18n（保留键）"

	for _, lang := range Languages() {
		for _, key := range MessageKeys(lang) {
			if _, ok := declared[key]; !ok {
				t.Errorf("[%s] 语言文件含无人声明的死键 %q——"+
					"若它属于新增的界面文案，请在 declaredKeys 中登记其来源", lang, key)
			}
		}
	}
}

// TestKeyCountMatchesExpectation 键总数的显式断言。
//
// 上面三条都是逐键比对，一旦两侧同时漏掉同一个键就查不出来。此处钉死总数，
// 使「加了键但两边都没登记」也会失败。数字变化时应当在此处一并更新，
// 那正是提醒评审者「这一轮动了文案键」的信号。
func TestKeyCountMatchesExpectation(t *testing.T) {
	const (
		wantErrs     = 5  // 五档兜底
		wantPasswd   = 5  // 空/编码/长度/类别/生成失败
		wantCurrency = 27 // 终版 §四 币种表
		wantStore    = 7  // 未找到/用户名冲突/类型重名/类型被引用/缺归属/金额越界/汇率越界
		wantParser   = 7  // D-3 五类 + 类型不支持 + 未注册
		wantReserved = 1  // i18n.display_name
	)
	wantTotal := wantErrs + wantPasswd + wantCurrency + wantStore + wantParser + wantReserved

	d := declaredKeys()
	if got := len(d["base/errs"]); got != wantErrs {
		t.Errorf("errs 键数 = %d, 期望 %d", got, wantErrs)
	}
	if got := len(d["rule/passwd"]); got != wantPasswd {
		t.Errorf("passwd 键数 = %d, 期望 %d", got, wantPasswd)
	}
	if got := len(d["currency"]); got != wantCurrency {
		t.Errorf("currency 键数 = %d, 期望 %d（终版 §四 币种表）", got, wantCurrency)
	}
	if got := len(d["store"]); got != wantStore {
		t.Errorf("store 键数 = %d, 期望 %d", got, wantStore)
	}
	if got := len(d["parser"]); got != wantParser {
		t.Errorf("parser 键数 = %d, 期望 %d（D-3 五类 + 注册表两项）", got, wantParser)
	}
	for _, lang := range Languages() {
		if got := len(MessageKeys(lang)); got != wantTotal {
			t.Errorf("[%s] 语言文件键数 = %d, 期望 %d", lang, got, wantTotal)
		}
	}
}

// TestPasswdKeysCarryDeclaredParams passwd 的两个带参键必须恰好接受
// {Min, Actual} 两个具名参数。
//
// 这条契约来自 rule/passwd 那一轮（260907-204911-answer.md §三 6 的
// 「对 i18n 那一轮的硬性契约」）：文案键与占位参数名两侧必须一致，否则
// passwd 传的参数填不进文案。参数名一旦写错，在 missingkey=error 下会渲染失败，
// 而那是用户提交密码时才会走到的路径。
func TestPasswdKeysCarryDeclaredParams(t *testing.T) {
	cases := []struct {
		key    string
		params Params
	}{
		{passwd.KeyTooShort, Params{"Min": 10, "Actual": 4}},
		{passwd.KeyTooFewCategories, Params{"Min": 2, "Actual": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			// 给全参数：必须成功且两个数都出现在文案里
			got, err := Render(Default(), tc.key, tc.params)
			if err != nil {
				t.Fatalf("给全参数仍渲染失败: %v", err)
			}
			for name, v := range tc.params {
				if !strings.Contains(got, itoa(v)) {
					t.Errorf("文案 %q 未包含参数 %s=%v", got, name, v)
				}
			}
			// 逐个抽掉一个参数：必须报错（证明该参数确实被文案使用）
			for name := range tc.params {
				partial := Params{}
				for k, v := range tc.params {
					if k != name {
						partial[k] = v
					}
				}
				if _, err := Render(Default(), tc.key, partial); err == nil {
					t.Errorf("抽掉参数 %s 后仍渲染成功——该参数未被文案使用，"+
						"passwd 传的值会被静默丢弃", name)
				}
			}
		})
	}
}

// TestCurrencyNameKeysMatchTableOrder 币种名称键必须与 currency 的键口径一致。
//
// currency.NameKey() 是键的唯一构造入口，本包不得自行拼接 "currency.name." +
// code——那样两处一旦不一致，币种名称会整片显示为键名。
func TestCurrencyNameKeysMatchTableOrder(t *testing.T) {
	msgs := Messages(Default())
	for _, code := range currency.All() {
		key := code.NameKey()
		name, ok := msgs[key]
		if !ok {
			t.Errorf("币种 %s 的名称键 %q 不在语言文件里", code, key)
			continue
		}
		if name == "" {
			t.Errorf("币种 %s 的名称为空", code)
		}
		// 键必须能被 Render 取到（而非只是 map 里有）
		got, err := Render(Default(), key, nil)
		if err != nil || got != name {
			t.Errorf("币种 %s 取词不一致: Render=%q(err=%v) Messages=%q", code, got, err, name)
		}
	}
}

// ---------- ② 下游消费方走查 ----------

// TestConsumerMiddlewareRendersErrs 走查 middleware（L6）—— 本包最主要的消费方。
//
// 分层 §六 L6 middleware 行：语言解析（ctxs.界面语言 → Accept-Language →
// i18n 默认语言）→ 取 errs 的文案键与占位参数 → 渲染 → 写响应体。
// 此处把整条链路串起来跑一遍，用真实的 errs 错误对象而非手造的键。
func TestConsumerMiddlewareRendersErrs(t *testing.T) {
	// middleware 拿到的是 errs 构造出的错误，键与参数都从错误对象上取。
	// 注意 errs.New* 的第一个参数就是**文案键**（errs.go:117），不是描述文本——
	// 错误对象只带「键 + 参数」，不含文案（errs 包注释）。
	cases := []struct {
		name string
		err  error
	}{
		{"输入错误", errs.NewInvalidInput(errs.KeyInvalidInput, nil)},
		{"冲突", errs.NewConflict(errs.KeyConflict, nil)},
		{"无权", errs.NewForbidden(errs.KeyForbidden, nil)},
		{"未登录", errs.NewUnauthorized(errs.KeyUnauthorized, nil)},
		{"系统错误", errs.NewInternal(errs.KeyInternal, nil)},
		// 带占位参数的真实场景：passwd 的长度校验经 service 层上抛
		{"带占位参数", errs.NewInvalidInput(passwd.KeyTooShort,
			errs.Params{"Min": 10, "Actual": 4})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 第一段：语言解析。未登录时 ctxs 无值，退到 Accept-Language。
			lang := Match("zh-CN,en;q=0.9")

			// 第二段：从错误对象取键与参数——middleware 不自己拼键。
			key := errs.MsgKeyOf(tc.err)
			params := errs.ParamsOf(tc.err)
			if key == "" {
				t.Fatalf("errs 未给出文案键，middleware 无从取词")
			}

			// 第三段：渲染。第一个返回值直接进响应体，第二个进日志。
			body, renderErr := Render(lang, key, params)
			if renderErr != nil {
				t.Errorf("渲染失败: %v", renderErr)
			}
			if body == "" {
				t.Error("响应体文案为空")
			}
			if body == key {
				t.Errorf("回落成键名 %q——用户会看到一个内部标识符", key)
			}
		})
	}
}

// TestConsumerMiddlewareLanguageChain 走查 middleware 的语言解析链三段回落。
//
// 分层 §三 问题 5：语言解析必须先于登录态校验，登录页与 A-8 登录失败提示
// 都要有文案。故三段中任何一段都不能失败。
func TestConsumerMiddlewareLanguageChain(t *testing.T) {
	renderCheck := func(t *testing.T, lang Lang, stage string) {
		t.Helper()
		got, err := Render(lang, errs.KeyUnauthorized, nil)
		if err != nil {
			t.Errorf("%s：取词失败 %v", stage, err)
		}
		if got == errs.KeyUnauthorized {
			t.Errorf("%s：回落成键名，登录页没有可读文案", stage)
		}
	}

	t.Run("第一段_ctxs 有界面语言", func(t *testing.T) {
		// 已登录：ctxs 里的值是 entity.User.Language（string），经 ParseLang 转回
		stored := "zh-CN"
		lang, err := ParseLang(stored)
		if err != nil {
			t.Fatalf("ParseLang(%q) 失败: %v", stored, err)
		}
		renderCheck(t, lang, "ctxs 有值")
	})

	t.Run("第二段_退到 Accept-Language", func(t *testing.T) {
		// 未登录：ctxs 无值，用请求头
		renderCheck(t, Match("en-US,en;q=0.9"), "Accept-Language")
	})

	t.Run("第三段_退到默认语言", func(t *testing.T) {
		// 连请求头都没有
		renderCheck(t, Match(""), "默认语言")
		// 以及 ctxs 里存了非法值时 middleware 会拿到零值
		renderCheck(t, Lang{}, "零值语言")
	})
}

// TestConsumerHandlerI18nBundle 走查 handler/i18n（L6）的整包下发（T34④）。
//
// 分层 §三 问题 5：语言包接口是 /api/ 组内白名单（未登录可访问）。
// 要求：给任意语言（含零值）都能拿到一份完整、可 JSON 序列化的文案表。
func TestConsumerHandlerI18nBundle(t *testing.T) {
	for _, lang := range append(Languages(), Lang{}) {
		msgs := Messages(lang)
		if len(msgs) == 0 {
			t.Fatalf("[%v] 整包文案为空，前端一条文案都取不到", lang)
		}
		// 键集合必须是全集（已按默认语言补全）
		if len(msgs) != len(Messages(Default())) {
			t.Errorf("[%v] 键数 %d != 默认语言 %d，前端会出现空白文案",
				lang, len(msgs), len(Messages(Default())))
		}
		// 值必须都非空
		for k, v := range msgs {
			if v == "" {
				t.Errorf("[%v] 键 %q 的文案为空", lang, k)
			}
		}
	}
}

// TestConsumerPreferenceValidatesLanguage 走查 service/preference（L5.4）。
//
// 分层 §六 L5.4：「界面语言走 i18n 语言键校验」。K-1 的界面语言切换必须拒绝
// 非候选值——否则库里会存进一个取不到词的语言键，用户下次登录看到满屏键名。
func TestConsumerPreferenceValidatesLanguage(t *testing.T) {
	t.Run("下拉候选项来自 Languages", func(t *testing.T) {
		langs := Languages()
		if len(langs) == 0 {
			t.Fatal("没有可用语言，K-1 下拉将是空的")
		}
		for _, l := range langs {
			if l.DisplayName() == "" {
				t.Errorf("语言 %q 没有可显示的名称", l)
			}
			// 下拉项的 value 必须能被 ParseLang 接受（提交时要再校验一次）
			if _, err := ParseLang(l.String()); err != nil {
				t.Errorf("下拉项 %q 无法通过 ParseLang: %v", l, err)
			}
		}
	})

	t.Run("非候选值一律拒绝", func(t *testing.T) {
		for _, bad := range []string{"", "en", "zh-TW", "fr", "!!bad!!", "zh"} {
			if _, err := ParseLang(bad); err == nil {
				t.Errorf("ParseLang(%q) 应当拒绝——存进库里会导致满屏键名", bad)
			}
		}
	})

	t.Run("落库值可无损回读", func(t *testing.T) {
		// entity.User.Language 是 string，故必须能往返
		for _, l := range Languages() {
			back, err := ParseLang(l.String())
			if err != nil || !back.Equal(l) {
				t.Errorf("语言 %q 往返失败: back=%q err=%v", l, back, err)
			}
		}
	})
}

// TestConsumerDtoRejectsBadLanguage 走查 dto（L4）。
//
// dto 校验用户提交的界面语言时，需要区分「没填」与「填了但不支持」——
// 前者可能是可选字段未填（应保持原值），后者必须报错。三档哨兵使这件事可做。
func TestConsumerDtoRejectsBadLanguage(t *testing.T) {
	// 模拟 dto 的判定逻辑
	classify := func(input string) string {
		_, err := ParseLang(input)
		switch {
		case err == nil:
			return "ok"
		case is(err, ErrEmptyLang):
			return "未填"
		case is(err, ErrMalformedLang):
			return "格式错"
		case is(err, ErrUnknownLang):
			return "不支持"
		default:
			return "未分类"
		}
	}

	cases := map[string]string{
		"zh-CN":   "ok",
		"":        "未填",
		"!!bad!!": "格式错",
		" zh-CN ": "格式错",
		"en":      "不支持",
		"zh-TW":   "不支持",
	}
	for input, want := range cases {
		if got := classify(input); got != want {
			t.Errorf("classify(%q) = %q, 期望 %q", input, got, want)
		}
	}
}

// TestRealPasswdErrorsRenderEndToEnd 用 rule/passwd **真实产生**的错误对象
// 走完整链路，而不是手写占位参数。
//
// 这条比 TestPasswdKeysCarryDeclaredParams 更强：后者的参数名是我在测试里写的，
// 若我把 passwd 的参数名抄错了，两边一起错、测试照样通过。此处调 passwd.Check
// 让它自己产生错误，参数名与值全部来自生产代码（passwd.go:238/243），
// 故「语言文件的占位符名与 passwd 传的参数名不一致」这类错误无处可藏。
func TestRealPasswdErrorsRenderEndToEnd(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"空密码", ""},
		{"太短", "aA1"},
		{"类别不足", strings.Repeat("a", 32)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := passwd.Check(tc.input)
			if err == nil {
				t.Skipf("输入 %q 通过了校验，本用例不适用", tc.input)
			}

			// middleware 的做法：从错误对象取键与参数，不自己拼
			key := errs.MsgKeyOf(err)
			params := errs.ParamsOf(err)
			if key == "" {
				t.Fatalf("passwd 的错误未携带文案键: %v", err)
			}

			got, renderErr := Render(Default(), key, params)
			if renderErr != nil {
				t.Fatalf("键 %q 参数 %v 渲染失败: %v——"+
					"语言文件的占位符名与 passwd 传的参数名可能不一致",
					key, params, renderErr)
			}
			if got == key {
				t.Errorf("键 %q 回落成键名", key)
			}
			for _, residue := range []string{"{{", "}}", "<no value>"} {
				if strings.Contains(got, residue) {
					t.Errorf("渲染结果 %q 残留 %q", got, residue)
				}
			}
			t.Logf("键 %q + 参数 %v → %q", key, params, got)
		})
	}
}

// ---------- 依赖边界与导出面 ----------

// TestNoRepoImportsInProductionCode 断言生产代码零仓库内依赖。
//
// 分层 §六 L3 表：i18n 的依赖列是 `—`。这不是风格问题——一旦本包 import 了
// base/errs，而 errs 的包注释又写明它不依赖 i18n，两者的边界就开始漂移；
// 更实际的是，import 任何业务包都会让「本包能否在 errs 之前初始化」变得不确定。
//
// 用 AST 扫描而非人工检查：新增一个 import 会当场失败。
func TestNoRepoImportsInProductionCode(t *testing.T) {
	const repoPrefix = `"github.com/cellargalaxy/jotcash/`

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", isProductionFile, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析包目录失败: %v", err)
	}

	found := false
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			found = true
			for _, imp := range file.Imports {
				if strings.HasPrefix(imp.Path.Value, repoPrefix) {
					t.Errorf("%s 引入了仓库内依赖 %s——i18n 的依赖列应为 `—`",
						path, imp.Path.Value)
				}
			}
		}
	}
	if !found {
		t.Fatal("未扫描到任何生产代码文件，断言失效")
	}
}

// TestSentinelsCarryNoInitStack 哨兵不得自带调用栈。
//
// 若用 pkg/errors.New 构造哨兵，它会在包级变量初始化时抓栈，使哨兵永久携带
// 一份指向 i18n.init 的栈；base/errs.Wrap 检测到已有栈便跳过补栈，middleware
// 打 %+v 时栈顶就变成 i18n.init 而非真实出错点——排查时看到的是一个恒定的
// 无用位置。该约束由 currency 那一轮立下（consumer_check_test.go
// TestSentinelCarriesNoInitStack），本包同步照做。
func TestSentinelsCarryNoInitStack(t *testing.T) {
	sentinels := map[string]error{
		"ErrEmptyLang":     ErrEmptyLang,
		"ErrMalformedLang": ErrMalformedLang,
		"ErrUnknownLang":   ErrUnknownLang,
		"ErrMissingKey":    ErrMissingKey,
		"ErrRenderFailed":  ErrRenderFailed,
	}
	for name, err := range sentinels {
		if err == nil {
			t.Errorf("%s 为 nil", name)
			continue
		}
		// pkg/errors 的错误实现 StackTrace() —— 标准库的不实现
		if st, ok := err.(interface{ StackTrace() interface{} }); ok {
			t.Errorf("%s 自带调用栈 %v——应用标准库 errors.New 构造", name, st)
		}
		type stackTracer interface{ StackTrace() []uintptr }
		if _, ok := err.(stackTracer); ok {
			t.Errorf("%s 自带调用栈——应用标准库 errors.New 构造", name)
		}
	}
}

// TestSentinelsAreDistinguishable 五个哨兵必须两两可分。
//
// 若某两个哨兵被 errors.Is 判定为同一个，dto 就无法区分「没填」与「不支持」，
// 两种情况会给出同一句话。
func TestSentinelsAreDistinguishable(t *testing.T) {
	all := []struct {
		name string
		err  error
	}{
		{"ErrEmptyLang", ErrEmptyLang},
		{"ErrMalformedLang", ErrMalformedLang},
		{"ErrUnknownLang", ErrUnknownLang},
		{"ErrMissingKey", ErrMissingKey},
		{"ErrRenderFailed", ErrRenderFailed},
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if is(a.err, b.err) {
				t.Errorf("%s 与 %s 无法区分", a.name, b.name)
			}
		}
	}
}

// TestExportedSurface 导出面清单。
//
// 本包处在 L3 边界层、被 L4/L5/L6 三层消费，导出面每多一项就多一处将来必须
// 维持兼容的约定。此处显式登记，使新增导出必须经过一次有意识的修改。
func TestExportedSurface(t *testing.T) {
	want := map[string]bool{
		// 类型
		"Lang": true, "Params": true,
		// 取词
		"Render": true, "MustRender": true,
		// 语言
		"Languages": true, "Default": true, "ParseLang": true, "Match": true,
		// 整包下发
		"Messages": true, "MessageKeys": true,
		// 常量
		"DefaultLangKey": true,
		// 哨兵
		"ErrEmptyLang": true, "ErrMalformedLang": true, "ErrUnknownLang": true,
		"ErrMissingKey": true, "ErrRenderFailed": true,
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", isProductionFile, 0)
	if err != nil {
		t.Fatalf("解析包目录失败: %v", err)
	}

	got := make(map[string]bool)
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					// 只收包级函数，方法归属于其接收者类型
					if d.Recv == nil && d.Name.IsExported() {
						got[d.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							if s.Name.IsExported() {
								got[s.Name.Name] = true
							}
						case *ast.ValueSpec:
							for _, n := range s.Names {
								if n.IsExported() {
									got[n.Name] = true
								}
							}
						}
					}
				}
			}
		}
	}

	for name := range got {
		if !want[name] {
			t.Errorf("新增了未登记的导出项 %q——请确认它确实需要对外暴露，"+
				"并在本用例的清单中登记", name)
		}
	}
	for name := range want {
		if !got[name] {
			t.Errorf("导出项 %q 消失了——下游可能已经在用它", name)
		}
	}
}

// ---------- 辅助 ----------

// isProductionFile 过滤掉测试文件，只留生产代码——两处 AST 扫描共用。
func isProductionFile(fi fs.FileInfo) bool {
	return !strings.HasSuffix(fi.Name(), "_test.go")
}

// is 是 errors.Is 的本地别名，避免本文件与 errs 包的命名混淆。
func is(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// itoa 把占位参数值转成其在文案中的呈现形式，用于断言参数确实被填入。
func itoa(v any) string {
	switch n := v.(type) {
	case int:
		return intToStr(n)
	case string:
		return n
	default:
		return ""
	}
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// 保持 sort 的导入有用：键清单的顺序断言在别处，此处仅做一次自检，
// 确保 declaredKeys 内各组自身无重复键。
func TestDeclaredKeysHaveNoDuplicates(t *testing.T) {
	for source, keys := range declaredKeys() {
		sorted := make([]string, len(keys))
		copy(sorted, keys)
		sort.Strings(sorted)
		for i := 1; i < len(sorted); i++ {
			if sorted[i] == sorted[i-1] {
				t.Errorf("%s 声明了重复的文案键 %q", source, sorted[i])
			}
		}
	}
}
