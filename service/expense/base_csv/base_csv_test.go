package base_csv

import (
	"os"
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
)

func TestMain(m *testing.M) {
	code := m.Run()
	//config包的init会在测试二进制的工作目录写配置文件
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

const testCsvHeader = "银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,记账币种,支出类型,摊销月数\n"

func TestSupport(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	if !parser.Support(ctx, []byte(testCsvHeader+",,2026-01-02,CNY,1,,,,,,\n")) {
		t.Errorf("本契约的表头应认领")
	}
	if !parser.Support(ctx, []byte(testCsvHeader)) {
		t.Errorf("只有表头也应认领")
	}
	//Excel另存的CSV开头带BOM，表头首列会被它顶掉
	if !parser.Support(ctx, []byte("\ufeff"+testCsvHeader+",,2026-01-02,CNY,1,,,,,,\n")) {
		t.Errorf("带BOM的表头也应认领")
	}

	//认的是表头，不是文件后缀：别家的CSV一样是.csv，不能硬解
	rejects := map[string]string{
		"别家CSV": "交易日期,摘要,发生额,余额\n2026-01-02,消费,100.50,0\n",
		"空文件":   "",
		"不是CSV": "%PDF-1.7\n二进制内容",
		"少一列":   "银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率\n",
		"多一列":   testCsvHeader[:len(testCsvHeader)-1] + ",乱七八糟\n",
		"换个顺序":  "卡号后四位,银行名称,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,支出类型\n",
	}
	for name, data := range rejects {
		if parser.Support(ctx, []byte(data)) {
			t.Errorf("%s不该认领", name)
		}
	}
}

// 解析器只填文件里给了的字段，派生字段留给上层
func TestParse(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	objects, err := parser.Parse(ctx, []byte(testCsvHeader+"招商银行,6789,2026-01-02,USD,-20.25,亚马逊,买书,7.1234,CNY,购物,\n"))
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
	if object.ExpenseAmount.String() != "-20.25" || object.ExchangeRate.String() != "7.1234" || object.AccountingCurrency != "CNY" {
		t.Errorf("金额、汇率或记账币种不符: %+v", object)
	}
	//记账金额是系统算的，不在CSV里
	if object.Id != 0 || !object.AccountingAmount.IsZero() || object.Version != 0 || !object.AmortizationStartMonth.IsZero() {
		t.Errorf("派生字段不该由解析器填: %+v", object)
	}

	//摊销月数填了就照填的来，留空留0让上层取默认
	objects, err = parser.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,CNY,1,,,,,,3\n"))
	if err != nil {
		t.Fatalf("解析异常: %+v", err)
	}
	if objects[0].AmortizationMonths != 3 {
		t.Errorf("摊销月数应解析到: %d", objects[0].AmortizationMonths)
	}

	//带BOM的文件要能照常解析出内容
	objects, err = parser.Parse(ctx, []byte("\ufeff"+testCsvHeader+"招商银行,,2026-01-02,CNY,1,,,,,,\n"))
	if err != nil {
		t.Fatalf("带BOM的文件解析异常: %+v", err)
	}
	if objects[0].BankName != "招商银行" {
		t.Errorf("带BOM时首列数据不该受影响: %+v", objects[0])
	}

	//可选列留空按空
	objects, err = parser.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,CNY,100.50,,,,,,\n"))
	if err != nil {
		t.Fatalf("可选列留空应能解析: %+v", err)
	}
	if objects[0].BankName != "" || objects[0].Counterparty != "" || objects[0].ExpenseAmount.String() != "100.5" {
		t.Errorf("留空的可选列应为空: %+v", objects[0])
	}
	//汇率与摊销月数留空都留0，上层据此兜底
	if !objects[0].ExchangeRate.IsZero() || objects[0].AmortizationMonths != 0 {
		t.Errorf("汇率与摊销月数留空应留0: %+v", objects[0])
	}

	//只有表头解析出0笔，是否报错由上层定
	objects, err = parser.Parse(ctx, []byte(testCsvHeader))
	if err != nil || len(objects) != 0 {
		t.Errorf("只有表头应解析出0笔: len=%d want=0 err=%+v", len(objects), err)
	}

	//CSV读的是不定长行，数据行比表头短时右边那截列取不到下标，按留空处理
	objects, err = parser.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,CNY,100.50\n"))
	if err != nil {
		t.Fatalf("数据行比表头短应能解析: %+v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("应解析出1笔: got=%d want=1", len(objects))
	}
	if objects[0].ExpenseAmount.String() != "100.5" || objects[0].ExpenseCurrency != "CNY" {
		t.Errorf("短行里给了的列应解析到: %+v", objects[0])
	}
	if objects[0].ExpenseType != "" || objects[0].AccountingCurrency != "" || !objects[0].ExchangeRate.IsZero() || objects[0].AmortizationMonths != 0 {
		t.Errorf("短行里没给的列应留空: %+v", objects[0])
	}
}

// CSV本身就读不出来的，认领与解析要给出一致的答案：不认领，硬解也报错
func TestSupportParseBroken(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	//引号没闭合，csv读到行尾也凑不出一个完整字段
	broken := []byte(testCsvHeader + `,,2026-01-02,CNY,1,"没闭合的引号,,,,,` + "\n")
	if parser.Support(ctx, broken) {
		t.Errorf("读不出来的CSV不该认领")
	}
	if _, err := parser.Parse(ctx, broken); err == nil {
		t.Errorf("读不出来的CSV硬解应报错")
	}
}

func TestParseInvalid(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	cases := map[string]string{
		"空文件":    "",
		"表头少一列":  "银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率\n,,2026-01-02,CNY,1,,,\n",
		"表头多一列":  testCsvHeader[:len(testCsvHeader)-1] + ",乱七八糟\n,,2026-01-02,CNY,1,,,,x\n",
		"表头换顺序":  "卡号后四位,银行名称,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,支出类型\n,,2026-01-02,CNY,1,,,,,,\n",
		"支出日期非法": testCsvHeader + ",,2026/01/02,CNY,1,,,,,,\n",
		"支出日期为空": testCsvHeader + ",,,CNY,1,,,,,,\n",
		"支出金额非法": testCsvHeader + ",,2026-01-02,CNY,abc,,,,,,\n",
		"折算汇率非法": testCsvHeader + ",,2026-01-02,USD,1,,,abc,CNY,,\n",
		"折算汇率非正": testCsvHeader + ",,2026-01-02,USD,1,,,0,CNY,,\n",
		//汇率是「支出币种兑记账币种」，没有记账币种就不知道兑给谁
		"填了汇率没填记账币种": testCsvHeader + ",,2026-01-02,USD,1,,,7.1234,,,\n",
		"摊销月数非法":     testCsvHeader + ",,2026-01-02,CNY,1,,,,,,abc\n",
		"摊销月数为0":     testCsvHeader + ",,2026-01-02,CNY,1,,,,,,0\n",
		"摊销月数为负":     testCsvHeader + ",,2026-01-02,CNY,1,,,,,,-1\n",
		//记账金额只能由系统算，进不了契约
		"表头带了记账金额": "银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,记账币种,记账金额,支出类型,摊销月数\n,,2026-01-02,CNY,1,,,,,1,,\n",
	}
	for name, csv := range cases {
		if _, err := parser.Parse(ctx, []byte(csv)); err == nil {
			t.Errorf("%s应报错", name)
		}
	}

	//行内的错误要带上行号，不然几十行的CSV没法定位
	_, err := parser.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,CNY,1,,,,,,\n,,2026-01-02,CNY,abc,,,,,,\n"))
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

	objects, err := parser.Parse(ctx, []byte(testCsvHeader+",,2026-01-02,XYZ,1,,,1,CNY,,\n"))
	if err != nil {
		t.Fatalf("解析异常: %+v", err)
	}
	if objects[0].ExpenseCurrency != "XYZ" {
		t.Errorf("支出币种应原样带出: %+v", objects[0])
	}
}
