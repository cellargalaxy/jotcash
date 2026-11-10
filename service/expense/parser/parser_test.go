package parser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

const testSupportData = "只有测试用的识别器认领这份内容"

type testParser struct {
	objects []*Expense
}

func (this *testParser) Support(ctx context.Context, data []byte) bool {
	return string(data) == testSupportData
}
func (this *testParser) Parse(ctx context.Context, data []byte) ([]*Expense, error) {
	return this.objects, nil
}

func TestParse(t *testing.T) {
	ctx := context.Background()
	object := &Expense{ExpenseDate: time.Date(2026, 1, 2, 0, 0, 0, 0, Loc), ExpenseCurrency: "CNY", ExpenseAmount: decimal.RequireFromString("1")}
	Register("test_one", &testParser{objects: []*Expense{object}})
	Register("test_empty", &testParser{})

	objects, err := Parse(ctx, "test_one", []byte(testSupportData))
	if err != nil || len(objects) != 1 {
		t.Fatalf("内容对得上应解析出1笔: %d %+v", len(objects), err)
	}
	//用户手选识别器，选错了要能从报错里看出来：名字不存在、内容对不上、解析不出明细，三种都得报错
	if _, err := Parse(ctx, "no_such_parser", []byte(testSupportData)); err == nil || !strings.Contains(err.Error(), "识别器不存在") {
		t.Errorf("识别器不存在应报错: %+v", err)
	}
	if _, err := Parse(ctx, "test_one", []byte("别的内容")); err == nil || !strings.Contains(err.Error(), "文件内容与所选识别器不符") {
		t.Errorf("内容与所选识别器不符应报错: %+v", err)
	}
	if _, err := Parse(ctx, "test_empty", []byte(testSupportData)); err == nil || !strings.Contains(err.Error(), "没有解析到明细") {
		t.Errorf("解析不出明细应报错，不能入库一个空批次: %+v", err)
	}
}

// 支出日期按墙钟字符串传：浏览器在哪个时区，序列化出来都是文件里那个日期时间
func TestExpenseJson(t *testing.T) {
	object := Expense{
		BankName:        "招商银行",
		ExpenseDate:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("UTC-5", -5*3600)),
		ExpenseCurrency: "USD",
		ExpenseAmount:   decimal.RequireFromString("-20.25"),
		ExchangeRate:    decimal.RequireFromString("7.1234"),
	}
	data, err := json.Marshal([]*Expense{&object})
	if err != nil {
		t.Fatalf("序列化异常: %+v", err)
	}
	text := string(data)
	for _, want := range []string{`"expense_date":"2026-01-02 03:04:05"`, `"expense_amount":"-20.25"`, `"exchange_rate":"7.1234"`, `"bank_name":"招商银行"`, `"amortization_months":0`} {
		if !strings.Contains(text, want) {
			t.Errorf("序列化结果缺 %s: %s", want, text)
		}
	}

	var objects []*Expense
	if err := json.Unmarshal(data, &objects); err != nil {
		t.Fatalf("反序列化异常: %+v", err)
	}
	got := objects[0]
	if got.ExpenseDate.Format(DateLayout) != "2026-01-02 03:04:05" || got.ExpenseDate.Location() != Loc {
		t.Errorf("支出日期应按东八区还原墙钟: %s %s", got.ExpenseDate, got.ExpenseDate.Location())
	}
	if !got.ExpenseAmount.Equal(object.ExpenseAmount) || !got.ExchangeRate.Equal(object.ExchangeRate) || got.BankName != object.BankName {
		t.Errorf("往返之后字段不符: %+v", got)
	}

	//日期只认一种写法，带时区的、只有日期的都不认，免得两边各按各的理解
	for _, bad := range []string{`[{"expense_date":"2026-01-02T03:04:05+08:00"}]`, `[{"expense_date":"2026-01-02"}]`, `[{"expense_date":""}]`, `[{}]`} {
		if err := json.Unmarshal([]byte(bad), &objects); err == nil {
			t.Errorf("支出日期非法应报错: %s", bad)
		}
	}
}
