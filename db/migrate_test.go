package db_test

import (
	"testing"

	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/ncruces/go-sqlite3/gormlite"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAutoMigrate(t *testing.T) {
	dsn := memdb.TestDB(t)
	gormDB, err := gorm.Open(gormlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}

	if err := db.AutoMigrate(gormDB); err != nil {
		t.Fatalf("自动建表失败: %v", err)
	}

	for _, entity := range []any{&model.Expense{}, &model.AuditLog{}, &model.FileMeta{}, &model.FileBlob{}} {
		if !gormDB.Migrator().HasTable(entity) {
			t.Errorf("表未建出: %T", entity)
		}
	}

	// 服务重启会再跑一次 AutoMigrate，需保证幂等（不报错、不重复建表）
	if err := db.AutoMigrate(gormDB); err != nil {
		t.Errorf("重复调用AutoMigrate应当幂等，实际报错: %v", err)
	}
}
