package i18n

import (
	"errors"
	"strings"
	"testing"
)

// 本文件覆盖本包的三条对外行为：取词（Render/MustRender）、语言校验
// （ParseLang）、Lang 类型自身。第三方库的四个陷阱与 load 的自检分支在
// catalog_test.go，下游消费方走查在 consumer_check_test.go。

// ---------- Render ----------

// TestRenderPlain 取无占位参数的文案。
func TestRenderPlain(t *testing.T) {
	zh := MustParseLangForTest(t, "zh-CN")

	cases := []struct{ key, want string }{
		{"errs.invalid_input", "输入的内容有误，请检查后重试"},
		{"errs.conflict", "该名称已存在，请换一个"},
		{"errs.forbidden", "无权访问该数据"},
		{"errs.unauthorized", "登录状态已失效，请重新登录"},
		{"errs.internal", "系统出现异常，请稍后重试"},
		{"passwd.empty", "请输入密码"},
		{"currency.name.CNY", "人民币"},
		{"currency.name.KWD", "科威特第纳尔"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			got, err := Render(zh, tc.key, nil)
			if err != nil {
				t.Fatalf("Render(%q) 返回错误: %v", tc.key, err)
			}
			if got != tc.want {
				t.Errorf("Render(%q) = %q, 期望 %q", tc.key, got, tc.want)
			}
		})
	}
}

// TestRenderParams 填充具名占位参数。
//
// 占位参数**必须具名**：语序是语言属性，位置参数在跨语种调序时必须回改 Go
// 代码，K-4 当场失效（base/errs 包注释）。
func TestRenderParams(t *testing.T) {
	zh := MustParseLangForTest(t, "zh-CN")

	t.Run("两个占位参数都被填充", func(t *testing.T) {
		got, err := Render(zh, "passwd.too_short", Params{"Min": 10, "Actual": 4})
		if err != nil {
			t.Fatalf("返回错误: %v", err)
		}
		const want = "密码至少需要 10 位，当前 4 位"
		if got != want {
			t.Errorf("= %q, 期望 %q", got, want)
		}
	})

	t.Run("类别档同样两个参数", func(t *testing.T) {
		got, err := Render(zh, "passwd.too_few_categories", Params{"Min": 2, "Actual": 1})
		if err != nil {
			t.Fatalf("返回错误: %v", err)
		}
		if !strings.Contains(got, "2") || !strings.Contains(got, "1") {
			t.Errorf("= %q, 两个参数都应出现", got)
		}
	})

	t.Run("多传的参数被忽略", func(t *testing.T) {
		got, err := Render(zh, "passwd.too_short", Params{"Min": 10, "Actual": 4, "Extra": "x"})
		if err != nil {
			t.Fatalf("多余参数不应报错: %v", err)
		}
		if strings.Contains(got, "x") {
			t.Errorf("= %q, 多余参数不应出现在文案里", got)
		}
	})

	t.Run("无占位符的键传了参数也不报错", func(t *testing.T) {
		got, err := Render(zh, "errs.internal", Params{"X": 1})
		if err != nil {
			t.Fatalf("返回错误: %v", err)
		}
		if got != "系统出现异常，请稍后重试" {
			t.Errorf("= %q", got)
		}
	})
}

// TestRenderZeroLangUsesDefault 锁死「零值语言按默认语言处理」。
//
// 这不是便利功能而是硬需求：middleware 的语言解析先于登录态校验（分层 §三
// 问题 5），A-6 之前 ctxs 尚未写入，登录页与 A-8 登录失败提示拿到的正是零值。
// 若此处不兜底，登录页一条文案都渲染不出来，而那是未登录用户唯一能看到的页面。
func TestRenderZeroLangUsesDefault(t *testing.T) {
	got, err := Render(Lang{}, "errs.unauthorized", nil)
	if err != nil {
		t.Fatalf("零值语言取词失败: %v", err)
	}
	want := MustRender(Default(), "errs.unauthorized", nil)
	if got != want {
		t.Errorf("零值语言 = %q, 期望与默认语言一致 %q", got, want)
	}
	if got == "errs.unauthorized" {
		t.Error("零值语言回落成了键名，登录页将没有可读文案")
	}
}

