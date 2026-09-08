package parser

import (
	"errors"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// TestMsgKeyLiterals 钉死 7 个文案键的**字面值**。
//
// 五个解析键的字面值是硬契约：base/errs 的 TestParserConsumerShape 已在那一侧
// 列出同样的字面串。两侧同时钉住，任何一侧改动都会立刻失败——这正是想要的，
// 因为它们同时是 K-4 语言文件的键，改了就等于把线上的提示文案变成缺词。
func TestMsgKeyLiterals(t *testing.T) {
	t.Parallel()

	//D-3 五类，字面值与 base/errs 的消费方走查逐字一致
	cases := map[string]string{
		"KeyFormatMismatch":  "parser.format_mismatch",
		"KeyContentCorrupt":  "parser.content_corrupt",
		"KeyFieldMissing":    "parser.field_missing",
		"KeyDecryptFailed":   "parser.decrypt_failed",
		"KeyTypeMismatch":    "parser.type_mismatch",
		"KeyTypeUnsupported": "parser.type_unsupported",
		"KeyNotRegistered":   "parser.not_registered",
	}
	got := map[string]string{
		"KeyFormatMismatch":  KeyFormatMismatch,
		"KeyContentCorrupt":  KeyContentCorrupt,
		"KeyFieldMissing":    KeyFieldMissing,
		"KeyDecryptFailed":   KeyDecryptFailed,
		"KeyTypeMismatch":    KeyTypeMismatch,
		"KeyTypeUnsupported": KeyTypeUnsupported,
		"KeyNotRegistered":   KeyNotRegistered,
	}
	for name, want := range cases {
		if got[name] != want {
			t.Errorf("%s 应为 %q，实际 %q", name, want, got[name])
		}
	}

	//键必须两两不同：i18n 靠键区分身份，重键等于两类错误共用一条文案
	seen := make(map[string]string, len(got))
	for name, key := range got {
		if prev, dup := seen[key]; dup {
			t.Errorf("文案键 %q 被 %s 与 %s 重复使用", key, prev, name)
		}
		seen[key] = name
	}

	//全部以 parser. 开头：约定 5 的命名规则「<包>.<场景>」
	for name, key := range got {
		if !strings.HasPrefix(key, "parser.") {
			t.Errorf("%s = %q 应以 \"parser.\" 开头（约定 5）", name, key)
		}
	}
}

// TestFiveClassesShareInvalidInputKind 走查 D-3 五类**共用** KindInvalidInput
// 一档、靠键区分身份（base/errs 包注释：细粒度身份由文案键承担，不靠码）。
//
// 同时钉住五类的 HTTP 落点一致（400）：五类都是「用户给的文件或选择有问题」。
func TestFiveClassesShareInvalidInputKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		key  string
	}{
		{"格式不符", NewFormatMismatchError("缺少表头"), KeyFormatMismatch},
		{"内容损坏", NewContentCorruptError(nil, 12, "金额", "abc"), KeyContentCorrupt},
		{"字段缺失", NewFieldMissingError(12, "金额"), KeyFieldMissing},
		{"解密失败", NewDecryptFailedError(nil), KeyDecryptFailed},
		{"类型不匹配", NewTypeMismatchError(enum.ParserTypeGenericCSV, enum.FileFormatPDF), KeyTypeMismatch},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if !errs.IsKind(c.err, errs.KindInvalidInput) {
				t.Errorf("应为用户输入错误档，实际 %v", errs.KindOf(c.err))
			}
			if got := errs.MsgKeyOf(c.err); got != c.key {
				t.Errorf("文案键应为 %q，实际 %q", c.key, got)
			}
			//不得同时落进系统错误档：那会让 middleware 报成 500
			if errs.IsKind(c.err, errs.KindInternal) {
				t.Error("解析错误不应落进系统错误档")
			}
		})
	}
}

