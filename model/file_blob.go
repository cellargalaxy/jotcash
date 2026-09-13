package model

import (
	"github.com/cellargalaxy/go_common/util"
)

type FileBlob struct {
	FileHash string `json:"file_hash"`
	FileData []byte `json:"-"`
}

func (this FileBlob) String() string {
	return util.JsonStruct2Str(this)
}
func (this FileBlob) TableName() string {
	return "" //todo
}

type FileBlobInquiry struct {
	FileHash []string `json:"file_hash"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
}

func (this FileBlobInquiry) String() string {
	return util.JsonStruct2Str(this)
}
