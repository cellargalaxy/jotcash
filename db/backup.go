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

	//审计要跟着快照一起被导出去，所以先写原库再备份
	gormDb, err := open(ctx, dbPath, token)
	if err != nil {
		return err
	}
	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeDbExport,
		Summary:       "导出加密数据库快照",
		Result:        model.ResultSuccess,
	}
	err = gormDb.WithContext(ctx).Create(&operationLog).Error
	if err != nil {
		util.CloseDb(ctx, gormDb)
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("导出数据库，写审计异常")
		return errors.Errorf("导出数据库，写审计异常: %+v", err)
	}
	//备份要另开一条连接读同一个库文件，这条先关掉
	err = util.CloseDb(ctx, gormDb)
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
	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeDbImport,
		Summary:       "导入加密数据库，整库覆盖",
		Result:        model.ResultSuccess,
	}
	err = gormDb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		//库结构校验要排在建表前面，否则建表会把缺的表补出来，外来库就混过去了
		migrator := tx.Migrator()
		for i := range migrateModels {
			table := migrateModels[i].TableName()
			if migrator.HasTable(table) {
				continue
			}
			logrus.WithContext(ctx).WithFields(logrus.Fields{"backupPath": backupPath, "table": table}).Error("导入数据库，缺表")
			return errors.Errorf("导入数据库，缺表: %s", table)
		}
		//列的差异不算结构不合法，那正是建表要补的，否则旧版本导出的库就导不回来
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
		util.CloseDb(ctx, gormDb)
		util.RemoveFile(ctx, backupPath)
		return err
	}
	//副本马上要改名顶上去，连接先关掉，别让它攥着旧路径
	err = util.CloseDb(ctx, gormDb)
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

	err = replace(ctx, backupPath, token, dbPath)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
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

	//换口令就是拿新口令备份出一份副本，副本齐了再顶回去，中途出错原库一个字节不动
	backupPath, err := genBackupPath(ctx)
	if err != nil {
		return err
	}
	err = backup(ctx, dbPath, oldToken, backupPath, newToken)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

	//审计要落在换好口令的副本里，跟着副本一起顶上去
	gormDb, err := open(ctx, backupPath, newToken)
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
	err = gormDb.WithContext(ctx).Create(&operationLog).Error
	if err != nil {
		util.CloseDb(ctx, gormDb)
		util.RemoveFile(ctx, backupPath)
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("更换口令，写审计异常")
		return errors.Errorf("更换口令，写审计异常: %+v", err)
	}
	//副本马上要改名顶上去，连接先关掉，别让它攥着旧路径
	err = util.CloseDb(ctx, gormDb)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
	}

	err = replace(ctx, backupPath, newToken, dbPath)
	if err != nil {
		util.RemoveFile(ctx, backupPath)
		return err
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
	//备份文件名是GenId，定长16位，按字符串升序排出来就是按时间从旧到新
	sort.Strings(filenames)

	for i := 0; i < len(filenames)-limit; i++ {
		filePath := filepath.Join(backupPath, filenames[i])
		//删一个算一个，中间有删不掉的先记下来，最后再抛
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