// TestRenderMissingKeyFallsBackToKey 锁死缺键时的两条约定：
// 第一个返回值回落为键名（恒非空、可诊断），第二个返回值为 ErrMissingKey。
//
// 两者必须同时成立：middleware 需要第一个值塞进响应体（它对取词失败无法做任何
// 有意义的处置），又需要第二个值打进日志（本包不 import base/log，失败无处可记）。
func TestRenderMissingKeyFallsBackToKey(t *testing.T) {
	zh := MustParseLangForTest(t, "zh-CN")
	const key = "nope.not_exist"

	got, err := Render(zh, key, nil)
	if got != key {
		t.Errorf("缺键时 = %q, 期望回落为键名 %q", got, key)
	}
	if !errors.Is(err, ErrMissingKey) {
		t.Errorf("缺键时 err = %v, 期望 ErrMissingKey", err)
	}
}

// TestRenderMissingParamIsNotSilent 是本包最重要的一条用例。
//
// go-i18n 默认 missingkey=default，实测会把「语言文件写了 {{.Actual}} 而调用方
// 没传」渲染成「当前 <no value> 位」且 err 为 nil——这类缺陷一旦上线，只能靠
// 用户截图发现。本包一律以 missingkey=error 覆盖，此用例锁死该覆盖生效。
func TestRenderMissingParamIsNotSilent(t *testing.T) {
	zh := MustParseLangForTest(t, "zh-CN")

	cases := []struct {
		name   string
		params Params
	}{
		{"少传一个", Params{"Min": 10}},
		{"完全不传", nil},
		{"空 map", Params{}},
		{"传错名字", Params{"Min": 10, "Actual2": 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Render(zh, "passwd.too_short", tc.params)
			if err == nil {
				t.Fatalf("占位参数不全却未报错，渲染出 %q", got)
			}
			if !errors.Is(err, ErrRenderFailed) {
				t.Errorf("err = %v, 期望 ErrRenderFailed", err)
			}
			if strings.Contains(got, "<no value>") {
				t.Errorf("回落值 %q 含 <no value>，不应把模板残渣暴露给用户", got)
			}
			if got != "passwd.too_short" {
				t.Errorf("回落值 = %q, 期望键名", got)
			}
		})
	}
}

// TestRenderEmptyKey 空键取不到词，且回落值也是空串，故必须报错——
// 否则用户看到的是一个完全空白的提示。
func TestRenderEmptyKey(t *testing.T) {
	got, err := Render(Default(), "", nil)
	if !errors.Is(err, ErrMissingKey) {
		t.Errorf("err = %v, 期望 ErrMissingKey", err)
	}
	if got != "" {
		t.Errorf("= %q, 空键无可回落的键名", got)
	}
}

// TestRenderNoTemplateResidue 全量扫描：任何一条文案渲染后都不得残留模板语法。
//
// 这条用例的作用是在**加语种或改文案时**当场发现漏填的占位参数——语言文件是
// 手写的，而 {{.X}} 写错一个字母在中文语境下肉眼极难发现。
func TestRenderNoTemplateResidue(t *testing.T) {
	for _, lang := range Languages() {
		for _, key := range MessageKeys(lang) {
			tpl := Messages(lang)[key]
			// 带占位符的文案需要参数，此处只查不带占位符的那些能否干净渲染。
			if strings.Contains(tpl, "{{") {
				continue
			}
			got, err := Render(lang, key, nil)
			if err != nil {
				t.Errorf("[%s] 键 %q 渲染失败: %v", lang, key, err)
				continue
			}
			for _, residue := range []string{"{{", "}}", "<no value>"} {
				if strings.Contains(got, residue) {
					t.Errorf("[%s] 键 %q 渲染结果 %q 残留 %q", lang, key, got, residue)
				}
			}
		}
	}
}

