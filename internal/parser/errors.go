package parser

import (
	"errors"
	"unicode"

	"github.com/cellargalaxy/jotcash/internal/base/calendar"
	"github.com/cellargalaxy/jotcash/internal/base/decimal"
	"github.com/cellargalaxy/jotcash/internal/base/errs"
	"github.com/cellargalaxy/jotcash/internal/currency"
	"github.com/cellargalaxy/jotcash/internal/enum"
)

// D-3 的五个解析错误文案键 + 注册表的两个键。
//
// # 键的字面值是硬契约，不能改
//
// 五个解析键的字面值已被 base/errs 的 TestParserConsumerShape 钉死
// （errs_test.go 中列出 parser.format_mismatch 等五项），改动字面值会让那个
// 消费方走查用例失败。它们同时是 K-4 语言文件的键，**语言文件必须包含全部
// 7 个键**（与 rule/passwd.KeyGenerateFailed 同例）。
//
// # 为什么细粒度身份靠键而不靠错误码
//
// base/errs 包注释明写：「档位既是错误类型也是错误码，不再另设一根独立的数字
// 业务码轴——D-3 的五类解析错误靠键区分，不靠码」。因此五类**共用**
// KindInvalidInput 一档（都是「用户给的文件或选择有问题」，HTTP 400），
// 靠键区分身份。
//
// # 占位参数用具名 map，且各键的参数集合固定
//
// K-4 要求具名参数（语序是语言属性，位置参数一旦调序就得改 Go 代码）。
// 各键的参数在下面逐一列明；实现应当尽量填全，但**参数缺失不会导致渲染失败**
// ——i18n 那一轮负责兜底，本包不强制。
const (
	// KeyTypeMismatch 是「所选解析器类型与文件不匹配」的文案键（D-3 第五类）。
	//
	// 占位参数：ParserType（用户所选类型键）、ParserFormat（该类型适用的格式）、
	// FileFormat（实际上传的文件格式）。
	//
	// 这一类**在解析前就能拦下**（终版 D-3、entity.File 的 ParserType 注释）：
	// 判据是 enum.ParserType.Format() != entity.File.Format，见 CheckFormat。
	// 判定发生在 service/file，而不是走进某个 Parser 实现之后。
	KeyTypeMismatch = "parser.type_mismatch"

	// KeyDecryptFailed 是「解密失败」的文案键（D-3 第四类）。
	//
	// 占位参数：无。**刻意不带任何参数**——口令本身绝不能进错误
	// （见 Input.Password 的注释），而「密码错」与「加密文档未提供密码」对
	// 用户的动作相同（去填对密码），不必分成两个键。
	KeyDecryptFailed = "parser.decrypt_failed"

	// KeyFormatMismatch 是「格式不符」的文案键（D-3 第一类）。
	//
	// 占位参数：Reason（不符之处的简短说明，如「缺少表头」「列数不足」）。
	//
	// 判断层级是**文件版式**：表头、列数、分隔符不符合该解析器的预期。
	// 与「内容损坏」的分界见包注释的判断层级表。
	KeyFormatMismatch = "parser.format_mismatch"

	// KeyContentCorrupt 是「内容损坏」的文案键（D-3 第二类）。
	//
	// 占位参数：Line（行号，1 起）、Field（列名）、Value（读到的原始文本）。
	//
	// 判断层级是**单元格内容**：单元格**有值**但解释不出（金额写成 abc、
	// 日期写成 2026-02-30、币种写成 XXX）。「单元格是空的」不属于本类，
	// 属字段缺失。
	KeyContentCorrupt = "parser.content_corrupt"

	// KeyFieldMissing 是「字段缺失」的文案键（D-3 第三类）。
	//
	// 占位参数：Line（行号，1 起）、Field（列名）。
	//
	// 判断层级是**单元格存在性**：必填单元格为空，或该列在文件里根本不存在。
	// 不带 Value——本类的语义就是没有值。
	KeyFieldMissing = "parser.field_missing"

	// KeyTypeUnsupported 是「解析器类型键非法或未指定」的文案键。
	//
	// 占位参数：ParserType（用户传来的原始值）。
	//
	// 档位为 KindInvalidInput（400）：用户没选或选了个不存在的键，改得了。
	// 与 KeyNotRegistered 分属两档，理由见后者。
	//
	// **不在 D-3 的五类之内**——D-3 说的是「解析过程」的五类错误，而这一条
	// 发生在拿到解析器之前。单独出一个键而不复用 KeyTypeMismatch：后者的语义
	// 是「类型合法但与文件对不上」，提示用户换个解析器；本键的语义是「这个
	// 类型不存在」，提示用户重新选择。混用会让前端提示指向错误的操作。
	KeyTypeUnsupported = "parser.type_unsupported"

	// KeyNotRegistered 是「解析器类型键合法但没有对应实现」的文案键。
	//
	// 占位参数：ParserType。
	//
	// 档位为 KindInternal（500），与 KeyTypeUnsupported 的 400 **刻意分档**：
	// 本键意味着 app 装配时漏注册了一个 L4 子包（或某项枚举被声明了但实现
	// 还没写），用户改不了任何东西。合并成一档必有一方是错的——把它算成 400
	// 会让用户反复换选项去试一个根本不存在的实现，把 KeyTypeUnsupported 算成
	// 500 又会把用户的输入错误报成系统故障。
	//
	// 正常链路走不到这里：NewRegistry 的构造期自校验 + CheckEnabledCoverage
	// 已让漏注册在**启动时**暴露（见 registry.go）。它拦的是那条防线失效后的
	// 情形，属「本该不可能，发生了就必须响亮失败」。
	KeyNotRegistered = "parser.not_registered"
)

