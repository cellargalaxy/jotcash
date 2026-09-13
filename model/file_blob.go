package model

import (
	"github.com/cellargalaxy/go_common/util"
)

type FileBlob struct {
	FileHash string `json:"file_hash" gorm:"column:file_hash;primaryKey"`
	FileData []byte `json:"-" gorm:"column:file_data;type:blob"`
}

func (this FileBlob) String() string {
	return util.JsonStruct2Str(this)
}
func (this FileBlob) TableName() string {
	return "file_blob"
}

type FileBlobInquiry struct {
	FileHash []string `json:"file_hash"`
	Sort     string   `json:"sort"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
}

func (this FileBlobInquiry) String() string {
	return util.JsonStruct2Str(this)
}
