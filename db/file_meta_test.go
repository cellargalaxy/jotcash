package db

import (
	"testing"

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
	if _, err := InsertFileMeta(ctx, origin); err != nil {
		t.Fatalf("插入文件元数据异常: %+v", err)
	}
	if origin.CreatedAt.IsZero() {
		t.Errorf("CreatedAt应由gorm按约定自动填充")
	}

	objects, count, err := SelectFileMeta(ctx, model.FileMetaInquiry{FileNameLike: "2609"})
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
	if _, err = InsertFileMeta(ctx, other); err != nil {
		t.Fatalf("插入文件元数据异常: %+v", err)
	}
	if _, count, _ = SelectFileMeta(ctx, model.FileMetaInquiry{FileHash: []string{origin.FileHash}}); count != 2 {
		t.Errorf("同哈希的元数据: count=%d want=2", count)
	}
}
