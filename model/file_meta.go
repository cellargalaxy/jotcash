package model

import (
	"time"

	"github.com/cellargalaxy/go_common/util"
)

type FileMeta struct {
	Id          int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement:false"`
	FileHash    string    `json:"file_hash" gorm:"column:file_hash;index"`
	FileName    string    `json:"file_name" gorm:"column:file_name;index"`
	FileSize    int64     `json:"file_size" gorm:"column:file_size"`
	OperationId int64     `json:"operation_id" gorm:"column:operation_id;index"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;index"`
}

func (this FileMeta) String() string {
	return util.JsonStruct2Str(this)
}
func (this FileMeta) TableName() string {
	return "file_meta"
}

type FileMetaInquiry struct {
	Id             []int64   `json:"id"`
	FileHash       []string  `json:"file_hash"`
	FileName       []string  `json:"file_name"`
	OperationId    []int64   `json:"operation_id"`
	FileNameLike   string    `json:"file_name_like"`
	CreatedAtStart time.Time `json:"created_at_start"`
	CreatedAtEnd   time.Time `json:"created_at_end"`
	Sort           string    `json:"sort"`
	Page           int       `json:"page"`
	PageSize       int       `json:"page_size"`
}

func (this FileMetaInquiry) String() string {
	return util.JsonStruct2Str(this)
}
