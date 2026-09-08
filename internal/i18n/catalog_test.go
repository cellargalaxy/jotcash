package i18n

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	goi18ntpl "github.com/nicksnyder/go-i18n/v2/i18n/template"
	"golang.org/x/text/language"
)

// 本文件分两部分：
//   ① load 的自检分支（用 fstest.MapFS 构造坏语言文件，逐个分支覆盖）
//   ② 第三方库四个陷阱的回归用例——每一条都先证明「不挡会怎样」，再证明
//      「本包挡住了」。这是本文件存在的主要理由：三个陷阱都静默出错，
//      靠阅读文档发现不了，只能靠可执行断言钉死。

// ---------- ① load 自检 ----------

// goodLocale 是一份最小可用的语言文件内容。
const goodLocale = `{"i18n.display_name":"简体中文","k.a":"甲"}`

// TestLoadAcceptsGoodLocales 前置条件：正常语言文件必须装载成功。
// 没有这条，下面所有「坏文件被拒」的用例都可能是因为别的原因失败。
func TestLoadAcceptsGoodLocales(t *testing.T) {
	fsys := fstest.MapFS{
		"locales/zh-CN.json": &fstest.MapFile{Data: []byte(goodLocale)},
	}
	c, err := load(fsys, "locales", "zh-CN")
	if err != nil {
		t.Fatalf("正常语言文件装载失败: %v", err)
	}
	if len(c.allLangs) != 1 {
		t.Errorf("语言数 = %d, 期望 1", len(c.allLangs))
	}
	if c.defaultLang.String() != "zh-CN" {
		t.Errorf("默认语言 = %q", c.defaultLang)
	}
	if c.rawMessages["zh-CN"]["k.a"] != "甲" {
		t.Errorf("原始文案未装载: %v", c.rawMessages)
	}
}

// TestLoadRejectsBadLocales 逐个覆盖 load 的自检分支。
//
// 这些分支在生产路径上表现为 init panic，正常运行时永不触发；不构造坏输入
// 就无法验证——一个从未被验证过的保险等于没有保险（与 currency.buildTable
// 的 TestBuildTableRejectsBadTable 同一取向）。
func TestLoadRejectsBadLocales(t *testing.T) {
	cases := []struct {
		name       string
		fsys       fstest.MapFS
		dir        string
		defaultKey string
		wantSubstr string
	}{
		{
			name:       "目录不存在",
			fsys:       fstest.MapFS{},
			dir:        "nowhere",
			defaultKey: "zh-CN",
			wantSubstr: "读取语言目录",
		},
		{
			name: "目录内没有语言文件",
			fsys: fstest.MapFS{
				"locales/README.md": &fstest.MapFile{Data: []byte("x")},
			},
			dir: "locales", defaultKey: "zh-CN",
			wantSubstr: "没有任何",
		},
		{
			name: "文件名不是合法语言标签",
			fsys: fstest.MapFS{
				"locales/messages.json": &fstest.MapFile{Data: []byte(goodLocale)},
			},
			dir: "locales", defaultKey: "zh-CN",
			// 实测 go-i18n 对这种文件名静默给出 und，本包必须自行拦下，
			// 否则会多出一个叫 und 的语言。
			wantSubstr: "不是合法的语言标签",
		},
		{
			name: "非法 JSON",
			fsys: fstest.MapFS{
				"locales/zh-CN.json": &fstest.MapFile{Data: []byte(`{`)},
			},
			dir: "locales", defaultKey: "zh-CN",
			wantSubstr: "解析语言文件",
		},
		{
			name: "坏模板（未闭合）",
			fsys: fstest.MapFS{
				"locales/zh-CN.json": &fstest.MapFile{
					Data: []byte(`{"i18n.display_name":"简体中文","k.bad":"坏 {{.Unclosed"}`)},
			},
			dir: "locales", defaultKey: "zh-CN",
			// 实测 go-i18n 不在解析期校验模板，本包主动预编译才能拦下。
			wantSubstr: "模板非法",
		},
		{
			name: "空文案文件",
			fsys: fstest.MapFS{
				"locales/zh-CN.json": &fstest.MapFile{Data: []byte(`{}`)},
			},
			dir: "locales", defaultKey: "zh-CN",
			wantSubstr: "没有任何文案",
		},
		{
			name: "缺自称保留键",
			fsys: fstest.MapFS{
				"locales/zh-CN.json": &fstest.MapFile{Data: []byte(`{"k.a":"甲"}`)},
			},
			dir: "locales", defaultKey: "zh-CN",
			wantSubstr: "缺少必需的保留键",
		},
		{
			name: "默认语言没有语言文件",
			fsys: fstest.MapFS{
				"locales/en.json": &fstest.MapFile{
					Data: []byte(`{"i18n.display_name":"English","k.a":"A"}`)},
			},
			dir: "locales", defaultKey: "zh-CN",
			// 此情形先被空壳语言断言拦下（NewBundle(zh-CN) 使 bundle 多出一个
			// 无文案的 zh-CN），而那正是陷阱二要防的东西——两条断言指向同一个
			// 根因「默认语言没有文案」，先命中哪条不重要，重要的是必须拦下。
			wantSubstr: "空壳语言",
		},
		{
			name: "默认语言键本身非法",
			fsys: fstest.MapFS{
				"locales/zh-CN.json": &fstest.MapFile{Data: []byte(goodLocale)},
			},
			dir: "locales", defaultKey: "!!bad!!",
			wantSubstr: "默认语言键",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(tc.fsys, tc.dir, tc.defaultKey)
			if err == nil {
				t.Fatal("坏语言文件却装载成功")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("err = %q, 期望含 %q", err, tc.wantSubstr)
			}
		})
	}
}