// TestRegistryErrorsSplitKinds 走查取舍 8：「类型键非法」与「未注册实现」
// **分属两档**，一个 400 一个 500。
//
// 合并成一档必有一方是错的：400 会让用户反复换选项去试一个不存在的实现，
// 500 会把用户的输入错误报成系统故障。
func TestRegistryErrorsSplitKinds(t *testing.T) {
	t.Parallel()

	unsupported := NewTypeUnsupportedError("no_such_parser")
	if !errs.IsKind(unsupported, errs.KindInvalidInput) {
		t.Errorf("类型键非法应为用户输入错误档（400），实际 %v", errs.KindOf(unsupported))
	}
	if got := errs.MsgKeyOf(unsupported); got != KeyTypeUnsupported {
		t.Errorf("文案键应为 %q，实际 %q", KeyTypeUnsupported, got)
	}
	if got := errs.ParamsOf(unsupported)["ParserType"]; got != "no_such_parser" {
		t.Errorf("应原样带回用户传的值，实际 %v", got)
	}

	notRegistered := NewNotRegisteredError(enum.ParserTypeGenericCSV)
	if !errs.IsKind(notRegistered, errs.KindInternal) {
		t.Errorf("未注册实现应为系统错误档（500），实际 %v", errs.KindOf(notRegistered))
	}
	if got := errs.MsgKeyOf(notRegistered); got != KeyNotRegistered {
		t.Errorf("文案键应为 %q，实际 %q", KeyNotRegistered, got)
	}

	//两者档位必须不同，这是本用例的核心断言
	if errs.KindOf(unsupported) == errs.KindOf(notRegistered) {
		t.Error("「类型键非法」与「未注册实现」必须分属两档（取舍 8）")
	}
}

// TestErrorParamsCarryContext 走查各类错误的占位参数集合，即 D-3「给出明确
// 错误提示」的素材是否齐备。
//
// 重点在两条：字段缺失**不带** Value（本类语义就是没有值），解密失败
// **不带任何参数**（口令绝不能进错误）。
func TestErrorParamsCarryContext(t *testing.T) {
	t.Parallel()

	t.Run("字段缺失带行号与列名，不带值", func(t *testing.T) {
		t.Parallel()
		params := errs.ParamsOf(NewFieldMissingError(12, "金额"))
		if got := params["Line"]; got != 12 {
			t.Errorf("Line 应为 12，实际 %v", got)
		}
		if got := params["Field"]; got != "金额" {
			t.Errorf("Field 应为 金额，实际 %v", got)
		}
		if _, ok := params["Value"]; ok {
			t.Error("字段缺失不应带 Value：本类语义就是没有值")
		}
	})

	t.Run("内容损坏带行号列名与原始值", func(t *testing.T) {
		t.Parallel()
		params := errs.ParamsOf(NewContentCorruptError(decimal.ErrSyntax, 7, "金额", "abc"))
		if got := params["Line"]; got != 7 {
			t.Errorf("Line 应为 7，实际 %v", got)
		}
		if got := params["Field"]; got != "金额" {
			t.Errorf("Field 应为 金额，实际 %v", got)
		}
		if got := params["Value"]; got != "abc" {
			t.Errorf("Value 应为 abc，实际 %v", got)
		}
	})

	t.Run("解密失败不带任何参数", func(t *testing.T) {
		t.Parallel()
		//以一个「像口令」的字符串作为底层错误内容，确认它不会被搬进参数
		params := errs.ParamsOf(NewDecryptFailedError(errors.New("bad password: s3cret")))
		if len(params) != 0 {
			t.Errorf("解密失败不应带占位参数（口令红线），实际 %v", params)
		}
	})

	t.Run("格式不符带原因", func(t *testing.T) {
		t.Parallel()
		params := errs.ParamsOf(NewFormatMismatchError("缺少表头"))
		if got := params["Reason"]; got != "缺少表头" {
			t.Errorf("Reason 应为 缺少表头，实际 %v", got)
		}
	})

	t.Run("类型不匹配带三项格式信息", func(t *testing.T) {
		t.Parallel()
		params := errs.ParamsOf(NewTypeMismatchError(enum.ParserTypeGenericCSV, enum.FileFormatPDF))
		if got := params["ParserType"]; got != enum.ParserTypeGenericCSV.Code() {
			t.Errorf("ParserType 应为 %q，实际 %v", enum.ParserTypeGenericCSV.Code(), got)
		}
		if got := params["ParserFormat"]; got != enum.FileFormatCSV.Code() {
			t.Errorf("ParserFormat 应为 csv，实际 %v", got)
		}
		if got := params["FileFormat"]; got != enum.FileFormatPDF.Code() {
			t.Errorf("FileFormat 应为 pdf，实际 %v", got)
		}
	})
}

