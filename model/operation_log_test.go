package model_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
)

func TestOperationLogTableName(t *testing.T) {
	if got := (model.OperationLog{}).TableName(); got != "operation_log" {
		t.Errorf("OperationLog表名: got=%s want=operation_log", got)
	}
}

func TestOperationLogJson(t *testing.T) {
	origin := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeDataEntry,
		ObjectType:    model.Expense{}.TableName(),
		ObjectId:      util.GenId(),
		Summary:       "入库 37 笔，来源 2609.csv",
		Result:        model.ResultSuccess,
		CreatedAt:     time.Now(),
	}
	data, err := json.Marshal(origin)
	if err != nil {
		t.Fatalf("序列化异常: %+v", err)
	}
	var loaded model.OperationLog
	if err = json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("反序列化异常: %+v", err)
	}
	//审计的Id/ObjectId都是雪花级int64，走json不能被浮点精度削掉
	if loaded.Id != origin.Id || loaded.ObjectId != origin.ObjectId {
		t.Errorf("int64往返不一致: got=(%d,%d) want=(%d,%d)", loaded.Id, loaded.ObjectId, origin.Id, origin.ObjectId)
	}
	if origin.String() != string(data) {
		t.Errorf("String()应与json.Marshal一致: got=%s want=%s", origin.String(), string(data))
	}
}

func TestOperationLogInquiryString(t *testing.T) {
	inquiry := model.OperationLogInquiry{Result: []string{model.ResultPartial}, PageSize: 20}
	if !strings.Contains(inquiry.String(), "\"page_size\":20") {
		t.Errorf("OperationLogInquiry.String()异常: %s", inquiry.String())
	}
}