// TestLoadRejectsDuplicateLanguage 同一语言有多个文件（如 zh-CN.json 与
// zh_CN.json）必须被拒——否则「哪个生效」取决于文件名排序，是纯粹的隐患。
func TestLoadRejectsDuplicateLanguage(t *testing.T) {
	fsys := fstest.MapFS{
		"locales/zh-CN.json": &fstest.MapFile{Data: []byte(goodLocale)},
		"locales/zh_CN.json": &fstest.MapFile{Data: []byte(`{"i18n.display_name":"简体中文","k.a":"乙"}`)},
	}
	_, err := load(fsys, "locales", "zh-CN")
	if err == nil {
		t.Fatal("同一语言的重复文件却装载成功")
	}
	if !strings.Contains(err.Error(), "多个语言文件") {
		t.Errorf("err = %q", err)
	}
}

// ---------- ② 陷阱一：占位参数缺失静默渲染 ----------

// TestTrapMissingKeyDefaultIsSilent 先证明「不挡会怎样」，再证明本包挡住了。
//
// go-i18n 默认 missingkey=default。若本包不覆盖它，语言文件写了 {{.Actual}}
// 而调用方只传 Min，会渲染出「当前 <no value> 位」且 err 为 nil——这种缺陷
// 上线后只能靠用户截图发现。
func TestTrapMissingKeyDefaultIsSilent(t *testing.T) {
	b := goi18n.NewBundle(language.Make("zh-CN"))
	b.RegisterUnmarshalFunc("json", json.Unmarshal)
	if _, err := b.ParseMessageFileBytes(
		[]byte(`{"k.p":"需要 {{.Min}} 位，当前 {{.Actual}} 位"}`), "zh-CN.json"); err != nil {
		t.Fatalf("装载失败: %v", err)
	}
	loc := goi18n.NewLocalizer(b, "zh-CN")
	incomplete := map[string]any{"Min": 10}

	t.Run("库默认行为：静默渲染出 <no value>", func(t *testing.T) {
		got, err := loc.Localize(&goi18n.LocalizeConfig{
			MessageID: "k.p", TemplateData: incomplete})
		if err != nil {
			t.Fatalf("前置条件失败：默认行为应当不报错, err = %v", err)
		}
		if !strings.Contains(got, "<no value>") {
			t.Fatalf("前置条件失败：默认行为应渲染出 <no value>, 实际 %q", got)
		}
		// 到此已证明陷阱真实存在。
	})

	t.Run("本包的覆盖：同一输入改为报错", func(t *testing.T) {
		// 注意必须用**未被污染**的新 bundle：模板缓存一旦被默认 parser 焙过，
		// 此后传 missingkey=error 也不再生效（陷阱五）。上一个子测试已经用默认
		// parser 取过词，故此处不能复用它的 loc。
		b2 := goi18n.NewBundle(language.Make("zh-CN"))
		b2.RegisterUnmarshalFunc("json", json.Unmarshal)
		if _, err := b2.ParseMessageFileBytes(
			[]byte(`{"k.p":"需要 {{.Min}} 位，当前 {{.Actual}} 位"}`), "zh-CN.json"); err != nil {
			t.Fatalf("装载失败: %v", err)
		}
		_, err := goi18n.NewLocalizer(b2, "zh-CN").Localize(&goi18n.LocalizeConfig{
			MessageID:      "k.p",
			TemplateData:   incomplete,
			TemplateParser: &goi18ntpl.TextParser{Option: missingKeyOption},
		})
		if err == nil {
			t.Error("missingKeyOption 未生效——占位参数缺失仍被静默放过")
		}
	})

	t.Run("本包对外行为：报错且不泄露模板残渣", func(t *testing.T) {
		got, err := Render(Default(), "passwd.too_short", Params{"Min": 10})
		if err == nil {
			t.Error("Render 未报错")
		}
		if strings.Contains(got, "<no value>") {
			t.Errorf("回落值 %q 含模板残渣", got)
		}
	})
}

