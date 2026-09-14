package base_csv

import (
	"context"
	"slices"
	"strings"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/expense"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

var columns = []string{model.CsvBankName, model.CsvCardLast4, model.CsvExpenseDate, model.CsvExpenseCurrency, model.CsvExpenseAmount, model.CsvCounterparty, model.CsvRemark, model.CsvExchangeRate, model.CsvExpenseType}

var requiredColumns = []string{model.CsvExpenseDate, model.CsvExpenseCurrency, model.CsvExpenseAmount}

func init() {
	expense.Register(new(Parser))
}

type Parser struct {
}

// 认表头里有没有本契约的列名。只要沾上一个就认领，缺必填列、写错列名这些错要留在Parse里报出来，
// 在这里挡掉的话用户只会收到一句「格式无法识别」
func (this *Parser) Support(ctx context.Context, data []byte) bool {
	lines, err := util.CsvData2Strs(ctx, data)
	if err != nil || len(lines) == 0 {
		return false
	}
	for i := range lines[0] {
		if slices.Contains(columns, strings.TrimSpace(lines[0][i])) {
			return true
		}
	}
	return false
}

func (this *Parser) Parse(ctx context.Context, data []byte) ([]*model.Expense, error) {
	lines, err := util.CsvData2Strs(ctx, data)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析CSV，为空")
		return nil, errors.Errorf("解析CSV，为空")
	}

	//未知列与重复列都报错，免得列错位了还照样入库
	index := make(map[string]int, len(lines[0]))
	for i := range lines[0] {
		name := strings.TrimSpace(lines[0][i])
		if !slices.Contains(columns, name) {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"column": name}).Warn("解析CSV，未知列")
			return nil, errors.Errorf("解析CSV，未知列: %s", name)
		}
		if _, ok := index[name]; ok {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"column": name}).Warn("解析CSV，重复列")
			return nil, errors.Errorf("解析CSV，重复列: %s", name)
		}
		index[name] = i
	}
	for _, name := range requiredColumns {
		if _, ok := index[name]; !ok {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"column": name}).Warn("解析CSV，缺必填列")
			return nil, errors.Errorf("解析CSV，缺必填列: %s", name)
		}
	}

	objects := make([]*model.Expense, 0, len(lines)-1)
	for i := 1; i < len(lines); i++ {
		object, err := parseExpense(ctx, index, lines[i])
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"line": i + 1}).Warn("解析CSV，行非法")
			return nil, errors.Errorf("解析CSV，第%d行，%s", i+1, err)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func parseExpense(ctx context.Context, index map[string]int, line []string) (*model.Expense, error) {
	expenseDate, err := util.ParseStr2Time(ctx, util.DateLayout_2006_01_02, csvValue(index, line, model.CsvExpenseDate), nil)
	if err != nil {
		return nil, errors.Errorf("支出日期非法: %+v", err)
	}
	//金额允许0与负数，退款与冲正都要能录进来
	expenseAmount, err := decimal.NewFromString(csvValue(index, line, model.CsvExpenseAmount))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析CSV，支出金额非法")
		return nil, errors.Errorf("支出金额非法: %+v", err)
	}
	//汇率列留空是允许的，留个0让上层按支出日期去取
	exchangeRate := decimal.Zero
	value := csvValue(index, line, model.CsvExchangeRate)
	if value != "" {
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

	object := model.Expense{
		BankName:        csvValue(index, line, model.CsvBankName),
		CardLast4:       csvValue(index, line, model.CsvCardLast4),
		ExpenseDate:     expenseDate,
		ExpenseCurrency: csvValue(index, line, model.CsvExpenseCurrency),
		ExpenseAmount:   expenseAmount,
		Counterparty:    csvValue(index, line, model.CsvCounterparty),
		Remark:          csvValue(index, line, model.CsvRemark),
		ExchangeRate:    exchangeRate,
		ExpenseType:     csvValue(index, line, model.CsvExpenseType),
	}
	return &object, nil
}

func csvValue(index map[string]int, line []string, name string) string {
	i, ok := index[name]
	if !ok || i >= len(line) {
		return ""
	}
	return strings.TrimSpace(line[i])
}
