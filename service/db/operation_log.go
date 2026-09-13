package db

import (
	"context"

	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	pageSizeDefault         = 20
	pageSizeMax             = 200
	operationLogSortDefault = "created_at desc"
)

var operationTypes = map[string]bool{
	model.OperationTypeSystemInit:      true,
	model.OperationTypeDataEntry:       true,
	model.OperationTypeExpenseEdit:     true,
	model.OperationTypeExpenseDelete:   true,
	model.OperationTypeCurrencySwitch:  true,
	model.OperationTypeClientTokenSwap: true,
	model.OperationTypeDbImport:        true,
	model.OperationTypeDbExport:        true,
}

var operationResults = map[string]bool{
	model.ResultSuccess: true,
	model.ResultFailure: true,
	model.ResultPartial: true,
}

func SelectOperationLog(ctx context.Context, inquiry model.OperationLogInquiry) ([]*model.OperationLog, int64, error) {
	inquiry, err := checkOperationLogInquiry(ctx, inquiry)
	if err != nil {
		return nil, 0, err
	}
	return db.SelectOperationLog(ctx, inquiry)
}

func checkOperationLogInquiry(ctx context.Context, inquiry model.OperationLogInquiry) (model.OperationLogInquiry, error) {
	for _, one := range inquiry.OperationType {
		if !operationTypes[one] {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"operationType": one}).Warn("查询审计，操作类型非法")
			return inquiry, errors.Errorf("查询审计，操作类型非法: %s", one)
		}
	}
	for _, one := range inquiry.Result {
		if !operationResults[one] {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"result": one}).Warn("查询审计，操作结果非法")
			return inquiry, errors.Errorf("查询审计，操作结果非法: %s", one)
		}
	}
	if !inquiry.CreatedAtStart.IsZero() && !inquiry.CreatedAtEnd.IsZero() && inquiry.CreatedAtStart.After(inquiry.CreatedAtEnd) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"start": inquiry.CreatedAtStart, "end": inquiry.CreatedAtEnd}).Warn("查询审计，时间区间倒挂")
		return inquiry, errors.Errorf("查询审计，时间区间倒挂")
	}
	//db层的pageSize<=0是不加limit，审计只涨不减，不兜底就会整表拉出来
	if inquiry.PageSize <= 0 {
		inquiry.PageSize = pageSizeDefault
	}
	if inquiry.PageSize > pageSizeMax {
		inquiry.PageSize = pageSizeMax
	}
	//db层默认id asc，审计列表要的是最新在前
	if inquiry.Sort == "" {
		inquiry.Sort = operationLogSortDefault
	}
	return inquiry, nil
}