// TestDecryptFailedNeverLeaksPassword 单独盯住口令红线：解密失败的错误无论
// 怎么渲染，都不得出现口令。
//
// 检查三条路径：占位参数、Error() 文本、%+v 展开。第三条是最容易漏的——
// cause 会被 %+v 打出来，因此**调用方传给 NewDecryptFailedError 的 cause
// 本身不能含口令**。本用例以此提醒并锁住本包这一侧：本包不会主动把
// Input.Password 放进任何地方。
func TestDecryptFailedNeverLeaksPassword(t *testing.T) {
	t.Parallel()

	const password = "MyS3cretP@ss"

	//模拟一个实现：拿到 Input 后解密失败，按约定只报分类，不带口令
	in := Input{Content: []byte("encrypted"), Password: password}
	err := NewDecryptFailedError(errors.New("pdf: incorrect password"))

	if strings.Contains(err.Error(), password) {
		t.Error("Error() 不得包含口令")
	}
	for k, v := range errs.ParamsOf(err) {
		if s, ok := v.(string); ok && strings.Contains(s, password) {
			t.Errorf("占位参数 %s 不得包含口令", k)
		}
	}
	//确认口令确实在入参里（否则本用例是空跑）
	if in.Password != password {
		t.Fatal("用例自身有误：入参未携带口令")
	}
}

// TestCheckFormat 穷举 D-3 第五类的判据（enum.ParserType.Format() 与
// entity.File.Format 比对），含三条边界：非法类型键、非法文件格式、以及
// 「已停用但键合法」应放行。
func TestCheckFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		pt         enum.ParserType
		fileFormat enum.FileFormat
		wantKey    string
	}{
		{"匹配则放行", enum.ParserTypeGenericCSV, enum.FileFormatCSV, ""},
		{"CSV 解析器收到 PDF", enum.ParserTypeGenericCSV, enum.FileFormatPDF, KeyTypeMismatch},
		{"CSV 解析器收到 Excel", enum.ParserTypeGenericCSV, enum.FileFormatExcel, KeyTypeMismatch},
		{"类型键为零值", enum.ParserType(""), enum.FileFormatCSV, KeyTypeUnsupported},
		{"类型键不存在", enum.ParserType("no_such"), enum.FileFormatCSV, KeyTypeUnsupported},
		{"文件格式为零值", enum.ParserTypeGenericCSV, enum.FileFormat(""), KeyTypeMismatch},
		{"文件格式不存在", enum.ParserTypeGenericCSV, enum.FileFormat("docx"), KeyTypeMismatch},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := CheckFormat(c.pt, c.fileFormat)
			if c.wantKey == "" {
				if err != nil {
					t.Fatalf("应放行，实际报错 %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("应报错 %q，实际放行", c.wantKey)
			}
			if got := errs.MsgKeyOf(err); got != c.wantKey {
				t.Errorf("文案键应为 %q，实际 %q", c.wantKey, got)
			}
		})
	}
}

// TestCheckFormatOrdersChecks 钉住 CheckFormat 的校验顺序：**先判类型键合法性**。
//
// 顺序写反的症状很隐蔽：非法键的 Format() 返回零值，与任何合法文件格式都不等，
// 于是会被报成「格式不匹配」——提示用户「换个解析器」，而真正的问题是这个键
// 根本不存在。本用例以「非法键 + 非法格式」同时出现的输入区分两种顺序。
func TestCheckFormatOrdersChecks(t *testing.T) {
	t.Parallel()

	err := CheckFormat(enum.ParserType("no_such"), enum.FileFormat("no_such"))
	if err == nil {
		t.Fatal("应报错")
	}
	if got := errs.MsgKeyOf(err); got != KeyTypeUnsupported {
		t.Errorf("类型键非法应优先报 %q，实际 %q（校验顺序写反了）", KeyTypeUnsupported, got)
	}
}