// NewTypeMismatchError 构造 D-3 第五类「所选解析器类型与文件不匹配」。
//
// 一般不直接调用，用 CheckFormat 更省事——它顺带把判据也统一了。
func NewTypeMismatchError(pt enum.ParserType, fileFormat enum.FileFormat) error {
	return errs.NewInvalidInput(KeyTypeMismatch, errs.Params{
		"ParserType":   pt.Code(),
		"ParserFormat": pt.Format().Code(),
		"FileFormat":   fileFormat.Code(),
	})
}

// CheckFormat 校验用户所选解析器类型与上传文件的格式是否匹配，即 D-3 第五类
// 错误的**唯一判据实现**。
//
// 判据取自 entity.File.ParserType 的注释与 enum.ParserMeta.Format 的注释：
// 「解析器声明自己吃哪种格式，service/file 据此在解析前就能拦下」。
//
// 校验顺序有意如此：
//  1. 先判类型键是否合法（含已停用）——非法键报 KeyTypeUnsupported。
//     必须先判，否则非法键的 Format() 返回零值，会被误报成「格式不匹配」，
//     而真正的问题是这个键根本不存在。
//  2. 再判文件格式是否合法——非法格式同样是用户输入问题。
//  3. 最后比对两个格式。
//
// **不校验「是否启用」**：那是 C-1 的业务规则（enum.ParserType.Enabled 的注释
// 明写「C-1 校验用户所选类型时应当用它而非 Valid」），归 service/file。本函数
// 只管格式匹配这一件事——历史文件用已停用的解析器重新解析时，格式判定照样要
// 能用。
func CheckFormat(pt enum.ParserType, fileFormat enum.FileFormat) error {
	if !pt.Valid() {
		return NewTypeUnsupportedError(pt.Code())
	}
	if !fileFormat.Valid() {
		//文件格式非法归到「类型不匹配」这一类：用户能看懂的表述就是「你选的
		//解析器和这个文件对不上」，而 FileFormat 非法本身没有独立的用户动作
		return NewTypeMismatchError(pt, fileFormat)
	}
	if pt.Format() != fileFormat {
		return NewTypeMismatchError(pt, fileFormat)
	}
	return nil
}

// NewDecryptFailedError 构造 D-3 第四类「解密失败」（口令错或未提供）。
//
// cause 可为 nil；非 nil 时作为错误链上的因保留，供 %+v 排查。
// **绝不把口令放进参数**，见 KeyDecryptFailed 与 Input.Password 的注释。
func NewDecryptFailedError(cause error) error {
	if cause == nil {
		return errs.NewInvalidInput(KeyDecryptFailed, nil)
	}
	return errs.Wrap(cause, errs.KindInvalidInput, KeyDecryptFailed, nil)
}

// NewFormatMismatchError 构造 D-3 第一类「格式不符」（文件版式层面）。
//
// reason 是给用户看的简短说明的**参数值**（如「缺少表头」），不是完整文案
// ——完整文案由 i18n 按 KeyFormatMismatch 渲染，reason 只作为占位参数填进去。
func NewFormatMismatchError(reason string) error {
	return errs.NewInvalidInput(KeyFormatMismatch, errs.Params{"Reason": reason})
}

