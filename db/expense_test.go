package db

import (
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func newTestExpense() *model.Expense {
	return &model.Expense{
		Id:                     util.GenId(),
		BankName:               "招商银行",
		CardLast4:              "6789",
		ExpenseDate:            time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC),
		ExpenseCurrency:        "USD",
		ExpenseAmount:          decimal.RequireFromString("-1234567890123456.7890"),
		Counterparty:           "亚马逊",
		Remark:                 "退款冲正",
		ExchangeRate:           decimal.RequireFromString("7.12345678"),
		AccountingCurrency:     "CNY",
		AccountingAmount:       decimal.RequireFromString("0.00000000"),
		ExpenseType:            "购物",
		AmortizationMonths:     3,
		AmortizationStartMonth: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		AmortizationEndMonth:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		OperationId:            util.GenId(),
		FileId:                 util.GenId(),
		Version:                1,
	}
}

func TestExpenseCrud(t *testing.T) {
	ctx := newTestCtx(t)
	origin := newTestExpense()

	count, err := InsertExpense(ctx, origin)
	if err != nil {
		t.Fatalf("插入支出明细异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("插入影响行数: got=%d want=1", count)
	}
	if origin.CreatedAt.IsZero() || origin.UpdatedAt.IsZero() {
		t.Errorf("CreatedAt/UpdatedAt应由gorm按约定自动填充")
	}

	objects, count, err := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{origin.Id}})
	if err != nil {
		t.Fatalf("查询支出明细异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 {
		t.Fatalf("查询结果: count=%d len=%d want=1", count, len(objects))
	}
	loaded := objects[0]
	if loaded.Id != origin.Id || loaded.OperationId != origin.OperationId || loaded.FileId != origin.FileId {
		t.Errorf("int64字段读写不一致: got=(%d,%d,%d)", loaded.Id, loaded.OperationId, loaded.FileId)
	}
	if !loaded.ExpenseAmount.Equal(origin.ExpenseAmount) {
		t.Errorf("ExpenseAmount精度丢失: got=%s want=%s", loaded.ExpenseAmount, origin.ExpenseAmount)
	}
	if !loaded.ExchangeRate.Equal(origin.ExchangeRate) {
		t.Errorf("ExchangeRate精度丢失: got=%s want=%s", loaded.ExchangeRate, origin.ExchangeRate)
	}
	if !loaded.AccountingAmount.Equal(origin.AccountingAmount) {
		t.Errorf("AccountingAmount零值读写不一致: got=%s want=%s", loaded.AccountingAmount, origin.AccountingAmount)
	}
	if !loaded.ExpenseDate.Equal(origin.ExpenseDate) || !loaded.AmortizationStartMonth.Equal(origin.AmortizationStartMonth) || !loaded.AmortizationEndMonth.Equal(origin.AmortizationEndMonth) {
		t.Errorf("日期字段读写不一致: got=(%v,%v,%v)", loaded.ExpenseDate, loaded.AmortizationStartMonth, loaded.AmortizationEndMonth)
	}
	if loaded.DeletedAt.Valid {
		t.Errorf("未删除的记录DeletedAt不应为有效值")
	}

	loaded.Remark = "改过的备注"
	count, err = UpdateExpense(ctx, loaded)
	if err != nil {
		t.Fatalf("更新支出明细异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("更新影响行数: got=%d want=1", count)
	}
	objects, _, err = SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{origin.Id}})
	if err != nil {
		t.Fatalf("更新后查询异常: %+v", err)
	}
	if len(objects) != 1 || objects[0].Remark != "改过的备注" {
		t.Fatalf("更新未生效: %+v", objects)
	}
	if objects[0].CreatedAt.IsZero() || !objects[0].ExpenseAmount.Equal(origin.ExpenseAmount) {
		t.Errorf("未改动的字段不应被抹掉: %+v", objects[0])
	}
	if !objects[0].CreatedAt.Equal(loaded.CreatedAt) || !objects[0].UpdatedAt.After(loaded.UpdatedAt) {
		t.Errorf("更新只应推进UpdatedAt: created=%v updated=%v", objects[0].CreatedAt, objects[0].UpdatedAt)
	}

	loaded.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
	if _, err = UpdateExpense(ctx, loaded); err != nil {
		t.Fatalf("更新支出明细异常: %+v", err)
	}
	if _, count, _ = SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{origin.Id}}); count != 1 {
		t.Errorf("编辑把明细软删掉了，软删是终态只能走删除接口")
	}
}

