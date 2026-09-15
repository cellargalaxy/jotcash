package expense

import (
	"context"
	"time"

	"github.com/bojanz/currency"
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
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
	err := checkCurrency(ctx, model.CsvAccountingCurrency, accountingCurrency)
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
	if object.AccountingCurrency == "" {
		object.AccountingCurrency = accountingCurrency
	}
	object.Id = util.GenId()
	object.Version = 1
	return Derive(ctx, object)
}

func Derive(ctx context.Context, object *model.Expense) error {
	err := checkCurrency(ctx, model.CsvAccountingCurrency, object.AccountingCurrency)
	if err != nil {
		return err
	}
	err = checkCurrency(ctx, model.CsvExpenseCurrency, object.ExpenseCurrency)
	if err != nil {
		return err
	}
	rate, err := getExchangeRate(ctx, object)
	if err != nil {
		return err
	}
	if object.AmortizationMonths < 1 {
		object.AmortizationMonths = 1
	}

	object.ExchangeRate = rate
	object.AccountingAmount = object.ExpenseAmount.Mul(rate).Round(config.GetConfig(ctx).AmountScale)
	object.AmortizationStartMonth = time.Date(object.ExpenseDate.Year(), object.ExpenseDate.Month(), 1, 0, 0, 0, 0, object.ExpenseDate.Location())
	object.AmortizationEndMonth = object.AmortizationStartMonth.AddDate(0, object.AmortizationMonths-1, 0)
	if object.AmortizationEndMonth.Before(object.AmortizationStartMonth) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"months": object.AmortizationMonths}).Warn("明细摊分，月数过大")
		return errors.Errorf("摊分月数过大: %d", object.AmortizationMonths)
	}
	return nil
}

func getExchangeRate(ctx context.Context, object *model.Expense) (decimal.Decimal, error) {
	if object.ExpenseCurrency == object.AccountingCurrency {
		return decimal.NewFromInt(1), nil
	}

	rate := object.ExchangeRate
	if rate.IsZero() {
		var err error
		rate, err = exchange_rate.GetExchangeRate(ctx, object.ExpenseCurrency, object.AccountingCurrency, object.ExpenseDate)
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

func checkCurrency(ctx context.Context, name, code string) error {
	if code != "" && currency.IsValid(code) {
		return nil
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"name": name, "currency": code}).Warn("币种，不在枚举内")
	return errors.Errorf("%s，不在枚举内: %s", name, code)
}
