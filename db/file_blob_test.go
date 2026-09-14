package db

import (
	"testing"

	"github.com/cellargalaxy/jotcash/model"
)

func TestFileBlobCrud(t *testing.T) {
	ctx := newTestCtx(t)

	origin := &model.FileBlob{FileHash: "deadbeefcafebabe", FileData: []byte{0x00, 0x01, 0xFF, 0x10}}
	if _, err := InsertFileBlob(ctx, origin); err != nil {
		t.Fatalf("插入文件内容异常: %+v", err)
	}

	objects, count, err := SelectFileBlob(ctx, model.FileBlobInquiry{FileHash: []string{origin.FileHash}})
	if err != nil {
		t.Fatalf("查询文件内容异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 {
		t.Fatalf("查询结果: count=%d len=%d want=1", count, len(objects))
	}
	if string(objects[0].FileData) != string(origin.FileData) {
		t.Errorf("二进制内容读写不一致: got=%v want=%v", objects[0].FileData, origin.FileData)
	}

	if _, err = InsertFileBlob(ctx, &model.FileBlob{FileHash: origin.FileHash, FileData: []byte("其他内容")}); err == nil {
		t.Errorf("重复内容哈希应插入失败")
	}
}

// D-2：内容寻址去重，同一哈希重复插要跳过而不是撞主键报错
func TestFileBlobInsertIgnore(t *testing.T) {
	ctx := newTestCtx(t)

	origin := &model.FileBlob{FileHash: "deadbeefcafebabe", FileData: []byte("原件")}
	//同一批里的重复也只插一条
	handler := NewFileBlobInsertIgnoreHandler(origin, origin)
	if err := Transaction(ctx, handler); err != nil {
		t.Fatalf("插入文件内容异常: %+v", err)
	}
	if handler.Count != 1 {
		t.Errorf("同一批里的重复内容应只插一条: count=%d", handler.Count)
	}

	//库里已有的哈希，再插一次不报错也不覆盖原内容
	handler = NewFileBlobInsertIgnoreHandler(&model.FileBlob{FileHash: origin.FileHash, FileData: []byte("其他内容")})
	if err := Transaction(ctx, handler); err != nil {
		t.Fatalf("重复内容哈希不应报错: %+v", err)
	}
	if handler.Count != 0 {
		t.Errorf("重复内容哈希不应插入: count=%d", handler.Count)
	}
	objects, count, err := SelectFileBlob(ctx, model.FileBlobInquiry{FileHash: []string{origin.FileHash}})
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
