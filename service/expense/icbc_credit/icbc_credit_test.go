package icbc_credit

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bojanz/currency"
	"github.com/cellargalaxy/go_common/util"
)

func TestMain(m *testing.M) {
	code := m.Run()
	//config包的init会在测试二进制的工作目录写配置文件
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

// 样例账单是真实流水、随时会被删，所以只按目录扫，不写死文件名，也不断言里面的任何内容
func rangeSamplePdf(t *testing.T, handle func(index int, data []byte)) {
	t.Helper()
	paths, err := filepath.Glob("../../../resource/*.pdf")
	if err != nil || len(paths) == 0 {
		t.Skip("没有样例PDF，跳过真实账单校验")
	}
	for i := range paths {
		data, err := os.ReadFile(paths[i])
		if err != nil {
			t.Fatalf("读取第%d个样例PDF异常: %+v", i+1, err)
		}
		handle(i+1, data)
	}
}

const testHeader = "中国工商银行\n中国工商银行信用卡历史明细（电子版）\n卡号: 9999999999999999\n户名：某某某\n起止日期：2099-01-01 — 2099-03-31\n" +
	"入账日期\n交易卡号\n收支\n交易币种\n交易金额\n入账币种\n入账金额\n账户余额\n摘要\n交易场所\n"

func TestSupport(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	//认的是账单内容，不是.pdf这个后缀：别家的账单一样是PDF，不能硬解
	rejects := map[string][]byte{
		"空文件":  []byte(""),
		"非PDF": []byte(testHeader),
		"CSV":  []byte("银行名称,卡号后四位,支出日期,支出币种,支出金额\n"),
		"坏PDF": []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF"),
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
		"表头后面跟明细行": testHeader + "2099-01-0112:30:00\n9999999999999999\n借\n人民币\n1.00\n人民币\n1.00\n-1.00\n消费\n某商户\n",
		"表头后面跟页脚":  testHeader + "本页支出算术合计：1.00本页交易笔数：1\n",
		"只有表头":     testHeader,
	}
	for name, text := range accepts {
		if !checkContract(text) {
			t.Errorf("%s应认领", name)
		}
	}

	rejects := map[string]string{
		"没有标题":   strings.Replace(testHeader, title, "某某银行信用卡明细", 1),
		"没有起止日期": strings.Replace(testHeader, "起止日期：", "账单周期：", 1),
		"没有卡号":   strings.Replace(testHeader, "卡号: ", "账号 ", 1),
		"少一列":    strings.Replace(testHeader, colBalance+"\n", "", 1),
		"多一列":    testHeader + "授权号\n2099-01-0112:30:00\n",
		"换顺序":    strings.Replace(testHeader, colDirection+"\n"+colTxCurrency, colTxCurrency+"\n"+colDirection, 1),
		"列名不一样":  strings.Replace(testHeader, colPlace, "交易地点", 1),
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
			t.Fatalf("第%d个样例应解析出明细", index)
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
			if !currency.IsValid(object.ExpenseCurrency) {
				t.Errorf("第%d笔支出币种不在枚举内: %s", i+1, object.ExpenseCurrency)
			}
			if object.ExpenseAmount.IsZero() {
				t.Errorf("第%d笔支出金额为0", i+1)
			}
			if object.Counterparty == "" || object.Remark == "" {
				t.Errorf("第%d笔交易对手方或交易备注为空", i+1)
			}
			//还款是账户之间的划转，不该留在支出里
			if slices.Contains(repaymentSummaries, object.Remark) {
				t.Errorf("第%d笔还款没被丢掉: %s", i+1, object.Remark)
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
		"坏PDF": []byte("%PDF-1.4 伪造的空PDF"),
	}
	for name, data := range rejects {
		if _, err := parser.Parse(ctx, data); err == nil {
			t.Errorf("%s应报错", name)
		}
	}
}

// 全部用虚构数据，真实账单里的卡号、金额、商户一律不进代码
func TestParseChunk(t *testing.T) {
	ctx := util.GenCtx()

	object, err := parseChunk(ctx, 1, 1, []string{"2099-01-0112:30:00", "9999888877771234", "借", "人民币", "100.50", "人民币", "100.50", "-100.50", "消费", "虚拟商户"})
	if err != nil || object == nil {
		t.Fatalf("正常消费应解析出来: %+v", err)
	}
	if object.BankName != bankName || object.CardLast4 != "1234" || object.ExpenseCurrency != "CNY" ||
		object.ExpenseAmount.String() != "100.5" || object.Remark != "消费" || object.Counterparty != "虚拟商户" {
		t.Errorf("正常消费字段不符: %+v", object)
	}
	if object.ExpenseDate.Format(util.DateLayout_2006_01_02_15_04_05) != "2099-01-01 12:30:00" {
		t.Errorf("支出日期不符: %s", object.ExpenseDate)
	}

	//贷方的退货、冲正是支出抵扣，记成负数支出
	object, err = parseChunk(ctx, 1, 2, []string{"2099-01-02", "9999888877775678", "贷", "美元", "20.00", "美元", "20.00", "-20.00", "退货", "虚拟商户"})
	if err != nil || object == nil {
		t.Fatalf("退货应解析出来: %+v", err)
	}
	if object.ExpenseCurrency != "USD" || object.ExpenseAmount.String() != "-20" {
		t.Errorf("退货金额或币种不符: %+v", object)
	}

	//交易场所折行
	object, err = parseChunk(ctx, 1, 3, []string{"2099-01-0318:00:00", "9999888877771234", "借", "人民币", "12,345.67", "人民币", "12,345.67", "-1.00", "消费", "虚拟商户", "（分店）"})
	if err != nil || object == nil {
		t.Fatalf("折行的交易场所应解析出来: %+v", err)
	}
	if object.Counterparty != "虚拟商户（分店）" || object.ExpenseAmount.String() != "12345.67" {
		t.Errorf("折行拼接或千分位金额不符: %+v", object)
	}
}

// 丢弃规则保守：只有贷方的还款摘要能丢，其余一律留着
func TestParseChunkDiscard(t *testing.T) {
	ctx := util.GenCtx()

	discards := map[string][]string{
		"贷方转帐": {"2099-01-15", "9999888877771234", "贷", "人民币", "1,000.00", "人民币", "1,000.00", "0.00", "转帐", "网点营业室"},
		"贷方转账": {"2099-01-15", "9999888877771234", "贷", "人民币", "1,000.00", "人民币", "1,000.00", "0.00", "转账", "网点营业室"},
		"贷方还款": {"2099-01-15", "9999888877771234", "贷", "人民币", "500.00", "人民币", "500.00", "0.00", "还款", "网点营业室"},
	}
	for name, chunk := range discards {
		object, err := parseChunk(ctx, 1, 1, chunk)
		if err != nil {
			t.Fatalf("%s解析异常: %+v", name, err)
		}
		if object != nil {
			t.Errorf("%s应丢掉: %+v", name, object)
		}
	}

	keeps := map[string][]string{
		"借方还款手续费":  {"2099-01-15", "9999888877771234", "借", "人民币", "10.00", "人民币", "10.00", "0.00", "还款", "网点营业室"},
		"商户名带还款字样": {"2099-01-16", "9999888877771234", "贷", "人民币", "88.00", "人民币", "88.00", "0.00", "退货", "虚拟还款惠生活商户"},
		"摘要带还款字样":  {"2099-01-17", "9999888877771234", "贷", "人民币", "88.00", "人民币", "88.00", "0.00", "还款冲正", "虚拟商户"},
		"说不清的贷方":   {"2099-01-18", "9999888877771234", "贷", "人民币", "9.90", "人民币", "9.90", "0.00", "年费返还", "虚拟商户"},
		"贷方结息":     {"2099-01-19", "9999888877771234", "贷", "人民币", "1.23", "人民币", "1.23", "0.00", "结息", "网点营业室"},
	}
	for name, chunk := range keeps {
		object, err := parseChunk(ctx, 1, 1, chunk)
		if err != nil {
			t.Fatalf("%s解析异常: %+v", name, err)
		}
		if object == nil {
			t.Errorf("%s应留着", name)
		}
	}
}

func TestParseChunkInvalid(t *testing.T) {
	ctx := util.GenCtx()

	rejects := map[string][]string{
		"列缺失":    {"2099-01-01", "9999888877771234"},
		"日期非法":   {"某年某月", "9999888877771234", "借", "人民币", "1.00", "人民币", "1.00", "0.00", "消费", "虚拟商户"},
		"币种非法":   {"2099-01-01", "9999888877771234", "借", "虚构币", "1.00", "人民币", "1.00", "0.00", "消费", "虚拟商户"},
		"金额非法":   {"2099-01-01", "9999888877771234", "借", "人民币", "一百", "人民币", "1.00", "0.00", "消费", "虚拟商户"},
		"收支方向非法": {"2099-01-01", "9999888877771234", "平", "人民币", "1.00", "人民币", "1.00", "0.00", "消费", "虚拟商户"},
		"卡号非法":   {"2099-01-01", "****", "借", "人民币", "1.00", "人民币", "1.00", "0.00", "消费", "虚拟商户"},
	}
	for name, chunk := range rejects {
		if _, err := parseChunk(ctx, 1, 1, chunk); err == nil {
			t.Errorf("%s应报错", name)
		}
	}
}

func TestParseCurrency(t *testing.T) {
	accepts := map[string]string{
		"人民币": "CNY",
		"美元":  "USD",
		"港币":  "HKD",
		"港元":  "HKD",
		"CNY": "CNY",
		"usd": "USD",
	}
	for value, want := range accepts {
		got, err := parseCurrency(value)
		if err != nil || got != want {
			t.Errorf("parseCurrency(%s) = (%s, %+v), want (%s, nil)", value, got, err, want)
		}
	}
	if _, err := parseCurrency("虚构币种"); err == nil {
		t.Errorf("未知币种应报错")
	}
}

func TestParseDate(t *testing.T) {
	ctx := util.GenCtx()

	accepts := map[string]string{
		"2099-01-0420:23:25":  "2099-01-04 20:23:25",
		"2099-01-04 20:23:25": "2099-01-04 20:23:25",
		"2099-01-04":          "2099-01-04 00:00:00",
	}
	for value, want := range accepts {
		object, err := parseDate(ctx, value)
		if err != nil {
			t.Errorf("parseDate(%s)异常: %+v", value, err)
			continue
		}
		got := object.Format(util.DateLayout_2006_01_02_15_04_05)
		if got != want {
			t.Errorf("parseDate(%s) = %s, want %s", value, got, want)
		}
	}
	if _, err := parseDate(ctx, "某年某月某日"); err == nil {
		t.Errorf("非法日期应报错")
	}
}