// WrapFormatMismatchError 同 NewFormatMismatchError，但保留底层错误作为因。
//
// 供实现把第三方库的错误（如 CSV 词法错误）转成本包分类时使用：链上保留原始
// 错误供 %+v 排查，对外只暴露文案键。
func WrapFormatMismatchError(cause error, reason string) error {
	if cause == nil {
		return NewFormatMismatchError(reason)
	}
	return errs.Wrap(cause, errs.KindInvalidInput, KeyFormatMismatch,
		errs.Params{"Reason": reason})
}

// NewFieldMissingError 构造 D-3 第三类「字段缺失」（单元格存在性层面）。
//
// line 是行号，**1 起**且指账单文件里的物理行号（含表头行），这样用户在
// Excel / 文本编辑器里能直接跳到那一行。field 是列名，用账单上的原文列名而非
// Go 字段名——用户看的是账单。
func NewFieldMissingError(line int, field string) error {
	return errs.NewInvalidInput(KeyFieldMissing, errs.Params{
		"Line":  line,
		"Field": field,
	})
}

// NewContentCorruptError 构造 D-3 第二类「内容损坏」（单元格内容层面）。
//
// value 是读到的原始文本，带上它用户才知道是哪个值出的问题。
// cause 可为 nil；非 nil 时保留为链上的因——通常是 decimal.ErrSyntax、
// calendar.ErrDateFormat、currency.ErrUnknownCode 这类跨包哨兵。
func NewContentCorruptError(cause error, line int, field, value string) error {
	params := errs.Params{
		"Line":  line,
		"Field": field,
		"Value": value,
	}
	if cause == nil {
		return errs.NewInvalidInput(KeyContentCorrupt, params)
	}
	return errs.Wrap(cause, errs.KindInvalidInput, KeyContentCorrupt, params)
}

// NewTypeUnsupportedError 构造「解析器类型键非法或未指定」（400）。
//
// 收 string 而非 enum.ParserType：调用点常常拿到的就是一个来自请求体的裸串，
// 且非法值根本转不成合法枚举值（enum.ParseParserType 会报错），用 string
// 才能把用户实际传了什么原样报回去。
func NewTypeUnsupportedError(rawType string) error {
	return errs.NewInvalidInput(KeyTypeUnsupported, errs.Params{"ParserType": rawType})
}

// NewNotRegisteredError 构造「类型键合法但未注册实现」（500）。
//
// 档位与 NewTypeUnsupportedError 不同，理由见 KeyNotRegistered 的注释。
func NewNotRegisteredError(pt enum.ParserType) error {
	return errs.NewInternal(KeyNotRegistered, errs.Params{"ParserType": pt.Code()})
}

// ClassifyValueError 把「解析一个**有值**的单元格时拿到的底层错误」归入 D-3
// 的字段缺失或内容损坏两类之一，是各 L4 实现的公共分类入口。
//
// # 为什么需要它
//
// 三个下游包（decimal / calendar / currency）各自抛自己的哨兵错误，而 D-3 的
// 分类是按「判断层级」而非按来源划的（见包注释）。若让每个 L4 实现各写一遍
// switch，同一个 currency.ErrEmptyCode 迟早在 CSV 实现里被归成内容损坏、在
// PDF 实现里被归成字段缺失——同一个输入在不同银行的账单上得到不同类别的错误。
// 集中在此处一份，映射表就只有一处可改、也只有一处要测。
//
// # 映射表
//
//	来源哨兵                                            归类
//	currency.ErrEmptyCode                              字段缺失（值是空的）
//	calendar.ErrDateFormat / ErrMonthFormat（空串时）    字段缺失（见下）
//	decimal.ErrSyntax                                  内容损坏
//	decimal.ErrIntDigitsExceeded / ErrScaleExceeded     内容损坏
//	calendar.ErrDateFormat / ErrMonthFormat（非空串）    内容损坏
//	calendar.ErrDateValue / ErrMonthValue / ErrYearRange 内容损坏
//	currency.ErrUnknownCode / ErrNotBaseCandidate       内容损坏
//	其余（含 nil 之外的未知错误）                        内容损坏（兜底）
//
// 两条容易踩反的边界：
//
//   - **currency.ErrEmptyCode 归字段缺失**，而 ErrUnknownCode 归内容损坏。
//     前者是「币种列是空的」（补数据），后者是「币种列写着 XXX」（查文件），
//     用户要做的事不同。calendar 的消费方走查用例
//     （TestConsumerParserSentinelErrors）也是这个取向：把 ErrDateFormat 与
//     值类错误分成两档。
//   - **decimal.ErrIntDigitsExceeded 归内容损坏而非字段缺失**：数值超界说明
//     单元格里那串东西不是一个本系统能表达的金额，属内容问题。
//
// # 空串的处理
//
// value 为空串（或仅空白）时一律归**字段缺失**，无视 err 是什么：调用方把一个
// 空单元格交给 decimal.Parse 会拿到 ErrSyntax，而那实际上是「没填」而不是
// 「填错」。判据放在 value 上而不是靠区分哨兵，因为 calendar.ParseDate("")
// 与 ParseDate("2026/01/15") 抛的是**同一个** ErrDateFormat，光看错误分不出来。
//
// err 为 nil 时返回 nil：让调用点可以无条件地把结果交给它，不必先判一次。
func ClassifyValueError(err error, line int, field, value string) error {
	if err == nil {
		return nil
	}
	if isBlank(value) {
		return NewFieldMissingError(line, field)
	}
	if errors.Is(err, currency.ErrEmptyCode) {
		return NewFieldMissingError(line, field)
	}
	//其余一律内容损坏：单元格有值但解释不出。逐一列出已知哨兵不会让行为不同
	//（它们本就都落在这一类），但会在下游新增哨兵时给出一个假的「已覆盖」感，
	//故直接兜底，并由 errors_test.go 用真实哨兵值逐个钉住归类结果
	return NewContentCorruptError(err, line, field, value)
}

