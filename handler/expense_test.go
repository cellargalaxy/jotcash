package handler_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/rdb"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
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
		rdb.NewExpenseInsertHandler(early, late, gone),
		rdb.NewExpenseDeleteHandler(model.ExpenseInquiry{Id: []int64{gone.Id}}),
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

// 前端导出csv靠不传分页参数一次拉全量，条数超过老的默认页大小也不能被截断
func TestSelectExpenseWithoutPage(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)

	count := 205
	expenses := make([]*model.Expense, 0, count)
	for index := 0; index < count; index++ {
		expenses = append(expenses, newTestExpense("亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "1"))
	}
	execTransaction(t, clientToken, rdb.NewExpenseInsertHandler(expenses...))

	resp := selectExpense(t, engine, jwt, model.ExpenseInquiry{})
	if resp.Data.Count != int64(count) || len(resp.Data.Object) != count {
		t.Errorf("不传分页应拉全量: count=%d len=%d want=%d", resp.Data.Count, len(resp.Data.Object), count)
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

type distinctResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Object []string `json:"object"`
		Count  int64    `json:"count"`
	} `json:"data"`
}

func selectExpenseDistinct(t *testing.T, engine *gin.Engine, jwt string, inquiry model.ExpenseDistinctInquiry) distinctResp {
	t.Helper()
	var resp distinctResp
	doRequest(t, engine, newRequest(config.PathExpenseDistinct, jwt, inquiry), &resp)
	return resp
}

// 四个组合框候选与表头币种提示都走这一个接口：按列去重、升序、空值不进候选
func TestSelectExpenseDistinct(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)

	first := newTestExpense("亚马逊", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "1")
	second := newTestExpense("苹果", time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), "2")
	second.BankName, second.CardLast4, second.ExpenseCurrency, second.ExpenseType = "中国银行", "0001", "CNY", "餐饮"
	blank := newTestExpense("美团", time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), "3")
	blank.ExpenseType = ""
	gone := newTestExpense("已删除的店", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), "4")
	gone.BankName, gone.ExpenseType = "交通银行", "医疗"
	execTransaction(t, clientToken,
		rdb.NewExpenseInsertHandler(first, second, blank, gone),
		rdb.NewExpenseDeleteHandler(model.ExpenseInquiry{Id: []int64{gone.Id}}),
	)

	cases := map[string]struct {
		inquiry model.ExpenseDistinctInquiry
		object  []string
	}{
		"银行名称去重并升序": {model.ExpenseDistinctInquiry{Field: "bank_name"}, []string{"中国银行", "招商银行"}},
		"卡号后四位":     {model.ExpenseDistinctInquiry{Field: "card_last_4"}, []string{"0001", "6789"}},
		"支出类型跳过空值":  {model.ExpenseDistinctInquiry{Field: "expense_type"}, []string{"购物", "餐饮"}},
		"支出币种":      {model.ExpenseDistinctInquiry{Field: "expense_currency"}, []string{"CNY", "USD"}},
		"记账币种":      {model.ExpenseDistinctInquiry{Field: "accounting_currency"}, []string{testAccountingCurrency}},
		"含已删除":      {model.ExpenseDistinctInquiry{Field: "bank_name", Deleted: model.DeletedAll}, []string{"中国银行", "交通银行", "招商银行"}},
		"只看已删除":     {model.ExpenseDistinctInquiry{Field: "bank_name", Deleted: model.DeletedOnly}, []string{"交通银行"}},
	}
	for name, one := range cases {
		resp := selectExpenseDistinct(t, engine, jwt, one.inquiry)
		if resp.Code != http.StatusOK {
			t.Errorf("%s应成功: %+v", name, resp)
			continue
		}
		if resp.Data.Count != int64(len(one.object)) || !slices.Equal(resp.Data.Object, one.object) {
			t.Errorf("%s不符: got=%v count=%d want=%v", name, resp.Data.Object, resp.Data.Count, one.object)
		}
	}
}

