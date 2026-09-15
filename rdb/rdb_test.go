package db

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
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

// 增删查改的静态函数已经收归/service/db，事务由调用方自己开。
// 下面这组是db包用例的脚手架：把「开事务→AddCommit→Exec→Close」四步收成一行，
// 好让用例继续把注意力放在handler与inquiry的行为上，而不是每条断言都重抄一遍事务样板。
func execTransaction(ctx context.Context, handlers ...util.TransactionHandler) error {
	transaction, err := NewTransaction(ctx)
	if err != nil {
		return err
	}
	defer transaction.Close(ctx)
	return transaction.AddCommit(handlers...).Exec(ctx)
}

func insertExpense(ctx context.Context, object ...*model.Expense) (int64, error) {
	handler := NewExpenseInsertHandler(object...)
	if err := execTransaction(ctx, handler); err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func updateExpense(ctx context.Context, object *model.Expense) (int64, error) {
	handler := NewExpenseUpdateHandler(object)
	if err := execTransaction(ctx, handler); err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func deleteExpense(ctx context.Context, inquiry model.ExpenseInquiry) (int64, error) {
	handler := NewExpenseDeleteHandler(inquiry)
	if err := execTransaction(ctx, handler); err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func selectExpense(ctx context.Context, inquiry model.ExpenseInquiry) ([]*model.Expense, int64, error) {
	handler := NewExpenseSelectHandler(inquiry)
	if err := execTransaction(ctx, handler); err != nil {
		return nil, 0, err
	}
	return handler.Object, handler.Count, nil
}

func insertFileBlob(ctx context.Context, object ...*model.FileBlob) (int64, error) {
	handler := NewFileBlobInsertHandler(object...)
	if err := execTransaction(ctx, handler); err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func updateFileBlob(ctx context.Context, object *model.FileBlob) (int64, error) {
	handler := NewFileBlobUpdateHandler(object)
	if err := execTransaction(ctx, handler); err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func selectFileBlob(ctx context.Context, inquiry model.FileBlobInquiry) ([]*model.FileBlob, int64, error) {
	handler := NewFileBlobSelectHandler(inquiry)
	if err := execTransaction(ctx, handler); err != nil {
		return nil, 0, err
	}
	return handler.Object, handler.Count, nil
}

func insertFileMeta(ctx context.Context, object ...*model.FileMeta) (int64, error) {
	handler := NewFileMetaInsertHandler(object...)
	if err := execTransaction(ctx, handler); err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func updateFileMeta(ctx context.Context, object *model.FileMeta) (int64, error) {
	handler := NewFileMetaUpdateHandler(object)
	if err := execTransaction(ctx, handler); err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func selectFileMeta(ctx context.Context, inquiry model.FileMetaInquiry) ([]*model.FileMeta, int64, error) {
	handler := NewFileMetaSelectHandler(inquiry)
	if err := execTransaction(ctx, handler); err != nil {
		return nil, 0, err
	}
	return handler.Object, handler.Count, nil
}

func insertOperationLog(ctx context.Context, object ...*model.OperationLog) (int64, error) {
	handler := NewOperationLogInsertHandler(object...)
	if err := execTransaction(ctx, handler); err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func updateOperationLog(ctx context.Context, object *model.OperationLog) (int64, error) {
	handler := NewOperationLogUpdateHandler(object)
	if err := execTransaction(ctx, handler); err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func selectOperationLog(ctx context.Context, inquiry model.OperationLogInquiry) ([]*model.OperationLog, int64, error) {
	handler := NewOperationLogSelectHandler(inquiry)
	if err := execTransaction(ctx, handler); err != nil {
		return nil, 0, err
	}
	return handler.Object, handler.Count, nil
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

// 服务起来就建库：首次部署用户还不知道任何口令，口令由建库这一步生成，再从日志里捞
func TestCreate(t *testing.T) {
	newTestDb(t)
	ctx := util.GenCtx()
	serverToken := config.GetConfig(ctx).ServerToken

	buffer := catchLog(t)
	if err := Create(ctx); err != nil {
		t.Fatalf("建库异常: %+v", err)
	}
	if util.GetPathInfo(ctx, config.DbPath) == nil {
		t.Fatalf("建库后库文件应存在: %s", config.DbPath)
	}
	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("建库前端口令没有打印: %s", buffer.String())
	}
	if findLogField(buffer.String(), "serverToken") != serverToken {
		t.Errorf("建库后端口令没有打印: %s", buffer.String())
	}
	//日志里捞出来的这把口令是用户唯一的入口，强度不够等于把库交出去
	if err := tool.CheckToken(ctx, clientToken); err != nil {
		t.Errorf("建库生成的口令不满足强度: %+v", err)
	}

	tokenCtx := newTokenCtx(clientToken)
	gormDb, err := Open(tokenCtx)
	if err != nil {
		t.Fatalf("用建库口令打开库异常: %+v", err)
	}
	for _, object := range []interface{}{&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{}} {
		if !gormDb.Migrator().HasTable(object) {
			t.Errorf("表未建出来: %T", object)
		}
	}
	util.CloseDb(tokenCtx, gormDb)
	objects, count, err := selectOperationLog(tokenCtx, model.OperationLogInquiry{OperationType: []string{model.OperationTypeSystemInit}})
	if err != nil {
		t.Fatalf("查询系统初始化审计异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || objects[0].Result != model.ResultSuccess {
		t.Errorf("系统初始化审计不符: count=%d %+v", count, objects)
	}

	buffer2 := catchLog(t)
	if err = Create(ctx); err != nil {
		t.Fatalf("重复建库异常: %+v", err)
	}
	if findLogField(buffer2.String(), "clientToken") != "" {
		t.Errorf("库已存在时不应再建一次、也不应再打印口令: %s", buffer2.String())
	}
	if _, count, _ = selectOperationLog(tokenCtx, model.OperationLogInquiry{}); count != 1 {
		t.Errorf("重复建库不应再记审计: count=%d", count)
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
		t.Fatalf("建库异常: %+v", err)
	}
	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("0字节库文件是残骸，应当重新建库: %s", buffer.String())
	}
	if err := CheckToken(newTokenCtx(clientToken)); err != nil {
		t.Errorf("重新建库后的口令应能打开库: %+v", err)
	}
	if err := CheckToken(newTokenCtx("wrong-client-token")); err == nil {
		t.Errorf("重新建库后其他口令不应能打开库")
	}
}

// 多个协程同时建库：建库必须独占，否则会互相截断、删掉对方刚建好的库
func TestConcurrentCreate(t *testing.T) {
	newTestDb(t)
	ctx := util.GenCtx()
	buffer := catchLog(t)

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Create(ctx); err != nil {
				errs <- err
			}
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
	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("并发建库没打印口令: %s", buffer.String())
	}
	if _, count, err := selectOperationLog(newTokenCtx(clientToken), model.OperationLogInquiry{}); err != nil || count != 1 {
		t.Errorf("建库审计应当只有1条: count=%d err=%+v", count, err)
	}
}

func TestOpenWithoutDbFile(t *testing.T) {
	ctx := newTestCtx(t)

	if _, err := insertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}
	util.RemoveFile(ctx, config.DbPath)

	if gormDb, err := Open(ctx); err == nil {
		util.CloseDb(ctx, gormDb)
		t.Errorf("库文件不存在时开库应报错")
	}
	//NewTransaction只做普通读写，不碰建库：库文件不在就直接报错，不能拿请求带的口令悄悄建一个空库顶上
	if _, _, err := selectExpense(newTokenCtx("other-client-token"), model.ExpenseInquiry{}); err == nil {
		t.Errorf("库文件不存在时查询应报错")
	}
	if util.GetPathInfo(ctx, config.DbPath) != nil {
		t.Errorf("业务事务不应把库文件建出来")
	}
	if err := CheckToken(ctx); err == nil {
		t.Errorf("库文件不存在时口令探针应报错")
	}

	if err := util.WriteData2File(ctx, nil, config.DbPath); err != nil {
		t.Fatalf("写0字节库文件异常: %+v", err)
	}
	//open只看库文件在不在，0字节按一个还没写过页的空库处理，开得起来
	gormDb, err := Open(ctx)
	if err != nil {
		t.Errorf("0字节库文件应能开库: %+v", err)
	}
	util.CloseDb(ctx, gormDb)
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
	_, count, _ := selectOperationLog(ctx, model.OperationLogInquiry{})
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
	if _, _, err = selectExpense(newTokenCtx("wrong-client-token"), model.ExpenseInquiry{}); err == nil {
		t.Errorf("错误口令查询应报错")
	}

	if err = util.WriteData2File(ctx, []byte("我不是数据库"), config.DbPath); err != nil {
		t.Fatalf("写坏库文件异常: %+v", err)
	}
	if gormDb, err = Open(ctx); err == nil {
		util.CloseDb(ctx, gormDb)
		t.Errorf("库文件损坏应报错")
	}
}

func TestTransactionWithoutClaims(t *testing.T) {
	newTestCtx(t)

	if _, _, err := selectExpense(util.GenCtx(), model.ExpenseInquiry{}); err == nil {
		t.Errorf("ctx无Claims时查询应报错")
	}
	if _, _, err := selectExpense(newTokenCtx(""), model.ExpenseInquiry{}); err == nil {
		t.Errorf("Claims无口令时查询应报错")
	}
	if gormDb, err := Open(util.GenCtx()); err == nil {
		util.CloseDb(util.GenCtx(), gormDb)
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
	transaction, err := NewTransaction(ctx)
	if err != nil {
		t.Fatalf("开事务异常: %+v", err)
	}
	err = transaction.AddCommit(NewExpenseInsertHandler(expense), NewOperationLogInsertHandler(operationLog)).Exec(ctx)
	transaction.Close(ctx)
	if err != nil {
		t.Fatalf("事务异常: %+v", err)
	}
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 1 {
		t.Errorf("事务内插入的支出明细未落库")
	}
	if _, count, _ := selectOperationLog(ctx, model.OperationLogInquiry{Id: []int64{operationLog.Id}}); count != 1 {
		t.Errorf("事务内插入的操作日志未落库")
	}

	rollbackExpense := newTestExpense()
	transaction, err = NewTransaction(ctx)
	if err != nil {
		t.Fatalf("开事务异常: %+v", err)
	}
	err = transaction.AddCommit(
		NewExpenseInsertHandler(rollbackExpense),
		NewOperationLogInsertHandler(&model.OperationLog{Id: operationLog.Id, Result: model.ResultSuccess}),
	).Exec(ctx)
	transaction.Close(ctx)
	if err == nil {
		t.Fatalf("主键冲突应报错")
	}
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{Id: []int64{rollbackExpense.Id}}); count != 0 {
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
				transaction, err := NewTransaction(ctx)
				if err != nil {
					errs <- err
					return
				}
				//先读后写的混合事务，是SQLite锁升级失败的典型场景
				err = transaction.AddCommit(
					NewExpenseSelectHandler(model.ExpenseInquiry{PageSize: 1}),
					NewExpenseInsertHandler(newTestExpense()),
				).Exec(ctx)
				transaction.Close(ctx)
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
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{}); count != 48 {
		t.Errorf("并发插入条数: got=%d want=48", count)
	}
}

// 事务里多待一会儿，把「事务在途」这个窗口拉开
type slowInsertHandler struct {
	expense *model.Expense
}

func (this *slowInsertHandler) Exec(ctx context.Context, tx *gorm.DB) error {
	err := tx.Create(this.expense).Error
	if err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	return nil
}

// 业务事务在途时换口令：换口令最后一步是os.Rename，读锁没罩住整条连接的话，在途事务会提交进被顶掉的旧文件，静默丢数据
func TestChangeTokenDuringTransaction(t *testing.T) {
	ctx := newTestCtx(t)
	newToken := "new-client-token2"
	expense := newTestExpense()

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	wg.Add(1)
	go func() {
		defer wg.Done()
		transaction, err := NewTransaction(ctx)
		if err != nil {
			errs <- err
			return
		}
		defer transaction.Close(ctx)
		if err = transaction.AddCommit(&slowInsertHandler{expense: expense}).Exec(ctx); err != nil {
			errs <- err
		}
	}()
	//等小红进到事务里，再让换口令插进来
	time.Sleep(100 * time.Millisecond)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := ChangeToken(ctx, newToken); err != nil {
			errs <- err
		}
	}()
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("并发异常: %+v", err)
	}
	if _, count, _ := selectExpense(newTokenCtx(newToken), model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 1 {
		t.Errorf("在途事务的数据不应被换口令顶掉: count=%d want=1", count)
	}
}

func TestConcurrent(t *testing.T) {
	ctx := newTestCtx(t)
	if _, err := insertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				if _, err := insertExpense(ctx, newTestExpense()); err != nil {
					errs <- err
					return
				}
				if _, _, err := selectExpense(ctx, model.ExpenseInquiry{}); err != nil {
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
	if _, _, err := selectExpense(ctx, model.ExpenseInquiry{}); err != nil {
		t.Errorf("并发之后查询异常: %+v", err)
	}
}

// 提交链失败时回滚钩子要跑起来，失败审计才补得进去
func TestTransactionRollbackHook(t *testing.T) {
	ctx := newTestCtx(t)

	origin := newTestExpense()
	if _, err := insertExpense(ctx, origin); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	rollbackLog := &model.OperationLog{Id: util.GenId(), OperationType: model.OperationTypeDataEntry, Result: model.ResultFailure}
	transaction, err := NewTransaction(ctx)
	if err != nil {
		t.Fatalf("开事务异常: %+v", err)
	}
	//第二个handler拿同一个主键再插一遍，逼提交链失败
	err = transaction.
		AddCommit(NewExpenseInsertHandler(newTestExpense()), NewExpenseInsertHandler(origin)).
		AddRollback(NewOperationLogInsertHandler(rollbackLog)).
		Exec(ctx)
	transaction.Close(ctx)
	if err == nil {
		t.Fatalf("主键冲突应报错")
	}

	if _, count, _ := selectOperationLog(ctx, model.OperationLogInquiry{Id: []int64{rollbackLog.Id}}); count != 1 {
		t.Errorf("回滚钩子没跑，失败审计没落库: count=%d want=1", count)
	}
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Errorf("回滚钩子不应把提交链里的插入留下来: count=%d want=1", count)
	}
}

// 开事务失败时读锁必须还回去，否则之后任何要写锁的操作都会永久卡住
func TestTransactionUnlockOnFail(t *testing.T) {
	ctx := newTestCtx(t)

	if object, err := NewTransaction(newTokenCtx("wrong-client-token")); err == nil {
		object.Close(ctx)
		t.Fatalf("错误口令开事务应报错")
	}

	done := make(chan error, 1)
	go func() {
		done <- ChangeToken(ctx, "new-client-token-3")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("换口令异常: %+v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("开事务失败没还回读锁，换口令被永久挡住")
	}
}
