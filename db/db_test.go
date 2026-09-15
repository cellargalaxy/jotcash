package db

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/sirupsen/logrus"
)

const testClientToken = "test-client-token"

func TestMain(m *testing.M) {
	logrus.SetLevel(logrus.WarnLevel)
	code := m.Run()
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

func newTestDb(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
}

func newTokenCtx(clientToken string) context.Context {
	return util.SetClaims(util.GenCtx(), &model.Claims{ClientToken: clientToken})
}

// 建库这条链已迁到service/db，db包的测试按同一条链自己把库建出来
func newDb(ctx context.Context, dbPath, token string, handlers ...util.TransactionHandler) error {
	//建库handler产连接、不吃tx，必须排在开事务之前
	err := NewCreateDbHandler(dbPath).Exec(ctx, nil)
	if err != nil {
		return err
	}
	object, err := newTransaction(ctx, dbPath, token)
	if err != nil {
		return err
	}
	defer object.Close(ctx)
	return object.
		AddCommit(NewMigrateHandler()).
		AddCommit(handlers...).
		AddRollback(NewDbRemoveHandler(dbPath)).
		Exec(ctx)
}

func newTestCtx(t *testing.T) context.Context {
	t.Helper()
	newTestDb(t)

	ctx := newTokenCtx(testClientToken)
	operationLog := model.OperationLog{Id: util.GenId(), OperationType: model.OperationTypeSystemInit, Result: model.ResultSuccess}
	err := newDb(ctx, config.DbPath, testClientToken, NewOperationLogInsertHandler(&operationLog))
	if err != nil {
		t.Fatalf("建测试库异常: %+v", err)
	}
	return ctx
}

func catchLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := new(bytes.Buffer)
	origin := logrus.StandardLogger().Out
	originLevel := logrus.GetLevel()
	logrus.SetOutput(buffer)
	//要捞的建库口令是Info级，TestMain把全局级别压到了Warn，捞的这段得临时放开
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(origin)
		logrus.SetLevel(originLevel)
	})
	return buffer
}

func findLogField(text, key string) string {
	index := strings.Index(text, "["+key+":")
	if index < 0 {
		return ""
	}
	text = text[index+len(key)+2:]
	index = strings.Index(text, "]")
	if index < 0 {
		return ""
	}
	return text[:index]
}

func TestOpenWithoutDbFile(t *testing.T) {
	ctx := newTestCtx(t)

	if _, err := InsertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}
	util.RemoveFile(ctx, config.DbPath)

	if _, err := Open(ctx); err == nil {
		t.Errorf("库文件不存在时开库应报错")
	}
	if _, _, err := SelectExpense(newTokenCtx("other-client-token"), model.ExpenseInquiry{}); err == nil {
		t.Errorf("库文件不存在时查询应报错")
	}
	if util.GetPathInfo(ctx, config.DbPath) != nil {
		t.Errorf("库文件不应被请求重新建出来")
	}
	if err := CheckToken(ctx); err == nil {
		t.Errorf("库文件不存在时口令探针应报错")
	}

	if err := util.WriteData2File(ctx, nil, config.DbPath); err != nil {
		t.Fatalf("写0字节库文件异常: %+v", err)
	}
	//existDb只看文件在不在，0字节按一个还没写过页的空库处理，开得起来
	if _, err := Open(ctx); err != nil {
		t.Errorf("0字节库文件应能开库: %+v", err)
	}
}

func TestAutoMigrate(t *testing.T) {
	ctx := newTestCtx(t)

	gormDb, err := Open(ctx)
	if err != nil {
		t.Fatalf("打开数据库异常: %+v", err)
	}
	defer util.CloseDb(ctx, gormDb)

	for _, object := range []interface{}{&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{}} {
		if !gormDb.Migrator().HasTable(object) {
			t.Errorf("表未建出来: %T", object)
		}
	}
	handler := NewMigrateHandler()
	if err = handler.Exec(ctx, gormDb); err != nil {
		t.Errorf("重复自动建表异常: %+v", err)
	}
	if err = handler.Exec(ctx, nil); err == nil {
		t.Errorf("连接为空时自动建表应报错")
	}
}

// 建表与调用方handler同处一个事务：handler失败，表结构要跟着回滚，不能留下半个库
func TestCreateRollbackWithHandler(t *testing.T) {
	newTestDb(t)
	ctx := util.GenCtx()
	dbPath := "resource/rollback.db"

	//同一条记录插两次，主键冲突让handler失败
	operationLog := model.OperationLog{Id: util.GenId(), OperationType: model.OperationTypeSystemInit, Result: model.ResultSuccess}
	if err := newDb(ctx, dbPath, testClientToken, NewOperationLogInsertHandler(&operationLog, &operationLog)); err == nil {
		t.Fatalf("handler失败时建库应报错")
	}

	if util.GetFileInfo(ctx, dbPath) != nil {
		t.Errorf("handler失败时回滚链应把没建成的库文件删掉")
	}
}

