package db

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func genBackupPath(ctx context.Context) (string, error) {
	err := util.CreateFolderPath(ctx, config.DbBackupPath)
	if err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%d.db", util.GenId())
	backupPath := filepath.Join(config.DbBackupPath, filename)
	return backupPath, nil
}

func backup(ctx context.Context, srcPath, srcToken, dstPath, dstToken string) error {
	if dstToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("备份数据库，口令为空")
		return errors.Errorf("备份数据库，口令为空")
	}

	gormDb, err := open(ctx, srcPath, srcToken)
	if err != nil {
		return err
	}
	defer Close(ctx, gormDb)

	sqlDb, err := gormDb.DB()
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("备份数据库，获取连接异常")
		return errors.Errorf("备份数据库，获取连接异常: %+v", err)
	}
	conn, err := sqlDb.Conn(ctx)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("备份数据库，占用连接异常")
		return errors.Errorf("备份数据库，占用连接异常: %+v", err)
	}
	defer util.CloseIo(ctx, conn)

	query := url.Values{}
	query.Set("vfs", vfsName)
	query.Set("textkey", dstToken)
	dsn := fmt.Sprintf("file:%s?%s", dstPath, query.Encode())
	err = conn.Raw(func(driverConn any) error {
		sqliteConn, ok := driverConn.(driver.Conn)
		if !ok {
			return errors.Errorf("连接类型异常: %T", driverConn)
		}
		return sqliteConn.Raw().Backup("main", dsn)
	})
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dstPath": dstPath, "err": err}).Error("备份数据库，异常")
		return errors.Errorf("备份数据库，异常: %+v", err)
	}
	return nil
}

func replace(ctx context.Context, backupPath, dbPath, token string) error {
	gormDb, err := open(ctx, backupPath, token)
	if err != nil {
		return err
	}
	err = Close(ctx, gormDb)
	if err != nil {
		return err
	}

	err = os.Rename(backupPath, dbPath)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"backupPath": backupPath, "err": err}).Error("替换数据库，异常")
		return errors.Errorf("替换数据库，异常: %+v", err)
	}
	return nil
}

func Export(ctx context.Context, writer io.Writer) error {
	dbPath := config.DbPath
	token, err := getToken(ctx)
	if err != nil {
		return err
	}

	dbLock.RLock()
	defer dbLock.RUnlock()

	err = export(ctx, dbPath, token, writer)
	if err != nil {
		return err
	}
	return nil
}
func export(ctx context.Context, dbPath, token string, writer io.Writer) error {
	backupPath, err := genBackupPath(ctx)
	if err != nil {
		return err
	}
	defer util.RemoveFile(ctx, backupPath)

	err = backup(ctx, dbPath, token, backupPath, token)
	if err != nil {
		return err
	}

	file, err := util.OpenReadFile(ctx, backupPath)
	if err != nil {
		return err
	}
	defer util.CloseIo(ctx, file)
	_, err = io.Copy(writer, file)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("导出数据库，写出异常")
		return errors.Errorf("导出数据库，写出异常: %+v", err)
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{}).Info("导出数据库，完成")
	return nil
}

func Import(ctx context.Context, reader io.Reader) error {
	dbPath := config.DbPath
	token, err := getToken(ctx)
	if err != nil {
		return err
	}

	dbLock.Lock()
	defer dbLock.Unlock()

	err = import_(ctx, dbPath, token, reader)
	if err != nil {
		return err
	}
	return nil
}
func import_(ctx context.Context, dbPath, token string, reader io.Reader) error {
	backupPath, err := genBackupPath(ctx)
	if err != nil {
		return err
	}
	err = util.WriteReader2File(ctx, reader, backupPath)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

	err = replace(ctx, backupPath, dbPath, token)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}
	resetMigrate()
	logrus.WithContext(ctx).WithFields(logrus.Fields{}).Info("导入数据库，完成")
	return nil
}

func ChangeToken(ctx context.Context, newToken string) error {
	dbPath := config.DbPath
	token, err := getToken(ctx)
	if err != nil {
		return err
	}

	dbLock.Lock()
	defer dbLock.Unlock()

	err = changeToken(ctx, dbPath, token, newToken)
	if err != nil {
		return err
	}
	return nil
}
func changeToken(ctx context.Context, dbPath, oldToken, newToken string) error {
	backupPath, err := genBackupPath(ctx)
	if err != nil {
		return err
	}

	err = backup(ctx, dbPath, oldToken, backupPath, newToken)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

	err = replace(ctx, backupPath, dbPath, newToken)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{}).Info("更换口令，完成")
	return nil
}

func ClearBackup(ctx context.Context) error {
	backupPath := config.DbBackupPath

	dbLock.Lock()
	defer dbLock.Unlock()

	err := util.CreateFolderPath(ctx, backupPath)
	if err != nil {
		return err
	}
	files, err := util.ListFile(ctx, backupPath)
	if err != nil {
		return err
	}
	var filenames []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		filenames = append(filenames, file.Name())
	}
	limit := config.GetConfig().DbBackupLimit
	if len(filenames) <= limit {
		return nil
	}
	sort.Strings(filenames)
	toDelete := filenames[:len(filenames)-limit]
	for _, name := range toDelete {
		filePath := filepath.Join(backupPath, name)
		eee := util.RemoveFile(ctx, filePath)
		if eee != nil {
			err = eee
		}
	}
	return err
}
