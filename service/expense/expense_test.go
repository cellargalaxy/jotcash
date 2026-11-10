package expense_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/service/expense"
	"github.com/cellargalaxy/jotcash/service/expense/parser"
	"github.com/shopspring/decimal"
)

func TestMain(m *testing.M) {
	code := m.Run()
	//config包的init会在测试二进制的工作目录写配置文件
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

func testExpense(currency, amount string) *parser.Expense {
	return &parser.Expense{
		BankName:        "招商银行",
		CardLast4:       "6789",
		ExpenseDate:     time.Date(2026, 1, 2, 0, 0, 0, 0, parser.Loc),
		ExpenseCurrency: currency,
		ExpenseAmount:   decimal.RequireFromString(amount),
	}
}

func TestFill(t *testing.T) {
	ctx := util.GenCtx()

	first := testExpense("CNY", "100.50")
	second := testExpense("USD", "-20.25")
	second.ExpenseDate = time.Date(2026, 3, 4, 0, 0, 0, 0, parser.Loc)
	second.ExchangeRate, second.AccountingCurrency = decimal.RequireFromString("7.1234"), "CNY"
	objects, err := expense.Fill(ctx, []*parser.Expense{first, second}, "CNY")
	if err != nil {
		t.Fatalf("填充异常: %+v", err)
	}
	if len(objects) != 2 {
		t.Fatalf("应填充出2笔: %d", len(objects))
	}
	one, two := objects[0], objects[1]

	//同币种的汇率恒为1
	if one.ExchangeRate.String() != "1" || !one.AccountingAmount.Equal(one.ExpenseAmount) {
		t.Errorf("同币种的折算不符: %+v", one)
	}
	//明细里给了汇率就用明细的，记账金额=支出金额×折算汇率，乘出来的-144.24885按分四舍五入
	if two.ExchangeRate.String() != "7.1234" || two.AccountingAmount.String() != "-144.25" {
		t.Errorf("跨币种的折算不符: %+v", two)
	}
	if one.AccountingCurrency != "CNY" || two.AccountingCurrency != "CNY" {
		t.Errorf("记账币种应取传入值: %+v %+v", one, two)
	}
	if one.Id == 0 || two.Id == 0 || one.Id == two.Id {
		t.Errorf("每笔都该有自己的ID: %d %d", one.Id, two.Id)
	}
	if one.Version != 1 {
		t.Errorf("版本号应从1开始: %d", one.Version)
	}
	if one.BankName != "招商银行" || one.CardLast4 != "6789" || !one.ExpenseAmount.Equal(decimal.RequireFromString("100.50")) {
		t.Errorf("文件里读到的字段应原样保留: %+v", one)
	}
	//摊销月数默认1，起始月=支出日期所属月，结束月=起始月+月数-1
	if one.AmortizationMonths != 1 {
		t.Errorf("摊销月数应默认1: %d", one.AmortizationMonths)
	}
	startMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, one.AmortizationStartMonth, nil)
	endMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, one.AmortizationEndMonth, nil)
	if startMonth != "2026-01-01" || endMonth != "2026-01-01" {
		t.Errorf("摊销起止月不符: %s %s", startMonth, endMonth)
	}
}

func TestFillAccountingCurrency(t *testing.T) {
	ctx := util.GenCtx()

	//明细逐笔给了记账币种就盖过请求带的，汇率留空由系统按支出日期查
	first := testExpense("USD", "100")
	first.AccountingCurrency, first.AmortizationMonths = "JPY", 3
	second := testExpense("USD", "100")
	objects, err := expense.Fill(ctx, []*parser.Expense{first, second}, "CNY")
	if err != nil {
		t.Fatalf("填充异常: %+v", err)
	}
	if objects[0].AccountingCurrency != "JPY" {
		t.Errorf("明细里的记账币种应优先: %+v", objects[0])
	}
	if !objects[0].ExchangeRate.IsPositive() || !objects[0].AccountingAmount.Equal(objects[0].ExpenseAmount.Mul(objects[0].ExchangeRate).Round(config.GetConfig(ctx).AmountScale)) {
		t.Errorf("汇率留空应由系统查出来再算记账金额: %+v", objects[0])
	}
	if objects[1].AccountingCurrency != "CNY" {
		t.Errorf("明细里没给记账币种应用请求带的: %+v", objects[1])
	}
	//摊销月数填了3，结束月=起始月+2
	startMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, objects[0].AmortizationStartMonth, nil)
	endMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, objects[0].AmortizationEndMonth, nil)
	if objects[0].AmortizationMonths != 3 || startMonth != "2026-01-01" || endMonth != "2026-03-01" {
		t.Errorf("摊销起止月不符: months=%d %s %s", objects[0].AmortizationMonths, startMonth, endMonth)
	}
}

// 浏览器的结果不可全信：识别器该挡的，服务端一律再挡一次
func TestFillInvalid(t *testing.T) {
	ctx := util.GenCtx()
	valid := func() *parser.Expense { return testExpense("CNY", "1") }

	if _, err := expense.Fill(ctx, []*parser.Expense{valid()}, ""); err == nil {
		t.Errorf("记账币种为空应报错")
	}
	if _, err := expense.Fill(ctx, []*parser.Expense{valid()}, "XYZ"); err == nil {
		t.Errorf("记账币种不在枚举内应报错")
	}
	if _, err := expense.Fill(ctx, nil, "CNY"); err == nil {
		t.Errorf("明细为空应报错，不能入库一个空批次")
	}
	if _, err := expense.Fill(ctx, []*parser.Expense{nil}, "CNY"); err == nil {
		t.Errorf("数组里有 null 应报错")
	}

	cases := map[string]func(object *parser.Expense){
		"支出日期为空":        func(object *parser.Expense) { object.ExpenseDate = time.Time{} },
		"支出币种不在枚举内":     func(object *parser.Expense) { object.ExpenseCurrency = "XYZ" },
		"支出币种为空":        func(object *parser.Expense) { object.ExpenseCurrency = "" },
		"明细里的记账币种不在枚举内": func(object *parser.Expense) { object.ExpenseCurrency, object.AccountingCurrency = "USD", "XYZ" },
		"填了汇率没填记账币种": func(object *parser.Expense) {
			object.ExpenseCurrency, object.ExchangeRate = "USD", decimal.RequireFromString("7")
		},
		"汇率非正": func(object *parser.Expense) {
			object.ExpenseCurrency, object.AccountingCurrency, object.ExchangeRate = "USD", "CNY", decimal.RequireFromString("-7")
		},
		"摊销月数为负": func(object *parser.Expense) { object.AmortizationMonths = -1 },
		//月数大到让结束月绕回起始月之前，宁可报错也不能把脏数据写进去
		"摊销月数过大": func(object *parser.Expense) { object.AmortizationMonths = 1 << 62 },
	}
	for name, mutate := range cases {
		object := valid()
		mutate(object)
		_, err := expense.Fill(ctx, []*parser.Expense{valid(), object}, "CNY")
		if err == nil {
			t.Errorf("%s应报错", name)
			continue
		}
		//报错要带笔数，几十笔的明细里才定位得到是哪一笔
		if !strings.Contains(err.Error(), "第2笔") {
			t.Errorf("%s的报错应带笔数: %+v", name, err)
		}
	}
}
