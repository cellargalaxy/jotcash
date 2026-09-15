package config

const (
	ListenAddress = ":7678"
)

const (
	PathExpenseInsert      = "/api/expense/insert"
	PathExpenseSelect      = "/api/expense/select"
	PathExpenseDelete      = "/api/expense/delete"
	PathExpenseDistinct    = "/api/expense/distinct"
	PathOperationLogSelect = "/api/operation_log/select"
	PathFileMetaSelect     = "/api/file_meta/select"
	PathChangeToken        = "/api/token/change"
	PathExportDb           = "/api/db/export"
	PathImportDb           = "/api/db/import"
)

const (
	ImportFileKey  = "file" // 导入数据库的上传字段名
	ExpenseFileKey = "file" // 明细入库的上传字段名
)
