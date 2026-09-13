package db

import (
	"context"

	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const expenseSortDefault = "expense_date desc"

var expenseDeleteds = map[int]bool{
	model.DeletedNo:   true,
	model.DeletedAll:  true,
	model.DeletedOnly: true,
}

func SelectExpense(ctx context.Context, inquiry model.ExpenseInquiry) ([]*model.Expense, int64, error) {
	inquiry, err := checkExpenseInquiry(ctx, inquiry)
	if err != nil {
		return nil, 0, err
	}
	return db.SelectExpense(ctx, inquiry)
}

func checkExpenseInquiry(ctx context.Context, inquiry model.ExpenseInquiry) (model.ExpenseInquiry, error) {
	//db层对没见过的取值会按「只查未删除」处理，静默少查数据不如直接报错
	if !expenseDeleteds[inquiry.Deleted] {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"deleted": inquiry.Deleted}).Warn("查询明细，删除筛选非法")
		return inquiry, errors.Errorf("查询明细，删除筛选非法: %d", inquiry.Deleted)
	}
	err := checkTimeRange(ctx, inquiry.ExpenseDateStart, inquiry.ExpenseDateEnd)
	if err != nil {
		return inquiry, err
	}
	err = checkAmountRange(ctx, inquiry.ExpenseAmountMin, inquiry.ExpenseAmountMax)
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
