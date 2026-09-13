package exchange_rate

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

func GetExchangeRate(ctx context.Context, expenseDate time.Time, expenseCurrency, accountingCurrency string) (decimal.Decimal, error) {
	//todo
	//具体实现先挂起，代码写死返回1
	return decimal.NewFromInt(1), nil
}
