package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/shopspring/decimal"
)

// 表名是db层拼SQL与打日志的依据，改名即改表，钉死避免误改
func TestTableName(t *testing.T) {
	if got := (model.Expense{}).TableName(); got != "expense" {
		t.Errorf("Expense表名: got=%s want=expense", got)
	}
	if got := (model.OperationLog{}).TableName(); got != "operation_log" {
		t.Errorf("OperationLog表名: got=%s want=operation_log", got)
	}
	if got := (model.FileMeta{}).TableName(); got != "file_meta" {
		t.Errorf("FileMeta表名: got=%s want=file_meta", got)
	}
	if got := (model.FileBlob{}).TableName(); got != "file_blob" {
		t.Errorf("FileBlob表名: got=%s want=file_blob", got)
	}
}

// decimal经JSON往返不能丢精度
func TestExpenseJson(t *testing.T) {
	origin := model.Expense{
		Id:            util.GenId(),
		ExpenseAmount: decimal.RequireFromString("-1234567890123456.7890"),
		ExchangeRate:  decimal.RequireFromString("7.12345678"),
	}
	data, err := json.Marshal(origin)
	if err != nil {
		t.Fatalf("序列化异常: %+v", err)
	}
	var loaded model.Expense
	if err = json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("反序列化异常: %+v", err)
	}
	if loaded.Id != origin.Id {
		t.Errorf("Id往返不一致: got=%d want=%d", loaded.Id, origin.Id)
	}
	if !loaded.ExpenseAmount.Equal(origin.ExpenseAmount) {
		t.Errorf("ExpenseAmount往返精度丢失: got=%s want=%s", loaded.ExpenseAmount, origin.ExpenseAmount)
	}
	if !loaded.ExchangeRate.Equal(origin.ExchangeRate) {
		t.Errorf("ExchangeRate往返精度丢失: got=%s want=%s", loaded.ExchangeRate, origin.ExchangeRate)
	}
	if origin.String() != string(data) {
		t.Errorf("String()应与json.Marshal一致: got=%s want=%s", origin.String(), string(data))
	}
}

// 文件内容打了json:"-"，不能随String()混进日志或响应体
func TestFileBlobJson(t *testing.T) {
	blob := model.FileBlob{FileHash: "abc123", FileData: []byte("secret-bytes")}
	if strings.Contains(blob.String(), "secret-bytes") {
		t.Errorf("FileData不应出现在序列化结果中: %s", blob.String())
	}
	if !strings.Contains(blob.String(), "abc123") {
		t.Errorf("FileHash应出现在序列化结果中: %s", blob.String())
	}
}

// ClientToken打了json:"-"：既不进String()/日志，也不会随json.Marshal进jwt载荷
func TestClaimsJson(t *testing.T) {
	claims := model.Claims{ClientToken: "secret-client-token"}
	claims.LogId = 123

	data, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("序列化异常: %+v", err)
	}
	if strings.Contains(string(data), "secret-client-token") {
		t.Errorf("ClientToken不应出现在序列化结果中: %s", string(data))
	}
	if !strings.Contains(string(data), "123") {
		t.Errorf("其余字段应正常序列化: %s", string(data))
	}
	if strings.Contains(claims.String(), "secret-client-token") {
		t.Errorf("ClientToken不应出现在String()中: %s", claims.String())
	}
}

// Inquiry也要能直接打日志
func TestInquiryString(t *testing.T) {
	inquiry := model.ExpenseInquiry{Id: []int64{1, 2}, PageSize: 10}
	if !strings.Contains(inquiry.String(), "\"page_size\":10") {
		t.Errorf("ExpenseInquiry.String()异常: %s", inquiry.String())
	}
}
