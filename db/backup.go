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
	"github.com/cellargalaxy/jotcash/tool"
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
	srcInfo := util.GetFileInfo(ctx, srcPath)
	if srcInfo == nil || srcInfo.Size() <= 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"srcPath": srcPath}).Error("备份数据库，来源文件不存在")
		return errors.Errorf("备份数据库，来源文件不存在")
	}
	if srcToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("备份数据库，来源口令为空")
		return errors.Errorf("备份数据库，来源口令为空")
	}
	dstInfo := util.GetFileInfo(ctx, dstPath)
	if dstInfo != nil && dstInfo.Size() > 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dstPath": dstPath}).Error("备份数据库，目标文件不存在")
		return errors.Errorf("备份数据库，目标文件不存在")
	}
	if dstToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("备份数据库，目标口令为空")
		return errors.Errorf("备份数据库，目标口令为空")
	}

	gormDb, err := open(ctx, srcPath, srcToken)
	if err != nil {
		return err
	}
	defer util.CloseDb(ctx, gormDb)

	sqlDb, err := gormDb.DB()
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("备份数据库，获取连接异常")
		return errors.Errorf("备份数据库，获取连接异常: %+v", err)
	}
	defer util.CloseIo(ctx, sqlDb)

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

func replace(ctx context.Context, srcPath, srcToken, dstPath string) error {
	err := checkToken(ctx, srcPath, srcToken)
	if err != nil {
		return err
	}
	err = os.Rename(srcPath, dstPath)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"srcPath": srcPath, "dstPath": dstPath, "err": err}).Error("替换数据库，异常")
		return errors.Errorf("替换数据库，异常: %+v", err)
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"srcPath": srcPath, "dstPath": dstPath}).Info("替换数据库，完成")
	return nil
}

func Export(ctx context.Context, writer io.Writer, handlers ...util.TransactionHandler) error {
	dbPath := config.DbPath
	token, err := tool.GetToken(ctx)
	if err != nil {
		return err
	}

	dbLock.RLock()
	defer dbLock.RUnlock()

	err = export(ctx, dbPath, token, writer, handlers...)
	if err != nil {
		return err
	}
	return nil
}
func export(ctx context.Context, dbPath, token string, writer io.Writer, handlers ...util.TransactionHandler) error {
	if len(handlers) > 0 {
		object, err := newTransaction(ctx, dbPath, token)
		if err != nil {
			return err
		}
		defer object.Close(ctx)
		err = object.AddCommit(handlers...).Exec(ctx)
		if err != nil {
			return err
		}
	}

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

func Import(ctx context.Context, reader io.Reader, handlers ...util.TransactionHandler) error {
	dbPath := config.DbPath
	token, err := tool.GetToken(ctx)
	if err != nil {
		return err
	}

	dbLock.Lock()
	defer dbLock.Unlock()

	err = import_(ctx, dbPath, token, reader, handlers...)
	if err != nil {
		return err
	}
	return nil
}
func import_(ctx context.Context, dbPath, token string, reader io.Reader, handlers ...util.TransactionHandler) error {
	//导入的是调用方自带的库，只校验它自身能打开拦不住越权：拿到后端口令就能签出jwt，再用自己加密的库把原库顶掉
	err := checkToken(ctx, dbPath, token)
	if err != nil {
		return err
	}

	backupPath, err := genBackupPath(ctx)
	if err != nil {
		return err
	}
	err = util.WriteReader2File(ctx, reader, backupPath)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

	//校验、补表、写审计都落在上传的这份副本上，它才是待会儿要顶上来的库
	gormDb, err := open(ctx, backupPath, token)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}
	defer util.CloseDb(ctx, gormDb)

	//库结构校验要排在建表前面，否则建表会把缺的表补出来，外来库就混过去了
	err = util.NewTransaction(gormDb).
		AddCommit(NewSchemaCheckHandler()).
		AddCommit(NewMigrateHandler()).
		AddCommit(handlers...).
		Exec(ctx)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

	//整库覆盖不可逆，原库先留一份，导错了还能拿它换回来
	originPath, err := genBackupPath(ctx)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}
	err = backup(ctx, dbPath, token, originPath, token)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

	err = replace(ctx, backupPath, dbPath, token)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{}).Info("导入数据库，完成")
	return nil
}

func ChangeToken(ctx context.Context, newToken string, handlers ...util.TransactionHandler) error {
	dbPath := config.DbPath
	token, err := tool.GetToken(ctx)
	if err != nil {
		return err
	}

	dbLock.Lock()
	defer dbLock.Unlock()

	err = changeToken(ctx, dbPath, token, newToken, handlers...)
	if err != nil {
		return err
	}
	return nil
}
func changeToken(ctx context.Context, dbPath, oldToken, newToken string, handlers ...util.TransactionHandler) error {
	backupPath, err := genBackupPath(ctx)
	if err != nil {
		return err
	}

	err = backup(ctx, dbPath, oldToken, backupPath, newToken)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

	if len(handlers) > 0 {
		object, err := newTransaction(ctx, backupPath, newToken)
		if err != nil {
			util.RemoveFile(ctx, backupPath)
			return err
		}
		defer object.Close(ctx)
		err = object.AddCommit(handlers...).Exec(ctx)
		if err != nil {
			util.RemoveFile(ctx, backupPath)
			return err
		}
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
	limit := config.GetConfig(ctx).DbBackupLimit

	dbLock.Lock()
	defer dbLock.Unlock()

	err := clearBackup(ctx, backupPath, limit)
	if err != nil {
		return err
	}
	return nil
}
func clearBackup(ctx context.Context, backupPath string, limit int) error {
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