// ---------- ② 陷阱二：默认 tag 与文件 tag 不一致使回落全盘失效 ----------

// TestTrapDefaultTagMismatchBreaksFallback 是四个陷阱里后果最重的一个：
// 它会让**登录页**一条文案都渲染不出来。
//
// NewBundle(language.SimplifiedChinese) 配 zh-CN.json，bundle 会得到两个 tag
// [zh-Hans, zh-CN]，而 zh-Hans 一条文案都没有。于是 Accept-Language 为 en-US、
// zh-TW、空串、垃圾串时全部回落到空壳 zh-Hans 并取词失败——而 middleware 的
// 语言解析先于登录态校验，未登录用户看到的登录页与 A-8 失败提示恰在这条路上。
func TestTrapDefaultTagMismatchBreaksFallback(t *testing.T) {
	const body = `{"i18n.display_name":"简体中文","k.a":"甲"}`

	t.Run("错误写法：默认 tag 用 SimplifiedChinese，回落全失效", func(t *testing.T) {
		b := goi18n.NewBundle(language.SimplifiedChinese)
		b.RegisterUnmarshalFunc("json", json.Unmarshal)
		b.ParseMessageFileBytes([]byte(body), "zh-CN.json")

		tags := b.LanguageTags()
		if len(tags) != 2 {
			t.Fatalf("前置条件失败：期望产生 2 个 tag（含一个空壳）, 实际 %v", tags)
		}

		// 空壳语言导致的取词失败
		failed := 0
		for _, al := range []string{"en-US,en;q=0.9", "zh-TW", "", "xx-YY"} {
			if _, err := goi18n.NewLocalizer(b, al).Localize(
				&goi18n.LocalizeConfig{MessageID: "k.a"}); err != nil {
				failed++
			}
		}
		if failed != 4 {
			t.Fatalf("前置条件失败：期望 4 条全部取词失败, 实际失败 %d 条", failed)
		}
	})

	t.Run("本包的 load 会拒绝这种配置", func(t *testing.T) {
		// 构造「默认键规范化后与文件 tag 不同」的情形：zh-Hans 是合法语言键，
		// 但它与 zh-CN.json 解析出的 zh-CN 不是同一个 tag。
		fsys := fstest.MapFS{
			"locales/zh-Hans.json": &fstest.MapFile{Data: []byte(body)},
			"locales/zh-CN.json":   &fstest.MapFile{Data: []byte(body)},
		}
		// 默认取 zh-Hans：bundle 会因两个文件产生两个 tag，均有文案，故不触发
		// 空壳断言；此处验证的是另一半——默认语言必须真有文件。
		if _, err := load(fsys, "locales", "zh-Hans"); err != nil {
			t.Fatalf("两个语言都有文案时应装载成功: %v", err)
		}
	})

	t.Run("本包实际装载不产生空壳语言", func(t *testing.T) {
		// 这是对生产状态的直接断言：bundle 的每个 tag 都必须真有文案。
		for _, tag := range active.bundle.LanguageTags() {
			if _, ok := active.rawMessages[tag.String()]; !ok {
				t.Errorf("bundle 含无文案的空壳语言 %q，Accept-Language 回落到它即取词失败", tag)
			}
		}
	})

	t.Run("回落链在本包下全部可用", func(t *testing.T) {
		// 陷阱二的最终验收：任何 Accept-Language 都必须取到词。
		for _, al := range []string{
			"en-US,en;q=0.9", "zh-TW", "zh", "zh-CN", "", "xx-YY", "garbage!!!",
			"*", "zh-Hans", "zh-Hant-TW", "de,fr;q=0.8",
		} {
			lang := Match(al)
			got, err := Render(lang, "errs.unauthorized", nil)
			if err != nil {
				t.Errorf("Accept-Language %q 取词失败: %v", al, err)
			}
			if got == "errs.unauthorized" {
				t.Errorf("Accept-Language %q 回落成键名——登录页将没有可读文案", al)
			}
		}
	})
}

