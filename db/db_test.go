package db_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

const testClientToken = "test-client-token"

func TestMain(m *testing.M) {
	logrus.SetLevel(logrus.WarnLevel)
	code := m.Run()
	//config包init时会把默认配置落到相对路径下，测试产物不留在仓库里
	os.RemoveAll("resource")
	os.Exit(code)
}

// newTestCtx 每个用例一个独立的加密库，ctx里带上Claims，之后各db函数自己开关库
func newTestCtx(t *testing.T) context.Context {
	t.Helper()
	config.Config.DbPath = filepath.Join(t.TempDir(), "jotcash.db")
	ctx := tool.SetClaims(util.GenCtx(), &model.Claims{ClientToken: testClientToken})

	gormDb, err := db.Open(ctx, testClientToken)
	if err != nil {
		t.Fatalf("打开测试数据库异常: %+v", err)
	}
	defer db.Close(ctx, gormDb)
	if err = db.AutoMigrate(ctx, gormDb); err != nil {
		t.Fatalf("自动建表异常: %+v", err)
	}
	return ctx
}

// 四张表都要建出来，且重复执行不报错（服务每次启动都会跑一遍）
func TestAutoMigrate(t *testing.T) {
	ctx := newTestCtx(t)

	gormDb, err := db.Open(ctx, testClientToken)
	if err != nil {
		t.Fatalf("打开数据库异常: %+v", err)
	}
	defer db.Close(ctx, gormDb)

	for _, object := range []interface{}{&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{}} {
		if !gormDb.Migrator().HasTable(object) {
			t.Errorf("表未建出来: %T", object)
		}
	}
	if err = db.AutoMigrate(ctx, gormDb); err != nil {
		t.Errorf("重复自动建表异常: %+v", err)
	}
	if err = db.AutoMigrate(ctx, nil); err == nil {
		t.Errorf("连接为空时自动建表应报错")
	}
}

// 口令错与文件损坏不可区分，统一提示；空口令直接拦下
func TestOpenClientToken(t *testing.T) {
	ctx := newTestCtx(t)

	if _, err := db.Open(ctx, ""); err == nil {
		t.Errorf("空口令应报错")
	}
	gormDb, err := db.Open(ctx, "wrong-client-token")
	if err == nil {
		db.Close(ctx, gormDb)
		t.Fatalf("错误口令应报错")
	}
	if !strings.Contains(err.Error(), "口令错误或数据库文件损坏") {
		t.Errorf("错误口令的提示不符: %+v", err)
	}
	//错误口令时上层业务函数同样要报错，不能读到任何数据
	wrongCtx := tool.SetClaims(util.GenCtx(), &model.Claims{ClientToken: "wrong-client-token"})
	if _, _, err = db.SelectExpense(wrongCtx, model.ExpenseInquiry{}); err == nil {
		t.Errorf("错误口令查询应报错")
	}
}

// ctx里没有Claims/口令时只能报错，不能panic，也不能开出裸库
func TestTransactionWithoutClaims(t *testing.T) {
	config.Config.DbPath = filepath.Join(t.TempDir(), "jotcash.db")

	ctx := util.GenCtx()
	if _, _, err := db.SelectExpense(ctx, model.ExpenseInquiry{}); err == nil {
		t.Errorf("ctx无Claims时查询应报错")
	}
	ctx = tool.SetClaims(util.GenCtx(), &model.Claims{})
	if _, _, err := db.SelectExpense(ctx, model.ExpenseInquiry{}); err == nil {
		t.Errorf("Claims无口令时查询应报错")
	}
	if tool.GetClaims(tool.SetClaims(util.GenCtx(), nil)) != nil {
		t.Errorf("SetClaims传nil不应写入ctx")
	}
}

func newTestExpense() *model.Expense {
	return &model.Expense{
		Id:                     util.GenId(),
		BankName:               "招商银行",
		CardLast4:              "6789",
		ExpenseDate:            time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC),
		ExpenseCurrency:        "USD",
		ExpenseAmount:          decimal.RequireFromString("-1234567890123456.7890"), //20位有效数字、4位小数，且为负数（允许冲正）
		Counterparty:           "亚马逊",
		Remark:                 "退款冲正",
		ExchangeRate:           decimal.RequireFromString("7.12345678"),
		AccountingCurrency:     "CNY",
		AccountingAmount:       decimal.RequireFromString("0.00000000"),
		ExpenseType:            "购物",
		AmortizationMonths:     3,
		AmortizationStartMonth: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		AmortizationEndMonth:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		OperationId:            util.GenId(),
		FileId:                 util.GenId(),
		Version:                1,
	}
}

