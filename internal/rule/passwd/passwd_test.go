package passwd

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// TestCheckPolicy 是 A-8 策略校验的主表，按分层 §十二 准则 3 对 rule/* 的要求
// 「A-8 策略逐条覆盖」组织：**每一条策略、每个判据、每个类别组合各有用例**。
//
// 表的每行都写清「为什么这条必须是这个结果」——策略类用例最容易退化成一堆
// 无来由的字符串，日后收紧策略时无人知道哪条是刻意为之、哪条只是随手写的。
func TestCheckPolicy(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantOK  bool
		wantKey string
		reason  string
	}{
		//---- 空值 ----
		{
			name: "空串", input: "", wantOK: false, wantKey: KeyEmpty,
			reason: "空密码单独成档，不该让用户去猜是长度还是类别问题",
		},

		//---- 长度下限：MinLength=10 的三个相邻点必须都在表里 ----
		{
			name: "长度9_下限减一", input: "Abcdefgh1", wantOK: false, wantKey: KeyTooShort,
			reason: "9 位含字母+数字两类，仅长度差一位；这条钉住边界不是 <=",
		},
		{
			name: "长度10_恰好下限", input: "Abcdefgh12", wantOK: true,
			reason: "A-8 是「≥ 10」，恰好 10 位必须通过",
		},
		{
			name: "长度11_下限加一", input: "Abcdefgh123", wantOK: true,
			reason: "下限加一，与上一条一起夹住边界",
		},
		{
			name: "长度1", input: "a", wantOK: false, wantKey: KeyTooShort,
			reason: "极短输入不能因为「只有一类」就报类别错，长度检查在类别之前",
		},

		//---- 类别数：三类的两两组合与单类别，逐个覆盖 ----
		{
			name: "字母加数字", input: "abcde12345", wantOK: true,
			reason: "A-8「至少两类」的第一种组合，无需符号",
		},
		{
			name: "字母加符号", input: "abcde!@#$%", wantOK: true,
			reason: "第二种组合，无需数字",
		},
		{
			name: "数字加符号", input: "12345!@#$%", wantOK: true,
			reason: "第三种组合，无需字母——A-8 没要求必须含字母",
		},
		{
			name: "三类齐全", input: "Str0ng-Passw0rd!", wantOK: true,
			reason: "三类都有，是两类的超集",
		},
		{
			name: "纯字母", input: "abcdefghij", wantOK: false, wantKey: KeyTooFewCategories,
			reason: "长度够但只有一类，必须被类别检查拦下",
		},
		{
			name: "纯数字", input: "1234567890", wantOK: false, wantKey: KeyTooFewCategories,
			reason: "单类别，同上",
		},
		{
			name: "纯符号", input: "!@#$%^&*()", wantOK: false, wantKey: KeyTooFewCategories,
			reason: "单类别，同上；符号多也不等于强",
		},
		{
			name: "大小写混合仍算一类", input: "AbCdEfGhIj", wantOK: false, wantKey: KeyTooFewCategories,
			reason: "A-8 的三类里大小写同属「字母」，混大小写不构成第二类——这条防止把 IsUpper/IsLower 误当作独立类别",
		},

		//---- 符号判据：Punct 与 Symbol 两个 Unicode 大类都要算符号 ----
		{
			name: "下划线算符号_Pc", input: "abcdefghij_", wantOK: true,
			reason: "_ 是 Unicode Pc（IsPunct 为真），实测确认；只判 IsSymbol 会漏掉它",
		},
		{
			name: "连字符算符号_Pd", input: "abcdefghij-", wantOK: true,
			reason: "- 是 Pd（IsPunct 为真），常见于 passphrase",
		},
		{
			name: "加号算符号_Sm", input: "abcdefghij+", wantOK: true,
			reason: "+ 是 Sm（IsSymbol 为真，IsPunct 为假）；只判 IsPunct 会漏掉它",
		},
		{
			name: "货币符号算符号_Sc", input: "abcdefghij$", wantOK: true,
			reason: "$ 是 Sc（IsSymbol 为真）",
		},

		//---- 不计类别但也不禁止：这两条互为约束，缺一条判据就会写错 ----
		{
			name: "空白不算符号", input: "aaaaaaaaa ", wantOK: false, wantKey: KeyTooFewCategories,
			reason: "9 字母 + 1 空格实质是单类别弱口令；若把空白算作符号它就假装成了两类",
		},
		{
			name: "控制字符不算符号", input: "aaaaaaaaa\x00", wantOK: false, wantKey: KeyTooFewCategories,
			reason: "同上，NUL 实测 IsPunct/IsSymbol 均为假",
		},
		{
			name: "No类数字不算数字", input: "aaaaaaaaa½", wantOK: false, wantKey: KeyTooFewCategories,
			reason: "½ 实测 IsDigit 为假（它是 No 类），故不构成第二类；判据是 IsDigit 而非 IsNumber",
		},
		{
			name: "含空白的passphrase通过", input: "hello world1", wantOK: true,
			reason: "字母 10 + 数字 1 实实在在两类，含空格不构成拒绝理由——与「空白不算符号」一起钉住「不计类别 != 禁止出现」",
		},
		{
			name: "长度计入不计类别的字符", input: "abcd 1234 ", wantOK: true,
			reason: "两个空格计入长度（共 10 rune）但不计类别；字母+数字已两类",
		},

		//---- 非 ASCII：长度按 rune 计的直接后果 ----
		{
			name: "汉字加数字", input: "密码密码密码密码密码1", wantOK: true,
			reason: "11 个 rune、字母（汉字 IsLetter 为真）+ 数字两类",
		},
		{
			name: "四汉字_按字节够按rune不够", input: "汉字密码1", wantOK: false, wantKey: KeyTooShort,
			reason: "13 字节但只有 5 个 rune；这条钉死长度按 rune 计，按字节实现会误判通过",
		},
		{
			name: "阿拉伯印度数字单类别", input: "٠١٢٣٤٥٦٧٨٩", wantOK: false, wantKey: KeyTooFewCategories,
			reason: "实测 IsDigit 为真，10 个都是数字仍是单类别",
		},
		{
			name: "emoji单类别", input: "🙂🙂🙂🙂🙂🙂🙂🙂🙂🙂", wantOK: false, wantKey: KeyTooFewCategories,
			reason: "emoji 实测 IsSymbol 为真，10 个符号仍是单类别",
		},
		{
			name: "全角片假名加数字", input: "ｱｲｳｴｵｶｷｸｹ1", wantOK: true,
			reason: "全角片假名 IsLetter 为真，与数字构成两类",
		},

		//---- 非法 UTF-8：单独成档，且必须优先于长度与类别 ----
		{
			name: "非法字节_长度看似满足", input: "aaaaaaaaa\xff", wantOK: false, wantKey: KeyInvalidEncoding,
			reason: "实测 RuneCount 为 10 且坏字节 IsSymbol 为真，若不先判编码会误判「合规」并落库，导致账户永久登不进",
		},
		{
			name: "非法字节_多个", input: "aaaaaaaaa\xff\xfe\xfd", wantOK: false, wantKey: KeyInvalidEncoding,
			reason: "多个坏字节，同上",
		},
		{
			name: "非法字节_短输入也先报编码", input: "a\xff", wantOK: false, wantKey: KeyInvalidEncoding,
			reason: "编码检查必须在长度之前，否则真正的问题被长度错误盖掉",
		},
		{
			name: "非法字节_截断的多字节序列", input: "abcdefghij\xe4\xb8", wantOK: false, wantKey: KeyInvalidEncoding,
			reason: "「一」的 UTF-8 前两字节，典型的传输截断形态",
		},
		{
			name: "合法的替换字符字面量", input: "aaaaaaaaa\ufffd", wantOK: true,
			reason: "U+FFFD 字面量是合法 UTF-8（实测解码长度 3，与坏字节的 1 不同）且 IsSymbol 为真，构成第二类；本包只拒非法编码，不拒这个字符",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Check(c.input)

			if c.wantOK {
				if err != nil {
					t.Fatalf("Check(%q) 应通过，却返回 %v；理由：%s", c.input, err, c.reason)
				}
				return
			}

			if err == nil {
				t.Fatalf("Check(%q) 应失败，却通过了；理由：%s", c.input, c.reason)
			}
			if got := errs.MsgKeyOf(err); got != c.wantKey {
				t.Errorf("Check(%q) 文案键 = %q，期望 %q；理由：%s", c.input, got, c.wantKey, c.reason)
			}
			//A-8 校验失败恒为「用户输入错误」档，不该是 Internal——否则接入层会回 500
			if !errs.IsKind(err, errs.KindInvalidInput) {
				t.Errorf("Check(%q) 档位 = %v，期望 KindInvalidInput", c.input, errs.KindOf(err))
			}
		})
	}
}

