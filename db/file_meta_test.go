package db

import (
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
)

func TestFileMetaCrud(t *testing.T) {
	ctx := newTestCtx(t)

	origin := &model.FileMeta{
		Id:          util.GenId(),
		FileHash:    "deadbeefcafebabe",
		FileName:    "2609.csv",
		FileSize:    1024,
		OperationId: util.GenId(),
	}
	if _, err := insertFileMeta(ctx, origin); err != nil {
		t.Fatalf("插入文件元数据异常: %+v", err)
	}
	if origin.CreatedAt.IsZero() {
		t.Errorf("CreatedAt应由gorm按约定自动填充")
	}

	objects, count, err := selectFileMeta(ctx, model.FileMetaInquiry{FileNameLike: "2609"})
	if err != nil {
		t.Fatalf("查询文件元数据异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 {
		t.Fatalf("查询结果: count=%d len=%d want=1", count, len(objects))
	}
	if objects[0].Id != origin.Id || objects[0].FileSize != origin.FileSize || objects[0].FileHash != origin.FileHash {
		t.Errorf("字段读写不一致: %+v", objects[0])
	}

	other := &model.FileMeta{Id: util.GenId(), FileHash: origin.FileHash, FileName: "2610.csv", OperationId: util.GenId()}
	if _, err = insertFileMeta(ctx, other); err != nil {
		t.Fatalf("插入文件元数据异常: %+v", err)
	}
	if _, count, _ = selectFileMeta(ctx, model.FileMetaInquiry{FileHash: []string{origin.FileHash}}); count != 2 {
		t.Errorf("同哈希的元数据: count=%d want=2", count)
	}
}

func TestFileMetaInquiry(t *testing.T) {
	ctx := newTestCtx(t)

	operationId := util.GenId()
	objects := []*model.FileMeta{
		{Id: util.GenId(), FileHash: "hash-a", FileName: "2609_对账.csv", FileSize: 1, OperationId: operationId},
		{Id: util.GenId(), FileHash: "hash-b", FileName: "2610%对账.csv", FileSize: 2, OperationId: operationId},
		{Id: util.GenId(), FileHash: "hash-c", FileName: "2611.csv", FileSize: 3, OperationId: util.GenId()},
	}
	if _, err := insertFileMeta(ctx, objects...); err != nil {
		t.Fatalf("插入文件元数据异常: %+v", err)
	}

	if _, count, _ := selectFileMeta(ctx, model.FileMetaInquiry{Id: []int64{objects[0].Id, objects[2].Id}}); count != 2 {
		t.Errorf("按Id筛选: count=%d want=2", count)
	}
	if _, count, _ := selectFileMeta(ctx, model.FileMetaInquiry{FileName: []string{"2611.csv"}}); count != 1 {
		t.Errorf("按文件名筛选: count=%d want=1", count)
	}
	if _, count, _ := selectFileMeta(ctx, model.FileMetaInquiry{OperationId: []int64{operationId}}); count != 2 {
		t.Errorf("按操作Id筛选: count=%d want=2", count)
	}
	//下划线是like的单字符通配符，没转义的话"2609_"会把"2610%"也匹进来
	if _, count, _ := selectFileMeta(ctx, model.FileMetaInquiry{FileNameLike: "2609_"}); count != 1 {
		t.Errorf("下划线未转义，被当成了通配符: count=%d want=1", count)
	}
	if _, count, _ := selectFileMeta(ctx, model.FileMetaInquiry{FileNameLike: "2610%"}); count != 1 {
		t.Errorf("百分号未转义: count=%d want=1", count)
	}

	if _, count, _ := selectFileMeta(ctx, model.FileMetaInquiry{
		CreatedAtStart: objects[0].CreatedAt.Add(-time.Minute),
		CreatedAtEnd:   objects[0].CreatedAt.Add(time.Minute),
	}); count != 3 {
		t.Errorf("创建时间区间内: count=%d want=3", count)
	}
	if _, count, _ := selectFileMeta(ctx, model.FileMetaInquiry{CreatedAtStart: objects[0].CreatedAt.Add(time.Minute)}); count != 0 {
		t.Errorf("创建时间区间外不应命中: count=%d want=0", count)
	}

	loaded, count, err := selectFileMeta(ctx, model.FileMetaInquiry{Sort: "file_name desc", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("分页查询异常: %+v", err)
	}
	if count != 3 || len(loaded) != 2 || loaded[0].FileName != "2611.csv" {
		t.Errorf("倒序分页不符: count=%d %+v", count, loaded)
	}
	if _, _, err = selectFileMeta(ctx, model.FileMetaInquiry{Sort: "file_size desc"}); err == nil {
		t.Errorf("白名单外的排序应报错")
	}
}

func TestFileMetaUpdate(t *testing.T) {
	ctx := newTestCtx(t)

	origin := &model.FileMeta{Id: util.GenId(), FileHash: "deadbeefcafebabe", FileName: "2609.csv", FileSize: 1024, OperationId: util.GenId()}
	if _, err := insertFileMeta(ctx, origin); err != nil {
		t.Fatalf("插入文件元数据异常: %+v", err)
	}

	origin.FileName = "2609-改名.csv"
	count, err := updateFileMeta(ctx, origin)
	if err != nil {
		t.Fatalf("更新文件元数据异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("更新影响行数: got=%d want=1", count)
	}
	objects, count, err := selectFileMeta(ctx, model.FileMetaInquiry{Id: []int64{origin.Id}})
	if err != nil {
		t.Fatalf("更新后查询异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || objects[0].FileName != "2609-改名.csv" {
		t.Fatalf("更新未生效: %+v", objects)
	}
	//CreatedAt没有主动改，不该被Select("*")连带抹成零值
	if objects[0].CreatedAt.IsZero() || objects[0].FileSize != origin.FileSize {
		t.Errorf("未改动的字段被抹掉了: %+v", objects[0])
	}
}
