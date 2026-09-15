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

func NewCreateDbHandler(dbPath, token string) *CreateDbHandler {
	handler := new(CreateDbHandler)
	handler.dbPath = dbPath
	handler.token = token
	return handler
}

type CreateDbHandler struct {
	dbPath string
	token  string
}

func (this *CreateDbHandler) Exec(ctx context.Context, tx *gorm.DB) error {
	if existDb(ctx, this.dbPath) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": this.dbPath}).Warn("创建数据库，库文件已存在")
		return nil
	}

	db, err := connect(ctx, this.dbPath, this.token)
	if err != nil {
		return err
	}
	defer util.CloseDb(ctx, db)
	return nil
}

func NewDbRemoveHandler(dbPath string) *DbRemoveHandler {
	handler := new(DbRemoveHandler)
	handler.dbPath = dbPath
	return handler
}

type DbRemoveHandler struct {
	dbPath string
}

func (this *DbRemoveHandler) Exec(ctx context.Context, tx *gorm.DB) error {
	logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": this.dbPath}).Info("删除数据库")
	err := util.RemoveFile(ctx, this.dbPath)
	if err != nil {
		return err
	}
	return nil
}

func NewTokenLogHandler(dbPath, token string) *CreateDbHandler {
	handler := new(CreateDbHandler)
	handler.dbPath = dbPath
	handler.token = token
	return handler
}

type TokenLogHandler struct {
	dbPath string
	token  string
}

func (this *TokenLogHandler) Exec(ctx context.Context, tx *gorm.DB) error {
	logrus.WithContext(ctx).WithFields(logrus.Fields{
		"dbPath":      this.dbPath,
		"serverToken": config.GetConfig(ctx).ServerToken,
		"clientToken": this.token,
	}).Info("创建数据库，口令")
	return nil
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

	return create(ctx, dbPath, token, operationLogHandler)
}
func create(ctx context.Context, dbPath, token string, handlers ...util.TransactionHandler) error {
	if existDb(ctx, dbPath) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Info("创建数据库，库文件已存在")
		return nil
	}

	gormDb, err := connect(ctx, dbPath, token)
	if err != nil {
		//连接是在事务之前建的库文件，它的残骸只能在事务之外清
		util.RemoveFile(ctx, dbPath)
		return err
	}
	defer util.CloseDb(ctx, gormDb)

	//建表与调用方的handler同处一个提交链，回滚链负责把没建成的库文件删掉
	err = util.NewTransaction(gormDb).
		AddCommit(NewMigrateHandler()).
		AddCommit(handlers...).
		AddRollback(NewDbRemoveHandler(dbPath)).
		Exec(ctx)
	if err != nil {
		return err
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{"clientToken": token}).Info("创建数据库，前端口令")
	logrus.WithContext(ctx).WithFields(logrus.Fields{"serverToken": config.GetConfig(ctx).ServerToken}).Info("创建数据库，后端口令")
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

func NewTransaction(ctx context.Context) (*util.Transaction, error) {
	dbPath := config.DbPath
	token, err := tool.GetToken(ctx)
	if err != nil {
		return nil, err
	}
	object, err := newTransaction(ctx, dbPath, token)
	if err != nil {
		return nil, err
	}
	return object, nil
}
func newTransaction(ctx context.Context, dbPath, token string) (*util.Transaction, error) {
	db, err := open(ctx, dbPath, token)
	if err != nil {
		return nil, err
	}
	object := util.NewTransaction(db)
	return object, nil
}
