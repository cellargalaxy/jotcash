package exchange_rate

import (
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/shopspring/decimal"
)

func TestGetExchangeRateSameCurrency(t *testing.T) {
	ctx := util.GenCtx()

	rate, err := GetExchangeRate(ctx, "CNY", "CNY", time.Now())
	if err != nil {
		t.Fatalf("同币种查询异常: %+v", err)
	}
	if !rate.Equal(decimal.NewFromInt(1)) {
		t.Errorf("同币种汇率应为1: got=%s want=1", rate)
	}
}

func TestGetExchangeRateHistorical(t *testing.T) {
	ctx := util.GenCtx()

	date := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	rate, err := GetExchangeRate(ctx, "USD", "CNY", date)
	if err != nil {
		t.Fatalf("历史汇率查询异常: %+v", err)
	}
	if !rate.IsPositive() {
		t.Fatalf("汇率应为正数: %s", rate)
	}
	//2026-01-15 USD兑CNY大约在6.9~7.0之间
	if rate.LessThan(decimal.NewFromInt(5)) || rate.GreaterThan(decimal.NewFromInt(10)) {
		t.Errorf("汇率数值偏离正常范围: %s", rate)
	}

	//测试缓存命中：第二次查询同一日期与币种，应快速返回相同汇率
	cachedRate, err := GetExchangeRate(ctx, "USD", "CNY", date)
	if err != nil {
		t.Fatalf("缓存读取异常: %+v", err)
	}
	if !cachedRate.Equal(rate) {
		t.Errorf("缓存汇率与首次查询结果不一致: got=%s want=%s", cachedRate, rate)
	}
}

func TestGetExchangeRateWeekend(t *testing.T) {
	ctx := util.GenCtx()

	//2026-01-17是周六（非交易日），应自动对齐到周五2026-01-16
	date := time.Date(2026, 1, 17, 0, 0, 0, 0, time.UTC)
	rate, err := GetExchangeRate(ctx, "USD", "CNY", date)
	if err != nil {
		t.Fatalf("非交易日汇率查询异常: %+v", err)
	}
	if !rate.IsPositive() {
		t.Fatalf("非交易日汇率应为正数: %s", rate)
	}
}

func TestGetExchangeRateInvalid(t *testing.T) {
	ctx := util.GenCtx()

	if _, err := GetExchangeRate(ctx, "", "CNY", time.Now()); err == nil {
		t.Errorf("来源币种为空应报错")
	}
	if _, err := GetExchangeRate(ctx, "USD", "", time.Now()); err == nil {
		t.Errorf("目标币种为空应报错")
	}
	if _, err := GetExchangeRate(ctx, "XYZ_NONEXISTENT", "CNY", time.Now()); err == nil {
		t.Errorf("不存在的币种应报错")
	}
}