// ClassifyRequired 判定一个必填单元格是否缺失：value 为空串或仅空白时返回
// 字段缺失错误，否则返回 nil。
//
// 与 ClassifyValueError 分开而不合并成一个函数：本函数在**解析之前**用
// （「这列必填，先看有没有值」），前者在**解析之后**用（「解析报错了，归哪类」）。
// 合并成一个「既判空又判错」的函数会迫使调用方在还没解析时先造一个 nil error
// 传进去，读起来像是在处理一个不存在的错误。
//
// 本函数是**可选工具**，不是强制关卡——必填性由各实现自行决定（见包注释
// 「不做业务必填校验」），本函数只保证判空口径与错误形状一致。
func ClassifyRequired(line int, field, value string) error {
	if isBlank(value) {
		return NewFieldMissingError(line, field)
	}
	return nil
}

// isBlank 报告字符串是否为空或仅含空白字符，判据与
// strings.TrimSpace(s) == "" **完全等价**（TestIsBlankMatchesTrimSpace 逐码点
// 对照钉住这条等价性）。
//
// 用 unicode.IsSpace 逐 rune 判而不是 strings.TrimSpace(s) == ""：后者在需要
// 裁剪时会分配一个新串，而本函数在每行每列都要调一次。range 遍历字符串本身
// 不分配，unicode.IsSpace 对 Latin-1 范围内的码点走查表快路径，因此本实现
// 既无分配也不比逐字符比较慢。
//
// **不自己列举空白字符**：初版曾手列 ASCII 空白 + U+3000，漏掉了 U+1680、
// U+2000~U+200A、U+2028、U+2029、U+202F、U+205F 共 7 个码点（差分测试实测
// 报出）。这些不是理论上的边角——PDF 文本抽取极常产出 en/em space（U+2000
// 系列）与窄不换行空格（U+202F），而 parser/pdf 是已规划的 L4 实现。漏判的
// 后果是一个「看起来是空的」单元格被判为「有值但解释不出」，于是 D-3 的提示
// 从「该字段缺失，请补」变成「内容损坏，请查文件」——指向完全错误的用户动作。
func isBlank(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// 编译期断言：本包用到的跨包哨兵确实存在且是 error。
//
// 这些变量的唯一作用是让「下游包改名或删掉某个哨兵」在**编译时**失败，而不是
// 等到某条错误分类的分支被跑到才发现归类失效。它们不占运行时开销（编译器会
// 消除），也不导出。
//
// 只列 ClassifyValueError 的映射表里点名的哨兵；未点名的哨兵走兜底分支，
// 不需要断言。
var (
	_ error = decimal.ErrSyntax
	_ error = decimal.ErrIntDigitsExceeded
	_ error = decimal.ErrScaleExceeded
	_ error = calendar.ErrDateFormat
	_ error = calendar.ErrDateValue
	_ error = calendar.ErrMonthValue
	_ error = calendar.ErrYearRange
	_ error = currency.ErrEmptyCode
	_ error = currency.ErrUnknownCode
)
