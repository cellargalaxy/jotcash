package expense_test

import (
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/service/expense"
	_ "github.com/cellargalaxy/jotcash/service/expense/base_csv"
)

const testCsvHeader = "银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,支出类型\n"

func TestParse(t *testing.T) {
	ctx := util.GenCtx()

	objects, err := expense.Parse(ctx, []byte(testCsvHeader+
		"招商银行,6789,2026-01-02,CNY,100.50,亚马逊,买书,,购物\n"+
		"中国银行,4321,2026-03-04,USD,-20.25,苹果,退款,7.1234,数码\n"), "CNY")
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
	//文件里给了汇率就用文件的，记账金额=支出金额×折算汇率
	if second.ExchangeRate.String() != "7.1234" || !second.AccountingAmount.Equal(second.ExpenseAmount.Mul(second.ExchangeRate)) {
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
	//摊分月数默认1，起始月=支出日期所属月，结束月=起始月+月数-1
	if first.AmortizationMonths != 1 {
		t.Errorf("摊分月数应默认1: %d", first.AmortizationMonths)
	}
	startMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, first.AmortizationStartMonth, nil)
	endMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, first.AmortizationEndMonth, nil)
	if startMonth != "2026-01-01" || endMonth != "2026-01-01" {
		t.Errorf("摊分起止月不符: %s %s", startMonth, endMonth)
	}
}

func TestParseInvalid(t *testing.T) {
	ctx := util.GenCtx()
	csv := []byte(testCsvHeader + "招商银行,6789,2026-01-02,CNY,100.50,亚马逊,买书,,购物\n")

	if _, err := expense.Parse(ctx, csv, ""); err == nil {
		t.Errorf("记账币种为空应报错")
	}
	if _, err := expense.Parse(ctx, csv, "XYZ"); err == nil {
		t.Errorf("记账币种不在枚举内应报错")
	}
	if _, err := expense.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,XYZ,1,,,1,\n"), "CNY"); err == nil {
		t.Errorf("支出币种不在枚举内应报错")
	}
	//别家的CSV没有解析器认领，不能当成本系统的格式硬解
	if _, err := expense.Parse(ctx, []byte("交易日期,摘要,发生额\n2026-01-02,消费,100.50\n"), "CNY"); err == nil {
		t.Errorf("没有解析器认领应报错")
	}
}
