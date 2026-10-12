package config

const (
	ListenAddress = ":7678"
)

const (
	PathExpenseInsert      = "/api/expense/insert"
	PathExpenseSelect      = "/api/expense/select"
	PathExpenseUpdate      = "/api/expense/update"
	PathExpenseDelete      = "/api/expense/delete"
	PathExpenseSwitch      = "/api/expense/switch_currency"
	PathExpenseDistinct    = "/api/expense/distinct"
	PathOperationLogSelect = "/api/operation_log/select"
	PathFileMetaSelect     = "/api/file_meta/select"
	PathFileMetaDownload   = "/api/file_meta/download"
	PathChangeToken        = "/api/token/change"
	PathExportDb           = "/api/db/export"
	PathImportDb           = "/api/db/import"
)

const (
	ImportFileKey     = "file"    // 导入数据库的上传字段名
	ExpenseFileKey    = "file"    // 明细入库的上传字段名
	ExpenseParserKey  = "parser"  // 明细入库所选识别器的字段名
	ExpenseContentKey = "content" // 明细入库时前端已提取好的内容，有它就解析它，原文件只留档
)
