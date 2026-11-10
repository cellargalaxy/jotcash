package parsers_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/expense"
	"github.com/cellargalaxy/jotcash/service/expense/parser"
	_ "github.com/cellargalaxy/jotcash/service/expense/parsers"
)

func TestMain(m *testing.M) {
	code := m.Run()
	//config包的init会在测试二进制的工作目录写配置文件
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

// 生效的识别器就这4个，汇丰 pulse 已删除；页面下拉与这份清单的对账在前端契约测试里
func TestNames(t *testing.T) {
	want := []string{"base_csv", "cmb_debit", "icbc_credit", "unionpay_screenshot"}
	if got := parser.Names(); !slices.Equal(got, want) {
		t.Errorf("生效识别器清单不符: %v", got)
	}
}

// 浏览器解析 → JSON → 服务端填充，整条链路在 Go 里走一遍：识别器的输出必须过得了服务端的校验
func fill(t *testing.T, name string, objects []*parser.Expense) []*model.Expense {
	t.Helper()
	data, err := json.Marshal(objects)
	if err != nil {
		t.Fatalf("%s 序列化异常: %+v", name, err)
	}
	var received []*parser.Expense
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("%s 反序列化异常: %+v", name, err)
	}
	expenses, err := expense.Fill(util.GenCtx(), received, "CNY")
	if err != nil {
		t.Fatalf("%s 服务端填充异常: %+v", name, err)
	}
	if len(expenses) != len(objects) {
		t.Fatalf("%s 填充前后笔数不符: %d %d", name, len(objects), len(expenses))
	}
	for i := range expenses {
		if !expenses[i].ExpenseDate.Equal(objects[i].ExpenseDate) || !expenses[i].ExpenseAmount.Equal(objects[i].ExpenseAmount) {
			t.Errorf("%s 第%d笔往返之后日期或金额变了: %s %s", name, i+1, expenses[i].ExpenseDate, expenses[i].ExpenseAmount)
		}
		if expenses[i].Id == 0 || expenses[i].Version != 1 || expenses[i].AmortizationStartMonth.IsZero() || !expenses[i].ExchangeRate.IsPositive() {
			t.Errorf("%s 第%d笔派生字段不符: %+v", name, i+1, expenses[i])
		}
	}
	return expenses
}

// 样例账单是真实流水、随时会被删，所以只按目录扫，不写死文件名，也不断言里面的任何内容
func TestParseSamplePdf(t *testing.T) {
	ctx := util.GenCtx()
	paths, err := filepath.Glob("../../../resource/*.pdf")
	if err != nil || len(paths) == 0 {
		t.Skip("没有样例PDF，跳过真实账单校验")
	}
	for i := range paths {
		data, err := os.ReadFile(paths[i])
		if err != nil {
			t.Fatalf("读取第%d个样例PDF异常: %+v", i+1, err)
		}
		//样例不写死是哪家的账单，逐个 PDF 识别器去试；每个样例都得恰好被一个识别器认领，认领两个就是口径不够严
		var claimed []string
		for _, name := range []string{"cmb_debit", "icbc_credit", "base_csv", "unionpay_screenshot"} {
			objects, err := parser.Parse(ctx, name, data)
			if err != nil {
				if strings.Contains(err.Error(), "文件内容与所选识别器不符") {
					continue
				}
				t.Fatalf("第%d个样例用%s解析异常: %+v", i+1, name, err)
			}
			claimed = append(claimed, name)
			for j, object := range objects {
				if object.BankName == "" || len(object.CardLast4) != 4 {
					t.Errorf("第%d个样例第%d笔银行名称或卡号后四位为空", i+1, j+1)
				}
			}
			fill(t, name, objects)
		}
		if len(claimed) != 1 {
			t.Errorf("第%d个样例应恰好被一个识别器认领: %v", i+1, claimed)
		}
	}
}

func TestParseSyntheticScreenshot(t *testing.T) {
	data, err := os.ReadFile("../unionpay_screenshot/testdata/synthetic.json")
	if err != nil {
		t.Fatalf("读取合成截图文字块异常: %+v", err)
	}
	objects, err := parser.Parse(util.GenCtx(), "unionpay_screenshot", data)
	if err != nil {
		t.Fatalf("合成截图解析异常: %+v", err)
	}
	if len(objects) != 13 {
		t.Fatalf("合成截图应解析出13笔明细: got %d", len(objects))
	}
	fill(t, "unionpay_screenshot", objects)
}

// 选错识别器是用户最可能犯的错：每个识别器都只认自己的格式，拿别家的样例去喂一律报「不符」
func TestParseWrongParser(t *testing.T) {
	ctx := util.GenCtx()
	screenshot, err := os.ReadFile("../unionpay_screenshot/testdata/synthetic.json")
	if err != nil {
		t.Fatalf("读取合成截图文字块异常: %+v", err)
	}
	csv := []byte("银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,记账币种,支出类型,摊销月数\n" +
		"招商银行,6789,2026-01-02,CNY,100.50,亚马逊,买书,,,购物,\n")
	samples := map[string][]byte{"unionpay_screenshot": screenshot, "base_csv": csv}
	for owner, data := range samples {
		for _, name := range parser.Names() {
			if name == owner {
				continue
			}
			if _, err := parser.Parse(ctx, name, data); err == nil || !strings.Contains(err.Error(), "文件内容与所选识别器不符") {
				t.Errorf("%s 的内容选成 %s 应报不符: %+v", owner, name, err)
			}
		}
	}
}
