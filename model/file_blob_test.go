package model_test

import (
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/model"
)

func TestFileBlobTableName(t *testing.T) {
	if got := (model.FileBlob{}).TableName(); got != "file_blob" {
		t.Errorf("FileBlob表名: got=%s want=file_blob", got)
	}
}

func TestFileBlobJson(t *testing.T) {
	blob := model.FileBlob{FileHash: "abc123", FileData: []byte("secret-bytes")}
	if strings.Contains(blob.String(), "secret-bytes") {
		t.Errorf("FileData不应出现在序列化结果中: %s", blob.String())
	}
	if !strings.Contains(blob.String(), "abc123") {
		t.Errorf("FileHash应出现在序列化结果中: %s", blob.String())
	}
}
