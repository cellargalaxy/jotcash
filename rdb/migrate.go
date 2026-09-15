package rdb

import (
	"context"
	"fmt"

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

var migrateTriggers = []struct {
	Table  string //挂触发器的表
	Reason string //RAISE(ABORT)抛出去的原因，会原样出现在报错里
}{
	{Table: model.Expense{}.TableName(), Reason: "明细只能软删除，禁止物理删除"},
	{Table: model.OperationLog{}.TableName(), Reason: "审计不可删除，禁止物理删除"},
	{Table: model.FileMeta{}.TableName(), Reason: "文件不可删除，禁止物理删除"},
	{Table: model.FileBlob{}.TableName(), Reason: "文件不可删除，禁止物理删除"},
}

func NewMigrateHandler() *MigrateHandler {
	handler := new(MigrateHandler)
	return handler
}

type MigrateHandler struct {
}

func (this *MigrateHandler) Exec(ctx context.Context, tx *gorm.DB) error {
	return migrate(ctx, tx)
}

func migrate(ctx context.Context, tx *gorm.DB) error {
	models := make([]any, 0, len(migrateModels))
	for i := range migrateModels {
		models = append(models, migrateModels[i])
	}
	err := tx.WithContext(ctx).AutoMigrate(models...)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("自动建表，异常")
		return errors.Errorf("自动建表，异常: %+v", err)
	}

	for i := range migrateTriggers {
		table, reason := migrateTriggers[i].Table, migrateTriggers[i].Reason
		sql := fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS `%s_forbid_delete` BEFORE DELETE ON `%s` BEGIN SELECT RAISE(ABORT, '%s'); END;", table, table, reason)
		err := tx.WithContext(ctx).Exec(sql).Error
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"table": table, "err": err}).Error("建防删触发器，异常")
			return errors.Errorf("建防删触发器，异常: %+v", err)
		}
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{}).Info("自动建表，完成")
	return nil
}
