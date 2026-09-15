package exchange_rate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bojanz/currency"
	"github.com/cellargalaxy/go_common/util"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

const (
	frankfurterApi = "https://api.frankfurter.dev/v1"
	fawazAhmedApi  = "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies"
)

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	cacheLock  sync.RWMutex
	rateCache  = make(map[string]decimal.Decimal)
)

func GetExchangeRate(ctx context.Context, expenseCurrency, accountingCurrency string, expenseDate time.Time) (decimal.Decimal, error) {
	expenseCurrency = strings.ToUpper(strings.TrimSpace(expenseCurrency))
	accountingCurrency = strings.ToUpper(strings.TrimSpace(accountingCurrency))
	if expenseCurrency == "" || accountingCurrency == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{
			"expenseCurrency":    expenseCurrency,
			"accountingCurrency": accountingCurrency,
		}).Warn("查询汇率，币种为空")
		return decimal.Zero, errors.Errorf("查询汇率，币种为空")
	}
	if !currency.IsValid(expenseCurrency) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"currency": expenseCurrency}).Warn("查询汇率，消费币种非法")
		return decimal.Zero, errors.Errorf("查询汇率，消费币种非法: %s", expenseCurrency)
	}
	if !currency.IsValid(accountingCurrency) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"currency": accountingCurrency}).Warn("查询汇率，记账币种非法")
		return decimal.Zero, errors.Errorf("查询汇率，记账币种非法: %s", accountingCurrency)
	}
	if expenseCurrency == accountingCurrency {
		return decimal.NewFromInt(1), nil
	}

	dateStr := expenseDate.Format("2006-01-02")
	if expenseDate.IsZero() {
		dateStr = time.Now().Format("2006-01-02")
	}
	cacheKey := fmt.Sprintf("%s_%s_%s", expenseCurrency, accountingCurrency, dateStr)

	cacheLock.RLock()
	rate, ok := rateCache[cacheKey]
	cacheLock.RUnlock()
	if ok {
		return rate, nil
	}

	cacheLock.Lock()
	defer cacheLock.Unlock()
	if rate, ok = rateCache[cacheKey]; ok {
		return rate, nil
	}

	rate, err := getExchangeRate(ctx, expenseCurrency, accountingCurrency, expenseDate)
	if err != nil {
		return decimal.Zero, err
	}
	rateCache[cacheKey] = rate
	return rate, nil
}

func getExchangeRate(ctx context.Context, expenseCurrency, accountingCurrency string, expenseDate time.Time) (decimal.Decimal, error) {
	rate, err := queryFrankfurter(ctx, expenseCurrency, accountingCurrency, expenseDate)
	if err == nil && rate.IsPositive() {
		return rate, nil
	}

	rate, fallbackErr := queryFawazAhmed(ctx, expenseCurrency, accountingCurrency, expenseDate)
	if fallbackErr == nil && rate.IsPositive() {
		return rate, nil
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{
		"expenseCurrency":    expenseCurrency,
		"accountingCurrency": accountingCurrency,
		"expenseDate":        expenseDate.Format("2006-01-02"),
		"err":                err,
		"fallbackErr":        fallbackErr,
	}).Error("查询汇率，获取异常")
	return decimal.Zero, errors.Errorf("查询汇率，获取异常: %s -> %s (%s)", expenseCurrency, accountingCurrency, expenseDate.Format("2006-01-02"))
}

func queryFrankfurter(ctx context.Context, expenseCurrency, accountingCurrency string, expenseDate time.Time) (decimal.Decimal, error) {
	dateStr := "latest"
	if !expenseDate.IsZero() && expenseDate.Before(time.Now().Truncate(24*time.Hour)) {
		dateStr = expenseDate.Format("2006-01-02")
	}
	requestUrl := fmt.Sprintf("%s/%s?base=%s&symbols=%s", frankfurterApi, dateStr, expenseCurrency, accountingCurrency)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestUrl, nil)
	if err != nil {
		return decimal.Zero, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"url": requestUrl, "err": err}).Warn("查询汇率，Frankfurter网络请求异常")
		return decimal.Zero, err
	}
	defer util.CloseIo(ctx, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, errors.Errorf("Frankfurter返回状态码异常: %d", resp.StatusCode)
	}

	var result struct {
		Rates map[string]decimal.Decimal `json:"rates"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"url": requestUrl, "err": err}).Warn("查询汇率，解析Frankfurter响应异常")
		return decimal.Zero, err
	}
	rate, ok := result.Rates[accountingCurrency]
	if !ok || !rate.IsPositive() {
		return decimal.Zero, errors.Errorf("Frankfurter未包含目标币种汇率: %s", accountingCurrency)
	}
	return rate, nil
}

func queryFawazAhmed(ctx context.Context, expenseCurrency, accountingCurrency string, expenseDate time.Time) (decimal.Decimal, error) {
	baseFrom := strings.ToLower(expenseCurrency)
	targetTo := strings.ToLower(accountingCurrency)

	requestUrl := fmt.Sprintf("%s/%s.json", fawazAhmedApi, baseFrom)
	if !expenseDate.IsZero() && expenseDate.Before(time.Now().Truncate(24*time.Hour)) {
		requestUrl = fmt.Sprintf("https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@%s/v1/currencies/%s.json", expenseDate.Format("2006-01-02"), baseFrom)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestUrl, nil)
	if err != nil {
		return decimal.Zero, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"url": requestUrl, "err": err}).Warn("查询汇率，jsDelivr网络请求异常")
		return decimal.Zero, err
	}
	defer util.CloseIo(ctx, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, errors.Errorf("jsDelivr返回状态码异常: %d", resp.StatusCode)
	}

	var result map[string]json.RawMessage
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return decimal.Zero, err
	}
	rawRates, ok := result[baseFrom]
	if !ok {
		return decimal.Zero, errors.Errorf("jsDelivr未找到来源币种汇率表: %s", baseFrom)
	}
	var rates map[string]decimal.Decimal
	err = json.Unmarshal(rawRates, &rates)
	if err != nil {
		return decimal.Zero, err
	}
	rate, ok := rates[targetTo]
	if !ok || !rate.IsPositive() {
		return decimal.Zero, errors.Errorf("jsDelivr未包含目标币种汇率: %s", targetTo)
	}
	return rate, nil
}
