package cmb_debit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/shopspring/decimal"
)

func TestMain(m *testing.M) {
	code := m.Run()
	//config包的init会在测试二进制的工作目录写配置文件
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

// 样例流水是真实流水、随时会被删，所以只按目录扫，不写死文件名，也不断言里面的任何内容
func rangeSamplePdf(t *testing.T, handle func(index int, data []byte)) {
	t.Helper()
	paths, err := filepath.Glob("../../../resource/*.pdf")
	if err != nil || len(paths) == 0 {
		t.Skip("没有样例PDF，跳过真实流水校验")
	}
	for i := range paths {
		data, err := os.ReadFile(paths[i])
		if err != nil {
			t.Fatalf("读取第%d个样例PDF异常: %+v", i+1, err)
		}
		handle(i+1, data)
	}
}

const testHeader = "招商银行交易流水\n账号：9999********1234\n" +
	"记账日期 货币 交易金额 联机余额 交易摘要 对手信息\nDate Currency Transaction Amount Balance Transaction Type Counter Party\n"

// 表头与6列的横坐标，数据行的单元格与列名左对齐
var testColumnX = []float64{40, 100, 160, 230, 300, 410}

func testHeaderChunks(y float64) []textChunk {
	chunks := make([]textChunk, 0, len(columns))
	for i := range columns {
		chunks = append(chunks, textChunk{x: testColumnX[i], y: y, text: columns[i]})
	}
	return chunks
}

func TestSupport(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	//认的是流水内容，不是.pdf这个后缀：别家的流水一样是PDF，不能硬解
	rejects := map[string][]byte{
		"空文件":  []byte(""),
		"非PDF": []byte(testHeader),
		"CSV":  []byte("银行名称,卡号后四位,支出日期,支出币种,支出金额\n"),
		"坏PDF": []byte("%PDF-1.7\n损坏的二进制内容"),
	}
	for name, data := range rejects {
		if parser.Support(ctx, data) {
			t.Errorf("%s不该认领", name)
		}
	}
}

// Support把PDF读成文本之后就交给它，列名与列序的判据都在这里
func TestCheckContract(t *testing.T) {
	accepts := map[string]string{
		"表头后面跟流水行": testHeader + "2099-01-01 CNY -1.00 1.00 快捷支付 虚拟商户\n",
		"表头后面跟页脚":  testHeader + "温馨提示：\n",
		"只有表头":     testHeader,
	}
	for name, text := range accepts {
		if !checkContract(text) {
			t.Errorf("%s应认领", name)
		}
	}

	rejects := map[string]string{
		"没有标题":    strings.Replace(testHeader, title, "某某银行交易流水", 1),
		"没有账号":    strings.Replace(testHeader, "账号：9999********1234", "户名：某某某", 1),
		"少一列":     strings.Replace(testHeader, " "+colBalance, "", 1),
		"多一列":     strings.Replace(testHeader, colCounterparty+"\n", colCounterparty+" 交易渠道\n", 1),
		"换顺序":     strings.Replace(testHeader, colCurrency+" "+colAmount, colAmount+" "+colCurrency, 1),
		"英文表头少一列": strings.Replace(testHeader, "Balance ", "", 1),
		"英文表头多一列": strings.Replace(testHeader, "Counter Party\n", "Counter Party Channel\n", 1),
		"表头后面多一列": testHeader + "授权号\n",
	}
	for name, text := range rejects {
		if checkContract(text) {
			t.Errorf("%s不该认领", name)
		}
	}
}

// 认领与解析共用同一套判据：认领了就必须解析得出来，没认领就必须报错
func TestParseSamplePdf(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	rangeSamplePdf(t, func(index int, data []byte) {
		objects, err := parser.Parse(ctx, data)
		if !parser.Support(ctx, data) {
			if err == nil {
				t.Errorf("第%d个样例没认领却解析成功", index)
			}
			return
		}
		if err != nil {
			t.Fatalf("第%d个样例解析异常: %+v", index, err)
		}
		if len(objects) == 0 {
			t.Fatalf("第%d个样例应解析出流水", index)
		}
		for i, object := range objects {
			if object.BankName != bankName {
				t.Errorf("第%d笔银行名称不符: got=%s want=%s", i+1, object.BankName, bankName)
			}
			if !cardLast4Regexp.MatchString(object.CardLast4) {
				t.Errorf("第%d笔卡号后四位不是4位数字: 长度=%d", i+1, len(object.CardLast4))
			}
			if object.ExpenseDate.IsZero() {
				t.Errorf("第%d笔支出日期为空", i+1)
			}
			if object.ExpenseCurrency == "" || object.ExpenseAmount.IsZero() || object.Remark == "" {
				t.Errorf("第%d笔支出币种、支出金额或交易备注为空", i+1)
			}
			//派生字段留给上层
			if object.Id != 0 || object.Version != 0 || !object.AccountingAmount.IsZero() || !object.AmortizationStartMonth.IsZero() {
				t.Errorf("第%d笔派生字段不该由解析器填: id=%d version=%d", i+1, object.Id, object.Version)
			}
		}
	})
}

func TestParseInvalid(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	rejects := map[string][]byte{
		"空文件":  []byte(""),
		"非PDF": []byte("不是PDF的内容"),
		"坏PDF": []byte("%PDF-1.7\n损坏的二进制内容"),
	}
	for name, data := range rejects {
		if _, err := parser.Parse(ctx, data); err == nil {
			t.Errorf("%s应报错", name)
		}
	}
}

// 丢弃规则保守：只有能严格判定的信用卡还款与纯入账收入才丢
func TestDiscardable(t *testing.T) {
	cases := map[string]struct {
		amount       string
		remark       string
		counterparty string
		discard      bool
	}{
		"扣款消费":       {amount: "-100.50", remark: "快捷支付", counterparty: "虚拟商户", discard: false},
		"摘要写明信用卡还款":  {amount: "-1000.00", remark: "信用卡还款", discard: true},
		"对手方写明信用卡还款": {amount: "-200.00", remark: "转账", counterparty: "某行信用卡还款", discard: true},
		"对手方写明贷记卡还款": {amount: "-200.00", remark: "还款", counterparty: "某行贷记卡", discard: true},
		"说不清的还款":     {amount: "-500.00", remark: "自动还款", counterparty: "某某", discard: false},
		"房贷还款":       {amount: "-5000.00", remark: "房贷还款", counterparty: "某行个人住房贷款", discard: false},
		"扣息":         {amount: "-9.90", remark: "贷款利息", counterparty: "某行", discard: false},
		"存款结息":       {amount: "0.15", remark: "账户结息", discard: true},
		"活期利息":       {amount: "5.20", remark: "利息", discard: true},
		"转账转入":       {amount: "1500.00", remark: "行内转账转入", discard: true},
		"网联收款":       {amount: "100.00", remark: "网联收款", discard: true},
		"银联代付":       {amount: "3000.00", remark: "银联代付", discard: true},
		"代发工资":       {amount: "10000.00", remark: "代发工资", discard: true},
		"商户名撞上收入词":   {amount: "88.00", remark: "快捷支付", counterparty: "虚拟收款惠生活商户", discard: false},
		"商户退款":       {amount: "50.00", remark: "商户退款", discard: false},
		"消费冲正":       {amount: "20.00", remark: "消费冲正", discard: false},
		"商场退货":       {amount: "99.00", remark: "商场退货", discard: false},
		"交易撤销":       {amount: "35.00", remark: "交易撤销", counterparty: "虚拟商户", discard: false},
		"商品退回":       {amount: "120.00", remark: "商品退回", counterparty: "虚拟平台", discard: false},
		"信用卡还款退回":    {amount: "300.00", remark: "信用卡还款退回", discard: false},
		"说不清的入账":     {amount: "88.00", remark: "其他交易", discard: false},
	}
	for name, one := range cases {
		got := discardable(decimal.RequireFromString(one.amount), one.remark, one.counterparty)
		if got != one.discard {
			t.Errorf("%s: discardable(%s, %s, %s) = %v, want %v", name, one.amount, one.remark, one.counterparty, got, one.discard)
		}
	}
}

// 列的左边界从表头取，换个页边距也跟得上
func TestFindColumns(t *testing.T) {
	bounds, headerY, ok := findColumns(append(testHeaderChunks(500), textChunk{x: 40, y: 700, text: "账号：9999********1234"}))
	if !ok {
		t.Fatalf("应找到表头")
	}
	if headerY != 500 {
		t.Errorf("表头纵坐标不符: got=%v want=500", headerY)
	}
	for i := range bounds {
		if bounds[i] != testColumnX[i] {
			t.Errorf("第%d列左边界不符: got=%v want=%v", i+1, bounds[i], testColumnX[i])
		}
	}

	rejects := map[string][]textChunk{
		"没有表头": {{x: 40, y: 700, text: "账号：9999********1234"}},
		"少一列":  testHeaderChunks(500)[:len(columns)-1],
		"多一列":  append(testHeaderChunks(500), textChunk{x: 500, y: 500, text: "交易渠道"}),
	}
	for name, chunks := range rejects {
		if _, _, ok := findColumns(chunks); ok {
			t.Errorf("%s不该找到表头", name)
		}
	}
}

// 表头以下按日期单元格切行，折行的半截按纵坐标并回原单元格，页脚够不着任何一行
func TestParsePageRows(t *testing.T) {
	chunks := append(testHeaderChunks(500),
		textChunk{x: 40, y: 460, text: "2099-01-01"},
		textChunk{x: 100, y: 460, text: "CNY"},
		textChunk{x: 160, y: 460, text: "-50.00"},
		textChunk{x: 230, y: 460, text: "1,000.00"},
		textChunk{x: 300, y: 460, text: "快捷支付"},
		textChunk{x: 410, y: 466, text: "虚拟商户"},
		textChunk{x: 410, y: 454, text: "（分店）"},
		textChunk{x: 40, y: 420, text: "2099-01-02"},
		textChunk{x: 100, y: 420, text: "CNY"},
		textChunk{x: 160, y: 420, text: "-88.88"},
		textChunk{x: 230, y: 420, text: "911.12"},
		textChunk{x: 300, y: 420, text: "扫码消费"},
		textChunk{x: 410, y: 420, text: "虚拟商户"},
		textChunk{x: 100, y: 100, text: "温馨提示与流水验真说明"},
	)

	rows, ok := parsePageRows(chunks)
	if !ok {
		t.Fatalf("应解析出流水行")
	}
	if len(rows) != 2 {
		t.Fatalf("流水行数不符: got=%d want=2", len(rows))
	}
	if rowValue(rows[0], colDate) != "2099-01-01" || rowValue(rows[0], colCurrency) != "CNY" ||
		rowValue(rows[0], colAmount) != "-50.00" || rowValue(rows[0], colBalance) != "1,000.00" ||
		rowValue(rows[0], colRemark) != "快捷支付" || rowValue(rows[0], colCounterparty) != "虚拟商户（分店）" {
		t.Errorf("首行单元格不符: %+v", rows[0].cells)
	}
	if rowValue(rows[1], colRemark) != "扫码消费" || rowValue(rows[1], colCounterparty) != "虚拟商户" {
		t.Errorf("次行单元格不符: %+v", rows[1].cells)
	}

	if _, ok := parsePageRows([]textChunk{{x: 40, y: 460, text: "2099-01-01"}}); ok {
		t.Errorf("没有表头不该解析出流水行")
	}
}

func TestParseRow(t *testing.T) {
	ctx := util.GenCtx()

	row := tableRow{y: 460, cells: []string{"2099-01-01", "CNY", "-1,234.56", "1.00", "快捷支付", "虚拟商户"}}
	object, err := parseRow(ctx, 1, "1234", row)
	if err != nil || object == nil {
		t.Fatalf("扣款应解析出来: %+v", err)
	}
	//扣款是负数，支出是正数
	if object.BankName != bankName || object.CardLast4 != "1234" || object.ExpenseCurrency != "CNY" ||
		object.ExpenseAmount.String() != "1234.56" || object.Remark != "快捷支付" || object.Counterparty != "虚拟商户" {
		t.Errorf("扣款字段不符: %+v", object)
	}
	if object.ExpenseDate.Format(util.DateLayout_2006_01_02) != "2099-01-01" {
		t.Errorf("支出日期不符: %s", object.ExpenseDate)
	}

	//退款入账是支出抵扣，记成负数支出
	object, err = parseRow(ctx, 2, "1234", tableRow{cells: []string{"2099-01-02", "CNY", "50.00", "1.00", "商户退款", "虚拟商户"}})
	if err != nil || object == nil {
		t.Fatalf("退款应解析出来: %+v", err)
	}
	if object.ExpenseAmount.String() != "-50" {
		t.Errorf("退款金额不符: %s", object.ExpenseAmount)
	}

	object, err = parseRow(ctx, 3, "1234", tableRow{cells: []string{"2099-01-03", "CNY", "0.15", "1.00", "账户结息", ""}})
	if err != nil {
		t.Fatalf("结息解析异常: %+v", err)
	}
	if object != nil {
		t.Errorf("结息应丢掉: %+v", object)
	}

	rejects := map[string][]string{
		"金额非法":  {"2099-01-04", "CNY", "一百", "1.00", "快捷支付", "虚拟商户"},
		"日期非法":  {"某年某月", "CNY", "-1.00", "1.00", "快捷支付", "虚拟商户"},
		"单元格缺失": {"2099-01-04", "CNY"},
	}
	for name, cells := range rejects {
		if _, err := parseRow(ctx, 1, "1234", tableRow{cells: cells}); err == nil {
			t.Errorf("%s应报错", name)
		}
	}
}

func TestParseCardLast4(t *testing.T) {
	accepts := map[string]string{
		"账号：9999********1234": "1234",
		"账号: 1234":            "1234",
	}
	for text, want := range accepts {
		got := parseCardLast4(append(testHeaderChunks(500), textChunk{x: 40, y: 700, text: text}))
		if got != want {
			t.Errorf("parseCardLast4(%s) = %s, want %s", text, got, want)
		}
	}

	rejects := map[string][]textChunk{
		"没有账号":    append(testHeaderChunks(500), textChunk{x: 40, y: 700, text: "户名：某某某"}),
		"账号在表头下面": append(testHeaderChunks(500), textChunk{x: 40, y: 300, text: "账号：9999********1234"}),
		"账号位数不够":  append(testHeaderChunks(500), textChunk{x: 40, y: 700, text: "账号：***"}),
		"没有表头":    {{x: 40, y: 700, text: "账号：9999********1234"}},
	}
	for name, chunks := range rejects {
		if got := parseCardLast4(chunks); got != "" {
			t.Errorf("%s应取不到卡号后四位: got=%s", name, got)
		}
	}
}
