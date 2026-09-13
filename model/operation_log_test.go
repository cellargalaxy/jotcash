package model_test

import (
	"testing"

	"github.com/cellargalaxy/jotcash/model"
)

func TestOperationLogTableName(t *testing.T) {
	if got := (model.OperationLog{}).TableName(); got != "operation_log" {
		t.Errorf("OperationLog表名: got=%s want=operation_log", got)
	}
}