func TestExpenseVersion(t *testing.T) {
	ctx := newTestCtx(t)

	origin := newTestExpense()
	if _, err := InsertExpense(ctx, origin); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	first := *origin
	second := *origin

	first.Remark = "先提交的改动"
	count, err := UpdateExpense(ctx, &first)
	if err != nil {
		t.Fatalf("更新异常: %+v", err)
	}
	if count != 1 || first.Version != origin.Version+1 {
		t.Fatalf("更新成功应把版本号+1并回填: count=%d version=%d", count, first.Version)
	}

	second.Remark = "后提交的改动"
	if _, err = UpdateExpense(ctx, &second); err == nil {
		t.Errorf("版本冲突应报错")
	}
	if second.Version != origin.Version {
		t.Errorf("冲突时不应回填版本号: got=%d", second.Version)
	}

	rollbackExpense := newTestExpense()
	transaction, err := NewTransaction(ctx)
	if err != nil {
		t.Fatalf("开事务异常: %+v", err)
	}
	err = transaction.AddCommit(NewExpenseInsertHandler(rollbackExpense), NewExpenseUpdateHandler(&second)).Exec(ctx)
	transaction.Close(ctx)
	if err == nil {
		t.Fatalf("版本冲突应报错")
	}
	if _, count, _ = SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{rollbackExpense.Id}}); count != 0 {
		t.Errorf("版本冲突未回滚同事务的插入")
	}

	objects, _, err := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{origin.Id}})
	if err != nil {
		t.Fatalf("查询异常: %+v", err)
	}
	if len(objects) != 1 || objects[0].Remark != "先提交的改动" || objects[0].Version != first.Version {
		t.Errorf("库里应是先提交的那份: %+v", objects[0])
	}
}

func TestExpenseDelete(t *testing.T) {
	ctx := newTestCtx(t)

	deleted := newTestExpense()
	kept := newTestExpense()
	if _, err := InsertExpense(ctx, deleted, kept); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	if _, err := DeleteExpense(ctx, model.ExpenseInquiry{}); err == nil {
		t.Errorf("无条件删除应报错")
	}

	count, err := DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{deleted.Id}})
	if err != nil {
		t.Fatalf("删除支出明细异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("删除影响行数: got=%d want=1", count)
	}
	objects, count, err := SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{deleted.Id}})
	if err != nil {
		t.Fatalf("删除后查询异常: %+v", err)
	}
	if count != 0 || len(objects) != 0 {
		t.Errorf("软删除后默认不应查到: count=%d len=%d", count, len(objects))
	}
	objects, count, err = SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{deleted.Id}, Deleted: model.DeletedOnly})
	if err != nil {
		t.Fatalf("查询已删除异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || !objects[0].DeletedAt.Valid {
		t.Errorf("只查已删除未命中: count=%d len=%d", count, len(objects))
	}
	if _, count, err = SelectExpense(ctx, model.ExpenseInquiry{Deleted: model.DeletedAll}); err != nil || count != 2 {
		t.Errorf("含已删除查询: count=%d err=%+v", count, err)
	}

	count, err = DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{deleted.Id}, Deleted: model.DeletedAll})
	if err != nil {
		t.Fatalf("重复删除异常: %+v", err)
	}
	if count != 0 {
		t.Errorf("已删除行应跳过: count=%d want=0", count)
	}
	if _, count, _ = SelectExpense(ctx, model.ExpenseInquiry{Deleted: model.DeletedAll}); count != 2 {
		t.Errorf("软删除的行被物理删掉了: count=%d want=2", count)
	}

	pageOne := newTestExpense()
	pageTwo := newTestExpense()
	if _, err = InsertExpense(ctx, pageOne, pageTwo); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}
	count, err = DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{pageOne.Id, pageTwo.Id}, Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("删除支出明细异常: %+v", err)
	}
	if count != 2 {
		t.Errorf("删除删的是筛选结果全集，分页参数不作数: got=%d want=2", count)
	}
}

