package model

import (
	"encoding/json"
	"time"
)

// ActionType AuditLog.ActionType 取值：操作类型枚举，对应文档 §8.4，共 8 类
const (
	ActionTypeSystemInit     = "系统初始化"  // A-1 系统初始化
	ActionTypeDataEntry      = "数据入库"   // E-2 单行新增/CSV批量/复制，统一为“新增”
	ActionTypeExpenseEdit    = "明细编辑"   // E-4 单行字段修改，不生成新文件、不改来源审计ID/文件ID
	ActionTypeExpenseDelete  = "明细删除"   // E-5 批量软删除
	ActionTypeCurrencySwitch = "记账币种切换" // F-4 记账币种切换
	ActionTypePasswordChange = "更换口令"   // A-5 更换口令
	ActionTypeDatabaseImport = "数据库导入"  // B-2 导入加密数据库
	ActionTypeDatabaseExport = "数据库导出"  // B-1 导出加密数据库
)

// Result AuditLog.Result 取值：操作结果枚举
const (
	ResultSuccess        = "成功"
	ResultFailure        = "失败"
	ResultPartialSuccess = "部分成功"
)

// AuditLog 操作审计：留痕枢纽，不承载业务动作、不构成回滚入口（对应文档 §四.2，共 8 项属性）
type AuditLog struct {
	Id int `json:"id" gorm:"column:id;primaryKey;autoIncrement"` // 审计ID：唯一标识，兼“入库批次号”

	ActionType string `json:"action_type" gorm:"column:action_type;type:varchar(32);index"` // 操作类型：8类枚举，见ActionType常量

	ObjectType string `json:"object_type" gorm:"column:object_type;type:varchar(32)"` // 操作对象类型：零值=无具体对象或批量操作
	ObjectId   int    `json:"object_id" gorm:"column:object_id;index"`                // 对象ID：零值=同上；历史指针，非外键

	Summary string `json:"summary" gorm:"column:summary"`           // 操作摘要：人可读描述，如“入库37笔，来源2609.csv”“复制自明细X”
	Changes string `json:"changes" gorm:"column:changes;type:text"` // 变更内容：前后值快照（JSON文本）；零值=非编辑类操作；不记口令值

	Result string `json:"result" gorm:"column:result;type:varchar(16)"` // 操作结果：成功/失败/部分成功，见Result常量

	OperatedAt time.Time `json:"operated_at" gorm:"column:operated_at;index"` // 操作时间：发生时点
}

func (this AuditLog) String() string {
	data, _ := json.Marshal(this)
	return string(data)
}