// 字段会拼进查询语句，白名单外必须直接报错，不能静默换一列查
func TestSelectExpenseDistinctInvalid(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)

	inquiries := map[string]model.ExpenseDistinctInquiry{
		"字段为空":    {},
		"字段不在白名单": {Field: "remark"},
		"字段带注入":   {Field: "bank_name from expense; drop table expense; --"},
		"删除筛选非法":  {Field: "bank_name", Deleted: 99},
	}
	for name, inquiry := range inquiries {
		if resp := selectExpenseDistinct(t, engine, jwt, inquiry); resp.Code == http.StatusOK {
			t.Errorf("%s应报错: %+v", name, resp)
		}
	}
	//报错之后库还在，没被注入语句打坏
	if resp := selectExpenseDistinct(t, engine, jwt, model.ExpenseDistinctInquiry{Field: "bank_name"}); resp.Code != http.StatusOK {
		t.Errorf("正常字段应照常可查: %+v", resp)
	}
}

func TestSelectExpenseDistinctWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	if resp := selectExpenseDistinct(t, engine, "", model.ExpenseDistinctInquiry{Field: "bank_name"}); resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}

type expenseObjectResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Object *model.Expense `json:"object"`
		Count  int64          `json:"count"`
	} `json:"data"`
}

func updateExpense(t *testing.T, engine *gin.Engine, jwt string, object model.Expense) expenseObjectResp {
	t.Helper()
	var resp expenseObjectResp
	doRequest(t, engine, newRequest(config.PathExpenseUpdate, jwt, object), &resp)
	return resp
}

// E-4：可手编的10项照请求改，派生字段按§8.1与§8.2重算
func TestUpdateExpense(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	early, _, _ := newTestExpenses(t, clientToken)

	req := *early
	req.BankName, req.CardLast4 = "中国银行", "0001"
	req.Counterparty, req.Remark = "亚马逊中国", "改过的备注"
	req.ExpenseAmount = decimalOf(t, "300")
	req.ExpenseType, req.AmortizationMonths = "数码", 3

	resp := updateExpense(t, engine, jwt, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("明细编辑应成功: %+v", resp)
	}
	object := resp.Data.Object
	if object == nil || resp.Data.Count != 1 {
		t.Fatalf("应返回更新后的那一条: %+v", resp.Data)
	}
	if object.BankName != req.BankName || object.CardLast4 != req.CardLast4 || object.Counterparty != req.Counterparty ||
		object.Remark != req.Remark || object.ExpenseType != req.ExpenseType || !object.ExpenseAmount.Equal(req.ExpenseAmount) {
		t.Errorf("可手编字段没改到: %+v", object)
	}
	//版本号是乐观锁，每存一次进一位
	if object.Version != early.Version+1 {
		t.Errorf("版本号应递增: got=%d want=%d", object.Version, early.Version+1)
	}
	//支出日期与支出币种都没动，汇率保持原值，记账金额按公式重算
	want := req.ExpenseAmount.Mul(early.ExchangeRate).Round(config.GetConfig(util.GenCtx()).AmountScale)
	if !object.ExchangeRate.Equal(early.ExchangeRate) || !object.AccountingAmount.Equal(want) {
		t.Errorf("记账金额应重算: rate=%s amount=%s want=%s", object.ExchangeRate, object.AccountingAmount, want)
	}
	//摊分起始月=支出日期所属月，结束月=起始月+月数-1
	ctx := util.GenCtx()
	startMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, object.AmortizationStartMonth, nil)
	endMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, object.AmortizationEndMonth, nil)
	if startMonth != "2026-01-01" || endMonth != "2026-03-01" {
		t.Errorf("摊分起止月应跟着刷新: %s %s", startMonth, endMonth)
	}
	//来源字段与记账币种不随编辑改动
	if object.OperationId != early.OperationId || object.FileId != early.FileId || object.AccountingCurrency != early.AccountingCurrency {
		t.Errorf("来源字段与记账币种不该变: %+v", object)
	}

	list := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{early.Id}})
	if list.Data.Count != 1 || list.Data.Object[0].Counterparty != req.Counterparty || list.Data.Object[0].Version != object.Version {
		t.Errorf("编辑结果应落库: %+v", list.Data)
	}
}

