package model

import (
	"time"

	"github.com/cellargalaxy/go_common/util"
)

type OperationLog struct {
	Id            int64     `json:"id"`
	OperationType string    `json:"operation_type"`
	ObjectType    string    `json:"object_type"`
	ObjectId      int64     `json:"object_id"`
	Summary       string    `json:"summary"`
	Changes       string    `json:"changes"`
	Result        string    `json:"result"`
	CreatedAt     time.Time `json:"created_at"`
}

func (this OperationLog) String() string {
	return util.JsonStruct2Str(this)
}
func (this OperationLog) TableName() string {
	return "" //todo
}

type OperationLogInquiry struct {
	Id            []int64  `json:"id"`
	OperationType []string `json:"operation_type"`
	ObjectType    []string `json:"object_type"`
	ObjectId      []int64  `json:"object_id"`
	Result        []string `json:"result"`
}

func (this OperationLogInquiry) String() string {
	return util.JsonStruct2Str(this)
}
