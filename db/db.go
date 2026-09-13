package db

import (
	"context"
	"fmt"
	"path"
	"strings"

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

var likeReplacer = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func Open(ctx context.Context, clientToken string) (*gorm.DB, error) {
	if clientToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("打开数据库，口令为空")
		return nil, errors.Errorf("打开数据库，口令为空")
	}

	dbPath := config.Config.DbPath
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
		return conn.Exec("PRAGMA temp_store=memory;")
	})
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath, "err": err}).Error("打开数据库，异常")
		return nil, errors.Errorf("打开数据库，异常: %+v", err)
	}

	//driver.Open是懒连接，Ping也不读文件，读一次库头才能判断口令对不对；口令错与文件损坏不可区分，统一提示
	var version int
	err = sqlDb.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	if err != nil {
		util.CloseIo(ctx, sqlDb)
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath, "err": err}).Error("打开数据库，口令错误或数据库文件损坏")
		return nil, errors.Errorf("打开数据库，口令错误或数据库文件损坏")
	}

	gormDb, err := gorm.Open(gormlite.OpenDB(sqlDb), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger:         util.NewDefaultGormLog(),
	})
	if err != nil {
		util.CloseIo(ctx, sqlDb)
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath, "err": err}).Error("打开数据库，gorm初始化异常")
		return nil, errors.Errorf("打开数据库，gorm初始化异常: %+v", err)
	}
	return gormDb, nil
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

// 逐请求开库关库：口令在Claims里，连接在db包内部开关，上层不感知
func Transaction(ctx context.Context, handlers ...util.TransactionHandler) error {
	claims := tool.GetClaims(ctx)
	if claims == nil || claims.ClientToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("数据库事务，口令为空")
		return errors.Errorf("数据库事务，口令为空")
	}
	db, err := Open(ctx, claims.ClientToken)
	if err != nil {
		return err
	}
	defer Close(ctx, db)
	return util.Transaction(ctx, db, handlers...)
}

func likeValue(value string) string {
	return fmt.Sprintf("%%%s%%", likeReplacer.Replace(value))
}

func pageLimit(ctx context.Context, tx *gorm.DB, page, pageSize int) (*gorm.DB, error) {
	if pageSize <= 0 {
		return tx, nil
	}
	offset := (page - 1) * pageSize
	tx = tx.Offset(offset).Limit(pageSize)
	return tx, nil
}

// 排序由上层直接传字符串，这里按白名单换成SQL片段，不在白名单的一律报错，杜绝拼接注入
func sortOrder(ctx context.Context, tx *gorm.DB, sortMap map[string]string, sort, defaultSort string) (*gorm.DB, error) {
	if sort == "" {
		sort = defaultSort
	}
	order := sortMap[sort]
	if order == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"sort": sort}).Error("排序，不在白名单内")
		return tx, errors.Errorf("排序，不在白名单内: %s", sort)
	}
	return tx.Order(order), nil
}
