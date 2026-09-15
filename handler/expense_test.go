package handler_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/rdb"
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
	early := newTestExpense("亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "-100.50")
	late := newTestExpense("苹果", time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), "200.25")
	gone := newTestExpense("已删除的店", time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), "50")
	//铺数据与软删除一次事务做完，少开一次库
	execTransaction(t, clientToken,
		db.NewExpenseInsertHandler(early, late, gone),
		db.NewExpenseDeleteHandler(model.ExpenseInquiry{Id: []int64{gone.Id}}),
	)
	return early, late, gone
}

func selectExpense(t *testing.T, engine *gin.Engine, jwt string, inquiry model.ExpenseInquiry) expenseResp {
	t.Helper()
	var resp expenseResp
	doRequest(t, engine, newRequest(config.PathExpenseSelect, jwt, inquiry), &resp)
	return resp
}

func TestSelectExpense(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
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
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
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
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
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
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
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
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)

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

func deleteExpense(t *testing.T, engine *gin.Engine, jwt string, inquiry model.ExpenseInquiry) expenseResp {
	t.Helper()
	var resp expenseResp
	doRequest(t, engine, newRequest(config.PathExpenseDelete, jwt, inquiry), &resp)
	return resp
}

// E-5：勾选具体行删除
func TestDeleteExpense(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	early, late, _ := newTestExpenses(t, clientToken)

	resp := deleteExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{early.Id}})
	if resp.Code != http.StatusOK {
		t.Fatalf("删除应成功: %+v", resp)
	}
	if resp.Data.Count != 1 {
		t.Errorf("应返回实际删除笔数: %+v", resp.Data)
	}

	//软删除：列表里看不到了，但带上DeletedOnly还在
	list := selectExpense(t, engine, jwt, model.ExpenseInquiry{})
	if list.Data.Count != 1 || list.Data.Object[0].Id != late.Id {
		t.Errorf("删除后默认列表应只剩未删除的: %+v", list.Data)
	}
	gone := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{early.Id}, Deleted: model.DeletedOnly})
	if gone.Data.Count != 1 || !gone.Data.Object[0].DeletedAt.Valid {
		t.Fatalf("删掉的那条应还在且带删除时间: %+v", gone.Data)
	}

	//E-5：记一条「明细删除」审计
	logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeExpenseDelete}})
	if logs.Data.Count != 1 {
		t.Fatalf("应记一条明细删除审计: %+v", logs.Data)
	}
	if logs.Data.Object[0].Summary == "" {
		t.Errorf("审计摘要应有人可读描述: %+v", logs.Data.Object[0])
	}
	//§四.2：批量操作的对象类型与对象ID留零值，且删除不是编辑类操作
	if logs.Data.Object[0].ObjectType != "" || logs.Data.Object[0].ObjectId != 0 || logs.Data.Object[0].Changes != "" {
		t.Errorf("批量删除的审计不应带对象与变更内容: %+v", logs.Data.Object[0])
	}
}

// E-5：全选当前筛选结果全集，不用把ID一条条传上来
func TestDeleteExpenseByFilter(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	newTestExpenses(t, clientToken)

	//筛出「亚马逊」这一条删掉，另一条不受影响
	resp := deleteExpense(t, engine, jwt, model.ExpenseInquiry{CounterpartyLike: "亚马"})
	if resp.Data.Count != 1 {
		t.Fatalf("按筛选条件删除不符: %+v", resp.Data)
	}
	if list := selectExpense(t, engine, jwt, model.ExpenseInquiry{}); list.Data.Count != 1 {
		t.Errorf("只该删掉命中的那条: %+v", list.Data)
	}

	//分页字段不该限制删除范围：全选的是筛选结果全集，不是当前这一页
	resp = deleteExpense(t, engine, jwt, model.ExpenseInquiry{BankName: []string{"招商银行"}, Page: 1, PageSize: 1})
	if resp.Code != http.StatusOK || resp.Data.Count != 1 {
		t.Errorf("剩下的1条应被删掉，不受PageSize限制: %+v", resp)
	}
	if list := selectExpense(t, engine, jwt, model.ExpenseInquiry{}); list.Data.Count != 0 {
		t.Errorf("未删除的应该没了: %+v", list.Data)
	}
}

// 一个条件都不带的删除会被gorm的空条件保护挡下来，db层的TestExpenseDelete已经钉过，这里确认这条约束透到了接口上
func TestDeleteExpenseNoCondition(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	newTestExpenses(t, clientToken)

	if resp := deleteExpense(t, engine, jwt, model.ExpenseInquiry{}); resp.Code == http.StatusOK {
		t.Errorf("无条件删除应报错: %+v", resp)
	}
	if list := selectExpense(t, engine, jwt, model.ExpenseInquiry{}); list.Data.Count != 2 {
		t.Errorf("无条件删除不应动数据: %+v", list.Data)
	}
	//DeletedAll查的是Unscoped全表，行数不变才说明一行都没被物理删掉
	if all := selectExpense(t, engine, jwt, model.ExpenseInquiry{Deleted: model.DeletedAll}); all.Data.Count != 3 {
		t.Errorf("无条件删除不应物理删掉任何行: %+v", all.Data)
	}
	if logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeExpenseDelete}}); logs.Data.Count != 0 {
		t.Errorf("无条件删除不应记审计: %+v", logs.Data)
	}
}