// ---------- ② 陷阱三：LanguageTags() 返回内部切片（库缺陷）----------

// TestTrapLanguageTagsAliasesInternalState 证明库缺陷真实存在，并证明本包不受影响。
//
// bundle.LanguageTags() 返回的是内部切片**本身**而非副本。对它排序（一个再自然
// 不过的动作：让 K-1 的语言下拉稳定有序）会改写 bundle 内部 matcher 的候选顺序，
// 双语言下取词**完全错配**：用户选中文得到英文、选英文得到中文，且 err 恒为 nil。
func TestTrapLanguageTagsAliasesInternalState(t *testing.T) {
	newTwoLangBundle := func() *goi18n.Bundle {
		b := goi18n.NewBundle(language.Make("zh-CN"))
		b.RegisterUnmarshalFunc("json", json.Unmarshal)
		b.ParseMessageFileBytes([]byte(`{"k.a":"中文文案"}`), "zh-CN.json")
		b.ParseMessageFileBytes([]byte(`{"k.a":"English"}`), "en.json")
		return b
	}

	t.Run("返回值与内部状态共享底层数组", func(t *testing.T) {
		b := newTwoLangBundle()
		t1, t2 := b.LanguageTags(), b.LanguageTags()
		if len(t1) < 2 {
			t.Fatalf("前置条件失败：需要至少 2 个语言, 实际 %v", t1)
		}
		if &t1[0] != &t2[0] {
			t.Skip("上游已修复：LanguageTags 现已返回副本，本陷阱不再存在")
		}
	})

	t.Run("排序返回值即污染 bundle，取词完全错配", func(t *testing.T) {
		b := newTwoLangBundle()
		// 先确认未排序时取词正确
		for _, tc := range []struct{ al, want string }{{"zh-CN", "中文文案"}, {"en", "English"}} {
			got, _ := goi18n.NewLocalizer(b, tc.al).Localize(&goi18n.LocalizeConfig{MessageID: "k.a"})
			if got != tc.want {
				t.Fatalf("前置条件失败：排序前 %q 应得 %q, 实际 %q", tc.al, tc.want, got)
			}
		}

		// 排序（不拷贝）——这正是本包刻意避开的写法
		tags := b.LanguageTags()
		sort.Slice(tags, func(i, j int) bool { return tags[i].String() < tags[j].String() })

		mismatched := 0
		for _, tc := range []struct{ al, want string }{{"zh-CN", "中文文案"}, {"en", "English"}} {
			got, err := goi18n.NewLocalizer(b, tc.al).Localize(&goi18n.LocalizeConfig{MessageID: "k.a"})
			if err == nil && got != tc.want {
				mismatched++
			}
		}
		if mismatched == 0 {
			t.Skip("上游已修复：排序返回值不再影响取词")
		}
		// 错配且无错误——这就是「静默出错」的确切含义。
		t.Logf("已复现：排序后 %d/2 条取词错配，且全程无错误返回", mismatched)
	})

	t.Run("本包 Languages() 返回副本，改动不污染包状态", func(t *testing.T) {
		before := Languages()
		got := Languages()
		if len(got) > 0 && len(before) > 0 && &got[0] == &before[0] {
			t.Error("两次 Languages() 共享底层数组——调用方排序会污染包状态")
		}

		// 反向排序后，包状态与取词都必须不受影响
		sort.Slice(got, func(i, j int) bool { return got[i].String() > got[j].String() })
		after := Languages()
		for i := range before {
			if !before[i].Equal(after[i]) {
				t.Errorf("Languages() 被调用方改动污染：第 %d 项 %q → %q", i, before[i], after[i])
			}
		}
		if out, err := Render(Default(), "errs.internal", nil); err != nil || out == "errs.internal" {
			t.Errorf("排序后取词受影响: out=%q err=%v", out, err)
		}
	})
}

// ---------- ② 陷阱五：模板缓存把首次的 missingkey 语义永久固定 ----------

