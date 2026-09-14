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
	"github.com/cellargalaxy/jotcash/tool"
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

	dbLock.Lock()
	defer dbLock.Unlock()
	migrated = false
}

func newTokenCtx(clientToken string) context.Context {
	return tool.SetClaims(util.GenCtx(), &model.Claims{ClientToken: clientToken})
}

func newTestCtx(t *testing.T) context.Context {
	t.Helper()
	newTestDb(t)

	ctx := newTokenCtx(testClientToken)
	operationLog := model.OperationLog{Id: util.GenId(), OperationType: model.OperationTypeSystemInit, Result: model.ResultSuccess}
	err := create(ctx, config.DbPath, testClientToken, NewOperationLogInsertHandler(&operationLog))
	if err != nil {
		t.Fatalf("建测试库异常: %+v", err)
	}
	return ctx
}

func catchLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := new(bytes.Buffer)
	origin := logrus.StandardLogger().Out
	logrus.SetOutput(buffer)
	t.Cleanup(func() { logrus.SetOutput(origin) })
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

func TestCreate(t *testing.T) {
	newTestDb(t)
	ctx := util.GenCtx()
	serverToken := config.GetConfig().ServerToken

	buffer := catchLog(t)
	if err := Create(ctx); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}
	if util.GetPathInfo(ctx, config.DbPath) == nil {
		t.Fatalf("初始化后库文件应存在: %s", config.DbPath)
	}
	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("初始前端口令没有打印: %s", buffer.String())
	}
	if findLogField(buffer.String(), "serverToken") != serverToken {
		t.Errorf("初始后端口令没有打印: %s", buffer.String())
	}
	if err := tool.CheckToken(ctx, clientToken); err != nil {
		t.Errorf("生成的初始口令不满足强度: %+v", err)
	}

	tokenCtx := newTokenCtx(clientToken)
	gormDb, err := Open(tokenCtx)
	if err != nil {
		t.Fatalf("用初始口令打开库异常: %+v", err)
	}
	for _, object := range []interface{}{&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{}} {
		if !gormDb.Migrator().HasTable(object) {
			t.Errorf("表未建出来: %T", object)
		}
	}
	Close(tokenCtx, gormDb)
	objects, count, err := SelectOperationLog(tokenCtx, model.OperationLogInquiry{OperationType: []string{model.OperationTypeSystemInit}})
	if err != nil {
		t.Fatalf("查询系统初始化审计异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || objects[0].Result != model.ResultSuccess {
		t.Errorf("系统初始化审计不符: count=%d %+v", count, objects)
	}

	buffer2 := catchLog(t)
	if err = Create(ctx); err != nil {
		t.Fatalf("重复初始化异常: %+v", err)
	}
	if findLogField(buffer2.String(), "clientToken") != "" {
		t.Errorf("库已存在时不应再打印初始口令: %s", buffer2.String())
	}
	if _, count, _ = SelectOperationLog(tokenCtx, model.OperationLogInquiry{}); count != 1 {
		t.Errorf("重复初始化不应再记审计: count=%d", count)
	}
}

func TestCreateEmptyDbFile(t *testing.T) {
	newTestDb(t)
	ctx := util.GenCtx()
	if err := util.WriteData2File(ctx, nil, config.DbPath); err != nil {
		t.Fatalf("写0字节库文件异常: %+v", err)
	}

	buffer := catchLog(t)
	if err := Create(ctx); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}
	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("0字节库文件是残骸，应当重新初始化: %s", buffer.String())
	}
	if err := CheckToken(newTokenCtx(clientToken)); err != nil {
		t.Errorf("重新初始化后的口令应能打开库: %+v", err)
	}
	if err := CheckToken(newTokenCtx("wrong-client-token")); err == nil {
		t.Errorf("重新初始化后其他口令不应能打开库")
	}
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
	if _, err := Open(ctx); err == nil {
		t.Errorf("0字节库文件开库应报错")
	}
}

func TestAutoMigrate(t *testing.T) {
	ctx := newTestCtx(t)

	gormDb, err := Open(ctx)
	if err != nil {
		t.Fatalf("打开数据库异常: %+v", err)
	}
	defer Close(ctx, gormDb)

	for _, object := range []interface{}{&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{}} {
		if !gormDb.Migrator().HasTable(object) {
			t.Errorf("表未建出来: %T", object)
		}
	}
	if err = AutoMigrate(ctx, gormDb); err != nil {
		t.Errorf("重复自动建表异常: %+v", err)
	}
	if err = AutoMigrate(ctx, nil); err == nil {
		t.Errorf("连接为空时自动建表应报错")
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
		Close(ctx, gormDb)
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
	if tool.GetClaims(tool.SetClaims(util.GenCtx(), nil)) != nil {
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
