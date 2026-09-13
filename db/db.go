package db

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/ncruces/go-sqlite3/gormlite"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

const dbKey = "db"

// 口令即密钥、逐请求开关库，连接不能进程级复用，由调用方开库后放进ctx透传
func SetDb(ctx context.Context, db *gorm.DB) context.Context {
	return util.SetCtxValue(ctx, dbKey, db)
}
func GetDb(ctx context.Context) *gorm.DB {
	return util.GetCtxValue[*gorm.DB](ctx, dbKey)
}

func Open(ctx context.Context, dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(gormlite.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger:         util.NewDefaultGormLog(),
	})
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("打开数据库，异常")
		return nil, errors.Errorf("打开数据库，异常: %+v", err)
	}
	return db, nil
}

func Close(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("关闭数据库，连接为空")
		return nil
	}
	sqlDb, err := db.DB()
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("关闭数据库，获取连接异常")
		return errors.Errorf("关闭数据库，获取连接异常: %+v", err)
	}
	err = sqlDb.Close()
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("关闭数据库，异常")
		return errors.Errorf("关闭数据库，异常: %+v", err)
	}
	return nil
}

func AutoMigrate(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("自动建表，连接为空")
		return errors.Errorf("自动建表，连接为空")
	}
	err := db.WithContext(ctx).AutoMigrate(&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{})
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("自动建表，异常")
		return errors.Errorf("自动建表，异常: %+v", err)
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{}).Info("自动建表，完成")
	return nil
}

func Transaction(ctx context.Context, handlers ...util.TransactionHandler) error {
	db := GetDb(ctx)
	if db == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("数据库事务，连接为空")
		return errors.Errorf("数据库事务，连接为空")
	}
	return util.Transaction(ctx, db, handlers...)
}
