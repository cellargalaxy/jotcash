package model

import (
	"encoding/json"
	"time"
)

// FileMeta 文件元数据：承载绝大部分查询，内容哈希指向 FileBlob。
type FileMeta struct {
	Id int64 `json:"id" gorm:"column:id;primaryKey"` // 由 go_common/util.GenId() 生成，不用数据库自增

	ContentHash string `json:"content_hash" gorm:"column:content_hash;type:varchar(64);index"`

	FileName string `json:"file_name" gorm:"column:file_name"`
	FileSize int64  `json:"file_size" gorm:"column:file_size"`

	AuditId int64 `json:"audit_id" gorm:"column:audit_id;index"`

	CreatedAt time.Time `json:"created_at" gorm:"column:created_at"`
}

func (this FileMeta) String() string {
	data, _ := json.Marshal(this)
	return string(data)
}
