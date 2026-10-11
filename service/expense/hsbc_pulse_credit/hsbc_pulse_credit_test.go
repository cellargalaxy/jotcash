package hsbc_pulse_credit

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bojanz/currency"
	"github.com/cellargalaxy/go_common/util"
)

func TestMain(m *testing.M) {
	code := m.Run()
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

// 样例账单是真实流水、随时会被删，所以只按目录扫，不写死文件名，也不断言里面的任何具体私密内容
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

func TestSupport(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	rejects := map[string][]byte{
		"空文件":  []byte(""),
		"非PDF": []byte("Statement of HSBC Pulse UnionPay Dual Currency Diamond Card\nPost date\nTrans date\n"),
		"CSV":  []byte("银行名称,卡号后四位,支出日期,支出币种,支出金额\n"),
		"坏PDF": []byte("%PDF-1.7 伪造的损坏PDF内容"),
	}
	for name, data := range rejects {
		if parser.Support(ctx, data) {
			t.Errorf("%s不该认领", name)
		}
	}
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
				t.Errorf("第%d笔卡号后四位不是4位数字: %s", i+1, object.CardLast4)
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
			if slices.ContainsFunc(repaymentWords, func(w string) bool {
				return object.Counterparty == w || object.Remark == w
			}) {
				t.Errorf("第%d笔还款没被丢掉: %+v", i+1, object)
			}
			//派生字段留给上层
			if object.Id != 0 || object.Version != 0 || !object.AccountingAmount.IsZero() || !object.AmortizationStartMonth.IsZero() {
				t.Errorf("第%d笔派生字段不该由解析器填: id=%d version=%d", i+1, object.Id, object.Version)
			}
		}
	})
}

// 全部用虚构数据测试单元分支，真实账单信息一律不进代码
func TestParsePageTransactionsSynthetic(t *testing.T) {
	ctx := util.GenCtx()

	// 构造虚构的第1页明细（人民币子账户）
	chunks := []textChunk{
		// 子账户表头与表头列
		{x: 64, y: 700, text: "Account number 9999888877771234 RMB Sub-account Statement balance CNY 100.00"},
		{x: 68, y: 664, text: "Post date"},
		{x: 100, y: 664, text: "Trans date"},
		{x: 135, y: 664, text: "Description of transaction"},
		{x: 500, y: 662, text: "Amount (CNY)"},

		// 笔1：常规消费
		{x: 68, y: 600, text: "02 OCT"},
		{x: 100, y: 600, text: "01 OCT"},
		{x: 135, y: 600, text: "VIRTUAL MERCHANT A CHN CN"},
		{x: 520, y: 600, text: "100.50"},

		// 笔2：商户退款（CR）
		{x: 68, y: 580, text: "03 OCT"},
		{x: 100, y: 580, text: "02 OCT"},
		{x: 135, y: 580, text: "VIRTUAL MERCHANT B CHN CN"},
		{x: 520, y: 580, text: "20.00"},
		{x: 560, y: 580, text: "CR"},

		// 笔3：自动还款（CR），应被丢弃
		{x: 68, y: 560, text: "04 OCT"},
		{x: 100, y: 560, text: "03 OCT"},
		{x: 135, y: 560, text: "AUTOPAY PAYMENT"},
		{x: 520, y: 560, text: "500.00"},
		{x: 560, y: 560, text: "CR"},

		// 笔4：商户名折行
		{x: 68, y: 540, text: "05 OCT"},
		{x: 100, y: 540, text: "04 OCT"},
		{x: 135, y: 540, text: "VIRTUAL SHOP"},
		{x: 520, y: 540, text: "88.00"},
		{x: 135, y: 532, text: "BRANCH STORE"}, // 换行折叠部分

		// 页脚标识
		{x: 68, y: 500, text: "Note: \"CR\" means Credit transaction / balance"},
	}

	expenses, err := parsePageTransactions(ctx, 1, chunks, 2099, time.October, "1234")
	if err != nil {
		t.Fatalf("解析虚构页面明细异常: %+v", err)
	}
	if len(expenses) != 3 {
		t.Fatalf("应解析出3笔（排除还款），实际解析出: %d笔", len(expenses))
	}

	// 笔1断言
	e1 := expenses[0]
	if e1.BankName != bankName || e1.CardLast4 != "1234" || e1.ExpenseCurrency != "CNY" ||
		e1.ExpenseAmount.String() != "100.5" || e1.Remark != summaryPurchase || e1.Counterparty != "VIRTUAL MERCHANT A CHN CN" {
		t.Errorf("笔1常规消费字段不符: %+v", e1)
	}
	if e1.ExpenseDate.Format("2006-01-02") != "2099-10-01" {
		t.Errorf("笔1支出日期不符: got=%s want=2099-10-01", e1.ExpenseDate.Format("2006-01-02"))
	}

	// 笔2断言（退款）
	e2 := expenses[1]
	if e2.ExpenseAmount.String() != "-20" || e2.Remark != summaryRefund || e2.Counterparty != "VIRTUAL MERCHANT B CHN CN" {
		t.Errorf("笔2退款字段不符: %+v", e2)
	}
	if e2.ExpenseDate.Format("2006-01-02") != "2099-10-02" {
		t.Errorf("笔2退款日期不符: got=%s want=2099-10-02", e2.ExpenseDate.Format("2006-01-02"))
	}

	// 笔3断言（换行拼接）
	e3 := expenses[2]
	if e3.ExpenseAmount.String() != "88" || e3.Counterparty != "VIRTUAL SHOP BRANCH STORE" {
		t.Errorf("笔3换行拼接字段不符: %+v", e3)
	}
}

func TestParseTxDateCrossYear(t *testing.T) {
	// 账单日为2099年1月，交易日在12月，应回算为2098年
	date, err := parseTxDate("28 DEC", 2099, time.January)
	if err != nil {
		t.Fatalf("跨年日期解析异常: %+v", err)
	}
	if date.Format("2006-01-02") != "2098-12-28" {
		t.Errorf("跨年年份计算不符: got=%s want=2098-12-28", date.Format("2006-01-02"))
	}

	// 同年日期
	date, err = parseTxDate("05 JAN", 2099, time.January)
	if err != nil {
		t.Fatalf("同年日期解析异常: %+v", err)
	}
	if date.Format("2006-01-02") != "2099-01-05" {
		t.Errorf("同年日期计算不符: got=%s want=2099-01-05", date.Format("2006-01-02"))
	}
}