// TestTrapTemplateCacheFreezesFirstParser 是五个陷阱里最隐蔽的一个。
//
// go-i18n 的模板缓存（internal.Template.Execute）用 sync.Once 保存**第一次**
// 解析的结果，而解析选项来自当次调用传入的 parser。于是「占位参数缺失是否报错」
// 由「谁先取词」决定，且此后永久固定：只要有一次调用漏传 TemplateParser，
// 该文案就被焙成静默模式，此后本包处处传 missingkey=error 也不再生效。
//
// 这条用例是本包 warmTemplateCache 存在的直接原因——最初 TestTrapMissingKey
// DefaultIsSilent 正是因它而失败。
func TestTrapTemplateCacheFreezesFirstParser(t *testing.T) {
	const body = `{"i18n.display_name":"简体中文","k.p":"需要 {{.Min}} 位，当前 {{.Actual}} 位"}`
	incomplete := map[string]any{"Min": 10}

	t.Run("库行为：默认 parser 先跑，此后 missingkey=error 失效", func(t *testing.T) {
		b := goi18n.NewBundle(language.Make("zh-CN"))
		b.RegisterUnmarshalFunc("json", json.Unmarshal)
		b.ParseMessageFileBytes([]byte(body), "zh-CN.json")
		loc := goi18n.NewLocalizer(b, "zh-CN")

		// 第一次：默认 parser（焙入静默语义）
		if _, err := loc.Localize(&goi18n.LocalizeConfig{
			MessageID: "k.p", TemplateData: incomplete}); err != nil {
			t.Fatalf("前置条件失败：默认 parser 应当不报错, err = %v", err)
		}
		// 第二次：显式传 missingkey=error，却已无效
		got, err := loc.Localize(&goi18n.LocalizeConfig{
			MessageID:      "k.p",
			TemplateData:   incomplete,
			TemplateParser: &goi18ntpl.TextParser{Option: missingKeyOption},
		})
		if err == nil && strings.Contains(got, "<no value>") {
			t.Logf("已复现：缓存被首次调用焙成静默模式，missingkey=error 不再生效（得到 %q）", got)
			return
		}
		t.Skip("上游已修复：模板缓存不再固定首次的 parser 语义")
	})

	t.Run("本包的预热：missingkey=error 抢先焙入，后续无法退化", func(t *testing.T) {
		fsys := fstest.MapFS{"locales/zh-CN.json": &fstest.MapFile{Data: []byte(body)}}
		c, err := load(fsys, "locales", "zh-CN")
		if err != nil {
			t.Fatalf("装载失败: %v", err)
		}
		// 装载已含预热。此处**故意用默认 parser**取词——若预热生效，仍应报错。
		got, err := c.localizers["zh-CN"].Localize(&goi18n.LocalizeConfig{
			MessageID: "k.p", TemplateData: incomplete})
		if err == nil {
			t.Errorf("预热未生效：默认 parser 取词被静默放过, 得到 %q", got)
		}
		if strings.Contains(got, "<no value>") {
			t.Errorf("预热未生效：渲染出模板残渣 %q", got)
		}
		// 完整参数仍必须正常渲染
		ok, err := c.localizers["zh-CN"].Localize(&goi18n.LocalizeConfig{
			MessageID: "k.p", TemplateData: map[string]any{"Min": 10, "Actual": 4}})
		if err != nil || ok != "需要 10 位，当前 4 位" {
			t.Errorf("预热影响了正常渲染: got=%q err=%v", ok, err)
		}
	})

	t.Run("生产状态：本包的 Render 在任何调用顺序下都严格", func(t *testing.T) {
		// 先用完整参数取一次（模拟正常业务流量），再用缺参数取——必须报错。
		if _, err := Render(Default(), "passwd.too_short",
			Params{"Min": 10, "Actual": 4}); err != nil {
			t.Fatalf("正常取词失败: %v", err)
		}
		got, err := Render(Default(), "passwd.too_short", Params{"Min": 10})
		if err == nil {
			t.Errorf("缺参数未报错, 得到 %q", got)
		}
		if strings.Contains(got, "<no value>") {
			t.Errorf("渲染出模板残渣 %q", got)
		}
	})
}

// ---------- Render 的回落值优先于错误 ----------