func TestCheckToken(t *testing.T) {
	ctx := newTestCtx(t)

	if err := CheckToken(ctx); err != nil {
		t.Fatalf("正确口令探测异常: %+v", err)
	}
	if err := CheckToken(newTokenCtx("wrong-client-token")); err == nil {
		t.Errorf("错误口令探测应报错")
	}
	_, count, _ := SelectOperationLog(ctx, model.OperationLogInquiry{})
	if count != 1 {
		t.Errorf("探针不该写数据，库里只该有建库那条: count=%d want=1", count)
	}
}

func TestOpenClientToken(t *testing.T) {
	ctx := newTestCtx(t)

	gormDb, err := Open(newTokenCtx("wrong-client-token"))
	if err == nil {
		util.CloseDb(ctx, gormDb)
		t.Fatalf("错误口令应报错")
	}
	if !strings.Contains(err.Error(), "口令错误或数据库文件损坏") {
		t.Errorf("错误口令的提示不符: %+v", err)
	}
	if _, _, err = SelectExpense(newTokenCtx("wrong-client-token"), model.ExpenseInquiry{}); err == nil {
		t.Errorf("错误口令查询应报错")
	}

	if err = util.WriteData2File(ctx, []byte("我不是数据库"), config.DbPath); err != nil {
		t.Fatalf("写坏库文件异常: %+v", err)
	}
	if _, err = Open(ctx); err == nil {
		t.Errorf("库文件损坏应报错")
	}
}

func TestTransactionWithoutClaims(t *testing.T) {
	newTestDb(t)

	if _, _, err := SelectExpense(util.GenCtx(), model.ExpenseInquiry{}); err == nil {
		t.Errorf("ctx无Claims时查询应报错")
	}
	if _, _, err := SelectExpense(newTokenCtx(""), model.ExpenseInquiry{}); err == nil {
		t.Errorf("Claims无口令时查询应报错")
	}
	if _, err := Open(util.GenCtx()); err == nil {
		t.Errorf("ctx无Claims时开库应报错")
	}
	if util.GetClaims[*model.Claims](util.SetClaims(util.GenCtx(), nil)) != nil {
		t.Errorf("SetClaims传nil不应写入ctx")
	}
}

func TestTransaction(t *testing.T) {
	ctx := newTestCtx(t)

	expense := newTestExpense()
	operationLog := &model.OperationLog{Id: util.GenId(), OperationType: model.OperationTypeDataEntry, Result: model.ResultSuccess}
	err := Transaction(ctx, NewExpenseInsertHandler(expense), NewOperationLogInsertHandler(operationLog))
	if err != nil {
		t.Fatalf("事务异常: %+v", err)
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 1 {
		t.Errorf("事务内插入的支出明细未落库")
	}
	if _, count, _ := SelectOperationLog(ctx, model.OperationLogInquiry{Id: []int64{operationLog.Id}}); count != 1 {
		t.Errorf("事务内插入的操作日志未落库")
	}

	rollbackExpense := newTestExpense()
	err = Transaction(ctx,
		NewExpenseInsertHandler(rollbackExpense),
		NewOperationLogInsertHandler(&model.OperationLog{Id: operationLog.Id, Result: model.ResultSuccess}),
	)
	if err == nil {
		t.Fatalf("主键冲突应报错")
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{rollbackExpense.Id}}); count != 0 {
		t.Errorf("事务未回滚")
	}
}

func TestConcurrentTransaction(t *testing.T) {
	ctx := newTestCtx(t)

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				//先读后写的混合事务，是SQLite锁升级失败的典型场景
				err := Transaction(ctx,
					NewExpenseSelectHandler(model.ExpenseInquiry{PageSize: 1}),
					NewExpenseInsertHandler(newTestExpense()),
				)
				if err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("并发混合事务异常: %+v", err)
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{}); count != 48 {
		t.Errorf("并发插入条数: got=%d want=48", count)
	}
}

func TestConcurrent(t *testing.T) {
	ctx := newTestCtx(t)
	if _, err := InsertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				if _, err := InsertExpense(ctx, newTestExpense()); err != nil {
					errs <- err
					return
				}
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
		for j := 0; j < 2; j++ {
			buffer := new(bytes.Buffer)
			if err := Export(ctx, buffer); err != nil {
				errs <- err
				return
			}
			if err := Import(ctx, bytes.NewReader(buffer.Bytes())); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("并发读写异常: %+v", err)
	}
	if _, _, err := SelectExpense(ctx, model.ExpenseInquiry{}); err != nil {
		t.Errorf("并发之后查询异常: %+v", err)
	}
}
