package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/shopspring/decimal"
)

func TestExpenseTableName(t *testing.T) {
	if got := (model.Expense{}).TableName(); got != "expense" {
		t.Errorf("Expense表名: got=%s want=expense", got)
	}
}

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

func TestExpenseInquiryString(t *testing.T) {
	inquiry := model.ExpenseInquiry{Id: []int64{1, 2}, PageSize: 10}
	if !strings.Contains(inquiry.String(), "\"page_size\":10") {
		t.Errorf("ExpenseInquiry.String()异常: %s", inquiry.String())
	}
}
