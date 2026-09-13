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

	//操作日志无DeletedAt，删除即物理删除
	if _, err = DeleteOperationLog(ctx, model.OperationLogInquiry{Id: []int64{origin.Id}}); err != nil {
		t.Fatalf("删除操作日志异常: %+v", err)
	}
	if _, count, err = SelectOperationLog(ctx, model.OperationLogInquiry{Id: []int64{origin.Id}}); err != nil || count != 0 {
		t.Errorf("删除后仍能查到: count=%d err=%+v", count, err)
	}
}
