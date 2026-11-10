package base_csv

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cellargalaxy/jotcash/service/expense/parser"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

// 列名与 model.Csv* 逐字一致（单测对账）。识别器跑在 wasm 里引用不了 model，只能在这里再写一份
const (
	colBankName           = "银行名称"
	colCardLast4          = "卡号后四位"
	colExpenseDate        = "支出日期"
	colExpenseCurrency    = "支出币种"
	colExpenseAmount      = "支出金额"
	colCounterparty       = "交易对手方"
	colRemark             = "交易备注"
	colExchangeRate       = "折算汇率"
	colAccountingCurrency = "记账币种"
	colExpenseType        = "支出类型"
	colAmortizationMonths = "摊销月数"

	dateLayout = "2006-01-02"
)

var columns = []string{
	colBankName,
	colCardLast4,
	colExpenseDate,
	colExpenseCurrency,
	colExpenseAmount,
	colCounterparty,
	colRemark,
	colExchangeRate,
	colAccountingCurrency,
	colExpenseType,
	colAmortizationMonths,
}

func init() {
	parser.Register("base_csv", new(Parser))
}

type Parser struct {
}

func (this *Parser) checkColumns(lines [][]string) bool {
	if len(lines) == 0 {
		return false
	}
	header := make([]string, 0, len(lines[0]))
	for i := range lines[0] {
		header = append(header, strings.TrimSpace(strings.TrimPrefix(lines[0][i], "\ufeff")))
	}
	return slices.Equal(header, columns)
}

// 与 go_common/util.CsvData2Strs 同一口径：标准库 encoding/csv、不限每行列数，裸引号这类格式错误直接报错
func readCsv(ctx context.Context, data []byte) ([][]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	lines, err := reader.ReadAll()
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析CSV异常")
		return nil, errors.Errorf("解析CSV异常: %+v", err)
	}
	return lines, nil
}

func (this *Parser) Support(ctx context.Context, data []byte) bool {
	lines, err := readCsv(ctx, data)
	if err != nil {
		return false
	}
	return this.checkColumns(lines)
}

func (this *Parser) Parse(ctx context.Context, data []byte) ([]*parser.Expense, error) {
	lines, err := readCsv(ctx, data)
	if err != nil {
		return nil, err
	}
	if !this.checkColumns(lines) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析CSV，表头与契约不一致")
		return nil, errors.Errorf("解析CSV，表头与契约不一致: %s", strings.Join(columns, ","))
	}

	objects := make([]*parser.Expense, 0, len(lines)-1)
	for i := 1; i < len(lines); i++ {
		object, err := parseExpense(ctx, lines[i])
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"i": i, "line": fmt.Sprintf("%q", lines[i])}).Warn("解析CSV，行非法")
			return nil, errors.Errorf("解析CSV，第%d行，%s", i+1, err)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func parseExpense(ctx context.Context, line []string) (*parser.Expense, error) {
	expenseDate, err := parser.ParseTime(dateLayout, csvValue(line, colExpenseDate))
	if err != nil {
		return nil, errors.Errorf("支出日期非法: %+v", err)
	}
	//金额允许0与负数，退款与冲正都要能录进来
	expenseAmount, err := decimal.NewFromString(csvValue(line, colExpenseAmount))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析CSV，支出金额非法")
		return nil, errors.Errorf("支出金额非法: %+v", err)
	}
	//汇率列留空是允许的，留个0让上层按记账币种去查
	exchangeRate := decimal.Zero
	accountingCurrency := csvValue(line, colAccountingCurrency)
	value := csvValue(line, colExchangeRate)
	if value != "" {
		if accountingCurrency == "" {
			logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析CSV，填了折算汇率却没填记账币种")
			return nil, errors.Errorf("填了折算汇率就必须填记账币种")
		}
		exchangeRate, err = decimal.NewFromString(value)
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析CSV，折算汇率非法")
			return nil, errors.Errorf("折算汇率非法: %+v", err)
		}
		if !exchangeRate.IsPositive() {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"rate": exchangeRate}).Warn("解析CSV，折算汇率非正")
			return nil, errors.Errorf("折算汇率非正: %s", exchangeRate)
		}
	}
	//摊销月数留空由上层取默认值，填了就得是不小于1的整数
	amortizationMonths := 0
	value = csvValue(line, colAmortizationMonths)
	if value != "" {
		amortizationMonths, err = strconv.Atoi(value)
		if err != nil || amortizationMonths < 1 {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"months": value}).Warn("解析CSV，摊销月数非法")
			return nil, errors.Errorf("摊销月数非法: %s", value)
		}
	}

	object := parser.Expense{
		BankName:           csvValue(line, colBankName),
		CardLast4:          csvValue(line, colCardLast4),
		ExpenseDate:        expenseDate,
		ExpenseCurrency:    csvValue(line, colExpenseCurrency),
		ExpenseAmount:      expenseAmount,
		Counterparty:       csvValue(line, colCounterparty),
		Remark:             csvValue(line, colRemark),
		ExchangeRate:       exchangeRate,
		AccountingCurrency: accountingCurrency,
		ExpenseType:        csvValue(line, colExpenseType),
		AmortizationMonths: amortizationMonths,
	}
	return &object, nil
}

// 表头已经与契约对齐，列名在契约里的下标就是这一行的下标
func csvValue(line []string, name string) string {
	i := slices.Index(columns, name)
	if i < 0 || i >= len(line) {
		return ""
	}
	return strings.TrimSpace(line[i])
}
