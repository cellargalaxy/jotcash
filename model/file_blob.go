package model

import "encoding/json"

// FileBlob 文件内容：全模型唯一的内容寻址表，主键即内容哈希（对应文档 §四.4，共 2 项属性）
//
// 无“创建时间”字段：复核后确认该字段没有独立业务用途（可从同哈希的 FileMeta.创建时间 间接推得，
// 也不被任何查询/校验逻辑直接使用），已在文档合并定稿中删除。
type FileBlob struct {
	ContentHash string `json:"content_hash" gorm:"column:content_hash;type:varchar(64);primaryKey"` // 内容哈希：主键即唯一ID，天然去重与完整性校验基准

	Content []byte `json:"-" gorm:"column:content;type:blob"` // 文件内容：原始字节；不进JSON序列化，避免日志/接口响应中出现整段二进制内容
}

func (this FileBlob) String() string {
	data, _ := json.Marshal(this)
	return string(data)
}