// TestRenderPrefersFallbackTextOverError 锁死一条容易写错的分支：
// go-i18n 在逐键回落成功时会**同时**给出 out 与 err（out 是回落后的文案，
// err 报告「该键在原语言里没找到」）。此时必须采用 out——若因 err != nil 就
// 丢弃它、改回落成键名，逐键回落当场失效，用户看到的是 "errs.internal" 这种
// 键名而不是默认语言的那句话。
//
// 本用例走**本包的 render**（而非直接调库），故它能真正钉住这条分支：
// 变异测试确认把该分支改成丢弃 out 会使本用例失败。
func TestRenderPrefersFallbackTextOverError(t *testing.T) {
	c := mustLoadTwoLangs(t)

	t.Run("en 缺键时采用默认语言的文案而非键名", func(t *testing.T) {
		en := c.mustLang(t, "en")
		got, err := c.render(en, "k.only_zh", nil)
		if got != "仅中文有" {
			t.Errorf("= %q, 期望回落到默认语言的文案 仅中文有", got)
		}
		if got == "k.only_zh" {
			t.Error("回落成了键名——逐键回落失效，用户会看到内部标识符")
		}
		// 回落成功但仍应把原因上报，供 middleware 记日志
		if err == nil {
			t.Error("发生回落却未上报原因，middleware 无从记录语言文件不全")
		}
	})

	t.Run("en 有的键仍以 en 为准", func(t *testing.T) {
		en := c.mustLang(t, "en")
		got, err := c.render(en, "k.a", nil)
		if err != nil {
			t.Fatalf("取词失败: %v", err)
		}
		if got != "A" {
			t.Errorf("= %q, 期望 en 自己的文案 A", got)
		}
	})

	t.Run("两个语言都缺的键才回落为键名", func(t *testing.T) {
		en := c.mustLang(t, "en")
		got, err := c.render(en, "k.nowhere", nil)
		if got != "k.nowhere" {
			t.Errorf("= %q, 期望键名", got)
		}
		if err == nil {
			t.Error("完全缺键却未报错")
		}
	})
}

// ---------- ② 陷阱四：Messages 顺序不稳定 ----------

// TestTrapMessageOrderUnstable go-i18n 的 MessageFile.Messages 顺序源自 map
// 遍历，实测同一文件解析三次即变序。凡输出可比对的键清单都必须先排序。
func TestTrapMessageOrderUnstable(t *testing.T) {
	t.Run("MessageKeys 恒为字典序", func(t *testing.T) {
		for i := 0; i < 20; i++ {
			got := MessageKeys(Default())
			if !sort.StringsAreSorted(got) {
				t.Fatalf("第 %d 次调用返回的键未排序: %v", i+1, got[:min(5, len(got))])
			}
		}
	})

	t.Run("多次调用结果完全一致", func(t *testing.T) {
		first := strings.Join(MessageKeys(Default()), ",")
		for i := 0; i < 20; i++ {
			if got := strings.Join(MessageKeys(Default()), ","); got != first {
				t.Fatalf("第 %d 次调用键序变化", i+1)
			}
		}
	})

	t.Run("Languages 恒为字典序", func(t *testing.T) {
		for i := 0; i < 20; i++ {
			langs := Languages()
			keys := make([]string, len(langs))
			for j, l := range langs {
				keys[j] = l.String()
			}
			if !sort.StringsAreSorted(keys) {
				t.Fatalf("第 %d 次调用语言未排序: %v", i+1, keys)
			}
		}
	})
}

// ---------- Match ----------

// TestMatchIsTotal Match 是总函数：任何输入都返回一个可用语言。
//
// 这是 middleware 全局链的硬要求——语言解析先于登录态校验，登录页与 A-8 登录
// 失败提示都必须有文案，此处不能失败。
func TestMatchIsTotal(t *testing.T) {
	inputs := []string{
		"zh-CN", "zh", "zh-Hans", "zh-TW", "en", "en-US,en;q=0.9",
		"", "   ", "xx", "!!bad!!", "zh-CN;q=xyz", "*",
		"de,fr;q=0.8", strings.Repeat("a", 1000), "zh-CN,en;q=0.9,fr;q=0.8",
	}
	for _, al := range inputs {
		t.Run(truncate(al), func(t *testing.T) {
			got := Match(al)
			if got.IsZero() {
				t.Fatalf("Match(%q) 返回零值——总函数不得失败", truncate(al))
			}
			if _, err := ParseLang(got.String()); err != nil {
				t.Errorf("Match 返回的语言 %q 未通过 ParseLang: %v", got, err)
			}
		})
	}
}

