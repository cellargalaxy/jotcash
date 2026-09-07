package enum

import (
	"database/sql/driver"
	"fmt"
)

// FileFormat 是文件格式，承载 File.格式（终版 §四 4、C-2）。
//
// 恰三项：CSV / Excel / PDF，取自终版 C-2「格式（CSV / Excel / PDF）」的字面枚举。
//
// **格式不决定解析器**——终版 §四 4 明写「用于展示、预览与下载；不决定解析器」，
// D-1 的理由是「同为 PDF，各银行版式不同」。走哪个解析器由 ParserType 决定，
// 两者是独立的两列。本枚举参与的是 C-4 的下载 / 预览（据此定 MIME 与扩展名）。
type FileFormat string

const (
	// FileFormatCSV 逗号分隔文本。D-1「本轮仅实现 CSV」指的是解析器实现，
	// 与本枚举的取值范围无关——三个格式的文件都可上传留存。
	FileFormatCSV FileFormat = "csv"

	// FileFormatExcel Excel 工作簿。
	FileFormatExcel FileFormat = "excel"

	// FileFormatPDF PDF 文档。加密 PDF 的解密口令走 File.文件密码（D-2）。
	FileFormatPDF FileFormat = "pdf"
)

// fileFormatSet 是 FileFormat 的枚举表。
var fileFormatSet = newSet([]entry[FileFormat]{
	{FileFormatCSV, "CSV"},
	{FileFormatExcel, "Excel"},
	{FileFormatPDF, "PDF"},
})

// FileFormats 返回全部文件格式，保持声明序。
func FileFormats() []FileFormat { return fileFormatSet.all() }

// ParseFileFormat 解析文件格式码值；未知码值返回 ErrUnknownCode。
//
// 注意本函数只认码值，**不接受文件扩展名或 MIME**：由扩展名推断格式属 service/file
// 的判断（还要考虑用户改名、大小写、双扩展名），不是枚举包的职责。
func ParseFileFormat(v string) (FileFormat, error) { return fileFormatSet.parse(v) }

// Valid 报告是否为合法文件格式。零值返回 false。
func (f FileFormat) Valid() bool { return fileFormatSet.valid(f) }

// IsZero 报告是否为零值（未指定）。
func (f FileFormat) IsZero() bool { return f == "" }

// Code 返回落库与对外契约使用的字符串码值。
func (f FileFormat) Code() string { return string(f) }

// String 返回展示名（CSV / Excel / PDF），仅供日志与排错。
func (f FileFormat) String() string { return fileFormatSet.name(f) }

// MarshalJSON 实现 json.Marshaler，序列化为字符串码值；非法码值报错（与 UnmarshalJSON 对称）。
func (f FileFormat) MarshalJSON() ([]byte, error) { return marshalJSON(f, fileFormatSet) }

// UnmarshalJSON 实现 json.Unmarshaler，校验码值合法性；null 与空串得零值。
func (f *FileFormat) UnmarshalJSON(data []byte) error { return unmarshalJSON(data, f, fileFormatSet) }

// Value 实现 driver.Valuer，落库为字符串码值；非法码值报错（与 Scan 对称，避免写进去读不回来）。
func (f FileFormat) Value() (driver.Value, error) { return value(f, fileFormatSet) }

// Scan 实现 sql.Scanner，校验码值合法性；NULL 与空串得零值。
func (f *FileFormat) Scan(src any) error { return scan(src, f, fileFormatSet) }