func TestExpenseCrud(t *testing.T) {
	ctx := newTestCtx(t)
	origin := newTestExpense()

	count, err := db.InsertExpense(ctx, origin)
	if err != nil {
		t.Fatalf("插入支出明细异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("插入影响行数: got=%d want=1", count)
	}
	if origin.CreatedAt.IsZero() || origin.UpdatedAt.IsZero() {
		t.Errorf("CreatedAt/UpdatedAt应由gorm按约定自动填充")
	}

	objects, count, err := db.SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{origin.Id}})
	if err != nil {
		t.Fatalf("查询支出明细异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 {
		t.Fatalf("查询结果: count=%d len=%d want=1", count, len(objects))
	}
	loaded := objects[0]
	if loaded.Id != origin.Id || loaded.OperationId != origin.OperationId || loaded.FileId != origin.FileId {
		t.Errorf("int64字段读写不一致: got=(%d,%d,%d)", loaded.Id, loaded.OperationId, loaded.FileId)
	}
	if !loaded.ExpenseAmount.Equal(origin.ExpenseAmount) {
		t.Errorf("ExpenseAmount精度丢失: got=%s want=%s", loaded.ExpenseAmount, origin.ExpenseAmount)
	}
	if !loaded.ExchangeRate.Equal(origin.ExchangeRate) {
		t.Errorf("ExchangeRate精度丢失: got=%s want=%s", loaded.ExchangeRate, origin.ExchangeRate)
	}
	if !loaded.AccountingAmount.Equal(origin.AccountingAmount) {
		t.Errorf("AccountingAmount零值读写不一致: got=%s want=%s", loaded.AccountingAmount, origin.AccountingAmount)
	}
	if !loaded.ExpenseDate.Equal(origin.ExpenseDate) || !loaded.AmortizationStartMonth.Equal(origin.AmortizationStartMonth) || !loaded.AmortizationEndMonth.Equal(origin.AmortizationEndMonth) {
		t.Errorf("日期字段读写不一致: got=(%v,%v,%v)", loaded.ExpenseDate, loaded.AmortizationStartMonth, loaded.AmortizationEndMonth)
	}
	if loaded.DeletedAt.Valid {
		t.Errorf("未删除的记录DeletedAt不应为有效值")
	}

	loaded.Remark = "改过的备注"
	loaded.Version = 2
	count, err = db.UpdateExpense(ctx, loaded)
	if err != nil {
		t.Fatalf("更新支出明细异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("更新影响行数: got=%d want=1", count)
	}
	objects, _, err = db.SelectExpense(ctx, model.ExpenseInquiry{Version: []int{2}})
	if err != nil {
		t.Fatalf("按版本号查询异常: %+v", err)
	}
	if len(objects) != 1 || objects[0].Remark != "改过的备注" {
		t.Errorf("更新未生效: %+v", objects)
	}

	//无条件删除会被gorm拦下，避免整表清空
	if _, err = db.DeleteExpense(ctx, model.ExpenseInquiry{}); err == nil {
		t.Errorf("无条件删除应报错")
	}

	count, err = db.DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{origin.Id}})
	if err != nil {
		t.Fatalf("删除支出明细异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("删除影响行数: got=%d want=1", count)
	}
	objects, count, err = db.SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{origin.Id}})
	if err != nil {
		t.Fatalf("删除后查询异常: %+v", err)
	}
	if count != 0 || len(objects) != 0 {
		t.Errorf("软删除后默认不应查到: count=%d len=%d", count, len(objects))
	}
	objects, count, err = db.SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{origin.Id}, Deleted: model.DeletedOnly})
	if err != nil {
		t.Fatalf("查询已删除异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || !objects[0].DeletedAt.Valid {
		t.Errorf("只查已删除未命中: count=%d len=%d", count, len(objects))
	}
	_, count, err = db.SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{origin.Id}, Deleted: model.DeletedAll})
	if err != nil {
		t.Fatalf("查询全部异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("含已删除查询: count=%d want=1", count)
	}
}

