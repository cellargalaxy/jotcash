package model

import "encoding/json"

// FileBlob 文件内容：全模型唯一的内容寻址表，主键即内容哈希。
type FileBlob struct {
	ContentHash string `json:"content_hash" gorm:"column:content_hash;type:varchar(64);primaryKey"`

	Content []byte `json:"-" gorm:"column:content;type:blob"` // 不进JSON，避免日志/响应中出现二进制内容
}

func (this FileBlob) String() string {
	data, _ := json.Marshal(this)
	return string(data)
}
