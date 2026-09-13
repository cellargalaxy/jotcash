package db

import (
	"bytes"
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
	reset := !migrated
	dbLock.Unlock()
	if !reset {
		t.Errorf("导入后应复位建表标记")
	}

	objects, count, err := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}})
	if err != nil {
		t.Fatalf("导回后查询异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || !objects[0].ExpenseAmount.Equal(expense.ExpenseAmount) {
		t.Errorf("导回后的数据不符: count=%d %+v", count, objects)
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
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Errorf("导入失败不应影响原库: count=%d", count)
	}
}

func TestClearBackup(t *testing.T) {
	ctx := newTestCtx(t)
	originLimit := config.Config.DbBackupLimit
	t.Cleanup(func() { config.Config.DbBackupLimit = originLimit })
	config.Config.DbBackupLimit = 2

	var backupPaths []string
	for i := 0; i < 5; i++ {
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
	if len(files) != config.Config.DbBackupLimit {
		t.Errorf("备份保留数量: got=%d want=%d", len(files), config.Config.DbBackupLimit)
	}
	for i := range backupPaths {
		exist := util.GetPathInfo(ctx, backupPaths[i]) != nil
		if i < 3 && exist {
			t.Errorf("旧备份未清理: %s", backupPaths[i])
		}
		if i >= 3 && !exist {
			t.Errorf("新备份不应清理: %s", backupPaths[i])
		}
	}
}
