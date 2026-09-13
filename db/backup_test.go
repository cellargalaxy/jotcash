package db

import (
	"bytes"
	"path/filepath"
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
	//换进来的库结构可能是旧版本，替换后下一次开库要重新建表
	migrateLock.Lock()
	reset := !migrated
	migrateLock.Unlock()
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

func TestChangeClientToken(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestExpense()
	if _, err := InsertExpense(ctx, expense); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	if err := ChangeClientToken(ctx, "123456"); err == nil {
		t.Errorf("弱口令应被拒绝")
	}
	if err := ChangeClientToken(newTokenCtx("wrong-client-token"), "new-client-token-1"); err == nil {
		t.Errorf("旧口令错误应报错")
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Fatalf("失败的换口令不应影响原库: count=%d", count)
	}

	newToken := "new-client-token-1"
	if err := ChangeClientToken(ctx, newToken); err != nil {
		t.Fatalf("换口令异常: %+v", err)
	}

	if err := CheckClientToken(ctx); err == nil {
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
	names, err := filepath.Glob(config.DbPath + "*.tmp")
	if err != nil || len(names) > 0 {
		t.Errorf("临时文件没清理: %v", names)
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

func TestImportMissingTable(t *testing.T) {
	ctx := newTestCtx(t)
	if _, err := InsertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	//造一个同口令、但一张表都没有的空库
	emptyPath := genTempPath()
	gormDb, err := open(ctx, emptyPath, testClientToken, true)
	if err != nil {
		t.Fatalf("建空库异常: %+v", err)
	}
	Close(ctx, gormDb)
	data, err := util.ReadFile2Data(ctx, emptyPath, nil)
	if err != nil {
		t.Fatalf("读空库异常: %+v", err)
	}
	util.RemoveFile(ctx, emptyPath)

	if err = Import(ctx, bytes.NewReader(data)); err == nil {
		t.Errorf("缺表的库应拒绝导入")
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Errorf("导入失败不应影响原库: count=%d", count)
	}
}
