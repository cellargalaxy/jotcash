package model

import (
	"time"

	"github.com/cellargalaxy/go_common/util"
)

const (
	OperationTypeSystemInit      = "系统初始化"
	OperationTypeDataEntry       = "数据入库"
	OperationTypeExpenseEdit     = "明细编辑"
	OperationTypeExpenseDelete   = "明细删除"
	OperationTypeCurrencySwitch  = "记账币种切换"
	OperationTypeClientTokenSwap = "更换口令"
	OperationTypeDbImport        = "数据库导入"
	OperationTypeDbExport        = "数据库导出"
)

const (
	ObjectTypeExpense = "支出明细"
)

const (
	ResultSuccess = "成功"
	ResultFailure = "失败"
	ResultPartial = "部分成功"
)

type OperationLog struct {
	Id            int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement:false"`
	OperationType string    `json:"operation_type" gorm:"column:operation_type;index"`
	ObjectType    string    `json:"object_type" gorm:"column:object_type;index"`
	ObjectId      int64     `json:"object_id" gorm:"column:object_id;index"`
	Summary       string    `json:"summary" gorm:"column:summary"`
	Changes       string    `json:"changes" gorm:"column:changes"`
	Result        string    `json:"result" gorm:"column:result;index"`
	CreatedAt     time.Time `json:"created_at" gorm:"column:created_at;index"`
}

func (this OperationLog) String() string {
	return util.JsonStruct2Str(this)
}
func (this OperationLog) TableName() string {
	return "operation_log"
}

type OperationLogInquiry struct {
	Id             []int64   `json:"id"`
	OperationType  []string  `json:"operation_type"`
	ObjectType     []string  `json:"object_type"`
	ObjectId       []int64   `json:"object_id"`
	Result         []string  `json:"result"`
	SummaryLike    string    `json:"summary_like"`
	CreatedAtStart time.Time `json:"created_at_start"`
	CreatedAtEnd   time.Time `json:"created_at_end"`
	Sort           string    `json:"sort"`
	Page           int       `json:"page"`
	PageSize       int       `json:"page_size"`
}

func (this OperationLogInquiry) String() string {
	return util.JsonStruct2Str(this)
}