// E-4：只记一条「明细编辑」审计，不写「数据入库」、不生成新文件
func TestUpdateExpenseAudit(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	early, _, _ := newTestExpenses(t, clientToken)

	req := *early
	req.Counterparty = "亚马逊中国"
	if resp := updateExpense(t, engine, jwt, req); resp.Code != http.StatusOK {
		t.Fatalf("明细编辑应成功: %+v", resp)
	}

	logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeExpenseEdit}})
	if logs.Data.Count != 1 {
		t.Fatalf("应记一条明细编辑审计: %+v", logs.Data)
	}
	log := logs.Data.Object[0]
	if log.ObjectType != model.ObjectTypeExpense || log.ObjectId != early.Id || log.Summary == "" || log.Result != model.ResultSuccess {
		t.Errorf("审计应指向被编辑的那条明细: %+v", log)
	}
	var changes model.ExpenseChanges
	if err := util.JsonStr2Struct(log.Changes, &changes); err != nil {
		t.Fatalf("变更内容应是JSON: changes=%s err=%+v", log.Changes, err)
	}
	if changes.Before == nil || changes.After == nil {
		t.Fatalf("变更内容应是前后值快照: %s", log.Changes)
	}
	if changes.Before.Counterparty != early.Counterparty || changes.After.Counterparty != req.Counterparty {
		t.Errorf("变更内容的前后值不符: before=%s after=%s", changes.Before.Counterparty, changes.After.Counterparty)
	}
	//编辑不产生新的入库批次与CSV快照
	if entry := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDataEntry}}); entry.Data.Count != 0 {
		t.Errorf("编辑不该写数据入库审计: %+v", entry.Data)
	}
	if files := selectFileMeta(t, engine, jwt, model.FileMetaInquiry{}); files.Data.Count != 0 {
		t.Errorf("编辑不该生成新文件: %+v", files.Data)
	}
}

// §8.1 折算联动：日期或币种变了自动重取汇率，手填的汇率优先，支出币种=记账币种时恒为1
func TestUpdateExpenseRate(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	date := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	auto := newTestExpense("改日期", date, "100")
	manual := newTestExpense("改日期并手填汇率", date, "100")
	same := newTestExpense("支出币种改成记账币种", date, "100")
	execTransaction(t, clientToken, rdb.NewExpenseInsertHandler(auto, manual, same))

	//改了支出日期又没动汇率，按新日期重新自动获取
	req := *auto
	req.ExpenseDate = date.AddDate(0, 1, 0)
	resp := updateExpense(t, engine, jwt, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("改支出日期应成功: %+v", resp)
	}
	if !resp.Data.Object.ExchangeRate.Equal(decimalOf(t, "1")) || !resp.Data.Object.AccountingAmount.Equal(decimalOf(t, "100")) {
		t.Errorf("改支出日期应重取汇率: rate=%s amount=%s", resp.Data.Object.ExchangeRate, resp.Data.Object.AccountingAmount)
	}

	//同一次编辑里手填了汇率，以手填值为准，不去自动获取
	req = *manual
	req.ExpenseDate = date.AddDate(0, 1, 0)
	req.ExchangeRate = decimalOf(t, "8")
	resp = updateExpense(t, engine, jwt, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("手填汇率应成功: %+v", resp)
	}
	if !resp.Data.Object.ExchangeRate.Equal(req.ExchangeRate) || !resp.Data.Object.AccountingAmount.Equal(decimalOf(t, "800")) {
		t.Errorf("手填汇率应优先: rate=%s amount=%s", resp.Data.Object.ExchangeRate, resp.Data.Object.AccountingAmount)
	}

	//支出币种改成与记账币种相同，汇率强制置1
	req = *same
	req.ExpenseCurrency = testAccountingCurrency
	resp = updateExpense(t, engine, jwt, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("改支出币种应成功: %+v", resp)
	}
	if !resp.Data.Object.ExchangeRate.Equal(decimalOf(t, "1")) {
		t.Errorf("支出币种=记账币种时汇率应为1: %s", resp.Data.Object.ExchangeRate)
	}
}

