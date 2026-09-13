package model

import (
	"time"

	"github.com/cellargalaxy/go_common/util"
)

type OperationLog struct {
	Id            int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement:false"`
	OperationType string    `json:"operation_type" gorm:"column:operation_type;index"`
	ObjectType    string    `json:"object_type" gorm:"column:object_type;index"`
	ObjectId      int64     `json:"object_id" gorm:"column:object_id;index"`
	Summary       string    `json:"summary" gorm:"column:summary"`
	Changes       string    `json:"changes" gorm:"column:changes"`
	Result        string    `json:"result" gorm:"column:result;index"`
	CreatedAt     time.Time `json:"created_at" gorm:"column:created_at"`
}

func (this OperationLog) String() string {
	return util.JsonStruct2Str(this)
}
func (this OperationLog) TableName() string {
	return "operation_log"
}

type OperationLogInquiry struct {
	Id            []int64  `json:"id"`
	OperationType []string `json:"operation_type"`
	ObjectType    []string `json:"object_type"`
	ObjectId      []int64  `json:"object_id"`
	Result        []string `json:"result"`
	Page          int      `json:"page"`
	PageSize      int      `json:"page_size"`
}

func (this OperationLogInquiry) String() string {
	return util.JsonStruct2Str(this)
}
