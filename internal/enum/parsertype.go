package enum

// 本文件承载 §六 L1.0 `enum` 行七项承载中的第 4 项：**解析器类型键**
// （不含解析实现，终版 §五 2）。
//
// 本枚举是七类中**唯一带「是否启用」属性**的一类，因此也是唯一区分两条校验路径的一类
// （包注释第三节）。它另有一个专属属性「适用格式」，故在本文件里另置属性表，
// 不把这两个可选字段塞进通用的 entry 结构。

import (
	"database/sql/driver"

	"github.com/cellargalaxy/jotcash/internal/base/errs"
)

// ParserType 解析器类型键，对应 `entity.File.解析器类型`。
//
// 依据终版 §五 2「**解析器类型枚举**（代码内置）：每项含**标识键、名称（机构 + 账单类型
// + 格式）、适用格式、是否启用、解析实现**。库中只存键」，以及 §四 4 `File.解析器类型`
// 「**决定走哪个解析器**；取值为代码内置解析器类型枚举的键（如「招商银行 · 信用卡账单 · PDF」），
// **由用户在上传页显式选择**」。
//
// # 本包只有键与元信息，没有解析实现
//
// 分层版 §五 准则 3：「**枚举项里的「实现」只存标识，实现的注册表在 L3、实现体在 L4**」，
// 并注明「照搬『枚举项含实现』会让 L1.0 依赖 L3/L4 必成环」。因此终版 §五 2 列的五项属性
// 在本仓库的落点是：
//
//	标识键 / 名称 / 适用格式 / 是否启用 → 本包（L1.0）
//	解析实现                          → 注册表在 L3 `parser`、实现体在 L4 `parser/csv`
//
// 本包不提供任何「取解析实现」的入口，`parser` 按本包的键路由（D-1）。
//
// # 「只能停用不能删除」的直接后果
//
// 终版 §五 2：「枚举项**只能停用不能删除**（历史 `File` 存着旧键）」，
// 叠加 C-4「文件只留存不物理删除」——一旦某解析器停用，存量 `File` 记录**必须仍能读出**。
// 这就是本类型区分 ParseParserType（仅已启用）与 ParseParserTypeStored（含已停用）
// 两条路径的原因；混用的双向后果见包注释第三节。
type ParserType string

const (
	// ParserTypeGenericCSV 通用 CSV 账单。
	//
	// **本轮唯一有解析实现的类型**（D-1「本轮仅实现 CSV」，实现体在 L4 `parser/csv`）。
	//
	// 需要说明的一处口径选择：终版 §五 2 把名称口径定为「机构 + 账单类型 + 格式」，
	// 举例是「招商银行 · 信用卡账单 · PDF」。但 D-1 同时明确「**适配各银行原生导出格式**，
	// 不定义统一模板」，而本轮尚未选定要适配哪家银行（分层版 §十一 阶段 6 才落 `parser/csv`）。
	// 因此本轮**不凭空发明银行名**——那会造出一个没有对应实现、且名称与实际解析行为不符的键，
	// 且键一旦落库便永不可改（§五 2）。此处只登记一个「机构 = 通用」的类型键占位，
	// 待阶段 6 选定银行后按同一命名口径新增具体银行的键（新增键不影响存量数据）。
	// 该取舍已登记在本轮 answer 的待复核项中。
	ParserTypeGenericCSV ParserType = "generic_csv"
)

// parserTypeAttr 解析器类型的专属属性：适用格式 + 是否启用。
//
// 单置一张表而非扩充通用 entry：另外 6 类枚举都用不到这两个字段，
// 塞进 entry 会让它们各留两个永远为空的位（enum.go 的 entry 注释已定此口径）。
type parserTypeAttr struct {
	// format 适用格式（终版 §五 2）。用于 C-1 上传页按用户选择的格式过滤可选解析器，
	// 以及 D-3 第 5 类错误「所选解析器类型与文件不匹配」的机械判据之一。
	format FileFormat
	// enabled 是否启用（终版 §五 2）。停用项**仍留在枚举表里**（历史 File 存着旧键），
	// 只是不再出现在 C-1 的可选列表中。
	enabled bool
}

