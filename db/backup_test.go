package db

import (
	"bytes"
	"sync"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
)

func TestExport(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestExpense()
	if _, err := InsertExpense(ctx, expense); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	buffer := new(bytes.Buffer)
	if err := Export(ctx, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}
	if buffer.Len() == 0 {
		t.Fatalf("导出内容为空")
	}
	if bytes.Contains(buffer.Bytes(), []byte("SQLite format 3")) {
		t.Errorf("导出的是明文库，加密没生效")
	}
	if bytes.Contains(buffer.Bytes(), []byte(expense.Counterparty)) {
		t.Errorf("导出内容里能直接搜到业务数据，加密没生效")
	}

	if err := Import(ctx, bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("导回异常: %+v", err)
	}
	dbLock.Lock()
	done := migrated
	dbLock.Unlock()
	if !done {
		t.Errorf("导入后应在写锁内把表结构补齐")
	}

	objects, count, err := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}})
	if err != nil {
		t.Fatalf("导回后查询异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || !objects[0].ExpenseAmount.Equal(expense.ExpenseAmount) {
		t.Errorf("导回后的数据不符: count=%d %+v", count, objects)
	}
}

func TestImportOldDb(t *testing.T) {
	ctx := newTestCtx(t)
	if _, err := InsertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	//造一个同口令、一张表都没有的库文件，冒充旧版本导出的库
	oldPath := "resource/old.db"
	gormDb, err := connect(ctx, oldPath, testClientToken)
	if err != nil {
		t.Fatalf("建旧库异常: %+v", err)
	}
	if err = gormDb.Exec("PRAGMA user_version=1").Error; err != nil {
		t.Fatalf("写旧库异常: %+v", err)
	}
	Close(ctx, gormDb)
	data, err := util.ReadFile2Data(ctx, oldPath, nil)
	if err != nil {
		t.Fatalf("读旧库异常: %+v", err)
	}
	util.RemoveFile(ctx, oldPath)

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				//建表标记要是在锁外从true翻回false，这里就会撞上缺表
				if _, _, err := SelectExpense(ctx, model.ExpenseInquiry{}); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 5; j++ {
			if err := Import(ctx, bytes.NewReader(data)); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("并发导入旧库异常: %+v", err)
	}
}

