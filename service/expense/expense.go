package expense

import (
	"context"
	"time"

	"github.com/bojanz/currency"
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/exchange_rate"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

type Parser interface {
	Support(ctx context.Context, data []byte) bool
	Parse(ctx context.Context, data []byte) ([]*model.Expense, error)
}

var parsers []Parser

func Register(parser Parser) {
	parsers = append(parsers, parser)
}

func Parse(ctx context.Context, data []byte, accountingCurrency string) ([]*model.Expense, error) {
	err := checkCurrency(ctx, accountingCurrency)
	if err != nil {
		return nil, err
	}
	var parser Parser
	for i := range parsers {
		if parsers[i].Support(ctx, data) {
			parser = parsers[i]
			break
		}
	}
	if parser == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"len": len(data)}).Warn("解析明细，没有解析器认领")
		return nil, errors.Errorf("解析明细，文件格式无法识别")
	}

	objects, err := parser.Parse(ctx, data)
	if err != nil {
		return nil, err
	}
	for i := range objects {
		err = fillExpense(ctx, objects[i], accountingCurrency)
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"index": i + 1}).Warn("解析明细，明细非法")
			return nil, errors.Errorf("解析明细，第%d笔，%s", i+1, err)
		}
	}
	return objects, nil
}

func fillExpense(ctx context.Context, object *model.Expense, accountingCurrency string) error {
	//文件里逐笔给的记账币种优先，没给才用请求带的
	if object.AccountingCurrency == "" {
		object.AccountingCurrency = accountingCurrency
	}
	err := checkCurrency(ctx, object.AccountingCurrency)
	if err != nil {
		return err
	}
	err = checkCurrency(ctx, object.ExpenseCurrency)
	if err != nil {
		return err
	}
	rate, err := getExchangeRate(ctx, object)
	if err != nil {
		return err
	}
	//摊分月数文件里没给就取默认值
	if object.AmortizationMonths < 1 {
		object.AmortizationMonths = 1
	}
	object.Id = util.GenId()
	object.ExchangeRate = rate
	object.AccountingAmount = object.ExpenseAmount.Mul(rate)
	object.AmortizationStartMonth = time.Date(object.ExpenseDate.Year(), object.ExpenseDate.Month(), 1, 0, 0, 0, 0, object.ExpenseDate.Location())
	object.AmortizationEndMonth = object.AmortizationStartMonth.AddDate(0, object.AmortizationMonths-1, 0)
	object.Version = 1
	return nil
}

func getExchangeRate(ctx context.Context, object *model.Expense) (decimal.Decimal, error) {
	//同币种的汇率恒为1，文件里填了别的值也不认
	if object.ExpenseCurrency == object.AccountingCurrency {
		return decimal.NewFromInt(1), nil
	}

	//解析器留下的0表示文件里没给，按支出日期兜底取
	rate := object.ExchangeRate
	if rate.IsZero() {
		var err error
		rate, err = exchange_rate.GetExchangeRate(ctx, object.ExpenseDate, object.ExpenseCurrency, object.AccountingCurrency)
		if err != nil {
			return decimal.Zero, err
		}
	}
	if !rate.IsPositive() {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"rate": rate}).Warn("解析明细，折算汇率非正")
		return decimal.Zero, errors.Errorf("折算汇率非正: %s", rate)
	}
	return rate, nil
}

func checkCurrency(ctx context.Context, code string) error {
	if code != "" && currency.IsValid(code) {
		return nil
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"currency": code}).Warn("币种，不在枚举内")
	return errors.Errorf("币种，不在枚举内: %s", code)
}
