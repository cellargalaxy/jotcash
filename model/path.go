package model

const (
	PathExpenseInsert      = "/api/expense/insert"
	PathExpenseSelect      = "/api/expense/select"
	PathExpenseDelete      = "/api/expense/delete"
	PathOperationLogSelect = "/api/operation_log/select"
	PathFileMetaSelect     = "/api/file_meta/select"
	PathChangeToken        = "/api/token/change"
	PathExportDb           = "/api/db/export"
	PathImportDb           = "/api/db/import"
)

// 导入数据库的上传字段名
const ImportFileKey = "file"

// 明细入库的上传字段名
const ExpenseFileKey = "file"
