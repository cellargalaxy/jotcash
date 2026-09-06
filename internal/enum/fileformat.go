package enum

// 本文件承载 §六 L1.0 `enum` 行七项承载中的第 3 项：**文件格式（CSV·Excel·PDF）**。

import (
	"database/sql/driver"
)

// FileFormat 文件格式，对应 `entity.File.格式`。
//
// 依据终版 §四 4 `File.格式`：「**文件类型标识**，用于展示、预览与下载
// （CSV / Excel / PDF）；**不决定解析器**」。三项即全集，分层版 §六 L1.0 `enum` 行
// 亦写明「文件格式（CSV·Excel·PDF）」。
//
// # 与 ParserType 的分工：这是 D-1 的核心区分，不可合并
//
// 终版 §二 D-1 原文：「由 `File.解析器类型` 决定走哪个解析器（**`格式` 不足以判定**
// ——同为 PDF，各银行版式不同）」。因此：
//
//	FileFormat  → 「这个文件是什么类型的文件」，决定**展示、预览与下载**的方式
//	ParserType  → 「用哪段代码去读它」，决定**解析**行为，由用户在上传页显式选择（D-2）
//
// 两者是多对一：多个解析器类型可以共享同一个格式（招行 PDF 与工行 PDF 都是 PDF）。
// 本类型因此**不含**任何解析语义，也不提供「按格式找解析器」的入口——那样做会把
// D-1 特意拆开的两件事又粘回去。
type FileFormat string

const (
	// FileFormatCSV CSV。本轮唯一有解析实现的格式（D-1「本轮仅实现 CSV」，
	// 实现体在 L4 `parser/csv`）。
	FileFormatCSV FileFormat = "csv"
	// FileFormatExcel Excel。格式枚举已含，解析实现按 D-1「新增银行或格式不影响上层流程」后续增补。
	FileFormatExcel FileFormat = "excel"
	// FileFormatPDF PDF。同上；D-1 特别指出「同为 PDF，各银行版式不同」，
	// 故 PDF 下会有多个解析器类型。
	FileFormatPDF FileFormat = "pdf"
)

// fileFormatTable 文件格式枚举表。声明顺序与终版 §四 4 的「CSV / Excel / PDF」一致。
var fileFormatTable = newTable("file_format", "文件格式",
	entry{code: string(FileFormatCSV), name: "CSV"},
	entry{code: string(FileFormatExcel), name: "Excel"},
	entry{code: string(FileFormatPDF), name: "PDF"},
)

// ParseFileFormat 校验并转换文件格式代码，是文件格式的唯一校验点。
func ParseFileFormat(code string) (FileFormat, error) {
	parsed, err := fileFormatTable.parse(code)
	return FileFormat(parsed), err
}

// ListFileFormats 按声明顺序返回全部文件格式，供 C-1 上传页与 F-4 筛选下拉使用。
func ListFileFormats() []FileFormat {
	codes := fileFormatTable.listCodes()
	formats := make([]FileFormat, 0, len(codes))
	for _, code := range codes {
		formats = append(formats, FileFormat(code))
	}
	return formats
}

// Valid 是否为已定义的文件格式。零值返回 false（`File.格式` 必填，终版 §四 4）。
func (f FileFormat) Valid() bool { return fileFormatTable.has(string(f)) }

// IsZero 是否为零值，即「未设置」。
func (f FileFormat) IsZero() bool { return f == "" }

// String 返回代码，即落库值与 JSON 值。
func (f FileFormat) String() string { return string(f) }

// Name 返回名称，仅供日志与诊断。
//
// 注：本枚举的三个「中文名称」恰好是英文技术名（CSV / Excel / PDF）——它们是
// 事实上的中文语境用词，不另造「逗号分隔值文件」这类无人使用的译名。
func (f FileFormat) Name() string { return fileFormatTable.nameOf(string(f)) }

// TextKey 返回 i18n 文案键。
func (f FileFormat) TextKey() string { return fileFormatTable.textKeyOf(string(f)) }

// Value 实现 driver.Valuer，以代码文本落库；零值与非法值报错。
func (f FileFormat) Value() (driver.Value, error) {
	return valueOf(fileFormatTable, string(f), false)
}

// Scan 实现 sql.Scanner。
func (f *FileFormat) Scan(src any) error {
	code, err := scanCode(src, fileFormatTable, false)
	if err != nil {
		return err
	}
	*f = FileFormat(code)
	return nil
}

// MarshalText 实现 encoding.TextMarshaler。
func (f FileFormat) MarshalText() ([]byte, error) {
	return marshalTextOf(fileFormatTable, string(f), false)
}

// UnmarshalText 实现 encoding.TextUnmarshaler。
func (f *FileFormat) UnmarshalText(data []byte) error {
	parsed, err := ParseFileFormat(string(data))
	if err != nil {
		return err
	}
	*f = parsed
	return nil
}
