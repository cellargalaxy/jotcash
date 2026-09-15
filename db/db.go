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

// 没有init：库在第一次开事务时才建出来，口令就是那次请求带的
func Create(ctx context.Context) error {
	dbPath := config.DbPath
	token, err := tool.GetToken(ctx)
	if err != nil {
		return err
	}

	info := util.GetFileInfo(ctx, dbPath)
	if info != nil && info.Size() > 0 {
		return nil
	}
	dbLock.Lock()
	defer dbLock.Unlock()
	info = util.GetFileInfo(ctx, dbPath)
	if info != nil && info.Size() > 0 {
		return nil
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Info("创建数据库")

	err = create(ctx, dbPath, token)
	if err != nil {
		return err
	}
	return nil
}
func create(ctx context.Context, dbPath, token string) error {
	info := util.GetFileInfo(ctx, dbPath)
	if info != nil && info.Size() > 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Error("创建数据库，库文件已存在")
		return errors.Errorf("创建数据库，库文件已存在")
	}
	if token == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("创建数据库，口令为空")
		return errors.Errorf("创建数据库，口令为空")
	}

	folderPath, _ := path.Split(dbPath)
	if folderPath != "" {
		err := util.CreateFolderPath(ctx, folderPath)
		if err != nil {
			return err
		}
	}
	err := util.WriteData2File(ctx, nil, dbPath)
	if err != nil {
		return err
	}
	db, err := open(ctx, dbPath, token)
	if err != nil {
		util.RemoveFile(ctx, dbPath)
		return err
	}
	defer util.CloseDb(ctx, db)

	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeSystemInit,
		Summary:       "系统初始化，创建加密数据库",
		Result:        model.ResultSuccess,
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := migrate(ctx, tx)
		if err != nil {
			return err
		}
		err = tx.Create(&operationLog).Error
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("创建数据库，写审计异常")
			return errors.Errorf("创建数据库，写审计异常: %+v", err)
		}
		return nil
	})
	if err != nil {
		util.CloseDb(ctx, db)
		util.RemoveFile(ctx, dbPath)
		return err
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{
		"dbPath":      dbPath,
		"serverToken": config.GetConfig(ctx).ServerToken,
		"clientToken": token,
	}).Info("创建数据库，口令")
	return nil
}

func Open(ctx context.Context) (*gorm.DB, error) {
	dbPath := config.DbPath
	token, err := tool.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	dbLock.RLock()
	defer dbLock.RUnlock()

	db, err := open(ctx, dbPath, token)
	if err != nil {
		return nil, err
	}
	return db, nil
}
func open(ctx context.Context, dbPath, token string) (*gorm.DB, error) {
	if util.GetFileInfo(ctx, dbPath) == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Error("打开数据库，库文件不存在")
		return nil, errors.Errorf("打开数据库，库文件不存在")
	}
	if token == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("连接数据库，口令为空")
		return nil, errors.Errorf("连接数据库，口令为空")
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
func CheckToken(ctx context.Context) error {
	db, err := Open(ctx)
	if err != nil {
		return err
	}
	return util.CloseDb(ctx, db)
}
func checkToken(ctx context.Context, dbPath, token string) error {
	db, err := open(ctx, dbPath, token)
	if err != nil {
		return err
	}
	return util.CloseDb(ctx, db)
}

func NewTransaction(ctx context.Context) (*DbTransaction, error) {
	dbPath := config.DbPath
	token, err := tool.GetToken(ctx)
	if err != nil {
		return nil, err
	}
	//库没就绪就地建出来，上层只管拿事务做业务读写，不感知建库建表
	err = Create(ctx)
	if err != nil {
		return nil, err
	}

	//建库自己要拿写锁，必须排在读锁之前，顺序反了同一条协程先读后写，RWMutex不可重入会自锁
	dbLock.RLock()
	object, err := newTransaction(ctx, dbPath, token)
	if err != nil {
		dbLock.RUnlock()
		return nil, err
	}
	return &DbTransaction{Transaction: object}, nil
}
func newTransaction(ctx context.Context, dbPath, token string) (*util.Transaction, error) {
	db, err := open(ctx, dbPath, token)
	if err != nil {
		return nil, err
	}
	object := util.NewTransaction(db)
	return object, nil
}

type DbTransaction struct {
	*util.Transaction
}

func (this *DbTransaction) Close(ctx context.Context) error {
	defer dbLock.RUnlock()
	return this.Transaction.Close(ctx)
}