// TestCheckErrorParams 断言每档错误的占位参数**恰好**是 i18n 渲染所需的那几个。
// 少一个占位符渲染出的是 %!s(MISSING)，多一个是无人使用的死字段。
func TestCheckErrorParams(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  errs.Params
	}{
		{name: "空", input: "", want: nil},
		{name: "非法编码", input: "\xff", want: nil},
		{
			name: "长度不足", input: "Abc1",
			want: errs.Params{"Min": MinLength, "Actual": 4},
		},
		{
			name: "长度不足_多字节按rune计数", input: "汉字密码1",
			want: errs.Params{"Min": MinLength, "Actual": 5},
		},
		{
			name: "类别不足_纯字母", input: "abcdefghij",
			want: errs.Params{"Min": MinCategories, "Actual": 1},
		},
		{
			name: "类别不足_空白不计类别", input: "aaaaaaaaa ",
			want: errs.Params{"Min": MinCategories, "Actual": 1},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Check(c.input)
			if err == nil {
				t.Fatalf("Check(%q) 应失败", c.input)
			}
			got := errs.ParamsOf(err)
			if len(got) != len(c.want) {
				t.Fatalf("占位参数 = %v（%d 项），期望 %v（%d 项）", got, len(got), c.want, len(c.want))
			}
			for k, wantV := range c.want {
				gotV, ok := got[k]
				if !ok {
					t.Errorf("缺少占位参数 %q，实际 = %v", k, got)
					continue
				}
				if gotV != wantV {
					t.Errorf("占位参数 %q = %v，期望 %v", k, gotV, wantV)
				}
			}
		})
	}
}