// TestCheckFormatAllowsDisabledType 走查「已停用但键合法」必须放行格式校验。
//
// 依据 enum.ParserType.Enabled 的注释：Valid 对已停用的历史键返回 true，
// 「停用项不得被新选择」是 C-1 的业务规则、归 service/file。格式校验若也按
// Enabled 卡，用停用解析器上传的历史文件就再也无法重新解析。
//
// 当前枚举表里没有停用项（generic_csv 是唯一项且启用），故本用例以「表里
// 是否存在停用项」为条件跑：有则断言放行，没有则显式跳过并说明——这样将来
// 真出现停用项时，用例会自动开始生效。
func TestCheckFormatAllowsDisabledType(t *testing.T) {
	t.Parallel()

	var disabled enum.ParserType
	for _, pt := range enum.ParserTypes() {
		if !pt.Enabled() {
			disabled = pt
			break
		}
	}
	if disabled.IsZero() {
		t.Skip("当前枚举表无停用项，本用例待出现停用项后自动生效")
	}
	if err := CheckFormat(disabled, disabled.Format()); err != nil {
		t.Errorf("已停用但键合法应放行格式校验，实际 %v", err)
	}
}

// TestClassifyValueErrorMapping 用**真实的跨包哨兵错误值**驱动分类映射表，
// 逐个钉住归类结果。
//
// 关键在于「真实值驱动」：如果这里用手写的 errors.New("syntax") 假错误，
// 那么下游 base/decimal 改名或删掉 ErrSyntax 时本用例照样通过，而生产代码里
// 的 errors.Is 已经失效——分类会静默退化到兜底分支。
func TestClassifyValueErrorMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		err     error
		value   string
		wantKey string
	}{
		//字段缺失：值是空的
		{"币种为空", currency.ErrEmptyCode, "", KeyFieldMissing},
		{"币种为空但错误另类", errors.New("whatever"), "", KeyFieldMissing},
		{"仅空白视为缺失", decimal.ErrSyntax, "   ", KeyFieldMissing},
		{"全角空格视为缺失", decimal.ErrSyntax, "\u3000", KeyFieldMissing},
		{"制表与换行视为缺失", decimal.ErrSyntax, "\t\n", KeyFieldMissing},
		{"不换行空格视为缺失", decimal.ErrSyntax, "\u00a0", KeyFieldMissing},
		//以下两条是「值非空但归缺失」的路径，即 ErrEmptyCode 分支本身。
		//真实形态：实现把单元格里的货币符号或占位符剥掉后再交给 currency.Parse，
		//剥完成了空串（Parse 报 ErrEmptyCode），而报错时带的 value 是**剥之前**
		//的原文。此时该归「字段缺失」——单元格里没有可用的币种信息。
		{"仅货币符号视为缺失", currency.ErrEmptyCode, "¥", KeyFieldMissing},
		{"占位横线视为缺失", currency.ErrEmptyCode, "-", KeyFieldMissing},

		//内容损坏：有值但解释不出
		{"金额语法错", decimal.ErrSyntax, "abc", KeyContentCorrupt},
		{"金额整数位超界", decimal.ErrIntDigitsExceeded, "1e1000000", KeyContentCorrupt},
		{"金额小数位超界", decimal.ErrScaleExceeded, "0.1234567890123", KeyContentCorrupt},
		{"日期格式错", calendar.ErrDateFormat, "2026/01/15", KeyContentCorrupt},
		{"日期不存在", calendar.ErrDateValue, "2026-02-30", KeyContentCorrupt},
		{"月份越界", calendar.ErrMonthValue, "2026-13", KeyContentCorrupt},
		{"年份越界", calendar.ErrYearRange, "10000-01-01", KeyContentCorrupt},
		{"未知币种", currency.ErrUnknownCode, "XXX", KeyContentCorrupt},
		{"非本位币候选", currency.ErrNotBaseCandidate, "KWD", KeyContentCorrupt},
		{"未知错误兜底", errors.New("某个新哨兵"), "有值", KeyContentCorrupt},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := ClassifyValueError(c.err, 5, "某列", c.value)
			if err == nil {
				t.Fatalf("应报错 %q，实际返回 nil", c.wantKey)
			}
			if got := errs.MsgKeyOf(err); got != c.wantKey {
				t.Errorf("ClassifyValueError(%v, %q) 应归 %q，实际 %q",
					c.err, c.value, c.wantKey, got)
			}
			//行号与列名必须一路带到底，否则 D-3 的「明确提示」落不了地
			params := errs.ParamsOf(err)
			if params["Line"] != 5 {
				t.Errorf("Line 应为 5，实际 %v", params["Line"])
			}
			if params["Field"] != "某列" {
				t.Errorf("Field 应为 某列，实际 %v", params["Field"])
			}
		})
	}
}

