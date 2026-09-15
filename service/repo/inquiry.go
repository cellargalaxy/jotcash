package repo

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

func checkTimeRange(ctx context.Context, start, end time.Time) error {
	if start.IsZero() || end.IsZero() || !start.After(end) {
		return nil
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"start": start, "end": end}).Warn("查询，时间区间倒挂")
	return errors.Errorf("查询，时间区间倒挂")
}

func checkAmountRange(ctx context.Context, min, max *decimal.Decimal) error {
	if min == nil || max == nil || !min.GreaterThan(*max) {
		return nil
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"min": min, "max": max}).Warn("查询，金额区间倒挂")
	return errors.Errorf("查询，金额区间倒挂")
}
