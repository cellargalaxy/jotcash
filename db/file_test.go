package db

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
)

// B-1 导出：导出的是加密态快照，必须还能用同一把口令打开、数据齐全，且不是明文
func TestExport(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestExpense()
	if _, err := InsertExpense(ctx, expense); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	buffer := new(bytes.Buffer)
	if err := Export(ctx, testClientToken, buffer); err != nil {
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

	//把导出的内容当成导入源，能用同一把口令导回来，数据对得上
	if err := Import(ctx, testClientToken, bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("导回异常: %+v", err)
	}
	objects, count, err := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}})
	if err != nil {
		t.Fatalf("导回后查询异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || !objects[0].ExpenseAmount.Equal(expense.ExpenseAmount) {
		t.Errorf("导回后的数据不符: count=%d %+v", count, objects)
	}
}

// 错误口令不能导出
func TestExportWrongToken(t *testing.T) {
	ctx := newTestCtx(t)

	buffer := new(bytes.Buffer)
	if err := Export(ctx, "wrong-client-token", buffer); err == nil {
		t.Errorf("错误口令导出应报错")
	}
}

// A-5 更换口令：新口令能打开、旧口令打不开、数据一条不少；弱口令与错旧口令一律拒绝且原库不动
func TestChangeClientToken(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestExpense()
	if _, err := InsertExpense(ctx, expense); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	//弱口令直接拒绝
	if err := ChangeClientToken(ctx, testClientToken, "123456"); err == nil {
		t.Errorf("弱口令应被拒绝")
	}
	//旧口令不对也拒绝，原库不能动
	if err := ChangeClientToken(ctx, "wrong-client-token", "new-client-token-1"); err == nil {
		t.Errorf("旧口令错误应报错")
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Fatalf("失败的换口令不应影响原库: count=%d", count)
	}

	newToken := "new-client-token-1"
	if err := ChangeClientToken(ctx, testClientToken, newToken); err != nil {
		t.Fatalf("换口令异常: %+v", err)
	}

	//旧口令作废
	if err := CheckClientToken(ctx, testClientToken); err == nil {
		t.Errorf("换口令后旧口令应打不开库")
	}
	//新口令能用，数据还在
	newCtx := tool.SetClaims(util.GenCtx(), &model.Claims{ClientToken: newToken})
	objects, count, err := SelectExpense(newCtx, model.ExpenseInquiry{Id: []int64{expense.Id}})
	if err != nil {
		t.Fatalf("换口令后查询异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || objects[0].Counterparty != expense.Counterparty {
		t.Errorf("换口令后数据不符: count=%d %+v", count, objects)
	}
	//临时文件不留下
	names, err := filepath.Glob(config.DbPath + "*.tmp")
	if err != nil || len(names) > 0 {
		t.Errorf("临时文件没清理: %v", names)
	}
}

// B-2 导入：口令对不上、或者压根不是本系统的库，都要拒绝，且原库不动
func TestImportIllegal(t *testing.T) {
	ctx := newTestCtx(t)
	if _, err := InsertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	buffer := new(bytes.Buffer)
	if err := Export(ctx, testClientToken, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}

	//口令对不上
	if err := Import(ctx, "wrong-client-token", bytes.NewReader(buffer.Bytes())); err == nil {
		t.Errorf("口令不匹配的库应拒绝导入")
	}
	//根本不是数据库文件
	if err := Import(ctx, testClientToken, bytes.NewReader([]byte("我不是数据库"))); err == nil {
		t.Errorf("非数据库文件应拒绝导入")
	}
	//原库还在，数据没丢
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Errorf("导入失败不应影响原库: count=%d", count)
	}
}

// 是个能用同一把口令打开的加密库，但表不全，照样要拒绝
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

	if err = Import(ctx, testClientToken, bytes.NewReader(data)); err == nil {
		t.Errorf("缺表的库应拒绝导入")
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Errorf("导入失败不应影响原库: count=%d", count)
	}
}

// 导入的库结构可能是旧版本，替换后下一次开库要重新建表
func TestImportResetMigrate(t *testing.T) {
	ctx := newTestCtx(t)

	buffer := new(bytes.Buffer)
	if err := Export(ctx, testClientToken, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}
	if err := Import(ctx, testClientToken, bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("导入异常: %+v", err)
	}

	migrateLock.Lock()
	defer migrateLock.Unlock()
	if migrated {
		t.Errorf("导入后应复位建表标记")
	}
}
