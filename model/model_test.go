package model_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/ncruces/go-sqlite3/gormlite"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openTestDB 用与生产计划一致的 SQLite 驱动（github.com/ncruces/go-sqlite3，见需求文档前提2）
// 打开一个独立的内存库，并执行 model.AutoMigrate 建表，验证“服务首次启动自动建表”这条诉求。
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := memdb.TestDB(t)
	db, err := gorm.Open(gormlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("自动建表失败: %v", err)
	}
	return db
}

// TestExpenseRoundTrip 验证 Expense 全字段（尤其是 decimal 金额/汇率、int64 ID、日期、软删除）
// 经 gorm 写入 SQLite 再读回后精度/取值不失真。
func TestExpenseRoundTrip(t *testing.T) {
	db := openTestDB(t)

	expenseDate := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	startMonth := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	endMonth := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	origin := model.Expense{
		Id:                     util.GenId(),
		BankName:               "招商银行",
		CardLast4:              "6789",
		ExpenseDate:            expenseDate,
		ExpenseCurrency:        "USD",
		ExpenseAmount:          decimal.RequireFromString("-1234567890123456.7890"), // 20位有效数字、4位小数，贴近decimal(20,4)上限，且为负数（允许冲正）
		Counterparty:           "亚马逊",
		Remark:                 "退款冲正",
		ExchangeRate:           decimal.RequireFromString("7.12345678"), // 8位小数，贴近decimal(20,8)精度上限
		AccountingCurrency:     "CNY",
		AccountingAmount:       decimal.RequireFromString("0.00000000"), // 零值也是允许的合法取值
		ExpenseType:            "购物",
		AmortizationMonths:     3,
		AmortizationStartMonth: startMonth,
		AmortizationEndMonth:   endMonth,
		AuditId:                util.GenId(),
		FileId:                 util.GenId(),
		Version:                1,
	}

	if err := db.Create(&origin).Error; err != nil {
		t.Fatalf("写入Expense失败: %v", err)
	}

	var loaded model.Expense
	if err := db.First(&loaded, origin.Id).Error; err != nil {
		t.Fatalf("读取Expense失败: %v", err)
	}

	if loaded.Id != origin.Id {
		t.Errorf("Id读写不一致: got=%d want=%d", loaded.Id, origin.Id)
	}
	if loaded.AuditId != origin.AuditId || loaded.FileId != origin.FileId {
		t.Errorf("AuditId/FileId读写不一致: got=(%d,%d) want=(%d,%d)", loaded.AuditId, loaded.FileId, origin.AuditId, origin.FileId)
	}
	if !loaded.ExpenseAmount.Equal(origin.ExpenseAmount) {
		t.Errorf("ExpenseAmount精度丢失: got=%s want=%s", loaded.ExpenseAmount.String(), origin.ExpenseAmount.String())
	}
	if !loaded.ExchangeRate.Equal(origin.ExchangeRate) {
		t.Errorf("ExchangeRate精度丢失: got=%s want=%s", loaded.ExchangeRate.String(), origin.ExchangeRate.String())
	}
	if !loaded.AccountingAmount.Equal(origin.AccountingAmount) {
		t.Errorf("AccountingAmount零值读写不一致: got=%s want=%s", loaded.AccountingAmount.String(), origin.AccountingAmount.String())
	}
	if !loaded.ExpenseDate.Equal(origin.ExpenseDate) {
		t.Errorf("ExpenseDate读写不一致: got=%v want=%v", loaded.ExpenseDate, origin.ExpenseDate)
	}
	if !loaded.AmortizationStartMonth.Equal(origin.AmortizationStartMonth) || !loaded.AmortizationEndMonth.Equal(origin.AmortizationEndMonth) {
		t.Errorf("摊分起止月读写不一致: got=(%v,%v) want=(%v,%v)", loaded.AmortizationStartMonth, loaded.AmortizationEndMonth, origin.AmortizationStartMonth, origin.AmortizationEndMonth)
	}
	if loaded.DeletedAt.Valid {
		t.Errorf("未删除的记录DeletedAt不应为有效值")
	}

	// 软删除：默认查询应自动过滤，Unscoped 才能看到
	if err := db.Delete(&loaded).Error; err != nil {
		t.Fatalf("软删除失败: %v", err)
	}
	var afterDelete model.Expense
	err := db.First(&afterDelete, origin.Id).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("软删除后默认查询应返回ErrRecordNotFound，实际: %v", err)
	}
	var unscoped model.Expense
	if err := db.Unscoped().First(&unscoped, origin.Id).Error; err != nil {
		t.Fatalf("Unscoped查询应能读到软删除记录: %v", err)
	}
	if !unscoped.DeletedAt.Valid {
		t.Errorf("软删除后DeletedAt应为有效值")
	}
}

