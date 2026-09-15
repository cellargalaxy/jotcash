package repo

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/rdb"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

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

// 建库口令是建库那一步生成的，测试从日志里捞出来当这条链路的前端口令
func newTestCtx(t *testing.T) context.Context {
	t.Helper()
	t.Chdir(t.TempDir())

	buffer := new(bytes.Buffer)
	origin, originLevel := logrus.StandardLogger().Out, logrus.GetLevel()
	logrus.SetOutput(buffer)
	//建库口令是Info级，TestMain把全局级别压到了Warn，只放开捞口令这一小段
	logrus.SetLevel(logrus.InfoLevel)
	err := rdb.Create(util.GenCtx())
	logrus.SetOutput(origin)
	logrus.SetLevel(originLevel)
	if err != nil {
		t.Fatalf("建测试库异常: %+v", err)
	}

	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("建库口令没有打印: %s", buffer.String())
	}
	return util.SetClaims(util.GenCtx(), &model.Claims{ClientToken: clientToken})
}

func newTestExpense(counterparty string, expenseDate time.Time) *model.Expense {
	return &model.Expense{
		Id:                 util.GenId(),
		BankName:           "招商银行",
		CardLast4:          "6789",
		ExpenseDate:        expenseDate,
		ExpenseCurrency:    "CNY",
		ExpenseAmount:      decimal.RequireFromString("100.50"),
		Counterparty:       counterparty,
		ExchangeRate:       decimal.RequireFromString("1"),
		AccountingCurrency: "CNY",
		AccountingAmount:   decimal.RequireFromString("100.50"),
		ExpenseType:        "购物",
		Version:            1,
	}
}

func newTestEntry(t *testing.T, ctx context.Context, counterparty string, expenseDate time.Time) *model.Expense {
	t.Helper()
	operationId, fileId := util.GenId(), util.GenId()
	expense := newTestExpense(counterparty, expenseDate)
	expense.OperationId, expense.FileId = operationId, fileId
	fileMeta := &model.FileMeta{Id: fileId, FileHash: util.EnSha256Hex(counterparty), FileName: counterparty + ".csv", FileSize: 1, OperationId: operationId}
	fileBlob := &model.FileBlob{FileHash: fileMeta.FileHash, FileData: []byte(counterparty)}
	if err := InsertExpense(ctx, operationId, []*model.Expense{expense}, fileMeta, fileBlob); err != nil {
		t.Fatalf("明细入库异常: %+v", err)
	}
	return expense
}

// 明细/文件/存档/审计四张表要在同一次事务里一次写完
func TestInsertExpense(t *testing.T) {
	ctx := newTestCtx(t)

	if err := InsertExpense(ctx, util.GenId(), nil, nil, nil); err == nil {
		t.Errorf("入库内容为空应报错")
	}

	expense := newTestEntry(t, ctx, "亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 1 {
		t.Errorf("明细未落库: count=%d want=1", count)
	}
	if _, count, _ := SelectFileMeta(ctx, model.FileMetaInquiry{Id: []int64{expense.FileId}}); count != 1 {
		t.Errorf("文件元数据未落库: count=%d want=1", count)
	}
	logs, count, err := SelectOperationLog(ctx, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDataEntry}})
	if err != nil {
		t.Fatalf("查询审计异常: %+v", err)
	}
	if count != 1 || len(logs) != 1 || logs[0].Id != expense.OperationId {
		t.Errorf("入库审计不符: count=%d %+v", count, logs)
	}

	//同一个主键再入一次，四张表必须一起回滚，不能只落下文件与审计
	repeat := *expense
	operationId := util.GenId()
	fileMeta := &model.FileMeta{Id: util.GenId(), FileHash: "hash-repeat", FileName: "repeat.csv", FileSize: 1, OperationId: operationId}
	fileBlob := &model.FileBlob{FileHash: fileMeta.FileHash, FileData: []byte("repeat")}
	if err = InsertExpense(ctx, operationId, []*model.Expense{&repeat}, fileMeta, fileBlob); err == nil {
		t.Fatalf("主键冲突应报错")
	}
	if _, count, _ = SelectFileMeta(ctx, model.FileMetaInquiry{Id: []int64{fileMeta.Id}}); count != 0 {
		t.Errorf("失败的入库把文件元数据留下来了: count=%d want=0", count)
	}
	if _, count, _ = SelectOperationLog(ctx, model.OperationLogInquiry{Id: []int64{operationId}}); count != 0 {
		t.Errorf("失败的入库把审计留下来了: count=%d want=0", count)
	}
}