// parserTypeTable 解析器类型枚举表。**含已停用项**——它是「读历史数据」路径的查表依据。
var parserTypeTable = newTable("parser_type", "解析器类型",
	entry{code: string(ParserTypeGenericCSV), name: "通用 · 账单 · CSV"},
)

// parserTypeAttrs 解析器类型的属性表，键须与 parserTypeTable 完全一致（由 init 校验）。
var parserTypeAttrs = map[ParserType]parserTypeAttr{
	ParserTypeGenericCSV: {format: FileFormatCSV, enabled: true},
}

// init 校验两张表的键集合完全一致，且每项的适用格式合法。
//
// 与 newTable 的 panic 同一取舍（见 enum.go newTable 注释）：这是写死在源码里的常量数据，
// 两张表对不齐属**编码错误**——新增一个解析器类型时若漏填属性表，
// 该类型的「适用格式」会是零值 FileFormat、「是否启用」会是 false，
// 表现为「这个解析器在上传页里死活不出现」，而排查方向根本不会指向漏填属性表。
// 故在包初始化期当场崩掉。
func init() {
	if len(parserTypeAttrs) != len(parserTypeTable.codes) {
		panic("enum：解析器类型的属性表与枚举表项数不符")
	}
	for _, code := range parserTypeTable.codes {
		attr, ok := parserTypeAttrs[ParserType(code)]
		if !ok {
			panic("enum：解析器类型缺少属性表项，code=" + code)
		}
		if !attr.format.Valid() {
			panic("enum：解析器类型的适用格式非法，code=" + code)
		}
	}
}

// ParseParserType 校验并转换解析器类型键，**仅接受已启用项**——用户输入路径
// （C-1 上传页显式选择解析器类型，D-2）。
//
// 已停用的键返回错误：文案键取 errs.KeyParserTypeUnknown（「解析器类型键不存在或未启用」，
// 其注释即写明「D-1 注册表未命中」），与「键根本不存在」共用同一文案
// ——对用户而言两者是同一件事（这个解析器现在不能用），不必区分。
func ParseParserType(code string) (ParserType, error) {
	if code == "" || !parserTypeTable.has(code) {
		return "", parserTypeUnknownErr(code)
	}
	if !parserTypeAttrs[ParserType(code)].enabled {
		return "", parserTypeUnknownErr(code)
	}
	return ParserType(code), nil
}

// ParseParserTypeStored 校验并转换解析器类型键，**接受含已停用项**——历史数据路径
// （`File.解析器类型` 落库值、Scan）。
//
// 这条路径存在的唯一理由见包注释第三节：C-4「文件只留存不物理删除」+ §五 2
// 「只能停用不能删除」，两条合起来要求「停用某解析器后，存量 File 记录仍能读出」。
func ParseParserTypeStored(code string) (ParserType, error) {
	parsed, err := parserTypeTable.parse(code)
	return ParserType(parsed), err
}

// parserTypeUnknownErr 构造「解析器类型键不存在或未启用」错误（D-1 注册表未命中）。
//
// 档位 KindInput：这是用户在 C-1 上传页选出来的值，属用户输入。
// 占位参数取 ArgType（`errs.ArgType` 注释即写明「解析器类型键、文件格式」）
// ——与 table.invalidErr 用 ArgField 不同：那条是通用的「字段取值非法」，
// 而本条有专属文案键，模板里要填的是解析器类型本身。
func parserTypeUnknownErr(code string) error {
	return errs.New(errs.KindInput, errs.KeyParserTypeUnknown, errs.A(errs.ArgType, code))
}

// ListEnabledParserTypes 按声明顺序返回**已启用**的解析器类型，供 C-1 上传页下拉使用。
//
// 只出已启用项：停用项若出现在下拉里就会被重新选中，而停用的语义正是「不再接受新文件」。
func ListEnabledParserTypes() []ParserType {
	types := make([]ParserType, 0, len(parserTypeTable.codes))
	for _, code := range parserTypeTable.codes {
		if parserTypeAttrs[ParserType(code)].enabled {
			types = append(types, ParserType(code))
		}
	}
	return types
}

