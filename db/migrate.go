package db

import (
	"context"

	"github.com/cellargalaxy/jotcash/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

//todo,只在数据库创建或者数据库导入，也就是数据库第一次就绪的时候，才创建表，这时候是肯定有口令的，其余普通业务不在处理自动建表

var migrated bool

var migrateModels = []schema.Tabler{&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{}}

func AutoMigrate(ctx context.Context, gormDb *gorm.DB) error {
	if gormDb == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("自动建表，连接为空")
		return errors.Errorf("自动建表，连接为空")
	}
	models := make([]any, 0, len(migrateModels))
	for i := range migrateModels {
		models = append(models, migrateModels[i])
	}
	err := gormDb.WithContext(ctx).AutoMigrate(models...)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("自动建表，异常")
		return errors.Errorf("自动建表，异常: %+v", err)
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{}).Info("自动建表，完成")
	return nil
}

func checkSchema(ctx context.Context, gormDb *gorm.DB) error {
	if gormDb == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("校验库结构，连接为空")
		return errors.Errorf("校验库结构，连接为空")
	}
	migrator := gormDb.WithContext(ctx).Migrator()
	for i := range migrateModels {
		table := migrateModels[i].TableName()
		if migrator.HasTable(table) {
			continue
		}
		logrus.WithContext(ctx).WithFields(logrus.Fields{"table": table}).Error("校验库结构，缺表")
		return errors.Errorf("校验库结构，缺表: %s", table)
	}
	return nil
}

func autoMigrate(ctx context.Context, dbPath, token string) error {
	dbLock.RLock()
	done := migrated
	dbLock.RUnlock()
	if done {
		return nil
	}

	dbLock.Lock()
	defer dbLock.Unlock()

	if migrated {
		return nil
	}
	return migrate(ctx, dbPath, token)
}

func migrate(ctx context.Context, dbPath, token string) error {
	gormDb, err := open(ctx, dbPath, token)
	if err != nil {
		return err
	}
	defer Close(ctx, gormDb)

	err = AutoMigrate(ctx, gormDb)
	if err != nil {
		return err
	}
	migrated = true
	return nil
}
