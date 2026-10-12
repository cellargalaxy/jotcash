package unionpay_screenshot

import (
	"os"
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
		"空内容":     []byte(""),
		"空数组":     []byte("[]"),
		"非JSON文本": []byte("交易记录\n银联交易\n全部 支出 收入\n"),
		"CSV":     []byte("银行名称,卡号后四位,支出日期,支出币种,支出金额\n"),
		"图片原文件":   {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46},
		"别的页面":    []byte(`[{"text":"微信支付明细","x":400,"y":100},{"text":"全部","x":100,"y":300}]`),
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

// 合成长截图全是虚构数据、随仓库提交。synthetic.json 是前端 OCR（static/js/ocr.js）识别 synthetic.png 的输出，
// 前端测试盯「图片 → 文字块」与它一致，这里盯「文字块 → 明细」，两段拼起来才是整条链路；
// 版面改了用 testdata/gen_synthetic.py 重新生成图片，再用前端 OCR 重新生成 synthetic.json
func TestParseSyntheticScreenshot(t *testing.T) {
	ctx := util.GenCtx()
	data, err := os.ReadFile("testdata/synthetic.json")
	if err != nil {
		t.Fatalf("读取合成截图文字块异常: %+v", err)
	}

	parser := new(Parser)
	if !parser.Support(ctx, data) {
		t.Fatalf("合成截图应当被Support认领")
	}
	expenses, err := parser.Parse(ctx, data)
	if err != nil {
		t.Fatalf("合成截图解析异常: %+v", err)
	}

	expected := []struct {
		bank, card, date, amount, counterparty, remark string
	}{
		{"招商银行", "1111", "2026-10-09 21:05", "12.50", "(特约)虚构便利店", "消费"},
		{"汇丰银行", "2222", "2026-10-09 18:40", "28.00", "虚构咖啡馆", "消费"},
		{"工商银行", "3333", "2026-10-08 12:10", "36.80", "(美团)虚构面馆", "消费"},
		{"建设银行", "4444", "2026-10-08 10:30", "-9.90", "虚构书店", "退款"},
		{"建设银行", "4444", "2026-10-05 20:15", "158.20", "虚构超市", "消费"},
		{"工商银行", "3333", "2026-10-05 07:45", "200.00", "(特约)虚构加油站", "消费"},
		{"招商银行", "1111", "2026-10-01 19:30", "75.00", "虚构电影院", "消费"},
		{"汇丰银行", "2222", "2026-10-01 17:20", "23.60", "虚构水果店", "消费"},
		{"工商银行", "3333", "2026-09-30 16:05", "45.30", "虚构药店", "消费"},
		{"建设银行", "4444", "2026-09-30 12:00", "19.90", "(美团)虚构快餐店", "消费"},
		{"招商银行", "1111", "2026-09-28 11:11", "30.00", "虚构洗衣店", "消费"},
		{"汇丰银行", "2222", "2026-09-28 09:30", "88.00", "虚构花店", "消费"},
		{"工商银行", "3333", "2026-09-28 08:20", "10.00", "虚构停车场", "消费"},
	}
	if len(expenses) != len(expected) {
		t.Fatalf("合成截图应识别出%d笔有效交易: got %d", len(expected), len(expenses))
	}
	for i, want := range expected {
		got := expenses[i]
		date, _ := time.ParseInLocation("2006-01-02 15:04", want.date, time.Local)
		if got.BankName != want.bank || got.CardLast4 != want.card || !got.ExpenseDate.Equal(date) ||
			!got.ExpenseAmount.Equal(decimal.RequireFromString(want.amount)) || got.Counterparty != want.counterparty || got.Remark != want.remark {
			t.Errorf("第%d笔不符: got %s|%s|%s|%s|%s|%s, want %+v", i+1, got.BankName, got.CardLast4,
				got.ExpenseDate.Format("2006-01-02 15:04"), got.ExpenseAmount, got.Counterparty, got.Remark, want)
		}
	}
}
