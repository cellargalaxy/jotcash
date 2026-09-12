package model

import (
	"encoding/json"
	"time"
)

// Purpose FileMeta.Purpose 取值：文件用途枚举
const (
	PurposeUploadOriginal = "上传原件" // 用户上传的CSV原件
	PurposeEntrySnapshot  = "入库快照" // 落库时系统生成的CSV快照
)

// FileMeta 文件元数据：承载绝大部分查询，内容哈希指向 FileBlob（对应文档 §四.3，共 7 项属性）
type FileMeta struct {
	Id int `json:"id" gorm:"column:id;primaryKey;autoIncrement"` // 文件ID：唯一标识

	ContentHash string `json:"content_hash" gorm:"column:content_hash;type:varchar(64);index"` // 内容哈希：指向FileBlob的外键，天然去重

	FileName string `json:"file_name" gorm:"column:file_name"`              // 文件名：上传原始文件名，或系统为单行新增生成的名字
	Purpose  string `json:"purpose" gorm:"column:purpose;type:varchar(16)"` // 用途：上传原件/入库快照，见Purpose常量
	FileSize int64  `json:"file_size" gorm:"column:file_size"`              // 文件大小（字节）

	AuditId int `json:"audit_id" gorm:"column:audit_id;index"` // 审计ID：在哪次操作中落档

	CreatedAt time.Time `json:"created_at" gorm:"column:created_at"` // 创建时间
}

func (this FileMeta) String() string {
	data, _ := json.Marshal(this)
	return string(data)
}
