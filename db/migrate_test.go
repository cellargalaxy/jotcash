package db

import (
	"strings"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"gorm.io/gorm"
)

// 四张表都不该被物理删：明细只软删，审计与文件只增不删
func TestMigrateTriggerForbidDelete(t *testing.T) {
	ctx := newTestCtx(t)
	db, err := Open(ctx)
	if err != nil {
		t.Fatalf("打开数据库异常: %+v", err)
	}
	defer util.CloseDb(ctx, db)

	operationId, fileId := util.GenId(), util.GenId()
	seeds := []any{
		&model.OperationLog{Id: operationId, OperationType: model.OperationTypeDataEntry, Result: model.ResultSuccess},
		&model.FileBlob{FileHash: "deadbeefcafebabe", FileData: []byte("原件")},
		&model.FileMeta{Id: fileId, FileHash: "deadbeefcafebabe", FileName: "2609.csv", OperationId: operationId},
		&model.Expense{Id: util.GenId(), ExpenseDate: time.Now(), OperationId: operationId, FileId: fileId},
	}
	for i := range seeds {
		if err := db.WithContext(ctx).Create(seeds[i]).Error; err != nil {
			t.Fatalf("造数异常: %+v", err)
		}
	}

	for _, table := range []string{
		model.Expense{}.TableName(),
		model.OperationLog{}.TableName(),
		model.FileMeta{}.TableName(),
		model.FileBlob{}.TableName(),
	} {
		//operation_log里还有建库时那条「系统初始化」，所以拿删之前的行数做基准，而不是写死1
		var before, after int64
		db.WithContext(ctx).Table(table).Count(&before)
		err := db.WithContext(ctx).Exec("delete from `" + table + "`").Error
		if err == nil {
			t.Errorf("%s 的物理删除应被触发器挡回", table)
			continue
		}
		if !strings.Contains(err.Error(), "禁止物理删除") {
			t.Errorf("%s 报错不是触发器抛的: %+v", table, err)
		}
		db.WithContext(ctx).Table(table).Count(&after)
		if after != before {
			t.Errorf("%s 被删掉了: got=%d want=%d", table, after, before)
		}
	}
}

// 软删走的是update，触发器不该碰它
func TestMigrateTriggerAllowSoftDelete(t *testing.T) {
	ctx := newTestCtx(t)

	object := &model.Expense{Id: util.GenId(), ExpenseDate: time.Now(), ExpenseCurrency: "CNY", OperationId: util.GenId()}
	if _, err := InsertExpense(ctx, object); err != nil {
		t.Fatalf("插入明细异常: %+v", err)
	}
	count, err := DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{object.Id}})
	if err != nil {
		t.Fatalf("软删明细异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("软删影响行数: got=%d want=1", count)
	}
	if _, count, _ = SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{object.Id}, Deleted: model.DeletedOnly}); count != 1 {
		t.Errorf("软删后应能用DeletedOnly查到: count=%d want=1", count)
	}
}

// gormlite改表是「建临时表→拷数据→DROP旧表→改名」，触发器会随旧表一起没掉，建表链必须每次重挂
func TestMigrateTriggerRebuild(t *testing.T) {
	ctx := newTestCtx(t)
	db, err := Open(ctx)
	if err != nil {
		t.Fatalf("打开数据库异常: %+v", err)
	}
	defer util.CloseDb(ctx, db)

	trigger := model.Expense{}.TableName() + "_forbid_delete"
	if err := db.WithContext(ctx).Exec("drop trigger `" + trigger + "`").Error; err != nil {
		t.Fatalf("删触发器异常: %+v", err)
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return migrate(ctx, tx) }); err != nil {
		t.Fatalf("重跑建表异常: %+v", err)
	}
	var count int64
	db.WithContext(ctx).Raw("select count(1) from sqlite_master where type='trigger' and name=?", trigger).Scan(&count)
	if count != 1 {
		t.Errorf("建表链没把触发器重挂回来: count=%d want=1", count)
	}
}