// ParserType 是解析器类型键，承载 File.解析器类型（终版 §五 2、D-1、D-2）。
//
// # 与其余 6 个枚举的三点不同
//
// 一、**它是本包唯一带元数据的枚举**。终版 §五 2 规定每项含「标识键、名称（机构 +
// 账单类型 + 格式）、适用格式、是否启用、解析实现」。前四项在本包（见 ParserMeta），
// **解析实现不在本包**——准则 3 写死「枚举项里的实现只存标识，实现的注册表在 L3、
// 实现体在 L4」；照搬「枚举项含实现」会让 L1.0 依赖 L3/L4，必成环。
//
// 二、**它是本包唯一有「是否启用」的枚举**。停用后不再出现在 D-2 上传页的下拉里
// （见 EnabledParserTypes），但历史 File 行仍存着该键、仍可正常读出与展示——这正是
// 「只能停用不能删除」的运行形态。
//
// 三、**它是唯一预期会持续新增项的枚举**。终版 §五 2「新增银行 = 改代码」，每加一
// 家银行 / 一种账单格式就在此加一项，并在 L4 加一个平级实现子包（分层方案 L4 表
// parser/csv 行）。其余 6 个枚举的取值范围由业务模型定死，不随实现推进而变。
//
// # 本轮为什么只有一项
//
// D-1 明写「本轮仅实现 CSV」，L4 现有且仅有 parser/csv 一个实现子包。枚举项与解析
// 实现必须一一对应：多声明一个键，D-2 的下拉就会出现一个选了必然报「未注册解析器」
// 的选项。故本轮只声明通用 CSV 一项，形态（ParserMeta 三个字段 + 停用位）已完备，
// 加银行只是往表里追加行。
//
// **注**：终版 §四 4 举的例子「招商银行 · 信用卡账单 · PDF」是**命名格式的示例**，
// 不是要求本轮就声明该项——PDF 解析器本轮不实现，声明了就是个死选项。
type ParserType string

const (
	// ParserTypeGenericCSV 通用 CSV 账单。本轮唯一实现（D-1），对应 L4 的 parser/csv。
	//
	// 码值 generic_csv 而非 csv：与 FileFormatCSV 的码值区分开，避免两列在库里
	// 看起来一样而被误当成同一个东西——它们是正交的两列（格式不决定解析器）。
	ParserTypeGenericCSV ParserType = "generic_csv"
)

// ParserMeta 是解析器类型的元数据，即终版 §五 2 除「解析实现」外的四项。
//
// 全部字段只读（由包级表持有并按值返回），调用方拿到的是副本，改不动枚举表。
type ParserMeta struct {
	// Name 是名称，格式为「机构 · 账单类型 · 格式」（终版 §四 4 示例
	// 「招商银行 · 信用卡账单 · PDF」）。
	//
	// 它是 D-2 上传页下拉的标签。**不走 i18n**：K-4 明写多语言「不含数据内容
	// （支出类型名称等）的翻译」，机构名与账单类型名同属数据内容。
	Name string

	// Format 是适用格式，即该解析器吃哪种文件。
	//
	// service/file 据此实现 D-3 的第五类错误「所选解析器类型与文件不匹配」：
	// 用户选了 PDF 解析器却传上来一个 CSV，在解析前就能拦下。
	Format FileFormat

	// Enabled 表示是否启用。停用项不再出现在 D-2 下拉（EnabledParserTypes 已过滤），
	// 但仍是合法码值——历史 File 行存着它，Valid 与 Scan 一律放行。
	Enabled bool
}

// parserMetas 是解析器类型的元数据表，键为码值。
//
// 与 parserTypeSet 分开维护，因此两者可能漏配。这不靠人工核对——包初始化时的
// init 会逐项校验一一对应且内容合法，漏配则进程启动即 panic。
var parserMetas = map[ParserType]ParserMeta{
	ParserTypeGenericCSV: {
		Name:    "通用 · 逐笔交易 · CSV",
		Format:  FileFormatCSV,
		Enabled: true,
	},
}

// parserTypeSet 是 ParserType 的枚举表，声明序即 D-2 下拉的呈现序。
var parserTypeSet = newSet([]entry[ParserType]{
	{ParserTypeGenericCSV, "通用 · 逐笔交易 · CSV"},
})

