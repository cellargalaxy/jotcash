package unionpay_screenshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/shopspring/decimal"
)

func TestMain(m *testing.M) {
	code := m.Run()
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

func TestSupportInvalid(t *testing.T) {
	ctx := util.GenCtx()
	parser := new(Parser)

	rejects := map[string][]byte{
		"空文件":   []byte(""),
		"非图片文本": []byte("交易记录\n银联交易\n全部 支出 收入\n"),
		"CSV":   []byte("银行名称,卡号后四位,支出日期,支出币种,支出金额\n"),
		"PDF":   []byte("%PDF-1.7 假的PDF内容"),
		"坏图片头":  []byte{0xFF, 0xD8, 0xFF, 0x00, 0x00, 0x00},
	}
	for name, data := range rejects {
		if parser.Support(ctx, data) {
			t.Errorf("%s 不应该被认领", name)
		}
	}
}

func TestNormalizeBank(t *testing.T) {
	cases := map[string]string{
		"滙豐":   "汇丰银行",
		"汇丰":   "汇丰银行",
		"招商":   "招商银行",
		"招商银行": "招商银行",
		"工行":   "工商银行",
		"工商":   "工商银行",
		"中国银行": "中国银行",
		"中行":   "中国银行",
		"建行":   "建设银行",
		"交行":   "交通银行",
		"农行":   "农业银行",
		"平安":   "平安银行",
		"测试银行": "测试银行",
		"某某":   "某某银行",
	}
	for in, expected := range cases {
		out := normalizeBank(in)
		if out != expected {
			t.Errorf("normalizeBank(%s) = %s, want %s", in, out, expected)
		}
	}
}

func TestCheckContractSynthetic(t *testing.T) {
	goodBlocks := []OcrBlock{
		{Text: "交易记录", X: 400, Y: 100},
		{Text: "银联交易", X: 300, Y: 200},
		{Text: "全部", X: 100, Y: 300},
		{Text: "支出", X: 200, Y: 300},
	}
	if !checkContract(goodBlocks) {
		t.Errorf("合法块应满足契约")
	}

	badBlocks := []OcrBlock{
		{Text: "微信支付明细", X: 400, Y: 100},
		{Text: "全部", X: 100, Y: 300},
	}
	if checkContract(badBlocks) {
		t.Errorf("缺少银联与交易记录特征不应满足契约")
	}
}

func TestParseBlocksSynthetic(t *testing.T) {
	ctx := util.GenCtx()

	// 100% 虚构的测试数据，覆盖正常消费、商户折行、退款、还款过滤与冻结过滤
	syntheticBlocks := []OcrBlock{
		{Text: "交易记录", X: 430, Y: 150},
		{Text: "银联交易", X: 360, Y: 300},
		{Text: "2026年10月→", X: 50, Y: 310},
		{Text: "全部", X: 80, Y: 420},
		{Text: "支出", X: 260, Y: 420},
		{Text: "收入", X: 440, Y: 420},

		// 10月分组
		{Text: "10月", X: 90, Y: 590},
		{Text: "支出 ￥100.00", X: 90, Y: 690},

		// 10.10 周五
		{Text: "10.10 周五", X: 100, Y: 860},

		// 交易1：普通消费（带特约前缀与金额）
		{Text: "(特约)", X: 220, Y: 950},
		{Text: "虚拟测试商户A", X: 380, Y: 950},
		{Text: "-￥30.50", X: 790, Y: 950},
		{Text: "招商[1234]·21:00", X: 200, Y: 1020},

		// 交易2：退款（+￥5.00，带退款标签）
		{Text: "虚拟测试退款商户", X: 220, Y: 1150},
		{Text: "+￥5.00", X: 790, Y: 1150},
		{Text: "退款", X: 910, Y: 1220},
		{Text: "工行[5678]·14:30", X: 200, Y: 1220},

		// 交易3：信用卡还款（应安全丢弃）
		{Text: "信用卡还款", X: 220, Y: 1350},
		{Text: "-￥1,000.00", X: 790, Y: 1350},
		{Text: "招商[1234]·10:00", X: 200, Y: 1420},

		// 交易4：预授权冻结（应安全丢弃）
		{Text: "押金商户", X: 220, Y: 1550},
		{Text: "￥1.00", X: 790, Y: 1550},
		{Text: "冻结", X: 910, Y: 1620},
		{Text: "招商[1234]·09:00", X: 200, Y: 1620},

		// 交易5：预授权解冻（应安全丢弃）
		{Text: "押金商户", X: 220, Y: 1750},
		{Text: "￥1.00", X: 790, Y: 1750},
		{Text: "解冻", X: 910, Y: 1820},
		{Text: "招商[1234]·09:01", X: 200, Y: 1820},
	}

	expenses, err := parseBlocks(ctx, syntheticBlocks)
	if err != nil {
		t.Fatalf("合成块解析异常: %+v", err)
	}

	// 交易3还款、交易4冻结、交易5解冻均被过滤，应剩余2笔
	if len(expenses) != 2 {
		t.Fatalf("预期识别2笔明细，实际: %d", len(expenses))
	}

	first := expenses[0]
	if first.BankName != "招商银行" || first.CardLast4 != "1234" {
		t.Errorf("第1笔银行或卡号不符: bank=%s card=%s", first.BankName, first.CardLast4)
	}
	if !first.ExpenseAmount.Equal(decimal.RequireFromString("30.50")) {
		t.Errorf("第1笔金额不符: %s", first.ExpenseAmount)
	}
	if first.Remark != "消费" {
		t.Errorf("第1笔备注应为消费: %s", first.Remark)
	}
	if first.Counterparty != "(特约)虚拟测试商户A" {
		t.Errorf("第1笔商户名不符: %s", first.Counterparty)
	}
	expectedDate1 := time.Date(2026, 10, 10, 21, 0, 0, 0, time.Local)
	if !first.ExpenseDate.Equal(expectedDate1) {
		t.Errorf("第1笔交易日期不符: got %v, want %v", first.ExpenseDate, expectedDate1)
	}

	second := expenses[1]
	if second.BankName != "工商银行" || second.CardLast4 != "5678" {
		t.Errorf("第2笔银行或卡号不符: bank=%s card=%s", second.BankName, second.CardLast4)
	}
	if !second.ExpenseAmount.Equal(decimal.RequireFromString("-5.00")) {
		t.Errorf("第2笔退款金额应为负数-5.00: %s", second.ExpenseAmount)
	}
	if second.Remark != "退款" {
		t.Errorf("第2笔备注应为退款: %s", second.Remark)
	}
}

// 样例截图是真实流水、随时会被删，所以只按目录扫，不写死文件名，也不断言里面的任何私密内容
func TestParseSampleScreenshot(t *testing.T) {
	ctx := util.GenCtx()

	paths, err := filepath.Glob("../../../resource/Screenshot_*.jpg")
	if err != nil || len(paths) == 0 {
		t.Skip("没有样例截图，跳过真实截图解析校验")
	}

	parser := new(Parser)
	for i := range paths {
		data, err := os.ReadFile(paths[i])
		if err != nil {
			t.Fatalf("读取第%d个样例截图异常: %+v", i+1, err)
		}
		if !parser.Support(ctx, data) {
			t.Fatalf("第%d个样例截图应当被Support认领", i+1)
		}
		expenses, err := parser.Parse(ctx, data)
		if err != nil {
			t.Fatalf("第%d个样例截图解析异常: %+v", i+1, err)
		}
		if len(expenses) != 30 {
			t.Fatalf("第%d个样例截图应识别出30笔有效交易: got %d", i+1, len(expenses))
		}
		for j, exp := range expenses {
			if exp.BankName != "汇丰银行" {
				t.Errorf("第%d笔银行名称不符: %s", j+1, exp.BankName)
			}
			if len(exp.CardLast4) != 4 {
				t.Errorf("第%d笔卡号后四位长度不为4: %s", j+1, exp.CardLast4)
			}
			if exp.ExpenseDate.IsZero() {
				t.Errorf("第%d笔交易时间为空", j+1)
			}
			if exp.ExpenseCurrency != "CNY" {
				t.Errorf("第%d笔币种不为CNY: %s", j+1, exp.ExpenseCurrency)
			}
			if exp.ExpenseAmount.IsZero() {
				t.Errorf("第%d笔交易金额为0", j+1)
			}
			if exp.Remark != "消费" && exp.Remark != "退款" {
				t.Errorf("第%d笔备注非法: %s", j+1, exp.Remark)
			}
		}
	}
}
