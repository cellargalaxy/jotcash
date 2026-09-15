package rdb

import (
	"testing"

	"github.com/cellargalaxy/jotcash/model"
)

func TestFileBlobCrud(t *testing.T) {
	ctx := newTestCtx(t)

	origin := &model.FileBlob{FileHash: "deadbeefcafebabe", FileData: []byte{0x00, 0x01, 0xFF, 0x10}}
	if _, err := insertFileBlob(ctx, origin); err != nil {
		t.Fatalf("插入文件内容异常: %+v", err)
	}

	objects, count, err := selectFileBlob(ctx, model.FileBlobInquiry{FileHash: []string{origin.FileHash}})
	if err != nil {
		t.Fatalf("查询文件内容异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 {
		t.Fatalf("查询结果: count=%d len=%d want=1", count, len(objects))
	}
	if string(objects[0].FileData) != string(origin.FileData) {
		t.Errorf("二进制内容读写不一致: got=%v want=%v", objects[0].FileData, origin.FileData)
	}
}

// 内容寻址去重，同一哈希重复插要跳过而不是撞主键报错
func TestFileBlobInsertRepeat(t *testing.T) {
	ctx := newTestCtx(t)

	origin := &model.FileBlob{FileHash: "deadbeefcafebabe", FileData: []byte("原件")}
	//同一批里的重复也只插一条
	transaction, err := NewTransaction(ctx)
	if err != nil {
		t.Fatalf("开事务异常: %+v", err)
	}
	handler := NewFileBlobInsertHandler(origin, origin)
	err = transaction.AddCommit(handler).Exec(ctx)
	transaction.Close(ctx)
	if err != nil {
		t.Fatalf("插入文件内容异常: %+v", err)
	}
	if handler.Count != 1 {
		t.Errorf("同一批里的重复内容应只插一条: count=%d", handler.Count)
	}

	//库里已有的哈希，再插一次不报错也不覆盖原内容
	count, err := insertFileBlob(ctx, &model.FileBlob{FileHash: origin.FileHash, FileData: []byte("其他内容")})
	if err != nil {
		t.Fatalf("重复内容哈希不应报错: %+v", err)
	}
	if count != 0 {
		t.Errorf("重复内容哈希不应插入: count=%d", count)
	}
	objects, count, err := selectFileBlob(ctx, model.FileBlobInquiry{FileHash: []string{origin.FileHash}})
	if err != nil {
		t.Fatalf("查询文件内容异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 {
		t.Fatalf("查询结果: count=%d len=%d want=1", count, len(objects))
	}
	if string(objects[0].FileData) != string(origin.FileData) {
		t.Errorf("原内容不应被覆盖: got=%s want=%s", objects[0].FileData, origin.FileData)
	}
}

func TestFileBlobUpdate(t *testing.T) {
	ctx := newTestCtx(t)

	origin := &model.FileBlob{FileHash: "deadbeefcafebabe", FileData: []byte("原件")}
	if _, err := insertFileBlob(ctx, origin); err != nil {
		t.Fatalf("插入文件内容异常: %+v", err)
	}

	//内容寻址下改内容等于换哈希，这里只钉住更新链路本身能走通
	origin.FileData = []byte("改过的原件")
	count, err := updateFileBlob(ctx, origin)
	if err != nil {
		t.Fatalf("更新文件内容异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("更新影响行数: got=%d want=1", count)
	}
	objects, count, err := selectFileBlob(ctx, model.FileBlobInquiry{FileHash: []string{origin.FileHash}})
	if err != nil {
		t.Fatalf("更新后查询异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || string(objects[0].FileData) != "改过的原件" {
		t.Errorf("更新未生效: %+v", objects)
	}

	if _, _, err = selectFileBlob(ctx, model.FileBlobInquiry{Sort: "file_data asc"}); err == nil {
		t.Errorf("白名单外的排序应报错")
	}
	//全表查一遍，顺带把默认排序与分页走到
	if objects, count, err = selectFileBlob(ctx, model.FileBlobInquiry{Page: 1, PageSize: 10}); err != nil || count != 1 || len(objects) != 1 {
		t.Errorf("分页查询: count=%d len=%d err=%+v", count, len(objects), err)
	}
}