// init 校验元数据表与枚举表严格一一对应，且每项内容合法。
//
// 放在 init 而非单测：单测能被跳过，init 不能。元数据缺项会让 D-2 下拉少一个选项
// 或标签为空，而这类错误在编译期与常规单测里都毫无征兆，只有真到上传页才发现。
func init() {
	mustCheckParserMetas(parserTypeSet, parserMetas)
}

// mustCheckParserMetas 是 init 的校验体：不自洽即 panic。
//
// 单独成函数是为了让「校验失败必须 panic」这一条本身可被测试。若直接把 panic 写在
// init 里，测试无从触发它——包级表是正确的，init 恒不 panic；而要让它 panic 就得
// 先把包级表写坏，那样整个测试包都加载不起来，什么也测不了。
//
// 为什么必须是 panic 而不是记日志后继续：元数据不自洽意味着 D-2 上传页会给出错误
// 的选项或空标签，属编程错误。让进程在启动的第一刻响亮失败，远好过等到用户上传
// 账单时才暴露——那时错误已经离现场很远了。
func mustCheckParserMetas(s *set[ParserType], metas map[ParserType]ParserMeta) {
	if err := checkParserMetas(s, metas); err != nil {
		panic(err.Error())
	}
}

// checkParserMetas 校验元数据表与枚举表的一致性，返回首个不一致的原因。
//
// 与 filterEnabledParserTypes 同理，逻辑以表为参数而非直接读包级变量：包级表本身
// 是正确的，直接读表的写法下「把某条校验删掉」不会让任何断言失败——校验形同虚设也
// 无人察觉。抽出后测试可以喂进各种写坏的表，逐条验证校验确实拦得住。
//
// 四条校验各自对应一种会静默出错的写错方式，都会让 D-2 上传页出问题而在编译期无声。
func checkParserMetas(s *set[ParserType], metas map[ParserType]ParserMeta) error {
	for _, pt := range s.codes {
		meta, ok := metas[pt]
		if !ok {
			return fmt.Errorf("enum: 解析器类型 %q 缺少元数据", string(pt))
		}
		if meta.Name == "" {
			return fmt.Errorf("enum: 解析器类型 %q 的名称为空", string(pt))
		}
		//适用格式必须是合法的 FileFormat：D-3 第五类错误「解析器与文件不匹配」
		//的判定完全依赖它，取值非法会让该判定恒不成立。
		if !meta.Format.Valid() {
			return fmt.Errorf("enum: 解析器类型 %q 的适用格式非法: %q", string(pt), string(meta.Format))
		}
		//枚举表的中文名与元数据的 Name 必须一致，否则同一个解析器在日志里与下拉里
		//显示成两个不同的名字，排错时对不上。
		if got := s.names[pt]; got != meta.Name {
			return fmt.Errorf("enum: 解析器类型 %q 的名称不一致: 枚举表 %q, 元数据 %q",
				string(pt), got, meta.Name)
		}
	}
	//反向核对：元数据表里不得有枚举表中不存在的项，否则那项永远不会被使用，
	//且会让「加了枚举项」的错觉长期存在。
	if len(metas) != len(s.codes) {
		return fmt.Errorf("enum: 解析器类型元数据表与枚举表项数不一致: %d vs %d",
			len(metas), len(s.codes))
	}
	return nil
}

// ParserTypes 返回全部解析器类型（含已停用），保持声明序。
//
// 含已停用项是刻意的：F-4 之类需要「展示历史行的解析器类型」的场景必须能取到全集。
// D-2 上传页的下拉应改用 EnabledParserTypes。
func ParserTypes() []ParserType { return parserTypeSet.all() }

// EnabledParserTypes 返回启用中的解析器类型，保持声明序。
//
// 这是 D-2「由用户显式选择解析器类型」的下拉数据源，也是 service/file 做 C-1
// 「校验用户所选解析器类型键的合法性」时该用的集合——停用项虽是合法码值，但不得
// 被新上传选中。
func EnabledParserTypes() []ParserType {
	return filterEnabledParserTypes(parserTypeSet.codes, parserMetas)
}