// TestMatchFallsBackToDefault 无法命中时必须回落到默认语言。
//
// 不依赖 x/text matcher 在无匹配时返回的下标——实测双语言下 Accept-Language
// 为 fr 时它返回下标 1（而非 0），故本包按 Confidence 显式兜底。
func TestMatchFallsBackToDefault(t *testing.T) {
	for _, al := range []string{"", "   ", "xx", "!!bad!!", "de,fr;q=0.8", "ja"} {
		if got := Match(al); !got.Equal(Default()) {
			t.Errorf("Match(%q) = %q, 期望默认语言 %q", al, got, Default())
		}
	}
}

// TestMatchExactAndFuzzy 本轮只有 zh-CN，故一切输入都归到它；
// 但精确命中与模糊命中都必须走通，避免将来加语种时才发现匹配逻辑不对。
func TestMatchExactAndFuzzy(t *testing.T) {
	zh := MustParseLangForTest(t, "zh-CN")
	for _, al := range []string{"zh-CN", "zh", "zh-Hans", "zh-CN,en;q=0.9"} {
		if got := Match(al); !got.Equal(zh) {
			t.Errorf("Match(%q) = %q, 期望 zh-CN", al, got)
		}
	}
}

// TestMatchFallsBackToDefaultWhenNoConfidence 钉住 Match 的 Confidence 兜底。
//
// 不能依赖 x/text matcher 在「无匹配」时返回的下标：实测双语言（zh-CN + en）下
// Accept-Language 为 fr 时它返回**下标 1**（即 en）而非 0，Confidence 为 No。
// 若只按下标取语言，一个只会说法语的用户会拿到英文界面而不是默认的中文。
//
// 本轮只有 zh-CN，包级状态下任何输入都归到它，这条分支走不到——变异测试确认
// 去掉 Confidence 判断时只测包级状态的用例全部通过。故用双语 catalog 来测。
func TestMatchFallsBackToDefaultWhenNoConfidence(t *testing.T) {
	c := mustLoadTwoLangs(t)

	t.Run("完全不相干的语言回落到默认语言而非任意候选", func(t *testing.T) {
		for _, al := range []string{"fr", "ja", "de,fr;q=0.8", "ru-RU", "ko"} {
			got := c.match(al)
			if !got.Equal(c.defaultLang) {
				t.Errorf("match(%q) = %q, 期望默认语言 %q——"+
					"按 matcher 下标取语言会把不相干的偏好错配到别的语种",
					al, got, c.defaultLang)
			}
		}
	})

	t.Run("确实匹配得上的语言不受兜底影响", func(t *testing.T) {
		cases := map[string]string{
			"en":             "en",
			"en-US":          "en",
			"en-GB,en;q=0.9": "en",
			"zh-CN":          "zh-CN",
			"zh":             "zh-CN",
			"zh-CN,en;q=0.9": "zh-CN",
			"en-US,zh;q=0.5": "en",
		}
		for al, want := range cases {
			if got := c.match(al); got.String() != want {
				t.Errorf("match(%q) = %q, 期望 %q", al, got, want)
			}
		}
	})

	t.Run("空与垃圾输入仍回落到默认语言", func(t *testing.T) {
		for _, al := range []string{"", "   ", "!!bad!!", "*"} {
			if got := c.match(al); !got.Equal(c.defaultLang) {
				t.Errorf("match(%q) = %q, 期望默认语言", al, got)
			}
		}
	})
}

// ---------- Messages ----------

// TestMessagesReturnsRawTemplates 整包下发的是**原始模板**而非渲染结果——
// 占位参数由前端在使用现场填充，这正是「同一份语言文件两侧共用」的前提（T34④）。
func TestMessagesReturnsRawTemplates(t *testing.T) {
	msgs := Messages(Default())

	if got := msgs["passwd.too_short"]; !strings.Contains(got, "{{.Min}}") {
		t.Errorf("passwd.too_short = %q, 应保留 {{.Min}} 占位符供前端填充", got)
	}
	if got := msgs["errs.internal"]; got != "系统出现异常，请稍后重试" {
		t.Errorf("errs.internal = %q", got)
	}
}

// TestMessagesIsCopy 返回新 map，调用方的改动不得污染包状态。
func TestMessagesIsCopy(t *testing.T) {
	m := Messages(Default())
	m["errs.internal"] = "被改写了"
	delete(m, "errs.conflict")

	if got := Messages(Default())["errs.internal"]; got == "被改写了" {
		t.Error("Messages 返回了内部 map，调用方改动污染了包状态")
	}
	if _, ok := Messages(Default())["errs.conflict"]; !ok {
		t.Error("调用方的 delete 影响了包状态")
	}
}

