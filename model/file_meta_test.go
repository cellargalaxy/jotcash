package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
)

func TestFileMetaTableName(t *testing.T) {
	if got := (model.FileMeta{}).TableName(); got != "file_meta" {
		t.Errorf("FileMeta表名: got=%s want=file_meta", got)
	}
}

func TestFileMetaJson(t *testing.T) {
	origin := model.FileMeta{Id: util.GenId(), FileHash: "deadbeefcafebabe", FileName: "2609.csv", FileSize: 1024, OperationId: util.GenId()}
	data, err := json.Marshal(origin)
	if err != nil {
		t.Fatalf("序列化异常: %+v", err)
	}
	var loaded model.FileMeta
	if err = json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("反序列化异常: %+v", err)
	}
	if loaded.Id != origin.Id || loaded.OperationId != origin.OperationId || loaded.FileSize != origin.FileSize {
		t.Errorf("int64往返不一致: %+v", loaded)
	}
	if origin.String() != string(data) {
		t.Errorf("String()应与json.Marshal一致: got=%s want=%s", origin.String(), string(data))
	}
}

func TestFileMetaInquiryString(t *testing.T) {
	inquiry := model.FileMetaInquiry{FileNameLike: "2609", PageSize: 30}
	if !strings.Contains(inquiry.String(), "\"page_size\":30") {
		t.Errorf("FileMetaInquiry.String()异常: %s", inquiry.String())
	}
}
