package db

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"gorm.io/gorm"
)

type ExpenseInquiry model.ExpenseInquiry

func (this ExpenseInquiry) Where(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if len(this.Id) > 0 {
		tx = tx.Where("id in (?)", this.Id)
	}
	if len(this.BankName) > 0 {
		tx = tx.Where("bank_name in (?)", this.BankName)
	}
	if len(this.CardLast4) > 0 {
		tx = tx.Where("card_last_4 in (?)", this.CardLast4)
	}
	if len(this.ExpenseCurrency) > 0 {
		tx = tx.Where("expense_currency in (?)", this.ExpenseCurrency)
	}
	if len(this.AccountingCurrency) > 0 {
		tx = tx.Where("accounting_currency in (?)", this.AccountingCurrency)
	}
	if len(this.ExpenseType) > 0 {
		tx = tx.Where("expense_type in (?)", this.ExpenseType)
	}
	if len(this.AmortizationMonths) > 0 {
		tx = tx.Where("amortization_months in (?)", this.AmortizationMonths)
	}
	if len(this.OperationId) > 0 {
		tx = tx.Where("operation_id in (?)", this.OperationId)
	}
	if len(this.FileId) > 0 {
		tx = tx.Where("file_id in (?)", this.FileId)
	}
	if len(this.Version) > 0 {
		tx = tx.Where("version in (?)", this.Version)
	}
	return tx
}
func (this ExpenseInquiry) Order(ctx context.Context, tx *gorm.DB) *gorm.DB {
	tx = tx.Order("id")
	return tx
}
func (this ExpenseInquiry) Limit(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if this.PageSize <= 0 {
		return tx
	}
	offset := (this.Page - 1) * this.PageSize
	tx = tx.Offset(offset).Limit(this.PageSize)
	return tx
}

func NewExpenseInsertHandler(object ...*model.Expense) *util.InsertHandler[model.Expense] {
	handler := util.NewInsertHandler[model.Expense](model.Expense{}.TableName(), object...)
	return handler
}

func NewExpenseUpdateHandler(object *model.Expense) *util.UpdateHandler[model.Expense] {
	handler := util.NewUpdateHandler[model.Expense](model.Expense{}.TableName(), object)
	return handler
}

func NewExpenseDeleteHandler(inquiry model.ExpenseInquiry) *util.DeleteHandler[model.Expense] {
	handler := util.NewDeleteHandler[model.Expense](model.Expense{}.TableName(), ExpenseInquiry(inquiry))
	return handler
}

func NewExpenseSelectHandler(inquiry model.ExpenseInquiry) *util.SelectHandler[model.Expense] {
	handler := util.NewSelectHandler[model.Expense](model.Expense{}.TableName(), ExpenseInquiry(inquiry))
	return handler
}

func InsertExpense(ctx context.Context, object ...*model.Expense) (int64, error) {
	handler := NewExpenseInsertHandler(object...)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func UpdateExpense(ctx context.Context, object *model.Expense) (int64, error) {
	handler := NewExpenseUpdateHandler(object)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func DeleteExpense(ctx context.Context, inquiry model.ExpenseInquiry) (int64, error) {
	handler := NewExpenseDeleteHandler(inquiry)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func SelectExpense(ctx context.Context, inquiry model.ExpenseInquiry) ([]*model.Expense, int64, error) {
	handler := NewExpenseSelectHandler(inquiry)
	err := Transaction(ctx, handler)
	if err != nil {
		return nil, 0, err
	}
	return handler.Object, handler.Count, nil
}