// TestClassifyValueErrorNilPassesThrough 走查「err 为 nil 返回 nil」：
// 让调用点可以无条件把解析结果交给它，不必先判一次。
func TestClassifyValueErrorNilPassesThrough(t *testing.T) {
	t.Parallel()

	if err := ClassifyValueError(nil, 1, "金额", "100.00"); err != nil {
		t.Errorf("err 为 nil 时应返回 nil，实际 %v", err)
	}
	//即使值是空的，nil 错误也不该被转成字段缺失：判空是 ClassifyRequired 的职责，
	//本函数只在「解析已经失败」时分类
	if err := ClassifyValueError(nil, 1, "金额", ""); err != nil {
		t.Errorf("err 为 nil 时即使值为空也应返回 nil，实际 %v", err)
	}
}

// TestClassifyValueErrorPreservesCause 走查底层哨兵被保留在错误链上：
// %+v 排查时能看到根因，而对外只暴露文案键。
func TestClassifyValueErrorPreservesCause(t *testing.T) {
	t.Parallel()

	err := ClassifyValueError(decimal.ErrSyntax, 3, "金额", "abc")
	if !errors.Is(err, decimal.ErrSyntax) {
		t.Error("错误链上应保留 decimal.ErrSyntax 作为因")
	}
	//同时仍是本包的分类错误
	if got := errs.MsgKeyOf(err); got != KeyContentCorrupt {
		t.Errorf("文案键应为 %q，实际 %q", KeyContentCorrupt, got)
	}
}