// ListEnabledParserTypesByFormat 按声明顺序返回指定格式下**已启用**的解析器类型。
//
// 供 C-1 上传页在用户选定格式后过滤下拉项（同为 PDF 会有多家银行，D-1）。
// 格式非法或无匹配项时返回空切片而非错误：「这个格式暂时没有可用解析器」是正常业务状态
// （如本轮的 Excel / PDF），不是错误。
func ListEnabledParserTypesByFormat(format FileFormat) []ParserType {
	types := make([]ParserType, 0, len(parserTypeTable.codes))
	for _, code := range parserTypeTable.codes {
		attr := parserTypeAttrs[ParserType(code)]
		if attr.enabled && attr.format == format {
			types = append(types, ParserType(code))
		}
	}
	return types
}

// Valid 是否为已定义的解析器类型键（**含已停用项**）。
//
// 口径与 Enabled 分开：Valid 回答「这个键本包认不认识」（读历史数据的判据），
// Enabled 回答「现在还能不能选它」（收用户输入的判据）。合成一个方法就必然有一条路径用错。
func (p ParserType) Valid() bool { return parserTypeTable.has(string(p)) }

// Enabled 是否已启用。未定义的键返回 false。
func (p ParserType) Enabled() bool {
	attr, ok := parserTypeAttrs[p]
	return ok && attr.enabled
}

// IsZero 是否为零值，即「未设置」。
func (p ParserType) IsZero() bool { return p == "" }

// Format 适用格式（终版 §五 2）。未定义的键返回零值 FileFormat。
func (p ParserType) Format() FileFormat { return parserTypeAttrs[p].format }

// MatchFormat 本解析器类型是否适用于给定格式。
//
// 供 D-3 第 5 类「所选解析器类型与文件不匹配」的**机械判据**：格式与解析器类型对不上时，
// 无需真去解析文件即可判定。注意这只是必要条件——格式对上但版式不符仍要由
// L4 的具体解析器返回 errs.KeyParserTypeMismatch。
func (p ParserType) MatchFormat(format FileFormat) bool {
	attr, ok := parserTypeAttrs[p]
	return ok && attr.format == format
}

// String 返回代码，即落库值与 JSON 值。
func (p ParserType) String() string { return string(p) }

// Name 返回中文名称（机构 + 账单类型 + 格式），仅供日志与诊断。
func (p ParserType) Name() string { return parserTypeTable.nameOf(string(p)) }

// TextKey 返回 i18n 文案键。
func (p ParserType) TextKey() string { return parserTypeTable.textKeyOf(string(p)) }

// Value 实现 driver.Valuer，以键文本落库。
//
// **落库校验用 Valid 而非 Enabled**（即 valueOf 的查表口径）：停用某解析器后，
// 存量 `File` 记录仍要能被更新（如 C-2 的内容校验值补算），若落库校验按 Enabled 走，
// 这些记录会变成「读得出、写不回」。
func (p ParserType) Value() (driver.Value, error) {
	return valueOf(parserTypeTable, string(p), false)
}

// Scan 实现 sql.Scanner，**走「读历史数据」路径**（接受含已停用项）。
func (p *ParserType) Scan(src any) error {
	code, err := scanCode(src, parserTypeTable, false)
	if err != nil {
		return err
	}
	*p = ParserType(code)
	return nil
}

// MarshalText 实现 encoding.TextMarshaler。
// 与 Value 同口径：含已停用项——C-1 文件管理页要展示存量文件的解析器类型。
func (p ParserType) MarshalText() ([]byte, error) {
	return marshalTextOf(parserTypeTable, string(p), false)
}

// UnmarshalText 实现 encoding.TextUnmarshaler，**走「收用户输入」路径**（仅已启用项）。
//
// 这是本类型两条路径唯一的不对称处，且是刻意的：JSON 反序列化的来源是
// `api/http/dto` 的请求体，即用户输入（C-1 上传时选择解析器类型），
// 因此必须按 ParseParserType 拦住已停用项。库内数据一律走 Scan，不经此方法。
func (p *ParserType) UnmarshalText(data []byte) error {
	parsed, err := ParseParserType(string(data))
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}
