package expense_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/expense"
	_ "github.com/cellargalaxy/jotcash/service/expense/base_csv"
	_ "github.com/cellargalaxy/jotcash/service/expense/cmb_debit"
	_ "github.com/cellargalaxy/jotcash/service/expense/icbc_credit"
	"github.com/shopspring/decimal"
)

func TestMain(m *testing.M) {
	code := m.Run()
	//config包的init会在测试二进制的工作目录写配置文件
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

const testCsvHeader = "银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,记账币种,支出类型,摊销月数\n"

func TestParse(t *testing.T) {
	ctx := util.GenCtx()

	objects, err := expense.Parse(ctx, []byte(testCsvHeader+
		"招商银行,6789,2026-01-02,CNY,100.50,亚马逊,买书,,,购物,\n"+
		"中国银行,4321,2026-03-04,USD,-20.25,苹果,退款,7.1234,CNY,数码,\n"), "CNY")
	if err != nil {
		t.Fatalf("解析异常: %+v", err)
	}
	if len(objects) != 2 {
		t.Fatalf("应解析出2笔: %d", len(objects))
	}
	first, second := objects[0], objects[1]

	//同币种的汇率恒为1
	if first.ExchangeRate.String() != "1" || !first.AccountingAmount.Equal(first.ExpenseAmount) {
		t.Errorf("同币种的折算不符: %+v", first)
	}
	//文件里给了汇率就用文件的，记账金额=支出金额×折算汇率，乘出来的-144.24885按分四舍五入
	if second.ExchangeRate.String() != "7.1234" || second.AccountingAmount.String() != "-144.25" {
		t.Errorf("跨币种的折算不符: %+v", second)
	}
	if first.AccountingCurrency != "CNY" || second.AccountingCurrency != "CNY" {
		t.Errorf("记账币种应取传入值: %+v %+v", first, second)
	}
	if first.Id == 0 || second.Id == 0 || first.Id == second.Id {
		t.Errorf("每笔都该有自己的ID: %d %d", first.Id, second.Id)
	}
	if first.Version != 1 {
		t.Errorf("版本号应从1开始: %d", first.Version)
	}
	//摊销月数默认1，起始月=支出日期所属月，结束月=起始月+月数-1
	if first.AmortizationMonths != 1 {
		t.Errorf("摊销月数应默认1: %d", first.AmortizationMonths)
	}
	startMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, first.AmortizationStartMonth, nil)
	endMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, first.AmortizationEndMonth, nil)
	if startMonth != "2026-01-01" || endMonth != "2026-01-01" {
		t.Errorf("摊销起止月不符: %s %s", startMonth, endMonth)
	}
}

func TestParseAccountingCurrency(t *testing.T) {
	ctx := util.GenCtx()

	//文件里逐笔给了记账币种就盖过请求带的，汇率留空由系统按支出日期查
	objects, err := expense.Parse(ctx, []byte(testCsvHeader+
		"招商银行,6789,2026-01-02,USD,100,亚马逊,买书,,JPY,购物,3\n"+
		"招商银行,6789,2026-01-02,USD,100,亚马逊,买书,,,购物,\n"), "CNY")
	if err != nil {
		t.Fatalf("解析异常: %+v", err)
	}
	if objects[0].AccountingCurrency != "JPY" {
		t.Errorf("文件里的记账币种应优先: %+v", objects[0])
	}
	if !objects[0].ExchangeRate.IsPositive() || !objects[0].AccountingAmount.Equal(objects[0].ExpenseAmount.Mul(objects[0].ExchangeRate)) {
		t.Errorf("汇率留空应由系统查出来再算记账金额: %+v", objects[0])
	}
	if objects[1].AccountingCurrency != "CNY" {
		t.Errorf("文件里没给记账币种应用请求带的: %+v", objects[1])
	}
	//摊销月数填了3，结束月=起始月+2
	startMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, objects[0].AmortizationStartMonth, nil)
	endMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, objects[0].AmortizationEndMonth, nil)
	if objects[0].AmortizationMonths != 3 || startMonth != "2026-01-01" || endMonth != "2026-03-01" {
		t.Errorf("摊销起止月不符: months=%d %s %s", objects[0].AmortizationMonths, startMonth, endMonth)
	}
}

func TestParseInvalid(t *testing.T) {
	ctx := util.GenCtx()
	csv := []byte(testCsvHeader + "招商银行,6789,2026-01-02,CNY,100.50,亚马逊,买书,,,购物,\n")

	if _, err := expense.Parse(ctx, csv, ""); err == nil {
		t.Errorf("记账币种为空应报错")
	}
	if _, err := expense.Parse(ctx, csv, "XYZ"); err == nil {
		t.Errorf("记账币种不在枚举内应报错")
	}
	if _, err := expense.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,XYZ,1,,,1,CNY,,\n"), "CNY"); err == nil {
		t.Errorf("支出币种不在枚举内应报错")
	}
	if _, err := expense.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,USD,1,,,1,XYZ,,\n"), "CNY"); err == nil {
		t.Errorf("文件里的记账币种不在枚举内应报错")
	}
	//月数大到让结束月绕回起始月之前，宁可报错也不能把脏数据写进去
	if _, err := expense.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,CNY,1,,,,,,99999999999999999999\n"), "CNY"); err == nil {
		t.Errorf("摊销月数过大应报错")
	}
	//别家的CSV没有解析器认领，不能当成本系统的格式硬解
	if _, err := expense.Parse(ctx, []byte("交易日期,摘要,发生额\n2026-01-02,消费,100.50\n"), "CNY"); err == nil {
		t.Errorf("没有解析器认领应报错")
	}
}

const testBadRateData = "只有测试用的解析器认领这份内容"

// 解析器是注册进来的，上层不能假定每个解析器都替它把汇率校验过：非正的汇率到了这一层还得再挡一次
type badRateParser struct {
}

func (this *badRateParser) Support(ctx context.Context, data []byte) bool {
	return string(data) == testBadRateData
}
func (this *badRateParser) Parse(ctx context.Context, data []byte) ([]*model.Expense, error) {
	object := model.Expense{
		ExpenseDate:     time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		ExpenseCurrency: "USD",
		ExpenseAmount:   decimal.RequireFromString("100"),
		ExchangeRate:    decimal.RequireFromString("-7.1234"),
	}
	return []*model.Expense{&object}, nil
}

func TestParseBadRate(t *testing.T) {
	ctx := util.GenCtx()
	expense.Register(new(badRateParser))

	objects, err := expense.Parse(ctx, []byte(testBadRateData), "CNY")
	if err == nil {
		t.Fatalf("解析器放过来的非正汇率应被挡下: %+v", objects)
	}
	//报错要带笔数，几十笔的文件里才定位得到是哪一笔
	if !strings.Contains(err.Error(), "第1笔") {
		t.Errorf("报错应带笔数: %+v", err)
	}
}
