package handler_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type expenseOneResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Object *model.Expense `json:"object"`
		Count  int64          `json:"count"`
	} `json:"data"`
}

func updateExpense(t *testing.T, engine *gin.Engine, jwt string, object model.Expense) expenseOneResp {
	t.Helper()
	var resp expenseOneResp
	doRequest(t, engine, newRequest(model.PathExpenseUpdate, jwt, object), &resp)
	return resp
}

// E-4：单行内联编辑，保存后重算派生字段并记一条「明细编辑」审计
func TestUpdateExpense(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	early, _, _ := newTestExpenses(t, clientToken)

	request := *early
	request.ExpenseAmount = decimal.RequireFromString("300")
	request.AmortizationMonths = 3
	request.Counterparty = "新对手方"
	//汇率留空表示让系统按支出日期重新取
	request.ExchangeRate = decimal.Zero
	resp := updateExpense(t, engine, jwt, request)
	if resp.Code != http.StatusOK {
		t.Fatalf("编辑明细应成功: %+v", resp)
	}

	object := resp.Data.Object
	if object.Counterparty != "新对手方" || !object.ExpenseAmount.Equal(request.ExpenseAmount) {
		t.Errorf("可手编字段应落库: %+v", object)
	}
	if object.Version != early.Version+1 {
		t.Errorf("版本号应递增: got=%d want=%d", object.Version, early.Version+1)
	}
	//F-3：记账金额是只读因变量，按新金额与新汇率重算
	if !object.ExchangeRate.IsPositive() || !object.AccountingAmount.Equal(object.ExpenseAmount.Mul(object.ExchangeRate).Round(2)) {
		t.Errorf("记账金额应重算: %+v", object)
	}
	//G-1：摊分月数改了，结束月跟着刷新
	endMonth := util.Time2Str(util.GenCtx(), util.DateLayout_2006_01_02, object.AmortizationEndMonth, nil)
	if endMonth != "2026-03-01" {
		t.Errorf("摊分结束月应刷新: got=%s want=2026-03-01", endMonth)
	}
	//§8.5：记账币种与来源的审计ID/文件ID编辑不动
	if object.AccountingCurrency != early.AccountingCurrency || object.OperationId != early.OperationId || object.FileId != early.FileId {
		t.Errorf("记账币种与来源字段不应变: %+v", object)
	}

	logResp := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeExpenseEdit}})
	if logResp.Data.Count != 1 {
		t.Fatalf("应记一条明细编辑审计: %+v", logResp.Data)
	}
	operationLog := logResp.Data.Object[0]
	if operationLog.ObjectType != model.ObjectTypeExpense || operationLog.ObjectId != early.Id {
		t.Errorf("审计的操作对象不符: %+v", operationLog)
	}
	var changes model.ExpenseChanges
	if err := util.JsonStr2Struct(operationLog.Changes, &changes); err != nil {
		t.Fatalf("审计的变更内容解析异常: changes=%s err=%+v", operationLog.Changes, err)
	}
	if changes.Before == nil || changes.After == nil {
		t.Fatalf("变更内容应是前后值快照: %s", operationLog.Changes)
	}
	if !changes.Before.ExpenseAmount.Equal(early.ExpenseAmount) || !changes.After.ExpenseAmount.Equal(request.ExpenseAmount) {
		t.Errorf("变更内容的前后值不符: %s", operationLog.Changes)
	}
}

func TestUpdateExpenseInvalid(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	early, _, gone := newTestExpenses(t, clientToken)

	//E-4：乐观锁，拿旧版本号保存要提示数据已落后
	stale := *early
	stale.Version = early.Version - 1
	if resp := updateExpense(t, engine, jwt, stale); resp.Code == http.StatusOK {
		t.Errorf("版本冲突应报错: %+v", resp)
	}
	//§一前提7：软删除是终态，已删除明细不可编辑
	if resp := updateExpense(t, engine, jwt, *gone); resp.Code == http.StatusOK {
		t.Errorf("已删除明细不应可编辑: %+v", resp)
	}
	//F-3：折算汇率恒大于0
	negative := *early
	negative.ExchangeRate = decimal.RequireFromString("-1")
	if resp := updateExpense(t, engine, jwt, negative); resp.Code == http.StatusOK {
		t.Errorf("折算汇率非正应报错: %+v", resp)
	}
	missing := *early
	missing.Id = util.GenId()
	if resp := updateExpense(t, engine, jwt, missing); resp.Code == http.StatusOK {
		t.Errorf("明细不存在应报错: %+v", resp)
	}

	//失败的编辑不应留下审计，也不应改动明细
	if logResp := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeExpenseEdit}}); logResp.Data.Count != 0 {
		t.Errorf("失败的编辑不应记审计: %+v", logResp.Data)
	}
	if resp := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{early.Id}}); resp.Data.Object[0].Version != early.Version {
		t.Errorf("失败的编辑不应改动明细: %+v", resp.Data.Object[0])
	}
}

func TestUpdateExpenseWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	if resp := updateExpense(t, engine, "", model.Expense{}); resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}
