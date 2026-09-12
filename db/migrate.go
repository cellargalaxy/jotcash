// Package db 实现无业务逻辑的数据库增删查改，输入输出统一复用 model 包内的 struct。
package db

import (
	"github.com/cellargalaxy/jotcash/model"
	"gorm.io/gorm"
)

// AutoMigrate 根据 model 包全部实体 struct 建表/建列，供服务首次启动时调用。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&model.Expense{}, &model.AuditLog{}, &model.FileMeta{}, &model.FileBlob{})
}