// filterEnabledParserTypes 是 EnabledParserTypes 的实现，按启用位过滤并保持声明序。
//
// 之所以把过滤逻辑单独提出来、以表为参数：本轮全部解析器都是启用状态，若直接遍历
// 包级表，「去掉过滤」这个改动不会让任何断言失败——过滤形同虚设也无人察觉。
// 抽出后，测试可以喂进一张含停用项的表，真正验证过滤这个**机制**成立，
// 而不是依赖「当前恰好没有停用项」这个随时会变的事实。
func filterEnabledParserTypes(codes []ParserType, metas map[ParserType]ParserMeta) []ParserType {
	out := make([]ParserType, 0, len(codes))
	for _, pt := range codes {
		if metas[pt].Enabled {
			out = append(out, pt)
		}
	}
	return out
}

// ParseParserType 解析解析器类型键；未知码值返回 ErrUnknownCode。
//
// 已停用的键同样解析成功——历史 File 行必须能读出来。要区分「合法」与「可被新选择」，
// 用 Enabled()。
func ParseParserType(v string) (ParserType, error) { return parserTypeSet.parse(v) }

// Valid 报告是否为合法解析器类型键（含已停用）。零值返回 false。
func (p ParserType) Valid() bool { return parserTypeSet.valid(p) }

// IsZero 报告是否为零值（未指定）。
func (p ParserType) IsZero() bool { return p == "" }

// Code 返回落库与对外契约使用的字符串码值。
func (p ParserType) Code() string { return string(p) }

// String 返回名称（机构 · 账单类型 · 格式），仅供日志与排错。
func (p ParserType) String() string { return parserTypeSet.name(p) }

// Meta 返回元数据副本。
//
// 第二个返回值表示该键是否存在，与 map 取值的惯用形态一致；不存在时返回零值
// ParserMeta（其 Enabled 为 false、Format 为零值），调用方即便忽略 ok 也不会拿到
// 一个「看起来启用」的错误元数据。
func (p ParserType) Meta() (ParserMeta, bool) {
	meta, ok := parserMetas[p]
	return meta, ok
}

// Enabled 报告该解析器类型是否启用。
//
// 未知键与零值返回 false。C-1 校验用户所选类型时应当用它而非 Valid——后者对已停用
// 的历史键也返回 true。
func (p ParserType) Enabled() bool { return parserMetas[p].Enabled }

// Format 返回适用的文件格式；未知键返回零值。
// service/file 用它实现 D-3 第五类错误「所选解析器类型与文件不匹配」的判定。
func (p ParserType) Format() FileFormat { return parserMetas[p].Format }

// MarshalJSON 实现 json.Marshaler，序列化为字符串码值；非法码值报错（与 UnmarshalJSON 对称）。
func (p ParserType) MarshalJSON() ([]byte, error) { return marshalJSON(p, parserTypeSet) }

// UnmarshalJSON 实现 json.Unmarshaler，校验码值合法性；null 与空串得零值。
//
// 只校验「是否为合法键」，**不校验是否启用**：反序列化是形态转换，而「停用项不得被
// 新选择」是业务规则，属 service/file 的 C-1 校验。混在一起会让读取历史数据的接口
// 也因该键已停用而失败。
func (p *ParserType) UnmarshalJSON(data []byte) error { return unmarshalJSON(data, p, parserTypeSet) }

// Value 实现 driver.Valuer，落库为字符串码值；非法码值报错（与 Scan 对称，避免写进去读不回来）。
func (p ParserType) Value() (driver.Value, error) { return value(p, parserTypeSet) }

// Scan 实现 sql.Scanner，校验码值合法性；NULL 与空串得零值。
// 同样不校验启用状态——历史行的键可能已停用，那是正常状态，不是数据损坏。
func (p *ParserType) Scan(src any) error { return scan(src, p, parserTypeSet) }
