package model

import (
	"encoding/json"
	"time"
)

// Purpose FileMeta.Purpose 取值：文件用途枚举
//
// 经复核确认有实际业务功能，非冗余字段：① D-4 明确把“用途”列为文件列表页的筛选维度之一
// （“按时间/文件名/用途/来源审计筛选并下载”）；② 关联关系 §七 明确 `FileMeta → Expense`
// 指向的是“CSV 快照”（即 PurposeEntrySnapshot），不是用户上传的原件，用途决定了同一次
// 入库落档的两条 FileMeta 记录里哪一条会被 Expense.FileId 引用。
const (
	PurposeUploadOriginal = "上传原件" // 用户上传的CSV原件
	PurposeEntrySnapshot  = "入库快照" // 落库时系统生成的CSV快照，Expense.FileId 指向此用途的记录（§七）
)

// FileMeta 文件元数据：承载绝大部分查询，内容哈希指向 FileBlob（对应文档 §四.3，共 7 项属性）
type FileMeta struct {
	// Id 文件ID：唯一标识。取值由应用层调用 go_common/util.GenId() 生成，不使用数据库自增。
	Id int64 `json:"id" gorm:"column:id;primaryKey"`

	ContentHash string `json:"content_hash" gorm:"column:content_hash;type:varchar(64);index"` // 内容哈希：指向FileBlob的外键，天然去重

	FileName string `json:"file_name" gorm:"column:file_name"`              // 文件名：上传原始文件名，或系统为单行新增生成的名字
	Purpose  string `json:"purpose" gorm:"column:purpose;type:varchar(16)"` // 用途：上传原件/入库快照，见Purpose常量
	FileSize int64  `json:"file_size" gorm:"column:file_size"`              // 文件大小（字节）：用于页面展示

	AuditId int64 `json:"audit_id" gorm:"column:audit_id;index"` // 审计ID：在哪次操作中落档

	CreatedAt time.Time `json:"created_at" gorm:"column:created_at"` // 创建时间
}

func (this FileMeta) String() string {
	data, _ := json.Marshal(this)
	return string(data)
}