// §四.1：只有10项可手编，其余一律以库内值为准
func TestUpdateExpenseImmutable(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	early, _, _ := newTestExpenses(t, clientToken)
	before := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{early.Id}})
	if before.Data.Count != 1 {
		t.Fatalf("夹具里应有这一条: %+v", before.Data)
	}

	req := *early
	req.AccountingCurrency, req.AccountingAmount = "USD", decimalOf(t, "999")
	req.OperationId, req.FileId = util.GenId(), util.GenId()
	req.AmortizationStartMonth = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	req.AmortizationEndMonth = req.AmortizationStartMonth
	req.CreatedAt = req.AmortizationStartMonth
	req.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}

	resp := updateExpense(t, engine, jwt, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("明细编辑应成功: %+v", resp)
	}
	object := resp.Data.Object
	if object.AccountingCurrency != early.AccountingCurrency || object.OperationId != early.OperationId || object.FileId != early.FileId {
		t.Errorf("记账币种与来源字段不该被请求改掉: %+v", object)
	}
	//记账金额是只读因变量，取公式值而不是请求值
	want := early.ExpenseAmount.Mul(early.ExchangeRate).Round(config.GetConfig(util.GenCtx()).AmountScale)
	if !object.AccountingAmount.Equal(want) {
		t.Errorf("记账金额应按公式算: got=%s want=%s", object.AccountingAmount, want)
	}
	if util.Time2Str(util.GenCtx(), util.DateLayout_2006_01_02, object.AmortizationStartMonth, nil) != "2026-01-01" {
		t.Errorf("摊分起始月应按支出日期算: %v", object.AmortizationStartMonth)
	}
	//响应里的创建时间与删除时间取库内值，不能把请求里塞的那份回显出去
	if object.DeletedAt.Valid || !object.CreatedAt.Equal(before.Data.Object[0].CreatedAt) {
		t.Errorf("响应不该回显请求里的创建时间与删除时间: created=%v deleted=%v", object.CreatedAt, object.DeletedAt)
	}
	//请求里塞的删除时间不能把行删掉，创建时间也不该被顶掉
	after := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{early.Id}})
	if after.Data.Count != 1 {
		t.Fatalf("编辑不该把行删掉: %+v", after.Data)
	}
	if !after.Data.Object[0].CreatedAt.Equal(before.Data.Object[0].CreatedAt) {
		t.Errorf("创建时间不该变: got=%v want=%v", after.Data.Object[0].CreatedAt, before.Data.Object[0].CreatedAt)
	}
}

// E-4：仅未删除明细可编辑，版本号对不上要拦住并提示刷新
func TestUpdateExpenseConflict(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	early, _, gone := newTestExpenses(t, clientToken)

	stale := *early
	stale.Version, stale.Counterparty = early.Version-1, "不该被写进去"
	resp := updateExpense(t, engine, jwt, stale)
	if resp.Code == http.StatusOK {
		t.Fatalf("版本号落后应报错: %+v", resp)
	}
	if !strings.Contains(resp.Msg, "数据已落后") {
		t.Errorf("版本冲突应提示数据已落后: %s", resp.Msg)
	}

	req := *gone
	req.Counterparty = "不该被写进去"
	if resp := updateExpense(t, engine, jwt, req); resp.Code == http.StatusOK {
		t.Errorf("已删除明细应不可编辑: %+v", resp)
	}

	list := selectExpense(t, engine, jwt, model.ExpenseInquiry{Deleted: model.DeletedAll})
	for _, object := range list.Data.Object {
		if object.Counterparty == "不该被写进去" {
			t.Errorf("失败的编辑不该落库: %+v", object)
		}
	}
	if logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeExpenseEdit}}); logs.Data.Count != 0 {
		t.Errorf("失败的编辑不该记审计: %+v", logs.Data)
	}
}

func TestUpdateExpenseInvalid(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	early, _, _ := newTestExpenses(t, clientToken)

	missing := *early
	missing.Id = util.GenId()
	badCurrency := *early
	badCurrency.ExpenseCurrency = "XYZ"
	badRate := *early
	badRate.ExchangeRate = decimalOf(t, "-1")

	objects := map[string]model.Expense{
		"明细不存在":  missing,
		"支出币种非法": badCurrency,
		"折算汇率非正": badRate,
	}
	for name, object := range objects {
		if resp := updateExpense(t, engine, jwt, object); resp.Code == http.StatusOK {
			t.Errorf("%s应报错: %+v", name, resp)
		}
	}
	//报错之后原明细一字未动
	list := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{early.Id}})
	if list.Data.Count != 1 || list.Data.Object[0].Version != early.Version || list.Data.Object[0].ExpenseCurrency != early.ExpenseCurrency {
		t.Errorf("失败的编辑不该改动明细: %+v", list.Data)
	}
}

func TestUpdateExpenseWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	if resp := updateExpense(t, engine, "", model.Expense{Id: util.GenId()}); resp.Code != http.StatusUnauthorized {
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