// TestClassifyValueErrorDrivenByRealParsing 端到端回放最贴近实现的用法：
// 把真实的账单文本喂给 decimal / calendar / currency，再把它们真实抛出的错误
// 交给分类器。
//
// 这条比 TestClassifyValueErrorMapping 更强：那里的哨兵是直接引用的常量，
// 这里的错误是**下游包实际产出**的（可能是被包装过的），能验证 errors.Is
// 在真实错误链上仍然成立。
func TestClassifyValueErrorDrivenByRealParsing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		field   string
		raw     string
		parse   func(string) error
		wantKey string
	}{
		{"金额 abc", "金额", "abc", func(s string) error {
			_, err := decimal.Parse(s)
			return err
		}, KeyContentCorrupt},
		{"金额空", "金额", "", func(s string) error {
			_, err := decimal.Parse(s)
			return err
		}, KeyFieldMissing},
		{"日期斜杠分隔", "支出日期", "2026/01/15", func(s string) error {
			_, err := calendar.ParseDate(s)
			return err
		}, KeyContentCorrupt},
		{"日期空", "支出日期", "", func(s string) error {
			_, err := calendar.ParseDate(s)
			return err
		}, KeyFieldMissing},
		{"日期不存在", "支出日期", "2026-02-30", func(s string) error {
			_, err := calendar.ParseDate(s)
			return err
		}, KeyContentCorrupt},
		{"币种 XXX", "支出币种", "XXX", func(s string) error {
			_, err := currency.Parse(s)
			return err
		}, KeyContentCorrupt},
		{"币种空", "支出币种", "", func(s string) error {
			_, err := currency.Parse(s)
			return err
		}, KeyFieldMissing},
		//剥掉货币符号后成空串：Parse 报 ErrEmptyCode，而报错带的是剥之前的原文。
		//这是 ClassifyValueError 里 ErrEmptyCode 分支的真实触发形态
		{"剥掉符号后为空", "支出币种", "¥", func(s string) error {
			_, err := currency.Parse(strings.Trim(s, "¥$€£"))
			return err
		}, KeyFieldMissing},
		{"金额合法", "金额", "100.00", func(s string) error {
			_, err := decimal.Parse(s)
			return err
		}, ""},
		{"日期合法", "支出日期", "2026-01-15", func(s string) error {
			_, err := calendar.ParseDate(s)
			return err
		}, ""},
		{"币种合法", "支出币种", "CNY", func(s string) error {
			_, err := currency.Parse(s)
			return err
		}, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			classified := ClassifyValueError(c.parse(c.raw), 9, c.field, c.raw)
			if c.wantKey == "" {
				if classified != nil {
					t.Fatalf("合法值不应报错，实际 %v", classified)
				}
				return
			}
			if classified == nil {
				t.Fatalf("应报错 %q，实际 nil", c.wantKey)
			}
			if got := errs.MsgKeyOf(classified); got != c.wantKey {
				t.Errorf("%q 应归 %q，实际 %q", c.raw, c.wantKey, got)
			}
		})
	}
}

// TestClassifyRequired 走查解析前的必填判空，含空白的各种形态。
func TestClassifyRequired(t *testing.T) {
	t.Parallel()

	missing := []string{"", " ", "\t", "\n", "\r\n", "  \t ", "\u3000", "\u00a0", "\v", "\f", "\u0085"}
	for _, v := range missing {
		err := ClassifyRequired(4, "金额", v)
		if err == nil {
			t.Errorf("ClassifyRequired(%q) 应报字段缺失", v)
			continue
		}
		if got := errs.MsgKeyOf(err); got != KeyFieldMissing {
			t.Errorf("ClassifyRequired(%q) 文案键应为 %q，实际 %q", v, KeyFieldMissing, got)
		}
	}

	present := []string{"0", "100.00", "-", "a", " x ", "　x"}
	for _, v := range present {
		if err := ClassifyRequired(4, "金额", v); err != nil {
			t.Errorf("ClassifyRequired(%q) 应放行，实际 %v", v, err)
		}
	}
}

// TestIsBlankMatchesTrimSpace 用差分测试钉住 isBlank 与
// strings.TrimSpace(s) == "" 的**完全等价性**，逐码点对照，不容许任何差异。
//
// 这条用例在编写时就抓到了一个真实缺陷：初版 isBlank 手列空白字符，漏掉了
// U+1680、U+2000/U+2001、U+2028、U+2029、U+202F、U+205F 共 7 个码点。它们
// 并非理论边角——PDF 文本抽取极常产出 en/em space 与窄不换行空格，而漏判会
// 让一个空单元格被归成「内容损坏」而不是「字段缺失」，D-3 的提示因此指向
// 完全错误的用户动作。修法是改用 unicode.IsSpace（见 isBlank 的注释）。
//
// 保留差分形态而不是改成「断言那 7 个码点为真」：后者只覆盖已知的 7 个，
// 而差分覆盖标准库判定的全部空白，将来 Unicode 版本增补空白字符时同样有效。
func TestIsBlankMatchesTrimSpace(t *testing.T) {
	t.Parallel()

	//逐个码点对照：ASCII 全域 + Unicode 空白区段（含 U+2000~U+200A 全段）
	//+ 若干典型汉字与符号
	var probes []rune
	for r := rune(0); r <= 0x7f; r++ {
		probes = append(probes, r)
	}
	for r := rune(0x2000); r <= 0x200b; r++ {
		probes = append(probes, r)
	}
	probes = append(probes,
		0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f,
		0x205f, 0x3000, 0xfeff, '中', '金', '0', '-',
	)

	for _, r := range probes {
		s := string(r)
		want := strings.TrimSpace(s) == ""
		if got := isBlank(s); got != want {
			t.Errorf("isBlank(U+%04X) = %v，strings.TrimSpace 判定为 %v（判据必须完全等价）",
				r, got, want)
		}
	}

	//多字符组合：空白串、混合串
	for _, c := range []struct {
		s    string
		want bool
	}{
		{"", true},
		{" \t\n\r", true},
		{"\u3000\u00a0 ", true},
		{"\u2003\u202f", true},
		{" a ", false},
		{"\t0\n", false},
		{"金额", false},
		//U+FEFF（BOM / 零宽不换行空格）不是空白：它有值、不该被判成「没填」。
		//CSV 文件头常带 BOM，若判成空白，一个只含 BOM 的单元格会被报「字段缺失」
		//——而 TrimSpace 同样不认它为空白，两者在此一致
		{"\ufeff", false},
	} {
		if got := isBlank(c.s); got != c.want {
			t.Errorf("isBlank(%q) = %v，期望 %v", c.s, got, c.want)
		}
	}
}

