package repo

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

const (
	pageSizeDefault = 20
	pageSizeMax     = 200
)

// db层的pageSize<=0是不加limit，上层不兜底就会整表拉出来
func checkPageSize(pageSize int) int {
	if pageSize <= 0 {
		return pageSizeDefault
	}
	if pageSize > pageSizeMax {
		return pageSizeMax
	}
	return pageSize
}

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