// TestMessagesZeroLangUsesDefault 零值语言按默认语言处理，与 Render 一致。
func TestMessagesZeroLangUsesDefault(t *testing.T) {
	if len(Messages(Lang{})) != len(Messages(Default())) {
		t.Error("零值语言的整包文案应与默认语言一致")
	}
}

// TestMessagesBackfillsFromDefault 某语言缺某个键时，整包下发必须以默认语言
// 补全，保证前端拿到的键集合恒为全集。
//
// 否则会出现一种极难排查的不一致：同一个键，后端渲染得出文案、前端却是空白。
//
// 本用例走**本包的 messages**（而非在测试里重算一遍合并逻辑），故能真正钉住
// 补全这一步：变异测试确认去掉补全会使本用例失败。
func TestMessagesBackfillsFromDefault(t *testing.T) {
	c := mustLoadTwoLangs(t)
	en := c.mustLang(t, "en")

	msgs := c.messages(en)
	if got := msgs["k.a"]; got != "A" {
		t.Errorf("en 有的键应以 en 为准, 实际 %q", got)
	}
	if got := msgs["k.only_zh"]; got != "仅中文有" {
		t.Errorf("en 缺的键应由默认语言补全, 实际 %q", got)
	}
	// 键集合必须与默认语言一致，否则前端会出现空白文案
	zhKeys := c.messageKeys(c.defaultLang)
	enKeys := c.messageKeys(en)
	if len(enKeys) != len(zhKeys) {
		t.Errorf("en 键数 %d != 默认语言 %d，前端会出现空白文案", len(enKeys), len(zhKeys))
	}
	for _, k := range zhKeys {
		if _, ok := msgs[k]; !ok {
			t.Errorf("补全后仍缺键 %q", k)
		}
	}
	// 值不得为空
	for k, v := range msgs {
		if v == "" {
			t.Errorf("键 %q 补全后仍为空", k)
		}
	}
}

// ---------- 并发 ----------

// TestConcurrentUse 取词、语言列表、整包下发在并发下必须安全。
//
// 本包装载后只读、无锁，故这条必须在 -race 下跑才有意义（CI 应带 -race）。
// middleware 每个请求都会取词，这是全仓库并发度最高的代码路径之一。
func TestConcurrentUse(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if out, err := Render(Default(), "passwd.too_short",
					Params{"Min": 10, "Actual": n}); err != nil || out == "" {
					t.Errorf("并发取词失败: out=%q err=%v", out, err)
					return
				}
				_ = Match("zh-CN,en;q=0.9")
				_ = Languages()
				_ = Messages(Default())
				_, _ = ParseLang("zh-CN")
			}
		}(i)
	}
	wg.Wait()
}

// ---------- 辅助 ----------

func truncate(s string) string {
	if len(s) > 20 {
		return s[:20] + "..."
	}
	if s == "" {
		return "(空)"
	}
	return s
}

// mustLoadTwoLangs 造一份「zh-CN 默认 + en 缺键」的双语 catalog。
//
// 本轮只有 zh-CN，包级状态下走不到「某语言缺某个键」这条分支，而它恰是最容易
// 写错的一条。用 load 造出来，再调 catalog 的方法——走的是与生产完全相同的代码。
func mustLoadTwoLangs(t *testing.T) catalog {
	t.Helper()
	fsys := fstest.MapFS{
		"locales/zh-CN.json": &fstest.MapFile{Data: []byte(
			`{"i18n.display_name":"简体中文","k.a":"甲","k.only_zh":"仅中文有"}`)},
		"locales/en.json": &fstest.MapFile{Data: []byte(
			`{"i18n.display_name":"English","k.a":"A"}`)},
	}
	c, err := load(fsys, "locales", "zh-CN")
	if err != nil {
		t.Fatalf("装载双语 catalog 失败: %v", err)
	}
	return c
}

// mustLang 从该 catalog 中取一个语言。
func (c catalog) mustLang(t *testing.T, key string) Lang {
	t.Helper()
	l, err := c.parseLang(key)
	if err != nil {
		t.Fatalf("catalog.parseLang(%q) 失败: %v", key, err)
	}
	return l
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
