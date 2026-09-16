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

var columns = []string{
	model.CsvBankName,           //银行名称
	model.CsvCardLast4,          //卡号后四位
	model.CsvExpenseDate,        //支出日期
	model.CsvExpenseCurrency,    //支出币种
	model.CsvExpenseAmount,      //支出金额
	model.CsvCounterparty,       //交易对手方
	model.CsvRemark,             //交易备注
	model.CsvExchangeRate,       //折算汇率
	model.CsvAccountingCurrency, //记账币种
	model.CsvExpenseType,        //支出类型
	model.CsvAmortizationMonths, //摊销月数
}

func init() {
	expense.Register(new(Parser))
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

func (this *Parser) Support(ctx context.Context, data []byte) bool {
	lines, err := util.CsvData2Strs(ctx, data)
	if err != nil {
		return false
	}
	return this.checkColumns(lines)
}

func (this *Parser) Parse(ctx context.Context, data []byte) ([]*model.Expense, error) {
	lines, err := util.CsvData2Strs(ctx, data)
	if err != nil {
		return nil, err
	}
	if !this.checkColumns(lines) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析CSV，表头与契约不一致")
		return nil, errors.Errorf("解析CSV，表头与契约不一致: %s", strings.Join(columns, ","))
	}

	objects := make([]*model.Expense, 0, len(lines)-1)
	for i := 1; i < len(lines); i++ {
		object, err := parseExpense(ctx, lines[i])
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"i": i, "line": util.JsonStruct2Str(lines[i])}).Warn("解析CSV，行非法")
			return nil, errors.Errorf("解析CSV，第%d行，%s", i+1, err)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func parseExpense(ctx context.Context, line []string) (*model.Expense, error) {
	expenseDate, err := util.ParseStr2Time(ctx, util.DateLayout_2006_01_02, csvValue(line, model.CsvExpenseDate), nil)
	if err != nil {
		return nil, errors.Errorf("支出日期非法: %+v", err)
	}
	//金额允许0与负数，退款与冲正都要能录进来
	expenseAmount, err := decimal.NewFromString(csvValue(line, model.CsvExpenseAmount))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析CSV，支出金额非法")
		return nil, errors.Errorf("支出金额非法: %+v", err)
	}
	//汇率列留空是允许的，留个0让上层按记账币种去查
	exchangeRate := decimal.Zero
	accountingCurrency := csvValue(line, model.CsvAccountingCurrency)
	value := csvValue(line, model.CsvExchangeRate)
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
	value = csvValue(line, model.CsvAmortizationMonths)
	if value != "" {
		amortizationMonths = util.Str2Int[int](value)
		if amortizationMonths < 1 {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"months": value}).Warn("解析CSV，摊销月数非法")
			return nil, errors.Errorf("摊销月数非法: %s", value)
		}
	}

	object := model.Expense{
		BankName:           csvValue(line, model.CsvBankName),
		CardLast4:          csvValue(line, model.CsvCardLast4),
		ExpenseDate:        expenseDate,
		ExpenseCurrency:    csvValue(line, model.CsvExpenseCurrency),
		ExpenseAmount:      expenseAmount,
		Counterparty:       csvValue(line, model.CsvCounterparty),
		Remark:             csvValue(line, model.CsvRemark),
		ExchangeRate:       exchangeRate,
		AccountingCurrency: accountingCurrency,
		ExpenseType:        csvValue(line, model.CsvExpenseType),
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
