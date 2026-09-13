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
