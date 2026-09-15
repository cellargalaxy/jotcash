package repo

import (
	"context"
	"fmt"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/rdb"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const expenseSortDefault = "expense_date desc"

var expenseDeleteds = map[int]bool{
	model.DeletedNo:   true,
	model.DeletedAll:  true,
	model.DeletedOnly: true,
}

func InsertExpense(ctx context.Context, operationId int64, expenses []*model.Expense, fileMeta *model.FileMeta, fileBlob *model.FileBlob) error {
	if len(expenses) == 0 || fileMeta == nil || fileBlob == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("明细入库，入库内容为空")
		return errors.Errorf("明细入库，入库内容为空")
	}
	operationLog := model.OperationLog{
		Id:            operationId,
		OperationType: model.OperationTypeDataEntry,
		Summary:       fmt.Sprintf("入库 %d 笔，来源 %s", len(expenses), fileMeta.FileName),
		Result:        model.ResultSuccess,
	}

	transaction, err := rdb.NewTransaction(ctx)
	if err != nil {
		return err
	}
	defer transaction.Close(ctx)

	fileBlobHandler := rdb.NewFileBlobInsertHandler(fileBlob)
	fileMetaHandler := rdb.NewFileMetaInsertHandler(fileMeta)
	expenseHandler := rdb.NewExpenseInsertHandler(expenses...)
	operationLogHandler := rdb.NewOperationLogInsertHandler(&operationLog)
	return transaction.AddCommit(fileBlobHandler, fileMetaHandler, expenseHandler, operationLogHandler).Exec(ctx)
}

func SelectExpense(ctx context.Context, inquiry model.ExpenseInquiry) ([]*model.Expense, int64, error) {
	inquiry, err := checkExpenseInquiry(ctx, inquiry)
	if err != nil {
		return nil, 0, err
	}

	transaction, err := rdb.NewTransaction(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer transaction.Close(ctx)

	expenseHandler := rdb.NewExpenseSelectHandler(inquiry)
	err = transaction.AddCommit(expenseHandler).Exec(ctx)
	if err != nil {
		return nil, 0, err
	}
	return expenseHandler.Object, expenseHandler.Count, nil
}

func checkExpenseRange(ctx context.Context, inquiry model.ExpenseInquiry) error {
	err := checkTimeRange(ctx, inquiry.ExpenseDateStart, inquiry.ExpenseDateEnd)
	if err != nil {
		return err
	}
	return checkAmountRange(ctx, inquiry.ExpenseAmountMin, inquiry.ExpenseAmountMax)
}

func checkExpenseInquiry(ctx context.Context, inquiry model.ExpenseInquiry) (model.ExpenseInquiry, error) {
	//db层对没见过的取值会按「只查未删除」处理，静默少查数据不如直接报错
	if !expenseDeleteds[inquiry.Deleted] {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"deleted": inquiry.Deleted}).Warn("查询明细，删除筛选非法")
		return inquiry, errors.Errorf("查询明细，删除筛选非法: %d", inquiry.Deleted)
	}
	err := checkExpenseRange(ctx, inquiry)
	if err != nil {
		return inquiry, err
	}
	inquiry.PageSize = checkPageSize(inquiry.PageSize)
	//db层默认id asc，明细列表要的是支出日期最新在前
	if inquiry.Sort == "" {
		inquiry.Sort = expenseSortDefault
	}
	return inquiry, nil
}

func DeleteExpense(ctx context.Context, inquiry model.ExpenseInquiry) (int64, error) {
	err := checkExpenseRange(ctx, inquiry)
	if err != nil {
		return 0, err
	}
	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeExpenseDelete,
		Summary:       "批量软删除明细",
		Result:        model.ResultSuccess,
	}

	transaction, err := rdb.NewTransaction(ctx)
	if err != nil {
		return 0, err
	}
	defer transaction.Close(ctx)

	//分页与排序对批量删除没有意义，db层的NewExpenseDeleteHandler会把它们清掉，也会强制只删未删除的行
	deleteHandler := rdb.NewExpenseDeleteHandler(inquiry)
	operationLogHandler := rdb.NewOperationLogInsertHandler(&operationLog)
	err = transaction.AddCommit(deleteHandler, operationLogHandler).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return deleteHandler.Count, nil
}