// §一前提7：明细只有软删除。db/expense.go的NewExpenseDeleteHandler靠强制Deleted=DeletedNo挡住Unscoped，
// 那一行要是丢了，请求带上DeletedAll就会变成物理删除，这里钉住它
func TestDeleteExpenseNeverPhysical(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	early, _, gone := newTestExpenses(t, clientToken)

	before := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{gone.Id}, Deleted: model.DeletedOnly})
	if before.Data.Count != 1 {
		t.Fatalf("夹具里应有一条已删除的: %+v", before.Data)
	}

	//请求里把Deleted调成Unscoped的那两种，都不该让删除变成物理删除
	for _, deleted := range []int{model.DeletedAll, model.DeletedOnly} {
		resp := deleteExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{early.Id, gone.Id}, Deleted: deleted})
		if resp.Code != http.StatusOK {
			t.Fatalf("deleted=%d 的删除应成功: %+v", deleted, resp)
		}
		all := selectExpense(t, engine, jwt, model.ExpenseInquiry{Deleted: model.DeletedAll})
		if all.Data.Count != 3 {
			t.Fatalf("deleted=%d 之后行数变了，说明发生了物理删除: %+v", deleted, all.Data)
		}
	}

	//已删除那条的删除时间自始至终没被覆盖
	after := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{gone.Id}, Deleted: model.DeletedOnly})
	if after.Data.Count != 1 {
		t.Fatalf("原本已删除的那条应还在: %+v", after.Data)
	}
	if !after.Data.Object[0].DeletedAt.Time.Equal(before.Data.Object[0].DeletedAt.Time) {
		t.Errorf("不该覆盖已删除行的删除时间: before=%v after=%v", before.Data.Object[0].DeletedAt, after.Data.Object[0].DeletedAt)
	}
}

// §8.5：已删除行跳过、不覆盖其删除时间
func TestDeleteExpenseSkipDeleted(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	_, _, gone := newTestExpenses(t, clientToken)

	before := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{gone.Id}, Deleted: model.DeletedOnly})
	if before.Data.Count != 1 {
		t.Fatalf("夹具里应有一条已删除的: %+v", before.Data)
	}

	//再删一次已删除的那条，应该一笔都删不动
	resp := deleteExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{gone.Id}})
	if resp.Code != http.StatusOK || resp.Data.Count != 0 {
		t.Fatalf("已删除行应跳过: %+v", resp)
	}
	after := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{gone.Id}, Deleted: model.DeletedOnly})
	if after.Data.Count != 1 {
		t.Fatalf("已删除行不该消失: %+v", after.Data)
	}
	if !after.Data.Object[0].DeletedAt.Time.Equal(before.Data.Object[0].DeletedAt.Time) {
		t.Errorf("不该覆盖已删除行的删除时间: before=%v after=%v", before.Data.Object[0].DeletedAt, after.Data.Object[0].DeletedAt)
	}
}

func TestDeleteExpenseInvalid(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	newTestExpenses(t, clientToken)

	min := decimal.RequireFromString("100")
	max := decimal.RequireFromString("1")
	inquiries := map[string]model.ExpenseInquiry{
		"日期区间倒挂":  {ExpenseDateStart: time.Now(), ExpenseDateEnd: time.Now().Add(-time.Hour)},
		"金额区间倒挂":  {ExpenseAmountMin: &min, ExpenseAmountMax: &max},
		"排序不在白名单": {Sort: "remark; drop table expense"},
	}
	for name, inquiry := range inquiries {
		if resp := deleteExpense(t, engine, jwt, inquiry); resp.Code == http.StatusOK {
			t.Errorf("%s应报错: %+v", name, resp)
		}
	}
	//报错的删除不能动数据，也不能留审计
	if list := selectExpense(t, engine, jwt, model.ExpenseInquiry{}); list.Data.Count != 2 {
		t.Errorf("报错的删除不应动数据: %+v", list.Data)
	}
	if logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeExpenseDelete}}); logs.Data.Count != 0 {
		t.Errorf("报错的删除不应记审计: %+v", logs.Data)
	}
}

func TestDeleteExpenseWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	if resp := deleteExpense(t, engine, "", model.ExpenseInquiry{}); resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}

func decimalOf(t *testing.T, text string) decimal.Decimal {
	t.Helper()
	value, err := decimal.NewFromString(text)
	if err != nil {
		t.Fatalf("解析金额异常: %+v", err)
	}
	return value
}
