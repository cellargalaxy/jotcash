package model

import "gorm.io/gorm"

// AutoMigrate 根据本包全部实体 struct 建表/建列，供服务首次启动时调用。
//
// 只做“建表”这一件事（gorm.AutoMigrate 只新增缺失的表/列/索引，不删除、不修改已有列），
// 不包含任何数据迁移或业务判断，因此不违反本包“不含业务逻辑”的约束。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&Expense{}, &AuditLog{}, &FileMeta{}, &FileBlob{})
}