// TestWrapFormatMismatchError 走查带因与不带因两条路径。
func TestWrapFormatMismatchError(t *testing.T) {
	t.Parallel()

	root := errors.New("csv: wrong number of fields")

	withCause := WrapFormatMismatchError(root, "列数不符")
	if !errors.Is(withCause, root) {
		t.Error("应保留底层错误作为因")
	}
	if got := errs.MsgKeyOf(withCause); got != KeyFormatMismatch {
		t.Errorf("文案键应为 %q，实际 %q", KeyFormatMismatch, got)
	}
	if got := errs.ParamsOf(withCause)["Reason"]; got != "列数不符" {
		t.Errorf("Reason 应为 列数不符，实际 %v", got)
	}

	//cause 为 nil 时退化为 NewFormatMismatchError，不得产出一个 cause 为 nil
	//的包装错误（那会让 %+v 打出一个空的因）
	withoutCause := WrapFormatMismatchError(nil, "缺少表头")
	if got := errs.MsgKeyOf(withoutCause); got != KeyFormatMismatch {
		t.Errorf("文案键应为 %q，实际 %q", KeyFormatMismatch, got)
	}
	if !errs.IsKind(withoutCause, errs.KindInvalidInput) {
		t.Error("应为用户输入错误档")
	}
}

// TestDecryptFailedWithAndWithoutCause 走查解密失败的两条构造路径。
func TestDecryptFailedWithAndWithoutCause(t *testing.T) {
	t.Parallel()

	root := errors.New("pdf: encrypted")

	withCause := NewDecryptFailedError(root)
	if !errors.Is(withCause, root) {
		t.Error("应保留底层错误作为因")
	}
	if got := errs.MsgKeyOf(withCause); got != KeyDecryptFailed {
		t.Errorf("文案键应为 %q，实际 %q", KeyDecryptFailed, got)
	}

	withoutCause := NewDecryptFailedError(nil)
	if got := errs.MsgKeyOf(withoutCause); got != KeyDecryptFailed {
		t.Errorf("文案键应为 %q，实际 %q", KeyDecryptFailed, got)
	}
	if !errs.IsKind(withoutCause, errs.KindInvalidInput) {
		t.Error("应为用户输入错误档")
	}
}

// TestContentCorruptWithNilCause 走查内容损坏在无因时也带齐三个参数。
func TestContentCorruptWithNilCause(t *testing.T) {
	t.Parallel()

	err := NewContentCorruptError(nil, 2, "对手方", "\x00\x01")
	if got := errs.MsgKeyOf(err); got != KeyContentCorrupt {
		t.Errorf("文案键应为 %q，实际 %q", KeyContentCorrupt, got)
	}
	params := errs.ParamsOf(err)
	for _, k := range []string{"Line", "Field", "Value"} {
		if _, ok := params[k]; !ok {
			t.Errorf("应带占位参数 %s", k)
		}
	}
}