func TestExpenseInquiry(t *testing.T) {
	ctx := newTestCtx(t)

	amounts := []string{"-100.5", "9.99", "100.10", "1000"}
	for i := range amounts {
		object := &model.Expense{
			Id:                 util.GenId(),
			ExpenseDate:        time.Date(2026, 9, 10+i, 0, 0, 0, 0, time.UTC),
			ExpenseAmount:      decimal.RequireFromString(amounts[i]),
			AccountingCurrency: []string{"CNY", "CNY", "USD", "JPY"}[i],
			Counterparty:       "亚马逊",
			Remark:             "100%退款",
		}
		if _, err := InsertExpense(ctx, object); err != nil {
			t.Fatalf("插入支出明细异常: %+v", err)
		}
	}

	_, count, err := SelectExpense(ctx, model.ExpenseInquiry{
		ExpenseDateStart: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC),
		ExpenseDateEnd:   time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("日期区间查询异常: %+v", err)
	}
	if count != 2 {
		t.Errorf("日期区间: count=%d want=2", count)
	}

	min := decimal.RequireFromString("10")
	max := decimal.RequireFromString("1000")
	_, count, err = SelectExpense(ctx, model.ExpenseInquiry{ExpenseAmountMin: &min, ExpenseAmountMax: &max})
	if err != nil {
		t.Fatalf("金额区间查询异常: %+v", err)
	}
	if count != 2 {
		t.Errorf("金额区间: count=%d want=2", count)
	}
	zero := decimal.Zero
	_, count, err = SelectExpense(ctx, model.ExpenseInquiry{ExpenseAmountMax: &zero})
	if err != nil {
		t.Fatalf("负数金额查询异常: %+v", err)
	}
	if count != 1 {
		t.Errorf("金额<=0: count=%d want=1", count)
	}

	_, count, err = SelectExpense(ctx, model.ExpenseInquiry{CounterpartyLike: "马逊"})
	if err != nil {
		t.Fatalf("模糊查询异常: %+v", err)
	}
	if count != 4 {
		t.Errorf("对手方模糊: count=%d want=4", count)
	}
	_, count, err = SelectExpense(ctx, model.ExpenseInquiry{RemarkLike: "100%退"})
	if err != nil {
		t.Fatalf("模糊查询异常: %+v", err)
	}
	if count != 4 {
		t.Errorf("备注模糊: count=%d want=4", count)
	}
	_, count, err = SelectExpense(ctx, model.ExpenseInquiry{RemarkLike: "100%不存在"})
	if err != nil {
		t.Fatalf("模糊查询异常: %+v", err)
	}
	if count != 0 {
		t.Errorf("通配符未转义，被当成了模式匹配: count=%d want=0", count)
	}

	_, count, err = SelectExpense(ctx, model.ExpenseInquiry{AccountingCurrencyNot: []string{"CNY"}, Deleted: model.DeletedAll})
	if err != nil {
		t.Fatalf("记账币种取反查询异常: %+v", err)
	}
	if count != 2 {
		t.Errorf("记账币种取反: count=%d want=2", count)
	}
}

func TestExpenseSort(t *testing.T) {
	ctx := newTestCtx(t)

	amounts := []string{"9.99", "100.10", "-1"}
	for i := range amounts {
		object := &model.Expense{Id: util.GenId(), ExpenseAmount: decimal.RequireFromString(amounts[i])}
		if _, err := InsertExpense(ctx, object); err != nil {
			t.Fatalf("插入支出明细异常: %+v", err)
		}
	}

	objects, _, err := SelectExpense(ctx, model.ExpenseInquiry{Sort: "expense_amount desc"})
	if err != nil {
		t.Fatalf("排序查询异常: %+v", err)
	}
	if len(objects) != 3 || objects[0].ExpenseAmount.String() != "100.1" || objects[2].ExpenseAmount.String() != "-1" {
		t.Errorf("金额倒序不符: %+v", objects)
	}
	objects, _, err = SelectExpense(ctx, model.ExpenseInquiry{Sort: "id desc"})
	if err != nil {
		t.Fatalf("排序查询异常: %+v", err)
	}
	if len(objects) != 3 || objects[0].Id < objects[2].Id {
		t.Errorf("ID倒序不符: %+v", objects)
	}
	objects, _, err = SelectExpense(ctx, model.ExpenseInquiry{})
	if err != nil {
		t.Fatalf("默认排序查询异常: %+v", err)
	}
	if len(objects) != 3 || objects[0].Id > objects[2].Id {
		t.Errorf("默认排序不符: %+v", objects)
	}

	for _, sort := range []string{"id asc; drop table expense", "expense_amount", "bank_name asc", "1"} {
		if _, _, err = SelectExpense(ctx, model.ExpenseInquiry{Sort: sort}); err == nil {
			t.Errorf("非法排序应报错: %s", sort)
		}
	}
	if _, err = DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{objects[0].Id}, Sort: "bank_name asc"}); err == nil {
		t.Errorf("非法排序的删除应报错")
	}
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{}); count != 3 {
		t.Errorf("非法排序的删除不应删掉数据: count=%d want=3", count)
	}
}

