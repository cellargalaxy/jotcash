package expense

import (
	"context"
	"time"

	"github.com/bojanz/currency"
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/exchange_rate"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/expense/parser"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

// 识别器在浏览器里跑，服务端收到的是已经解析好的明细，只负责校验与填充汇率、摊销这些附加数据。
// 浏览器的结果不可全信：契约规则（日期、币种、汇率、摊销月数）在这里一律再挡一次
func Fill(ctx context.Context, objects []*parser.Expense, accountingCurrency string) ([]*model.Expense, error) {
	err := CheckCurrency(ctx, model.CsvAccountingCurrency, accountingCurrency)
	if err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("填充明细，明细为空")
		return nil, errors.Errorf("填充明细，明细为空")
	}

	expenses := make([]*model.Expense, 0, len(objects))
	for i := range objects {
		object, err := toExpense(ctx, objects[i])
		if err == nil {
			err = fillExpense(ctx, object, accountingCurrency)
		}
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"index": i + 1, "err": err}).Warn("填充明细，明细非法")
			return nil, errors.Errorf("填充明细，第%d笔，%s", i+1, err)
		}
		expenses = append(expenses, object)
	}
	return expenses, nil
}

func toExpense(ctx context.Context, object *parser.Expense) (*model.Expense, error) {
	if object == nil {
		return nil, errors.Errorf("明细为空")
	}
	if object.ExpenseDate.IsZero() {
		return nil, errors.Errorf("支出日期为空")
	}
	if !object.ExchangeRate.IsZero() && object.AccountingCurrency == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("填充明细，填了折算汇率却没填记账币种")
		return nil, errors.Errorf("填了折算汇率就必须填记账币种")
	}
	if object.AmortizationMonths < 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"months": object.AmortizationMonths}).Warn("填充明细，摊销月数非法")
		return nil, errors.Errorf("摊销月数非法: %d", object.AmortizationMonths)
	}
	return &model.Expense{
		BankName:           object.BankName,
		CardLast4:          object.CardLast4,
		ExpenseDate:        object.ExpenseDate,
		ExpenseCurrency:    object.ExpenseCurrency,
		ExpenseAmount:      object.ExpenseAmount,
		Counterparty:       object.Counterparty,
		Remark:             object.Remark,
		ExchangeRate:       object.ExchangeRate,
		AccountingCurrency: object.AccountingCurrency,
		ExpenseType:        object.ExpenseType,
		AmortizationMonths: object.AmortizationMonths,
	}, nil
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
	err := CheckCurrency(ctx, model.CsvAccountingCurrency, object.AccountingCurrency)
	if err != nil {
		return err
	}
	err = CheckCurrency(ctx, model.CsvExpenseCurrency, object.ExpenseCurrency)
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
		logrus.WithContext(ctx).WithFields(logrus.Fields{"months": object.AmortizationMonths}).Warn("明细摊销，月数过大")
		return errors.Errorf("摊销月数过大: %d", object.AmortizationMonths)
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

func CheckCurrency(ctx context.Context, name, code string) error {
	if code != "" && currency.IsValid(code) {
		return nil
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"name": name, "currency": code}).Warn("币种，不在枚举内")
	return errors.Errorf("%s，不在枚举内: %s", name, code)
}
