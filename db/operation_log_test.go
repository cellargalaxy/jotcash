package db

import (
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
)

func TestOperationLogCrud(t *testing.T) {
	ctx := newTestCtx(t)

	origin := &model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeDataEntry,
		Summary:       "入库 37 笔，来源 2609.csv",
		Result:        model.ResultSuccess,
	}
	if _, err := InsertOperationLog(ctx, origin); err != nil {
		t.Fatalf("插入操作日志异常: %+v", err)
	}
	if origin.CreatedAt.IsZero() {
		t.Errorf("CreatedAt应由gorm按约定自动填充")
	}

	objects, count, err := SelectOperationLog(ctx, model.OperationLogInquiry{
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
	_, count, err = SelectOperationLog(ctx, model.OperationLogInquiry{CreatedAtStart: origin.CreatedAt.Add(time.Minute)})
	if err != nil {
		t.Fatalf("查询操作日志异常: %+v", err)
	}
	if count != 0 {
		t.Errorf("时间区间外不应命中: count=%d", count)
	}

	origin.Result = model.ResultPartial
	count, err = UpdateOperationLog(ctx, origin)
	if err != nil {
		t.Fatalf("更新操作日志异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("更新影响行数: got=%d want=1", count)
	}
	if _, count, _ = SelectOperationLog(ctx, model.OperationLogInquiry{Result: []string{model.ResultPartial}}); count != 1 {
		t.Errorf("更新未生效: count=%d want=1", count)
	}
}

func TestOperationLogInquiry(t *testing.T) {
	ctx := newTestCtx(t)

	objectId := util.GenId()
	objects := []*model.OperationLog{
		{Id: util.GenId(), OperationType: model.OperationTypeExpenseEdit, ObjectType: model.Expense{}.TableName(), ObjectId: objectId, Summary: "改了 100%的备注", Result: model.ResultSuccess},
		{Id: util.GenId(), OperationType: model.OperationTypeExpenseDelete, ObjectType: model.Expense{}.TableName(), ObjectId: util.GenId(), Summary: "改了 100元的备注", Result: model.ResultFailure},
		{Id: util.GenId(), OperationType: model.OperationTypeDbExport, ObjectType: model.FileMeta{}.TableName(), ObjectId: util.GenId(), Summary: "导出快照", Result: model.ResultPartial},
	}
	if _, err := InsertOperationLog(ctx, objects...); err != nil {
		t.Fatalf("插入操作日志异常: %+v", err)
	}

	//建库那条「系统初始化」也在库里，按Id筛选才数得准
	if _, count, _ := SelectOperationLog(ctx, model.OperationLogInquiry{Id: []int64{objects[0].Id, objects[1].Id}}); count != 2 {
		t.Errorf("按Id筛选: count=%d want=2", count)
	}
	if _, count, _ := SelectOperationLog(ctx, model.OperationLogInquiry{ObjectType: []string{model.Expense{}.TableName()}}); count != 2 {
		t.Errorf("按对象类型筛选: count=%d want=2", count)
	}
	if _, count, _ := SelectOperationLog(ctx, model.OperationLogInquiry{ObjectId: []int64{objectId}}); count != 1 {
		t.Errorf("按对象Id筛选: count=%d want=1", count)
	}
	if _, count, _ := SelectOperationLog(ctx, model.OperationLogInquiry{Result: []string{model.ResultFailure, model.ResultPartial}}); count != 2 {
		t.Errorf("按结果筛选: count=%d want=2", count)
	}
	//百分号没转义的话"100%的"会退化成"100"开头的模式，把另一条也匹进来
	if _, count, _ := SelectOperationLog(ctx, model.OperationLogInquiry{SummaryLike: "100%的"}); count != 1 {
		t.Errorf("摘要通配符未转义: count=%d want=1", count)
	}

	loaded, count, err := SelectOperationLog(ctx, model.OperationLogInquiry{
		ObjectType: []string{model.Expense{}.TableName()},
		Sort:       "id desc",
		Page:       2,
		PageSize:   1,
	})
	if err != nil {
		t.Fatalf("分页查询异常: %+v", err)
	}
	if count != 2 || len(loaded) != 1 || loaded[0].Id != objects[0].Id {
		t.Errorf("倒序分页不符: count=%d %+v", count, loaded)
	}
	if _, _, err = SelectOperationLog(ctx, model.OperationLogInquiry{Sort: "operation_type asc"}); err == nil {
		t.Errorf("白名单外的排序应报错")
	}
}
