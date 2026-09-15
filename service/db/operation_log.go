package db

import (
	"context"

	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const operationLogSortDefault = "created_at desc"

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

	transaction, err := db.NewTransaction(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer transaction.Close(ctx)

	operationLogHandler := db.NewOperationLogSelectHandler(inquiry)
	err = transaction.AddCommit(operationLogHandler).Exec(ctx)
	if err != nil {
		return nil, 0, err
	}
	return operationLogHandler.Object, operationLogHandler.Count, nil
}

func checkOperationLogInquiry(ctx context.Context, inquiry model.OperationLogInquiry) (model.OperationLogInquiry, error) {
	for _, operationType := range inquiry.OperationType {
		if !operationTypes[operationType] {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"operationType": operationType}).Warn("查询审计，操作类型非法")
			return inquiry, errors.Errorf("查询审计，操作类型非法: %s", operationType)
		}
	}
	for _, result := range inquiry.Result {
		if !operationResults[result] {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"result": result}).Warn("查询审计，操作结果非法")
			return inquiry, errors.Errorf("查询审计，操作结果非法: %s", result)
		}
	}
	err := checkTimeRange(ctx, inquiry.CreatedAtStart, inquiry.CreatedAtEnd)
	if err != nil {
		return inquiry, err
	}
	inquiry.PageSize = checkPageSize(inquiry.PageSize)
	//db层默认id asc，审计列表要的是最新在前
	if inquiry.Sort == "" {
		inquiry.Sort = operationLogSortDefault
	}
	return inquiry, nil
}