// TestAuditLogRoundTrip 验证 AuditLog 用 CreatedAt 承载“操作时间”后，gorm 约定的自动填充与读写正确。
func TestAuditLogRoundTrip(t *testing.T) {
	db := openTestDB(t)

	origin := model.AuditLog{
		Id:         util.GenId(),
		ActionType: model.ActionTypeDataEntry,
		ObjectType: "",
		ObjectId:   0,
		Summary:    "入库 37 笔，来源 2609.csv",
		Changes:    "",
		Result:     model.ResultSuccess,
	}
	if err := db.Create(&origin).Error; err != nil {
		t.Fatalf("写入AuditLog失败: %v", err)
	}
	if origin.CreatedAt.IsZero() {
		t.Errorf("CreatedAt应由gorm按约定自动填充，不应为零值")
	}

	var loaded model.AuditLog
	if err := db.First(&loaded, origin.Id).Error; err != nil {
		t.Fatalf("读取AuditLog失败: %v", err)
	}
	if loaded.ActionType != model.ActionTypeDataEntry || loaded.Result != model.ResultSuccess {
		t.Errorf("枚举字段读写不一致: got=(%s,%s)", loaded.ActionType, loaded.Result)
	}
	if loaded.Id != origin.Id {
		t.Errorf("Id读写不一致: got=%d want=%d", loaded.Id, origin.Id)
	}
}

// TestFileMetaAndFileBlobRoundTrip 验证 FileMeta/FileBlob 的int64 ID、Purpose枚举、二进制内容读写正确。
func TestFileMetaAndFileBlobRoundTrip(t *testing.T) {
	db := openTestDB(t)

	blob := model.FileBlob{ContentHash: "deadbeefcafebabe", Content: []byte{0x00, 0x01, 0xFF, 0x10}}
	if err := db.Create(&blob).Error; err != nil {
		t.Fatalf("写入FileBlob失败: %v", err)
	}

	meta := model.FileMeta{
		Id:          util.GenId(),
		ContentHash: blob.ContentHash,
		FileName:    "2609.csv",
		Purpose:     model.PurposeEntrySnapshot,
		FileSize:    int64(len(blob.Content)),
		AuditId:     util.GenId(),
	}
	if err := db.Create(&meta).Error; err != nil {
		t.Fatalf("写入FileMeta失败: %v", err)
	}

	var loadedMeta model.FileMeta
	if err := db.First(&loadedMeta, meta.Id).Error; err != nil {
		t.Fatalf("读取FileMeta失败: %v", err)
	}
	if loadedMeta.Purpose != model.PurposeEntrySnapshot {
		t.Errorf("Purpose读写不一致: got=%s want=%s", loadedMeta.Purpose, model.PurposeEntrySnapshot)
	}
	if loadedMeta.FileSize != meta.FileSize {
		t.Errorf("FileSize读写不一致: got=%d want=%d", loadedMeta.FileSize, meta.FileSize)
	}

	var loadedBlob model.FileBlob
	if err := db.First(&loadedBlob, "content_hash = ?", blob.ContentHash).Error; err != nil {
		t.Fatalf("读取FileBlob失败: %v", err)
	}
	if string(loadedBlob.Content) != string(blob.Content) {
		t.Errorf("Content二进制内容读写不一致: got=%v want=%v", loadedBlob.Content, blob.Content)
	}
}

// TestFileBlobContentExcludedFromJSON 验证 FileBlob.Content 打了 json:"-"，不会随 String()/序列化混入日志或响应体。
func TestFileBlobContentExcludedFromJSON(t *testing.T) {
	blob := model.FileBlob{ContentHash: "abc123", Content: []byte("secret-bytes")}
	data, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if strings.Contains(string(data), "secret-bytes") {
		t.Errorf("FileBlob.Content不应出现在JSON序列化结果中，实际输出: %s", string(data))
	}
	if blob.String() != string(data) {
		t.Errorf("String()应与json.Marshal结果一致")
	}
}

// TestExpenseJSONRoundTrip 验证 decimal.Decimal 字段经 JSON 序列化/反序列化后精度不丢失。
func TestExpenseJSONRoundTrip(t *testing.T) {
	origin := model.Expense{
		Id:            util.GenId(),
		ExpenseAmount: decimal.RequireFromString("99.99"),
		ExchangeRate:  decimal.RequireFromString("1.00000001"),
	}
	data, err := json.Marshal(origin)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var loaded model.Expense
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if !loaded.ExpenseAmount.Equal(origin.ExpenseAmount) {
		t.Errorf("ExpenseAmount的JSON往返精度丢失: got=%s want=%s", loaded.ExpenseAmount.String(), origin.ExpenseAmount.String())
	}
	if !loaded.ExchangeRate.Equal(origin.ExchangeRate) {
		t.Errorf("ExchangeRate的JSON往返精度丢失: got=%s want=%s", loaded.ExchangeRate.String(), origin.ExchangeRate.String())
	}
}