// TestNoPasswordInError 是密码红线的回归测试：**任何错误分支都不得把明文、
// 也不得把明文的任何片段带进错误里**。
//
// 这条不可省。errs 的 Error() 实测会把 Params 逐项打出来，而错误会同时流向
// 日志与前端（base/errs 包注释）——放进去等于两处同时泄露。用一个不会偶然
// 出现的哨兵明文，同时检查 Error() 文本、Params 的键与值。
func TestNoPasswordInError(t *testing.T) {
	//三个哨兵各自命中一个失败档（空串档没有入参可带，无需哨兵）。
	//注意类别档的哨兵必须**纯字母**：含数字的哨兵会构成两类而通过校验，
	//那条红线用例就成了空转——本用例第一版正是这么写错的，被下面的
	//「用例前提不成立」断言当场拦下，故这两条前提断言一并保留。
	cases := []struct {
		name    string
		input   string
		wantKey string
	}{
		{name: "长度不足", input: "Zq7Wv", wantKey: KeyTooShort},
		{name: "类别不足", input: "ZqWvXbKmPdRs", wantKey: KeyTooFewCategories},
		{name: "非法编码", input: "Zq7Wv3Xb9Km\xff", wantKey: KeyInvalidEncoding},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := c.input
			err := Check(input)
			if err == nil {
				t.Fatalf("Check(%q) 应失败，用例前提不成立", input)
			}
			if got := errs.MsgKeyOf(err); got != c.wantKey {
				t.Fatalf("Check(%q) 命中 %q 档而非预期的 %q 档，用例前提不成立",
					input, got, c.wantKey)
			}

			//1. Error() 文本不得含明文，也不得含明文的任何 4 字符以上片段
			text := err.Error()
			if strings.Contains(text, input) {
				t.Fatalf("错误文本含密码明文：%s", text)
			}
			for i := 0; i+4 <= len(input); i++ {
				if frag := input[i : i+4]; utf8.ValidString(frag) && strings.Contains(text, frag) {
					t.Fatalf("错误文本含密码片段 %q：%s", frag, text)
				}
			}

			//2. Params 的键与值都不得含明文片段
			for k, v := range errs.ParamsOf(err) {
				if s, ok := v.(string); ok {
					t.Errorf("占位参数 %q 的值是字符串 %q——本包只应放计数，字符串值有携带明文的风险", k, s)
				}
				if strings.Contains(k, input) {
					t.Errorf("占位参数的键含明文：%q", k)
				}
			}
		})
	}
}

