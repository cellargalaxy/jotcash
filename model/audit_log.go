package model

import (
	"encoding/json"
	"time"
)

// ActionType AuditLog.ActionType 取值
const (
	ActionTypeSystemInit     = "系统初始化"
	ActionTypeDataEntry      = "数据入库" // 单行新增/CSV批量/复制，统一为“新增”
	ActionTypeExpenseEdit    = "明细编辑"
	ActionTypeExpenseDelete  = "明细删除"
	ActionTypeCurrencySwitch = "记账币种切换"
	ActionTypePasswordChange = "更换口令"
	ActionTypeDatabaseImport = "数据库导入"
	ActionTypeDatabaseExport = "数据库导出"
)

// Result AuditLog.Result 取值
const (
	ResultSuccess        = "成功"
	ResultFailure        = "失败"
	ResultPartialSuccess = "部分成功"
)

// AuditLog 操作审计：留痕枢纽，不承载业务动作。
//
// 只有 CreatedAt，没有 OperatedAt：字段名用 CreatedAt 是为了让 gorm 按约定自动填充（按字段名识别，非列名）。
type AuditLog struct {
	Id int64 `json:"id" gorm:"column:id;primaryKey"` // 由 go_common/util.GenId() 生成，不用数据库自增

	ActionType string `json:"action_type" gorm:"column:action_type;type:varchar(32);index"`

	ObjectType string `json:"object_type" gorm:"column:object_type;type:varchar(32)"` // 零值=无具体对象或批量操作
	ObjectId   int64  `json:"object_id" gorm:"column:object_id;index"`                // 历史指针，非外键

	Summary string `json:"summary" gorm:"column:summary"`
	Changes string `json:"changes" gorm:"column:changes;type:text"` // 前后值快照JSON；不记口令值

	Result string `json:"result" gorm:"column:result;type:varchar(16)"`

	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;index"`
}

func (this AuditLog) String() string {
	data, _ := json.Marshal(this)
	return string(data)
}
