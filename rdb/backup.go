package rdb

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
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
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
	if util.GetFileInfo(ctx, srcPath) == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"srcPath": srcPath}).Error("备份数据库，来源文件不存在")
		return errors.Errorf("备份数据库，来源文件不存在")
	}
	if srcToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("备份数据库，来源口令为空")
		return errors.Errorf("备份数据库，来源口令为空")
	}
	if util.GetFileInfo(ctx, dstPath) != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dstPath": dstPath}).Error("备份数据库，目标文件已存在")
		return errors.Errorf("备份数据库，目标文件已存在")
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

func Export(ctx context.Context, writer io.Writer) error {
	dbPath := config.DbPath
	token, err := tool.GetToken(ctx)
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
	if util.GetFileInfo(ctx, dbPath) == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Error("导出数据库，库文件不存在")
		return errors.Errorf("导出数据库，库文件不存在")
	}
	if token == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("导出数据库，口令为空")
		return errors.Errorf("导出数据库，口令为空")
	}
	if writer == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("导出数据库，写出目标为空")
		return errors.Errorf("导出数据库，写出目标为空")
	}

	db, err := open(ctx, dbPath, token) //包含token合法性校验，避免越权
	if err != nil {
		return err
	}
	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeDbExport,
		Summary:       "导出加密数据库快照",
		Result:        model.ResultSuccess,
	}
	err = db.WithContext(ctx).Create(&operationLog).Error
	if err != nil {
		util.CloseDb(ctx, db)
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("导出数据库，写审计异常")
		return errors.Errorf("导出数据库，写审计异常: %+v", err)
	}
	err = util.CloseDb(ctx, db)
	if err != nil {
		return err
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

	logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Info("导出数据库，完成")
	return nil
}

func Import(ctx context.Context, reader io.Reader) error {
	dbPath := config.DbPath
	token, err := tool.GetToken(ctx)
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
	if util.GetFileInfo(ctx, dbPath) == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Error("导入数据库，库文件不存在")
		return errors.Errorf("导入数据库，库文件不存在")
	}
	if token == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("导入数据库，口令为空")
		return errors.Errorf("导入数据库，口令为空")
	}
	if reader == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("导入数据库，读入来源为空")
		return errors.Errorf("导入数据库，读入来源为空")
	}

	err := checkToken(ctx, dbPath, token) //包含token合法性校验，避免越权
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

	db, err := open(ctx, backupPath, token)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}
	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeDbImport,
		Summary:       "导入加密数据库，整库覆盖",
		Result:        model.ResultSuccess,
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		migrator := tx.Migrator()
		for i := range migrateModels {
			table := migrateModels[i].TableName()
			if migrator.HasTable(table) {
				continue
			}
			logrus.WithContext(ctx).WithFields(logrus.Fields{"backupPath": backupPath, "table": table}).Error("导入数据库，缺表")
			return errors.Errorf("导入数据库，缺表: %s", table)
		}
		err := migrate(ctx, tx)
		if err != nil {
			return err
		}
		err = tx.Create(&operationLog).Error
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("导入数据库，写审计异常")
			return errors.Errorf("导入数据库，写审计异常: %+v", err)
		}
		return nil
	})
	if err != nil {
		util.CloseDb(ctx, db)
		util.RemoveFile(ctx, backupPath)
		return err
	}
	err = util.CloseDb(ctx, db)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

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

	err = os.Rename(backupPath, dbPath)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("导入数据库，替换异常")
		return errors.Errorf("导入数据库，替换异常: %+v", err)
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath, "originPath": originPath}).Info("导入数据库，完成")
	return nil
}

func ChangeToken(ctx context.Context, newToken string) error {
	dbPath := config.DbPath
	token, err := tool.GetToken(ctx)
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
	if util.GetFileInfo(ctx, dbPath) == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Error("更换口令，库文件不存在")
		return errors.Errorf("更换口令，库文件不存在")
	}
	if oldToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("更换口令，旧口令为空")
		return errors.Errorf("更换口令，旧口令为空")
	}
	if newToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("更换口令，新口令为空")
		return errors.Errorf("更换口令，新口令为空")
	}

	backupPath, err := genBackupPath(ctx)
	if err != nil {
		return err
	}
	err = backup(ctx, dbPath, oldToken, backupPath, newToken) //包含token合法性校验，避免越权
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

	db, err := open(ctx, backupPath, newToken)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}
	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeClientTokenSwap,
		Summary:       "数据库已用新口令重新加密",
		Result:        model.ResultSuccess,
	}
	err = db.WithContext(ctx).Create(&operationLog).Error
	if err != nil {
		util.CloseDb(ctx, db)
		util.RemoveFile(ctx, backupPath)
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("更换口令，写审计异常")
		return errors.Errorf("更换口令，写审计异常: %+v", err)
	}
	err = util.CloseDb(ctx, db)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

	err = os.Rename(backupPath, dbPath)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("更换口令，替换异常")
		return errors.Errorf("更换口令，替换异常: %+v", err)
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{"dbPath": dbPath}).Info("更换口令，完成")
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
	if backupPath == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("清理备份，备份目录为空")
		return errors.Errorf("清理备份，备份目录为空")
	}
	if limit <= 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"limit": limit}).Error("清理备份，保留数量非法")
		return errors.Errorf("清理备份，保留数量非法: %d", limit)
	}

	err := util.CreateFolderPath(ctx, backupPath)
	if err != nil {
		return err
	}
	files, err := util.ListFile(ctx, backupPath)
	if err != nil {
		return err
	}
	filenames := make([]string, 0, len(files))
	for i := range files {
		if files[i].IsDir() {
			continue
		}
		filenames = append(filenames, files[i].Name())
	}
	if len(filenames) <= limit {
		return nil
	}
	sort.Strings(filenames)

	for i := 0; i < len(filenames)-limit; i++ {
		filePath := filepath.Join(backupPath, filenames[i])
		eee := util.RemoveFile(ctx, filePath)
		if eee != nil {
			err = eee
		}
	}
	if err != nil {
		return err
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{"backupPath": backupPath, "count": len(filenames) - limit}).Info("清理备份，完成")
	return nil
}
