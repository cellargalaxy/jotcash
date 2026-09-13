package model_test

import (
	"testing"

	"github.com/cellargalaxy/jotcash/model"
)

func TestFileMetaTableName(t *testing.T) {
	if got := (model.FileMeta{}).TableName(); got != "file_meta" {
		t.Errorf("FileMeta表名: got=%s want=file_meta", got)
	}
}
