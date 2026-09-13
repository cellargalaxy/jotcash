package handler_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type expenseResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Object []*model.Expense `json:"object"`
		Count  int64            `json:"count"`
	} `json:"data"`
}

func newTestExpense(counterparty string, date time.Time, amount string) *model.Expense {
	return &model.Expense{
		Id:                 util.GenId(),
		BankName:           "招商银行",
		CardLast4:          "6789",
		ExpenseDate:        date,
		ExpenseCurrency:    "USD",
		ExpenseAmount:      decimal.RequireFromString(amount),
		Counterparty:       counterparty,
		Remark:             "备注" + counterparty,
		ExchangeRate:       decimal.RequireFromString("7.12345678"),
		AccountingCurrency: "CNY",
		AccountingAmount:   decimal.RequireFromString("0"),
		ExpenseType:        "购物",
		AmortizationMonths: 1,
		OperationId:        util.GenId(),
		FileId:             util.GenId(),
		Version:            1,
	}
}

// 铺三条：两条未删除（日期与金额都错开），一条已软删除
func newTestExpenses(t *testing.T, clientToken string) (*model.Expense, *model.Expense, *model.Expense) {
	t.Helper()
	ctx := newTokenCtx(clientToken)
	early := newTestExpense("亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "-100.50")
	late := newTestExpense("苹果", time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), "200.25")
	gone := newTestExpense("已删除的店", time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), "50")
	if _, err := db.InsertExpense(ctx, early, late, gone); err != nil {
		t.Fatalf("插入测试明细异常: %+v", err)
	}
	if _, err := db.DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{gone.Id}}); err != nil {
		t.Fatalf("软删除测试明细异常: %+v", err)
	}
	return early, late, gone
}

func selectExpense(t *testing.T, engine *gin.Engine, jwt string, inquiry model.ExpenseInquiry) expenseResp {
	t.Helper()
	var resp expenseResp
	doRequest(t, engine, newRequest(model.PathExpenseSelect, jwt, inquiry), &resp)
	return resp
}

func TestSelectExpense(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	early, late, _ := newTestExpenses(t, clientToken)

	resp := selectExpense(t, engine, jwt, model.ExpenseInquiry{})
	if resp.Code != http.StatusOK {
		t.Fatalf("查询明细应成功: %+v", resp)
	}
	//E-7：已删除默认不显示
	if resp.Data.Count != 2 || len(resp.Data.Object) != 2 {
		t.Fatalf("默认应只返回未删除的2条: count=%d len=%d", resp.Data.Count, len(resp.Data.Object))
	}
	//不传排序时按支出日期最新在前
	if resp.Data.Object[0].Id != late.Id || resp.Data.Object[1].Id != early.Id {
		t.Errorf("默认排序应为支出日期最新在前: %+v", resp.Data.Object)
	}
	//金额是文本列，往返不能丢精度
	if !resp.Data.Object[1].ExpenseAmount.Equal(early.ExpenseAmount) {
		t.Errorf("金额读写不一致: got=%s want=%s", resp.Data.Object[1].ExpenseAmount, early.ExpenseAmount)
	}
}

func TestSelectExpenseDeleted(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	_, _, gone := newTestExpenses(t, clientToken)

	if resp := selectExpense(t, engine, jwt, model.ExpenseInquiry{Deleted: model.DeletedAll}); resp.Data.Count != 3 {
		t.Errorf("全部应返回3条: %+v", resp.Data)
	}
	resp := selectExpense(t, engine, jwt, model.ExpenseInquiry{Deleted: model.DeletedOnly})
	if resp.Data.Count != 1 || resp.Data.Object[0].Id != gone.Id {
		t.Fatalf("只查已删除应返回软删除那条: %+v", resp.Data)
	}
	//§一前提7：软删除是终态，删除时间要能读出来
	if !resp.Data.Object[0].DeletedAt.Valid {
		t.Errorf("已删除明细应带删除时间: %+v", resp.Data.Object[0])
	}
}

func TestSelectExpenseFilter(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	early, late, _ := newTestExpenses(t, clientToken)

	//E-7 顶部筛选逐项
	min := decimal.RequireFromString("0")
	cases := map[string]struct {
		inquiry model.ExpenseInquiry
		count   int64
	}{
		"日期区间":   {model.ExpenseInquiry{ExpenseDateStart: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}, 1},
		"金额区间":   {model.ExpenseInquiry{ExpenseAmountMin: &min}, 1},
		"支出币种":   {model.ExpenseInquiry{ExpenseCurrency: []string{"USD"}}, 2},
		"记账币种":   {model.ExpenseInquiry{AccountingCurrency: []string{"JPY"}}, 0},
		"对手方模糊":  {model.ExpenseInquiry{CounterpartyLike: "亚马"}, 1},
		"备注模糊":   {model.ExpenseInquiry{RemarkLike: "苹果"}, 1},
		"支出类型模糊": {model.ExpenseInquiry{ExpenseTypeLike: "购"}, 2},
		"银行名称":   {model.ExpenseInquiry{BankName: []string{"招商银行"}}, 2},
		"卡号后四位":  {model.ExpenseInquiry{CardLast4: []string{"0000"}}, 0},
		"来源审计ID": {model.ExpenseInquiry{OperationId: []int64{early.OperationId}}, 1},
		"来源文件ID": {model.ExpenseInquiry{FileId: []int64{late.FileId}}, 1},
	}
	for name, one := range cases {
		if resp := selectExpense(t, engine, jwt, one.inquiry); resp.Data.Count != one.count {
			t.Errorf("%s筛选不符: got=%d want=%d", name, resp.Data.Count, one.count)
		}
	}
}

func TestSelectExpensePage(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	early, _, _ := newTestExpenses(t, clientToken)

	//count是筛选结果全集，不受分页影响
	resp := selectExpense(t, engine, jwt, model.ExpenseInquiry{Page: 1, PageSize: 1, Sort: "expense_date asc"})
	if resp.Data.Count != 2 || len(resp.Data.Object) != 1 {
		t.Fatalf("首页不符: count=%d len=%d", resp.Data.Count, len(resp.Data.Object))
	}
	if resp.Data.Object[0].Id != early.Id {
		t.Errorf("排序应可配: %+v", resp.Data.Object[0])
	}
	//金额列是文本，按金额排序要走数值口径
	resp = selectExpense(t, engine, jwt, model.ExpenseInquiry{Sort: "expense_amount asc"})
	if len(resp.Data.Object) != 2 || resp.Data.Object[0].Id != early.Id {
		t.Errorf("按金额排序不符: %+v", resp.Data.Object)
	}
}

func TestSelectExpenseInvalid(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)

	min := decimal.RequireFromString("100")
	max := decimal.RequireFromString("1")
	inquiries := map[string]model.ExpenseInquiry{
		"删除筛选非法":  {Deleted: 99},
		"日期区间倒挂":  {ExpenseDateStart: time.Now(), ExpenseDateEnd: time.Now().Add(-time.Hour)},
		"金额区间倒挂":  {ExpenseAmountMin: &min, ExpenseAmountMax: &max},
		"排序不在白名单": {Sort: "remark; drop table expense"},
	}
	for name, inquiry := range inquiries {
		if resp := selectExpense(t, engine, jwt, inquiry); resp.Code == http.StatusOK {
			t.Errorf("%s应报错: %+v", name, resp)
		}
	}
}

func TestSelectExpenseWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	if resp := selectExpense(t, engine, "", model.ExpenseInquiry{}); resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}
