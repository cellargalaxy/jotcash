package db

import (
	"context"
	"fmt"
	"path"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/gormlite"
	_ "github.com/ncruces/go-sqlite3/vfs/adiantum"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

const vfsName = "adiantum"

func init() {
	ctx := util.GenCtx()
	dbPath := config.DbPath
	err := Init(ctx, dbPath)
	if err != nil {
		util.RemoveFile(ctx, dbPath)
		resetMigrate()
		panic(err)
	}
}

func getToken(ctx context.Context) (string, error) {
	claims := tool.GetClaims(ctx)
	if claims == nil || claims.ClientToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("获取数据库口令，为空")
		return "", errors.Errorf("获取数据库口令，为空")
	}
	return claims.ClientToken, nil
}

func connect(ctx context.Context, dbPath, clientToken string) (*gorm.DB, error) {
	if clientToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("连接数据库，口令为空")
		return nil, errors.Errorf("连接数据库，口令为空")
	}
	folderPath, _ := path.Split(dbPath)
	if folderPath != "" {
		err := util.CreateFolderPath(ctx, folderPath)
		if err != nil {
			return nil, err
		}
	}

	sqlDb, err := driver.Open(fmt.Sprintf("file:%s?vfs=%s", dbPath, vfsName), func(conn *sqlite3.Conn) error {
		err := conn.Exec(fmt.Sprintf("PRAGMA textkey=%s;", sqlite3.Quote(clientToken)))
		if err != nil {
			return err
		}
		return conn.Exec("PRAGMA temp_store=memory;PRAGMA busy_timeout=10000;")
	})
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath, "err": err}).Error("连接数据库，异常")
		return nil, errors.Errorf("连接数据库，异常: %+v", err)
	}

	var version int
	err = sqlDb.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	if err != nil {
		util.CloseIo(ctx, sqlDb)
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath, "err": err}).Error("连接数据库，口令错误或数据库文件损坏")
		return nil, errors.Errorf("连接数据库，口令错误或数据库文件损坏")
	}

	gormDb, err := gorm.Open(gormlite.OpenDB(sqlDb), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger:         util.NewDefaultGormLog(),
	})
	if err != nil {
		util.CloseIo(ctx, sqlDb)
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath, "err": err}).Error("连接数据库，gorm初始化异常")
		return nil, errors.Errorf("连接数据库，gorm初始化异常: %+v", err)
	}
	return gormDb, nil
}

func Init(ctx context.Context, dbPath string) error {
	if util.GetPathInfo(ctx, dbPath) != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Info("初始化数据库，库文件已存在")
		return nil
	}

	clientToken, err := tool.GenToken(ctx, tool.TokenLen)
	if err != nil {
		return err
	}
	gormDb, err := connect(ctx, dbPath, clientToken)
	if err != nil {
		return err
	}
	defer Close(ctx, gormDb)

	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeSystemInit,
		Summary:       "系统初始化，创建加密数据库",
		Result:        model.ResultSuccess,
	}
	err = util.Transaction(ctx, gormDb, NewOperationLogInsertHandler(&operationLog))
	if err != nil {
		return err
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{"clientToken": clientToken}).Warn("系统初始化，前端口令")
	logrus.WithContext(ctx).WithFields(logrus.Fields{"serverToken": config.Config.ServerToken}).Warn("系统初始化，后端口令")
	return nil
}

func Open(ctx context.Context, dbPath string) (*gorm.DB, error) {
	if util.GetPathInfo(ctx, dbPath) == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Info("打开数据库，库文件不存在")
		return nil, nil
	}

	clientToken, err := getToken(ctx)
	if err != nil {
		return nil, err
	}
	gormDb, err := connect(ctx, dbPath, clientToken)
	if err != nil {
		return nil, err
	}

	err = autoMigrate(ctx, gormDb)
	if err != nil {
		Close(ctx, gormDb)
		return nil, err
	}

	return gormDb, nil
}

func Close(ctx context.Context, gormDb *gorm.DB) error {
	if gormDb == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("关闭数据库，连接为空")
		return nil
	}
	sqlDb, err := gormDb.DB()
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

func CheckClientToken(ctx context.Context) error {
	gormDb, err := Open(ctx, config.DbPath)
	if err != nil {
		return err
	}
	return Close(ctx, gormDb)
}

func Transaction(ctx context.Context, handlers ...util.TransactionHandler) error {
	gormDb, err := Open(ctx, config.DbPath)
	if err != nil {
		return err
	}
	defer Close(ctx, gormDb)
	return util.Transaction(ctx, gormDb, handlers...)
}