func TestSelectExpense(t *testing.T) {
	ctx := newTestCtx(t)

	early := newTestEntry(t, ctx, "亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	late := newTestEntry(t, ctx, "苹果", time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC))

	objects, count, err := SelectExpense(ctx, model.ExpenseInquiry{})
	if err != nil {
		t.Fatalf("查询明细异常: %+v", err)
	}
	if count != 2 || len(objects) != 2 {
		t.Fatalf("查询结果: count=%d len=%d want=2", count, len(objects))
	}
	//db层默认id asc，明细列表兜的是支出日期最新在前
	if objects[0].Id != late.Id || objects[1].Id != early.Id {
		t.Errorf("默认排序应是支出日期倒序: %+v", objects)
	}

	//上层不兜底pageSize，db层会整表拉出来
	if _, _, err = SelectExpense(ctx, model.ExpenseInquiry{Deleted: 99}); err == nil {
		t.Errorf("枚举外的删除筛选应报错")
	}
	if _, _, err = SelectExpense(ctx, model.ExpenseInquiry{Sort: "bank_name asc"}); err == nil {
		t.Errorf("白名单外的排序应报错")
	}
}

func TestDeleteExpense(t *testing.T) {
	ctx := newTestCtx(t)

	expense := newTestEntry(t, ctx, "亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	count, err := DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}})
	if err != nil {
		t.Fatalf("删除明细异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("删除影响行数: got=%d want=1", count)
	}

	if _, count, _ = SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 0 {
		t.Errorf("软删除后默认不应查到: count=%d want=0", count)
	}
	if _, count, _ = SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}, Deleted: model.DeletedOnly}); count != 1 {
		t.Errorf("只查已删除未命中: count=%d want=1", count)
	}
	//删除与审计同一次事务，删几条就得有几条审计
	if _, count, _ = SelectOperationLog(ctx, model.OperationLogInquiry{OperationType: []string{model.OperationTypeExpenseDelete}}); count != 1 {
		t.Errorf("删除审计不符: count=%d want=1", count)
	}

	now := time.Now()
	if _, err = DeleteExpense(ctx, model.ExpenseInquiry{ExpenseDateStart: now, ExpenseDateEnd: now.Add(-time.Hour)}); err == nil {
		t.Errorf("日期区间倒挂应报错")
	}
}

func TestSelectFileMetaAndOperationLog(t *testing.T) {
	ctx := newTestCtx(t)

	newTestEntry(t, ctx, "亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	late := newTestEntry(t, ctx, "苹果", time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC))

	files, count, err := SelectFileMeta(ctx, model.FileMetaInquiry{})
	if err != nil {
		t.Fatalf("查询文件元数据异常: %+v", err)
	}
	if count != 2 || len(files) != 2 || files[0].Id != late.FileId {
		t.Errorf("文件列表应最新在前: count=%d %+v", count, files)
	}

	//建库那条「系统初始化」在最前面，两次入库跟在后面
	logs, count, err := SelectOperationLog(ctx, model.OperationLogInquiry{})
	if err != nil {
		t.Fatalf("查询审计异常: %+v", err)
	}
	if count != 3 || len(logs) != 3 {
		t.Fatalf("审计条数: count=%d len=%d want=3", count, len(logs))
	}
	if logs[len(logs)-1].OperationType != model.OperationTypeSystemInit {
		t.Errorf("审计列表应最新在前，系统初始化该在最后: %+v", logs)
	}
	if _, _, err = SelectOperationLog(ctx, model.OperationLogInquiry{OperationType: []string{"不存在的操作类型"}}); err == nil {
		t.Errorf("枚举外的操作类型应报错")
	}
}

func TestToken(t *testing.T) {
	ctx := newTestCtx(t)

	if err := CheckToken(ctx); err != nil {
		t.Fatalf("正确口令探测异常: %+v", err)
	}
	wrongCtx := util.SetClaims(util.GenCtx(), &model.Claims{ClientToken: "wrong-client-token"})
	if err := CheckToken(wrongCtx); err == nil {
		t.Errorf("错误口令探测应报错")
	}

	//强度校验在换口令之前，弱口令不该走到动库文件那一步
	if err := ChangeToken(ctx, "123456"); err == nil {
		t.Errorf("弱口令应报错")
	}
	if err := CheckToken(ctx); err != nil {
		t.Errorf("被强度校验拦下的换口令不应影响原库: %+v", err)
	}

	newToken := "new-client-token-1"
	if err := ChangeToken(ctx, newToken); err != nil {
		t.Fatalf("换口令异常: %+v", err)
	}
	if err := CheckToken(ctx); err == nil {
		t.Errorf("换口令后旧口令应打不开库")
	}
	if err := CheckToken(util.SetClaims(util.GenCtx(), &model.Claims{ClientToken: newToken})); err != nil {
		t.Errorf("换口令后新口令应能打开库: %+v", err)
	}
}

// rdb.NewTransaction的读锁不可重入：RWMutex在有写者排队时新读者也要排在写者后面，
// 门面要是在自己的事务里再开一把事务，撞上排队中的导入就是死锁。
// 这条用例让导入反复来抢写锁，同时把五个门面轮着跑，谁嵌套了就会卡在这儿
func TestFacadeNotNestTransaction(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestEntry(t, ctx, "亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	buffer := new(bytes.Buffer)
	if err := Export(ctx, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}

	errs := make(chan error, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				if err := Import(ctx, bytes.NewReader(buffer.Bytes())); err != nil {
					errs <- err
					return
				}
			}
		}()
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 3; j++ {
					operationId, fileId := util.GenId(), util.GenId()
					object := newTestExpense("苹果", time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC))
					object.OperationId, object.FileId = operationId, fileId
					fileMeta := &model.FileMeta{Id: fileId, FileHash: util.EnSha256Hex(object.Counterparty), FileName: "nest.csv", FileSize: 1, OperationId: operationId}
					fileBlob := &model.FileBlob{FileHash: fileMeta.FileHash, FileData: []byte(object.Counterparty)}
					if err := InsertExpense(ctx, operationId, []*model.Expense{object}, fileMeta, fileBlob); err != nil {
						errs <- err
						return
					}
					if _, _, err := SelectExpense(ctx, model.ExpenseInquiry{}); err != nil {
						errs <- err
						return
					}
					if _, _, err := SelectFileMeta(ctx, model.FileMetaInquiry{}); err != nil {
						errs <- err
						return
					}
					if _, _, err := SelectOperationLog(ctx, model.OperationLogInquiry{}); err != nil {
						errs <- err
						return
					}
					if _, err := DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); err != nil {
						errs <- err
						return
					}
				}
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatalf("门面与导入并发时卡死，大概率是某个门面在自己的事务里又开了一次事务")
	}
	close(errs)
	for err := range errs {
		t.Errorf("并发异常: %+v", err)
	}
}