// TestMustRender 只返回文案，失败时同样回落为键名而不 panic。
//
// 不 panic 是刻意的：本函数供「键与参数都已被测试锁死」的调用点使用，但即便
// 有人误用，让界面显示一个键名也远好过让整个请求崩掉。
func TestMustRender(t *testing.T) {
	zh := MustParseLangForTest(t, "zh-CN")

	if got := MustRender(zh, "errs.conflict", nil); got != "该名称已存在，请换一个" {
		t.Errorf("= %q", got)
	}
	if got := MustRender(zh, "nope.missing", nil); got != "nope.missing" {
		t.Errorf("缺键时 = %q, 期望键名", got)
	}
	// 参数不全时也不 panic
	if got := MustRender(zh, "passwd.too_short", nil); got != "passwd.too_short" {
		t.Errorf("参数不全时 = %q, 期望键名", got)
	}
}

// ---------- ParseLang ----------

// TestParseLangAccepts 已装载语言的语言键必须被接受，且大小写与分隔符按
// BCP 47 规范化——zh_CN 与 ZH-cn 指的就是同一个语言。
//
// 注意规范化**不含**补全或剥离 script 子标签：实测 language.Parse("zh-Hans-CN")
// 的 String() 仍是 "zh-Hans-CN" 而非 "zh-CN"，故它按「未提供该语种」被拒
// （见 TestParseLangRejects）。这与精确校验的取向一致。
func TestParseLangAccepts(t *testing.T) {
	cases := []struct{ input, want string }{
		{"zh-CN", "zh-CN"},
		{"zh_CN", "zh-CN"}, // 下划线分隔
		{"ZH-cn", "zh-CN"}, // 大小写
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseLang(tc.input)
			if err != nil {
				t.Fatalf("ParseLang(%q) 失败: %v", tc.input, err)
			}
			if got.String() != tc.want {
				t.Errorf("ParseLang(%q).String() = %q, 期望 %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestParseLangRejects 三档错误必须两两可分——它们对用户是三句不同的话：
// 没填 / 格式不对 / 不支持该语种。service/preference 的 K-1 据此给不同文案键。
func TestParseLangRejects(t *testing.T) {
	cases := []struct {
		input string
		want  error
		why   string
	}{
		{"", ErrEmptyLang, "没填"},
		{"!!bad!!", ErrMalformedLang, "格式非法"},
		{" zh-CN ", ErrMalformedLang, "带空白，不裁剪（与 language.Parse 口径一致）"},
		{"en", ErrUnknownLang, "格式合法但本轮未提供英文"},
		{"en-US", ErrUnknownLang, "同上"},
		{"zh", ErrUnknownLang, "泛中文不等于 zh-CN，精确校验不放宽"},
		{"zh-Hans", ErrUnknownLang, "script 子标签不被剥离，实测 String() 仍是 zh-Hans"},
		{"zh-Hans-CN", ErrUnknownLang, "同上，实测 String() 仍是 zh-Hans-CN"},
		{"zh-TW", ErrUnknownLang, "繁体未提供"},
		{"fr", ErrUnknownLang, "未提供"},
	}
	for _, tc := range cases {
		t.Run(tc.input+"/"+tc.why, func(t *testing.T) {
			got, err := ParseLang(tc.input)
			if !errors.Is(err, tc.want) {
				t.Errorf("ParseLang(%q) err = %v, 期望 %v", tc.input, err, tc.want)
			}
			if !got.IsZero() {
				t.Errorf("ParseLang(%q) 失败时应返回零值, 实际 %q", tc.input, got)
			}
		})
	}
}

// TestParseLangIsStricterThanMatch 锁死「精确校验」与「模糊匹配」的分工。
//
// 二者刻意分开：K-1 的持久化取值必须精确（校验放宽会让库里存进一个取不到词的
// 语言键，那是唯一能让整个界面变成键名的途径），而请求头是外部输入、应当宽容。
func TestParseLangIsStricterThanMatch(t *testing.T) {
	// zh-TW：Match 能命中 zh-CN，ParseLang 必须拒绝
	if _, err := ParseLang("zh-TW"); !errors.Is(err, ErrUnknownLang) {
		t.Errorf("ParseLang(zh-TW) 应拒绝, err = %v", err)
	}
	if got := Match("zh-TW"); got.IsZero() {
		t.Error("Match(zh-TW) 应返回一个可用语言")
	}
}

// TestParseLangRoundTripsWithEntityField 走查 entity.User.Language 的往返。
//
// entity/user.go:114 定义该字段为 string（因 entity 在 L1.1、i18n 在 L3，
// import 会成环），故 Lang 与 string 之间必须能无损往返，否则 K-1 存进去的值
// 下次读出来就校验不过了。
func TestParseLangRoundTripsWithEntityField(t *testing.T) {
	for _, lang := range Languages() {
		stored := lang.String() // 落库
		got, err := ParseLang(stored)
		if err != nil {
			t.Errorf("%q 回读失败: %v", stored, err)
			continue
		}
		if !got.Equal(lang) {
			t.Errorf("往返后 %q != %q", got, lang)
		}
	}
}

// ---------- Lang 类型 ----------

// TestLangZeroValue 零值可用、不 panic——上层不应被迫先判空。
func TestLangZeroValue(t *testing.T) {
	var l Lang
	if !l.IsZero() {
		t.Error("零值 IsZero 应为 true")
	}
	if l.String() != "" {
		t.Errorf("零值 String() = %q, 期望空串（与 entity.User.Language 的 string 零值对应）", l.String())
	}
	if l.DisplayName() != "" {
		t.Errorf("零值 DisplayName() = %q, 期望空串", l.DisplayName())
	}
	if !l.Equal(Lang{}) {
		t.Error("两个零值应相等")
	}
	if l.Equal(Default()) {
		t.Error("零值不应等于默认语言——「未指定」与「指定为中文」是两件事")
	}
}

// TestLangComparableAndMapKey Lang 须可比较、可作 map 键（同一语言共享同一
// 条目指针），否则 L6 无法用它做按语言分组的缓存。
func TestLangComparableAndMapKey(t *testing.T) {
	a := MustParseLangForTest(t, "zh-CN")
	b := MustParseLangForTest(t, "zh_CN") // 规范化后应是同一个语言
	if a != b {
		t.Error("同一语言的两次解析应得到相等的 Lang")
	}
	m := map[Lang]int{a: 1}
	m[b]++
	if len(m) != 1 || m[a] != 2 {
		t.Errorf("同一语言应命中同一个 map 键, m = %v", m)
	}
}

// TestLangDisplayNameIsSelfName 语言自称必须存在且不等于语言键。
//
// 用自称而非按当前界面语言翻译，是唯一自洽的做法：用户之所以要切语言，往往
// 正因为当前界面的语言他读不懂——把「英语」这一项写成中文，看不懂中文的用户
// 就找不到它。
func TestLangDisplayNameIsSelfName(t *testing.T) {
	for _, lang := range Languages() {
		if lang.DisplayName() == "" {
			t.Errorf("语言 %q 没有自称，K-1 下拉将没有标签", lang)
		}
		if lang.DisplayName() == lang.String() {
			t.Errorf("语言 %q 的自称与语言键相同，下拉里会显示 %q 而非人类可读的名称",
				lang, lang.String())
		}
	}
	if got := Default().DisplayName(); got != "简体中文" {
		t.Errorf("默认语言自称 = %q, 期望 简体中文", got)
	}
}

// ---------- 测试辅助 ----------

// MustParseLangForTest 解析语言键，失败即 t.Fatal。
//
// 命名带 ForTest 后缀并置于 _test.go 内，故不进生产导出面
// （consumer_check_test.go 的 TestExportedSurface 会核对这一点）。
func MustParseLangForTest(t *testing.T, key string) Lang {
	t.Helper()
	l, err := ParseLang(key)
	if err != nil {
		t.Fatalf("ParseLang(%q) 失败: %v", key, err)
	}
	return l
}
