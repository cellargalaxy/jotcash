package db

import (
	"context"

	"github.com/cellargalaxy/jotcash/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var migrateModels = []schema.Tabler{
	&model.Expense{},
	&model.OperationLog{},
	&model.FileMeta{},
	&model.FileBlob{},
}

func NewMigrateHandler() *MigrateHandler {
	handler := new(MigrateHandler)
	return handler
}

type MigrateHandler struct {
}

func (this *MigrateHandler) Exec(ctx context.Context, tx *gorm.DB) error {
	if tx == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("自动建表，连接为空")
		return errors.Errorf("自动建表，连接为空")
	}
	models := make([]any, 0, len(migrateModels))
	for i := range migrateModels {
		models = append(models, migrateModels[i])
	}
	err := tx.WithContext(ctx).AutoMigrate(models...)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("自动建表，异常")
		return errors.Errorf("自动建表，异常: %+v", err)
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{}).Info("自动建表，完成")
	return nil
}
