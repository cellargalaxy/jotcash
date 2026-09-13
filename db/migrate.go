package db

import (
	"context"
	"sync"

	"github.com/cellargalaxy/jotcash/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var migrateLock sync.Mutex
var migrated bool

func AutoMigrate(ctx context.Context, gormDb *gorm.DB) error {
	if gormDb == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("自动建表，连接为空")
		return errors.Errorf("自动建表，连接为空")
	}
	err := gormDb.WithContext(ctx).AutoMigrate(&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{})
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("自动建表，异常")
		return errors.Errorf("自动建表，异常: %+v", err)
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{}).Info("自动建表，完成")
	return nil
}

func autoMigrate(ctx context.Context, gormDb *gorm.DB) error {
	migrateLock.Lock()
	defer migrateLock.Unlock()

	if migrated {
		return nil
	}
	err := AutoMigrate(ctx, gormDb)
	if err != nil {
		return err
	}
	migrated = true
	return nil
}

func resetMigrate() {
	migrateLock.Lock()
	defer migrateLock.Unlock()

	migrated = false
}