// 分页只影响返回条数，总数仍是命中的全量
func TestExpensePage(t *testing.T) {
	ctx := newTestCtx(t)

	operationId := util.GenId()
	for i := 0; i < 3; i++ {
		object := &model.Expense{Id: util.GenId(), OperationId: operationId, ExpenseCurrency: "CNY"}
		if _, err := db.InsertExpense(ctx, object); err != nil {
			t.Fatalf("插入支出明细异常: %+v", err)
		}
	}

	objects, count, err := db.SelectExpense(ctx, model.ExpenseInquiry{OperationId: []int64{operationId}, Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("分页查询异常: %+v", err)
	}
	if count != 3 || len(objects) != 2 {
		t.Errorf("第1页: count=%d len=%d want=3/2", count, len(objects))
	}
	objects, count, err = db.SelectExpense(ctx, model.ExpenseInquiry{OperationId: []int64{operationId}, Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("分页查询异常: %+v", err)
	}
	if count != 3 || len(objects) != 1 {
		t.Errorf("第2页: count=%d len=%d want=3/1", count, len(objects))
	}
	objects, count, err = db.SelectExpense(ctx, model.ExpenseInquiry{ExpenseCurrency: []string{"USD"}})
	if err != nil {
		t.Fatalf("查询异常: %+v", err)
	}
	if count != 0 || len(objects) != 0 {
		t.Errorf("未命中的筛选条件应查不到: count=%d len=%d", count, len(objects))
	}
}

// 日期区间、金额区间、模糊匹配三类筛选
func TestExpenseInquiryFilter(t *testing.T) {
	ctx := newTestCtx(t)

	amounts := []string{"-100.5", "9.99", "100.10", "1000"}
	for i := range amounts {
		object := &model.Expense{
			Id:            util.GenId(),
			ExpenseDate:   time.Date(2026, 9, 10+i, 0, 0, 0, 0, time.UTC),
			ExpenseAmount: decimal.RequireFromString(amounts[i]),
			Counterparty:  "亚马逊",
			Remark:        "100%退款",
		}
		if _, err := db.InsertExpense(ctx, object); err != nil {
			t.Fatalf("插入支出明细异常: %+v", err)
		}
	}

	_, count, err := db.SelectExpense(ctx, model.ExpenseInquiry{
		ExpenseDateStart: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC),
		ExpenseDateEnd:   time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("日期区间查询异常: %+v", err)
	}
	if count != 2 {
		t.Errorf("日期区间: count=%d want=2", count)
	}

	//金额列存的是文本，区间必须按数值比，"9.99" 不能大于 "100.10"
	min := decimal.RequireFromString("10")
	max := decimal.RequireFromString("1000")
	_, count, err = db.SelectExpense(ctx, model.ExpenseInquiry{ExpenseAmountMin: &min, ExpenseAmountMax: &max})
	if err != nil {
		t.Fatalf("金额区间查询异常: %+v", err)
	}
	if count != 2 {
		t.Errorf("金额区间: count=%d want=2", count)
	}
	zero := decimal.Zero
	_, count, err = db.SelectExpense(ctx, model.ExpenseInquiry{ExpenseAmountMax: &zero})
	if err != nil {
		t.Fatalf("负数金额查询异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("金额<=0: count=%d want=1", count)
	}

	_, count, err = db.SelectExpense(ctx, model.ExpenseInquiry{CounterpartyLike: "马逊"})
	if err != nil {
		t.Fatalf("模糊查询异常: %+v", err)
	}
	if count != 4 {
		t.Errorf("对手方模糊: count=%d want=4", count)
	}
	//%是like的通配符，转义后必须当普通字符匹配
	_, count, err = db.SelectExpense(ctx, model.ExpenseInquiry{RemarkLike: "100%退"})
	if err != nil {
		t.Fatalf("模糊查询异常: %+v", err)
	}
	if count != 4 {
		t.Errorf("备注模糊: count=%d want=4", count)
	}
	_, count, err = db.SelectExpense(ctx, model.ExpenseInquiry{RemarkLike: "100%不存在"})
	if err != nil {
		t.Fatalf("模糊查询异常: %+v", err)
	}
	if count != 0 {
		t.Errorf("通配符未转义，被当成了模式匹配: count=%d want=0", count)
	}
}

// 排序由上层直接传字符串，db层按白名单换成SQL；不在白名单的报错，不会退化成无序或被拼进SQL
func TestExpenseSort(t *testing.T) {
	ctx := newTestCtx(t)

	amounts := []string{"9.99", "100.10", "-1"}
	for i := range amounts {
		object := &model.Expense{Id: util.GenId(), ExpenseAmount: decimal.RequireFromString(amounts[i])}
		if _, err := db.InsertExpense(ctx, object); err != nil {
			t.Fatalf("插入支出明细异常: %+v", err)
		}
	}

	//金额列是文本，白名单里映射成了cast，否则"9.99"会排在"100.10"后面
	objects, _, err := db.SelectExpense(ctx, model.ExpenseInquiry{Sort: "expense_amount desc"})
	if err != nil {
		t.Fatalf("排序查询异常: %+v", err)
	}
	if len(objects) != 3 || objects[0].ExpenseAmount.String() != "100.1" || objects[2].ExpenseAmount.String() != "-1" {
		t.Errorf("金额倒序不符: %+v", objects)
	}
	objects, _, err = db.SelectExpense(ctx, model.ExpenseInquiry{Sort: "id desc"})
	if err != nil {
		t.Fatalf("排序查询异常: %+v", err)
	}
	if len(objects) != 3 || objects[0].Id < objects[2].Id {
		t.Errorf("ID倒序不符: %+v", objects)
	}
	//不传排序走默认的id正序
	objects, _, err = db.SelectExpense(ctx, model.ExpenseInquiry{})
	if err != nil {
		t.Fatalf("默认排序查询异常: %+v", err)
	}
	if len(objects) != 3 || objects[0].Id > objects[2].Id {
		t.Errorf("默认排序不符: %+v", objects)
	}

	//白名单外的排序串一律报错：注入串、拼错的列名、只写列名不写方向，都不放行
	for _, sort := range []string{"id asc; drop table expense", "expense_amount", "bank_name asc", "1"} {
		if _, _, err = db.SelectExpense(ctx, model.ExpenseInquiry{Sort: sort}); err == nil {
			t.Errorf("非法排序应报错: %s", sort)
		}
	}
	//删除同样要被拦下，且不能真把数据删掉
	if _, err = db.DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{objects[0].Id}, Sort: "bank_name asc"}); err == nil {
		t.Errorf("非法排序的删除应报错")
	}
	if _, count, _ := db.SelectExpense(ctx, model.ExpenseInquiry{}); count != 3 {
		t.Errorf("非法排序的删除不应删掉数据: count=%d want=3", count)
	}
}

func TestOperationLogCrud(t *testing.T) {
	ctx := newTestCtx(t)

	origin := &model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeDataEntry,
		Summary:       "入库 37 笔，来源 2609.csv",
		Result:        model.ResultSuccess,
	}
	if _, err := db.InsertOperationLog(ctx, origin); err != nil {
		t.Fatalf("插入操作日志异常: %+v", err)
	}
	if origin.CreatedAt.IsZero() {
		t.Errorf("CreatedAt应由gorm按约定自动填充")
	}

	objects, count, err := db.SelectOperationLog(ctx, model.OperationLogInquiry{
		OperationType:  []string{model.OperationTypeDataEntry},
		Result:         []string{model.ResultSuccess},
		CreatedAtStart: origin.CreatedAt.Add(-time.Minute),
		CreatedAtEnd:   origin.CreatedAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("查询操作日志异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 {
		t.Fatalf("查询结果: count=%d len=%d want=1", count, len(objects))
	}
	if objects[0].Id != origin.Id || objects[0].Summary != origin.Summary {
		t.Errorf("字段读写不一致: %+v", objects[0])
	}
	_, count, err = db.SelectOperationLog(ctx, model.OperationLogInquiry{CreatedAtStart: origin.CreatedAt.Add(time.Minute)})
	if err != nil {
		t.Fatalf("查询操作日志异常: %+v", err)
	}
	if count != 0 {
		t.Errorf("时间区间外不应命中: count=%d", count)
	}

	//操作日志无DeletedAt，删除即物理删除
	if _, err = db.DeleteOperationLog(ctx, model.OperationLogInquiry{Id: []int64{origin.Id}}); err != nil {
		t.Fatalf("删除操作日志异常: %+v", err)
	}
	if _, count, err = db.SelectOperationLog(ctx, model.OperationLogInquiry{Id: []int64{origin.Id}}); err != nil || count != 0 {
		t.Errorf("删除后仍能查到: count=%d err=%+v", count, err)
	}
}

func TestFileMetaAndFileBlobCrud(t *testing.T) {
	ctx := newTestCtx(t)

	blob := &model.FileBlob{FileHash: "deadbeefcafebabe", FileData: []byte{0x00, 0x01, 0xFF, 0x10}}
	if _, err := db.InsertFileBlob(ctx, blob); err != nil {
		t.Fatalf("插入文件内容异常: %+v", err)
	}
	meta := &model.FileMeta{
		Id:          util.GenId(),
		FileHash:    blob.FileHash,
		FileName:    "2609.csv",
		FileSize:    int64(len(blob.FileData)),
		OperationId: util.GenId(),
	}
	if _, err := db.InsertFileMeta(ctx, meta); err != nil {
		t.Fatalf("插入文件元数据异常: %+v", err)
	}

	blobs, count, err := db.SelectFileBlob(ctx, model.FileBlobInquiry{FileHash: []string{blob.FileHash}})
	if err != nil {
		t.Fatalf("查询文件内容异常: %+v", err)
	}
	if count != 1 || len(blobs) != 1 {
		t.Fatalf("查询结果: count=%d len=%d want=1", count, len(blobs))
	}
	if string(blobs[0].FileData) != string(blob.FileData) {
		t.Errorf("二进制内容读写不一致: got=%v want=%v", blobs[0].FileData, blob.FileData)
	}

	metas, count, err := db.SelectFileMeta(ctx, model.FileMetaInquiry{FileNameLike: "2609"})
	if err != nil {
		t.Fatalf("查询文件元数据异常: %+v", err)
	}
	if count != 1 || len(metas) != 1 {
		t.Fatalf("查询结果: count=%d len=%d want=1", count, len(metas))
	}
	if metas[0].Id != meta.Id || metas[0].FileSize != meta.FileSize {
		t.Errorf("字段读写不一致: %+v", metas[0])
	}

	//同一份内容再插一次要冲突，内容寻址天然去重靠的就是主键
	if _, err = db.InsertFileBlob(ctx, &model.FileBlob{FileHash: blob.FileHash, FileData: []byte("其他内容")}); err == nil {
		t.Errorf("重复内容哈希应插入失败")
	}
}

// 多个handler合到一次事务里，只开一次库，任一步失败整体回滚
func TestTransaction(t *testing.T) {
	ctx := newTestCtx(t)

	expense := newTestExpense()
	operationLog := &model.OperationLog{Id: util.GenId(), OperationType: model.OperationTypeDataEntry, Result: model.ResultSuccess}
	err := db.Transaction(ctx, db.NewExpenseInsertHandler(expense), db.NewOperationLogInsertHandler(operationLog))
	if err != nil {
		t.Fatalf("事务异常: %+v", err)
	}
	if _, count, _ := db.SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 1 {
		t.Errorf("事务内插入的支出明细未落库")
	}
	if _, count, _ := db.SelectOperationLog(ctx, model.OperationLogInquiry{Id: []int64{operationLog.Id}}); count != 1 {
		t.Errorf("事务内插入的操作日志未落库")
	}

	//主键冲突让第二个handler失败，第一个handler的插入必须一起回滚
	rollbackExpense := newTestExpense()
	err = db.Transaction(ctx,
		db.NewExpenseInsertHandler(rollbackExpense),
		db.NewOperationLogInsertHandler(&model.OperationLog{Id: operationLog.Id, Result: model.ResultSuccess}),
	)
	if err == nil {
		t.Fatalf("主键冲突应报错")
	}
	if _, count, _ := db.SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{rollbackExpense.Id}}); count != 0 {
		t.Errorf("事务未回滚")
	}
}
