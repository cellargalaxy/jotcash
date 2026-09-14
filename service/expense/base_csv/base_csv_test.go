package base_csv

import (
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
)

const testCsvHeader = "银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,支出类型\n"

func TestSupport(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	for _, filename := range []string{"2609.csv", "2609.CSV"} {
		if !parser.Support(ctx, filename, nil) {
			t.Errorf("CSV应认领: %s", filename)
		}
	}
	for _, filename := range []string{"2609.xlsx", "2609", ""} {
		if parser.Support(ctx, filename, nil) {
			t.Errorf("不是CSV不该认领: %s", filename)
		}
	}
}

// 解析器只填文件里给了的字段，派生字段留给上层
func TestParse(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	objects, err := parser.Parse(ctx, []byte(testCsvHeader+"招商银行,6789,2026-01-02,USD,-20.25,亚马逊,买书,7.1234,购物\n"))
	if err != nil {
		t.Fatalf("解析异常: %+v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("应解析出1笔: %d", len(objects))
	}
	object := objects[0]
	if object.BankName != "招商银行" || object.CardLast4 != "6789" || object.Counterparty != "亚马逊" || object.Remark != "买书" || object.ExpenseType != "购物" {
		t.Errorf("字段没解析到: %+v", object)
	}
	if util.Time2Str(ctx, util.DateLayout_2006_01_02, object.ExpenseDate, nil) != "2026-01-02" {
		t.Errorf("支出日期不符: %v", object.ExpenseDate)
	}
	if object.ExpenseAmount.String() != "-20.25" || object.ExchangeRate.String() != "7.1234" {
		t.Errorf("金额或汇率不符: %+v", object)
	}
	if object.Id != 0 || object.AccountingCurrency != "" || !object.AccountingAmount.IsZero() || object.AmortizationMonths != 0 || object.Version != 0 {
		t.Errorf("派生字段不该由解析器填: %+v", object)
	}

	//列顺序无关，可选列缺失按空
	objects, err = parser.Parse(ctx, []byte("支出金额,支出币种,支出日期\n100.50,CNY,2026-01-02\n"))
	if err != nil {
		t.Fatalf("只带必填列应能解析: %+v", err)
	}
	if objects[0].BankName != "" || objects[0].ExpenseAmount.String() != "100.5" {
		t.Errorf("缺失的可选列应为空: %+v", objects[0])
	}
	//汇率列留空留个0，上层据此兜底
	if !objects[0].ExchangeRate.IsZero() {
		t.Errorf("汇率列留空应留0: %s", objects[0].ExchangeRate)
	}

	//只有表头解析出0笔，是否报错由上层定
	objects, err = parser.Parse(ctx, []byte(testCsvHeader))
	if err != nil || len(objects) != 0 {
		t.Errorf("只有表头应解析出0笔: len=%d err=%+v", len(objects), err)
	}
}

func TestParseInvalid(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	cases := map[string]string{
		"空文件":    "",
		"未知列":    "支出日期,支出币种,支出金额,乱七八糟\n2026-01-02,CNY,1,x\n",
		"缺必填列":   "支出日期,支出币种\n2026-01-02,CNY\n",
		"重复列":    "支出日期,支出币种,支出金额,支出金额\n2026-01-02,CNY,1,2\n",
		"支出日期非法": testCsvHeader + ",,2026/01/02,CNY,1,,,,\n",
		"支出日期为空": testCsvHeader + ",,,CNY,1,,,,\n",
		"支出金额非法": testCsvHeader + ",,2026-01-02,CNY,abc,,,,\n",
		"折算汇率非法": testCsvHeader + ",,2026-01-02,USD,1,,,abc,\n",
		"折算汇率非正": testCsvHeader + ",,2026-01-02,USD,1,,,0,\n",
	}
	for name, csv := range cases {
		if _, err := parser.Parse(ctx, []byte(csv)); err == nil {
			t.Errorf("%s应报错", name)
		}
	}

	//行内的错误要带上行号，不然几十行的CSV没法定位
	_, err := parser.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,CNY,1,,,,\n,,2026-01-02,CNY,abc,,,,\n"))
	if err == nil {
		t.Fatalf("第2行金额非法应报错")
	}
	if !strings.Contains(err.Error(), "第3行") {
		t.Errorf("报错应带行号: %+v", err)
	}
}

// 支出币种不在解析器里校验，留给上层统一挡
func TestParseCurrency(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	objects, err := parser.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,XYZ,1,,,1,\n"))
	if err != nil {
		t.Fatalf("解析异常: %+v", err)
	}
	if objects[0].ExpenseCurrency != "XYZ" {
		t.Errorf("支出币种应原样带出: %+v", objects[0])
	}
}
