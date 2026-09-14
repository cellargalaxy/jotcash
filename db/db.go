package db

import (
	"context"
	"fmt"
	"path"
	"sync"

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

var dbLock sync.RWMutex

func init() {
	ctx := util.GenCtx()
	err := Create(ctx)
	if err != nil {
		panic(err)
	}
}

func existDb(ctx context.Context, dbPath string) bool {
	info := util.GetFileInfo(ctx, dbPath)
	return info != nil && info.Size() > 0
}

func connect(ctx context.Context, dbPath, token string) (*gorm.DB, error) {
	if token == "" {
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

	sqlDb, err := driver.Open(fmt.Sprintf("file:%s?vfs=%s&_txlock=immediate", dbPath, vfsName), func(conn *sqlite3.Conn) error {
		err := conn.Exec(fmt.Sprintf("PRAGMA textkey=%s;", sqlite3.Quote(token)))
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

func Create(ctx context.Context) error {
	dbPath := config.DbPath
	token, err := tool.GenToken(ctx, tool.TokenLen)
	if err != nil {
		return err
	}

	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeSystemInit,
		Summary:       "系统初始化，创建加密数据库",
		Result:        model.ResultSuccess,
	}
	operationLogHandler := NewOperationLogInsertHandler(&operationLog)

	dbLock.Lock()
	defer dbLock.Unlock()

	err = create(ctx, dbPath, token, operationLogHandler)
	if err != nil {
		util.RemoveFile(ctx, dbPath)
		migrated = false
		return err
	}
	return nil
}
func create(ctx context.Context, dbPath, token string, handlers ...util.TransactionHandler) error {
	if existDb(ctx, dbPath) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Info("创建数据库，库文件已存在")
		return nil
	}

	gormDb, err := connect(ctx, dbPath, token)
	if err != nil {
		return err
	}
	defer Close(ctx, gormDb)

	err = AutoMigrate(ctx, gormDb)
	if err != nil {
		return err
	}
	if len(handlers) > 0 {
		err = util.Transaction(ctx, gormDb, handlers...)
		if err != nil {
			return err
		}
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{"clientToken": token}).Info("创建数据库，前端口令")
	logrus.WithContext(ctx).WithFields(logrus.Fields{"serverToken": config.GetConfig(ctx).ServerToken}).Info("创建数据库，后端口令")
	return nil
}

func Open(ctx context.Context) (*gorm.DB, error) {
	ctx = detachCtx(ctx)
	dbPath := config.DbPath
	token, err := getToken(ctx)
	if err != nil {
		return nil, err
	}
	err = autoMigrate(ctx, dbPath, token)
	if err != nil {
		return nil, err
	}

	dbLock.RLock()
	defer dbLock.RUnlock()

	gormDb, err := open(ctx, dbPath, token)
	if err != nil {
		return nil, err
	}
	return gormDb, nil
}
func open(ctx context.Context, dbPath, token string) (*gorm.DB, error) {
	if !existDb(ctx, dbPath) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Error("打开数据库，库文件不存在或为空")
		return nil, errors.Errorf("打开数据库，库文件不存在或为空")
	}

	gormDb, err := connect(ctx, dbPath, token)
	if err != nil {
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

func CheckToken(ctx context.Context) error {
	gormDb, err := Open(ctx)
	if err != nil {
		return err
	}
	return Close(ctx, gormDb)
}
func checkToken(ctx context.Context, dbPath, token string) error {
	gormDb, err := open(ctx, dbPath, token)
	if err != nil {
		return err
	}
	return Close(ctx, gormDb)
}

func Transaction(ctx context.Context, handlers ...util.TransactionHandler) error {
	ctx = detachCtx(ctx)
	dbPath := config.DbPath
	token, err := getToken(ctx)
	if err != nil {
		return err
	}
	err = autoMigrate(ctx, dbPath, token)
	if err != nil {
		return err
	}

	dbLock.RLock()
	defer dbLock.RUnlock()

	return transaction(ctx, dbPath, token, handlers...)
}
func transaction(ctx context.Context, dbPath, token string, handlers ...util.TransactionHandler) error {
	gormDb, err := open(ctx, dbPath, token)
	if err != nil {
		return err
	}
	defer Close(ctx, gormDb)
	return util.Transaction(ctx, gormDb, handlers...)
}