// TestCheckDoesNotMutateOrRetain 断言 Check 是纯函数：不改入参（Go 的 string
// 不可变，此条实际验的是不存在「先规范化再校验」的隐藏行为），且同一输入
// 多次调用结果一致。
//
// 后半条是防「把校验结果缓存进包级 map」这类优化——那会让明文以 map 键的形式
// 驻留在内存里，违反包注释的「无任何包级可变状态」。
func TestCheckDoesNotMutateOrRetain(t *testing.T) {
	for _, input := range []string{"Abcdefgh12", "abcdefghij", "", "\xff\xfe"} {
		original := input
		first := Check(input)
		if input != original {
			t.Fatalf("入参被修改：%q -> %q", original, input)
		}
		for i := 0; i < 3; i++ {
			again := Check(input)
			if (first == nil) != (again == nil) {
				t.Fatalf("Check(%q) 多次调用结果不一致", input)
			}
			if first != nil && errs.MsgKeyOf(first) != errs.MsgKeyOf(again) {
				t.Fatalf("Check(%q) 多次调用文案键不一致：%q vs %q",
					input, errs.MsgKeyOf(first), errs.MsgKeyOf(again))
			}
		}
	}
}

// TestPolicyConstants 钉住两个阈值与终版 A-8 的字面一致。
//
// 看着像废话，作用是让「悄悄把下限从 10 调到 6」这种改动必须同时改测试，
// 从而在 code review 里现形（与 base/pwdhash 钉 Argon2 参数同一手法）。
func TestPolicyConstants(t *testing.T) {
	if MinLength != 10 {
		t.Errorf("MinLength = %d，终版 A-8 规定「长度 ≥ 10」", MinLength)
	}
	if MinCategories != 2 {
		t.Errorf("MinCategories = %d，终版 A-8 规定「至少含两类」", MinCategories)
	}
}

// TestMsgKeyNaming 断言本包所有文案键都遵 base/errs 的 <包>.<场景> 命名约定，
// 且互不重复。重复的键会让两种不同的失败渲染成同一句话。
func TestMsgKeyNaming(t *testing.T) {
	keys := []string{KeyEmpty, KeyInvalidEncoding, KeyTooShort, KeyTooFewCategories, KeyGenerateFailed}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if !strings.HasPrefix(k, "passwd.") {
			t.Errorf("文案键 %q 未以 \"passwd.\" 开头，违反 <包>.<场景> 约定", k)
		}
		if len(k) <= len("passwd.") {
			t.Errorf("文案键 %q 缺少场景段", k)
		}
		if seen[k] {
			t.Errorf("文案键 %q 重复", k)
		}
		seen[k] = true
	}
}
