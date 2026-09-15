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

func newTestCtx(t *testing.T) context.Context {
	t.Helper()
	newTestDb(t)

	ctx := newTokenCtx(testClientToken)
	err := create(ctx, config.DbPath, testClientToken)
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

// 没有init了：库在第一次开事务时才建出来，口令就是那次请求带的
func TestCreate(t *testing.T) {
	newTestDb(t)
	serverToken := config.GetConfig(util.GenCtx()).ServerToken
	ctx := newTokenCtx(testClientToken)

	buffer := catchLog(t)
	object, err := NewTransaction(ctx)
	if err != nil {
		t.Fatalf("首次开事务异常: %+v", err)
	}
	object.Close(ctx)
	if util.GetPathInfo(ctx, config.DbPath) == nil {
		t.Fatalf("首次开事务后库文件应存在: %s", config.DbPath)
	}
	if findLogField(buffer.String(), "clientToken") != testClientToken {
		t.Errorf("建库前端口令没有打印: %s", buffer.String())
	}
	if findLogField(buffer.String(), "serverToken") != serverToken {
		t.Errorf("建库后端口令没有打印: %s", buffer.String())
	}

	gormDb, err := Open(ctx)
	if err != nil {
		t.Fatalf("用建库口令打开库异常: %+v", err)
	}
	for _, object := range []interface{}{&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{}} {
		if !gormDb.Migrator().HasTable(object) {
			t.Errorf("表未建出来: %T", object)
		}
	}
	util.CloseDb(ctx, gormDb)
	objects, count, err := SelectOperationLog(ctx, model.OperationLogInquiry{OperationType: []string{model.OperationTypeSystemInit}})
	if err != nil {
		t.Fatalf("查询系统初始化审计异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || objects[0].Result != model.ResultSuccess {
		t.Errorf("系统初始化审计不符: count=%d %+v", count, objects)
	}

	buffer2 := catchLog(t)
	object, err = NewTransaction(ctx)
	if err != nil {
		t.Fatalf("再次开事务异常: %+v", err)
	}
	object.Close(ctx)
	if findLogField(buffer2.String(), "clientToken") != "" {
		t.Errorf("库已存在时不应再打印建库口令: %s", buffer2.String())
	}
	if _, count, _ = SelectOperationLog(ctx, model.OperationLogInquiry{}); count != 1 {
		t.Errorf("重复开事务不应再记审计: count=%d", count)
	}
}

func TestCreateEmptyDbFile(t *testing.T) {
	newTestDb(t)
	ctx := newTokenCtx(testClientToken)
	if err := util.WriteData2File(ctx, nil, config.DbPath); err != nil {
		t.Fatalf("写0字节库文件异常: %+v", err)
	}

	buffer := catchLog(t)
	object, err := NewTransaction(ctx)
	if err != nil {
		t.Fatalf("首次开事务异常: %+v", err)
	}
	object.Close(ctx)
	if findLogField(buffer.String(), "clientToken") != testClientToken {
		t.Fatalf("0字节库文件是残骸，应当重新建库: %s", buffer.String())
	}
	if err = CheckToken(ctx); err != nil {
		t.Errorf("重新建库后的口令应能打开库: %+v", err)
	}
	if err = CheckToken(newTokenCtx("wrong-client-token")); err == nil {
		t.Errorf("重新建库后其他口令不应能打开库")
	}
}

// 库文件还不存在时多个请求同时开事务：建库必须独占，否则会互相截断、删掉对方刚建好的库
func TestConcurrentCreate(t *testing.T) {
	newTestDb(t)
	ctx := newTokenCtx(testClientToken)

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			object, err := NewTransaction(ctx)
			if err != nil {
				errs <- err
				return
			}
			object.Close(ctx)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("并发建库异常: %+v", err)
	}
	if util.GetFileInfo(ctx, config.DbPath) == nil {
		t.Fatalf("并发建库之后库文件不应消失")
	}
	if _, count, err := SelectOperationLog(ctx, model.OperationLogInquiry{}); err != nil || count != 1 {
		t.Errorf("建库审计应当只有1条: count=%d err=%+v", count, err)
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
	//建库封装进了NewTransaction：业务事务碰上库文件不在，就地把库建出来，上层不感知
	if _, _, err := SelectExpense(newTokenCtx("other-client-token"), model.ExpenseInquiry{}); err != nil {
		t.Errorf("库文件不存在时查询应就地建库: %+v", err)
	}
	if util.GetPathInfo(ctx, config.DbPath) == nil {
		t.Errorf("库文件应被业务事务重新建出来")
	}
	//重建出来的库用的是那次请求的口令，原口令自然开不了
	if err := CheckToken(ctx); err == nil {
		t.Errorf("重建后原口令探针应报错")
	}

	if err := util.WriteData2File(ctx, nil, config.DbPath); err != nil {
		t.Fatalf("写0字节库文件异常: %+v", err)
	}
	//open只看库文件在不在，0字节按一个还没写过页的空库处理，开得起来
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

// 建库失败时不能在盘上留下半个库：库文件是事务之前占位建的，得在事务之外清掉
func TestCreateRollback(t *testing.T) {
	newTestDb(t)
	ctx := util.GenCtx()
	dbPath := "resource/rollback.db"

	//口令为空，占位文件已经建出来了，连库这一步才失败
	if err := create(ctx, dbPath, ""); err == nil {
		t.Fatalf("口令为空时建库应报错")
	}

	if util.GetFileInfo(ctx, dbPath) != nil {
		t.Errorf("建库失败时应把已占位的库文件清掉")
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