func TestTokenSpecialChar(t *testing.T) {
	newTestDb(t)
	//口令里的空格经url编码会变成加号，而SQLite的uri只认%XX转义，备份目标库会落到另一把口令上
	token := "pass word 1234"
	ctx := newTokenCtx(token)
	if err := create(ctx, config.DbPath, token); err != nil {
		t.Fatalf("建库异常: %+v", err)
	}
	expense := newTestExpense()
	if _, err := InsertExpense(ctx, expense); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	buffer := new(bytes.Buffer)
	if err := Export(ctx, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}
	if err := Import(ctx, bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("导出的库用同一口令导不回来: %+v", err)
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 1 {
		t.Errorf("导回后数据不符: count=%d want=1", count)
	}

	newToken := "new pass word 1234"
	if err := ChangeToken(ctx, newToken); err != nil {
		t.Fatalf("换口令异常: %+v", err)
	}
	if err := CheckToken(newTokenCtx(newToken)); err != nil {
		t.Errorf("换成含空格的口令后打不开库: %+v", err)
	}
}

func TestExportWrongToken(t *testing.T) {
	newTestCtx(t)

	buffer := new(bytes.Buffer)
	if err := Export(newTokenCtx("wrong-client-token"), buffer); err == nil {
		t.Errorf("错误口令导出应报错")
	}
}

func TestChangeToken(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestExpense()
	if _, err := InsertExpense(ctx, expense); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	if err := ChangeToken(newTokenCtx("wrong-client-token"), "new-client-token-1"); err == nil {
		t.Errorf("旧口令错误应报错")
	}
	if err := ChangeToken(ctx, ""); err == nil {
		t.Errorf("新口令为空应报错")
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Fatalf("失败的换口令不应影响原库: count=%d", count)
	}

	newToken := "new-client-token-1"
	if err := ChangeToken(ctx, newToken); err != nil {
		t.Fatalf("换口令异常: %+v", err)
	}

	if err := CheckToken(ctx); err == nil {
		t.Errorf("换口令后旧口令应打不开库")
	}
	newCtx := newTokenCtx(newToken)
	objects, count, err := SelectExpense(newCtx, model.ExpenseInquiry{Id: []int64{expense.Id}})
	if err != nil {
		t.Fatalf("换口令后查询异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || objects[0].Counterparty != expense.Counterparty {
		t.Errorf("换口令后数据不符: count=%d %+v", count, objects)
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) > 0 {
		t.Errorf("备份文件没清理: %d", len(files))
	}
}

// 换口令时附带的写入落在换好口令的副本里，写失败就整个放弃，原库一个字节不动
func TestChangeTokenHandlerFail(t *testing.T) {
	ctx := newTestCtx(t)
	operationLog := &model.OperationLog{Id: util.GenId(), OperationType: model.OperationTypeDataEntry, Result: model.ResultSuccess}
	if _, err := InsertOperationLog(ctx, operationLog); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	newToken := "new-client-token-1"
	//主键撞车，副本里的事务必失败
	if err := ChangeToken(ctx, newToken, NewOperationLogInsertHandler(&model.OperationLog{Id: operationLog.Id})); err == nil {
		t.Fatalf("副本写入失败时应报错")
	}
	if err := CheckToken(ctx); err != nil {
		t.Errorf("失败后原口令应照常可用: %+v", err)
	}
	if err := CheckToken(newTokenCtx(newToken)); err == nil {
		t.Errorf("失败后新口令不应能打开")
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) > 0 {
		t.Errorf("失败的副本没清理: %d", len(files))
	}
}

func TestImportIllegal(t *testing.T) {
	ctx := newTestCtx(t)
	if _, err := InsertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	buffer := new(bytes.Buffer)
	if err := Export(ctx, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}

	if err := Import(newTokenCtx("wrong-client-token"), bytes.NewReader(buffer.Bytes())); err == nil {
		t.Errorf("口令不匹配的库应拒绝导入")
	}
	if err := Import(ctx, bytes.NewReader([]byte("我不是数据库"))); err == nil {
		t.Errorf("非数据库文件应拒绝导入")
	}
	if err := Import(ctx, bytes.NewReader(nil)); err == nil {
		t.Errorf("0字节文件应拒绝导入")
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Errorf("导入失败不应影响原库: count=%d", count)
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) > 0 {
		t.Errorf("导入失败没清理临时文件: %d", len(files))
	}
}

func TestClearBackup(t *testing.T) {
	ctx := newTestCtx(t)
	limit := config.GetConfig().DbBackupLimit

	var backupPaths []string
	for i := 0; i < limit+2; i++ {
		backupPath, err := genBackupPath(ctx)
		if err != nil {
			t.Fatalf("生成备份路径异常: %+v", err)
		}
		if err = util.WriteData2File(ctx, []byte("backup"), backupPath); err != nil {
			t.Fatalf("写备份文件异常: %+v", err)
		}
		backupPaths = append(backupPaths, backupPath)
	}

	if err := ClearBackup(ctx); err != nil {
		t.Fatalf("清理备份异常: %+v", err)
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) != limit {
		t.Errorf("备份保留数量: got=%d want=%d", len(files), limit)
	}
	for i := range backupPaths {
		exist := util.GetPathInfo(ctx, backupPaths[i]) != nil
		if i < 2 && exist {
			t.Errorf("旧备份未清理: %s", backupPaths[i])
		}
		if i >= 2 && !exist {
			t.Errorf("新备份不应清理: %s", backupPaths[i])
		}
	}
}
