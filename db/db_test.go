package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	logrus.SetLevel(logrus.WarnLevel)
	os.Exit(m.Run())
}

// openTestDb 开一个独立内存库并建表，返回已带上连接的ctx，供各用例复用
func openTestDb(t *testing.T) context.Context {
	t.Helper()
	ctx := util.GenCtx()
	gormDb, err := db.Open(ctx, memdb.TestDB(t))
	if err != nil {
		t.Fatalf("打开测试数据库异常: %+v", err)
	}
	err = db.AutoMigrate(ctx, gormDb)
	if err != nil {
		t.Fatalf("自动建表异常: %+v", err)
	}
	return db.SetDb(ctx, gormDb)
}

// 四张表都要建出来，且重复执行不报错（服务每次启动都会跑一遍）
func TestAutoMigrate(t *testing.T) {
	ctx := openTestDb(t)
	gormDb := db.GetDb(ctx)

	for _, object := range []interface{}{&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{}} {
		if !gormDb.Migrator().HasTable(object) {
			t.Errorf("表未建出来: %T", object)
		}
	}
	if err := db.AutoMigrate(ctx, gormDb); err != nil {
		t.Errorf("重复自动建表异常: %+v", err)
	}
	if err := db.AutoMigrate(ctx, nil); err == nil {
		t.Errorf("连接为空时自动建表应报错")
	}
}

// 连接没放进ctx时只能报错，不能panic
func TestTransactionWithoutDb(t *testing.T) {
	ctx := util.GenCtx()
	if _, _, err := db.SelectExpense(ctx, model.ExpenseInquiry{}); err == nil {
		t.Errorf("ctx无连接时查询应报错")
	}
}

func TestExpenseCrud(t *testing.T) {
	ctx := openTestDb(t)

	expenseDate := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	origin := &model.Expense{
		Id:                     util.GenId(),
		BankName:               "招商银行",
		CardLast4:              "6789",
		ExpenseDate:            expenseDate,
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
		t.Errorf("int64字段读写不一致: got=(%d,%d,%d) want=(%d,%d,%d)", loaded.Id, loaded.OperationId, loaded.FileId, origin.Id, origin.OperationId, origin.FileId)
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
		t.Errorf("软删除后默认查询不应查到: count=%d len=%d", count, len(objects))
	}
	var unscoped model.Expense
	if err = db.GetDb(ctx).Unscoped().Take(&unscoped, origin.Id).Error; err != nil {
		t.Fatalf("Unscoped应能读到软删除记录: %+v", err)
	}
	if !unscoped.DeletedAt.Valid {
		t.Errorf("软删除后DeletedAt应为有效值")
	}
}

// 分页只影响返回条数，总数仍是命中的全量
func TestExpensePage(t *testing.T) {
	ctx := openTestDb(t)

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

func TestOperationLogCrud(t *testing.T) {
	ctx := openTestDb(t)

	origin := &model.OperationLog{
		Id:            util.GenId(),
		OperationType: "数据入库",
		Summary:       "入库 37 笔，来源 2609.csv",
		Result:        "成功",
	}
	if _, err := db.InsertOperationLog(ctx, origin); err != nil {
		t.Fatalf("插入操作日志异常: %+v", err)
	}
	if origin.CreatedAt.IsZero() {
		t.Errorf("CreatedAt应由gorm按约定自动填充")
	}

	objects, count, err := db.SelectOperationLog(ctx, model.OperationLogInquiry{OperationType: []string{"数据入库"}, Result: []string{"成功"}})
	if err != nil {
		t.Fatalf("查询操作日志异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 {
		t.Fatalf("查询结果: count=%d len=%d want=1", count, len(objects))
	}
	if objects[0].Id != origin.Id || objects[0].Summary != origin.Summary {
		t.Errorf("字段读写不一致: %+v", objects[0])
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
	ctx := openTestDb(t)

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

	metas, count, err := db.SelectFileMeta(ctx, model.FileMetaInquiry{FileHash: []string{blob.FileHash}})
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