// 口令打不开库时，五个门面都得把开事务的错误如实抛上去，不能静默返回空列表当成「查无此物」
func TestWrongTokenFacade(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestEntry(t, ctx, "亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	wrongCtx := util.SetClaims(util.GenCtx(), &model.Claims{ClientToken: "wrong-client-token"})

	fileMeta := &model.FileMeta{Id: util.GenId(), FileHash: "hash-wrong", FileName: "wrong.csv", FileSize: 1}
	fileBlob := &model.FileBlob{FileHash: fileMeta.FileHash, FileData: []byte("wrong")}
	if err := InsertExpense(wrongCtx, util.GenId(), []*model.Expense{newTestExpense("苹果", time.Now())}, fileMeta, fileBlob); err == nil {
		t.Errorf("错误口令入库应报错")
	}
	if _, _, err := SelectExpense(wrongCtx, model.ExpenseInquiry{}); err == nil {
		t.Errorf("错误口令查询明细应报错")
	}
	if _, err := DeleteExpense(wrongCtx, model.ExpenseInquiry{Id: []int64{expense.Id}}); err == nil {
		t.Errorf("错误口令删除明细应报错")
	}
	if _, _, err := SelectFileMeta(wrongCtx, model.FileMetaInquiry{}); err == nil {
		t.Errorf("错误口令查询文件元数据应报错")
	}
	if _, _, err := SelectOperationLog(wrongCtx, model.OperationLogInquiry{}); err == nil {
		t.Errorf("错误口令查询审计应报错")
	}

	//五次都没打开过库，原库里的那笔与那条入库审计一条不少
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 1 {
		t.Errorf("错误口令不应动到原库: count=%d want=1", count)
	}
	if _, count, _ := SelectOperationLog(ctx, model.OperationLogInquiry{}); count != 2 {
		t.Errorf("错误口令不应留下审计: count=%d want=2", count)
	}
}

func TestExportImport(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestEntry(t, ctx, "亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))

	buffer := new(bytes.Buffer)
	if err := Export(ctx, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}
	if buffer.Len() == 0 {
		t.Fatalf("导出内容为空")
	}

	//导回来之后原来那笔还在，说明导出的是一份能用的整库快照
	if err := Import(ctx, bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("导入异常: %+v", err)
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 1 {
		t.Errorf("导回后数据不符: count=%d want=1", count)
	}
	if err := Import(ctx, bytes.NewReader([]byte("我不是数据库"))); err == nil {
		t.Errorf("非数据库文件应拒绝导入")
	}
}
