package model

import (
	"time"

	"github.com/cellargalaxy/go_common/util"
)

type FileMeta struct {
	Id          int64     `json:"id"`
	FileHash    string    `json:"file_hash"`
	FileName    string    `json:"file_name"`
	FileSize    int64     `json:"file_size"`
	OperationId int64     `json:"operation_id"`
	CreatedAt   time.Time `json:"created_at"`
}

func (this FileMeta) String() string {
	return util.JsonStruct2Str(this)
}
func (this Expense) TableName() string {
	return "" //todo
}

type FileMetaInquiry struct {
	Id          []int64  `json:"id"`
	FileHash    []string `json:"file_hash"`
	OperationId []int64  `json:"operation_id"`
}

func (this FileMetaInquiry) String() string {
	return util.JsonStruct2Str(this)
}