func TestExpensePage(t *testing.T) {
	ctx := newTestCtx(t)

	operationId := util.GenId()
	for i := 0; i < 3; i++ {
		object := &model.Expense{Id: util.GenId(), OperationId: operationId, ExpenseCurrency: "CNY"}
		if _, err := InsertExpense(ctx, object); err != nil {
			t.Fatalf("插入支出明细异常: %+v", err)
		}
	}

	objects, count, err := SelectExpense(ctx, model.ExpenseInquiry{OperationId: []int64{operationId}, Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("分页查询异常: %+v", err)
	}
	if count != 3 || len(objects) != 2 {
		t.Errorf("第1页: count=%d len=%d want=3/2", count, len(objects))
	}
	objects, count, err = SelectExpense(ctx, model.ExpenseInquiry{OperationId: []int64{operationId}, Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("分页查询异常: %+v", err)
	}
	if count != 3 || len(objects) != 1 {
		t.Errorf("第2页: count=%d len=%d want=3/1", count, len(objects))
	}
	objects, count, err = SelectExpense(ctx, model.ExpenseInquiry{ExpenseCurrency: []string{"USD"}})
	if err != nil {
		t.Fatalf("查询异常: %+v", err)
	}
	if count != 0 || len(objects) != 0 {
		t.Errorf("未命中的筛选条件应查不到: count=%d len=%d", count, len(objects))
	}
}

// ExpenseInquiry的等值筛选项逐个走一遍，漏接一项就是静默查全表
func TestExpenseInquiryField(t *testing.T) {
	ctx := newTestCtx(t)

	fileId := util.GenId()
	hit := &model.Expense{
		Id: util.GenId(), BankName: "招商银行", CardLast4: "6789", ExpenseDate: time.Now(),
		ExpenseCurrency: "USD", AccountingCurrency: "CNY", ExpenseType: "购物_线上",
		AmortizationMonths: 3, OperationId: util.GenId(), FileId: fileId, Version: 1,
	}
	miss := &model.Expense{
		Id: util.GenId(), BankName: "工商银行", CardLast4: "4321", ExpenseDate: time.Now(),
		ExpenseCurrency: "JPY", AccountingCurrency: "CNY", ExpenseType: "购物X线下",
		AmortizationMonths: 6, OperationId: util.GenId(), FileId: util.GenId(), Version: 2,
	}
	if _, err := InsertExpense(ctx, hit, miss); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	for name, inquiry := range map[string]model.ExpenseInquiry{
		"银行名称":  {BankName: []string{hit.BankName}},
		"卡号后四位": {CardLast4: []string{hit.CardLast4}},
		"支出币种":  {ExpenseCurrency: []string{hit.ExpenseCurrency}},
		"记账币种":  {AccountingCurrency: []string{hit.AccountingCurrency}, BankName: []string{hit.BankName}},
		"支出类型":  {ExpenseType: []string{hit.ExpenseType}},
		"摊分月数":  {AmortizationMonths: []int{hit.AmortizationMonths}},
		"操作Id":  {OperationId: []int64{hit.OperationId}},
		"文件Id":  {FileId: []int64{fileId}},
		"版本号":   {Version: []int{hit.Version}},
		//下划线是like的单字符通配符，没转义就会把"购物X线下"也匹进来
		"支出类型模糊": {ExpenseTypeLike: "购物_线"},
	} {
		objects, count, err := SelectExpense(ctx, inquiry)
		if err != nil {
			t.Fatalf("%s筛选异常: %+v", name, err)
		}
		if count != 1 || len(objects) != 1 || objects[0].Id != hit.Id {
			t.Errorf("%s筛选不符: count=%d %+v", name, count, objects)
		}
	}

	//记账币种取反把两条都排掉
	if _, count, _ := SelectExpense(ctx, model.ExpenseInquiry{AccountingCurrencyNot: []string{"CNY"}}); count != 0 {
		t.Errorf("记账币种取反: count=%d want=0", count)
	}
}

// 空入参一路走到底是「什么都不做」，不能报错、更不能误伤已有数据
func TestExpenseEmptyObject(t *testing.T) {
	ctx := newTestCtx(t)

	origin := newTestExpense()
	if _, err := InsertExpense(ctx, origin); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	count, err := InsertExpense(ctx)
	if err != nil || count != 0 {
		t.Errorf("空插入: count=%d err=%+v", count, err)
	}
	count, err = UpdateExpense(ctx, nil)
	if err != nil || count != 0 {
		t.Errorf("空更新: count=%d err=%+v", count, err)
	}

	objects, count, err := SelectExpense(ctx, model.ExpenseInquiry{})
	if err != nil {
		t.Fatalf("查询异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || objects[0].Version != origin.Version {
		t.Errorf("空入参误伤了已有数据: count=%d %+v", count, objects)
	}
}
