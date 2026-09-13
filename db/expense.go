package db

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var expenseSortMap = map[string]string{
	"id asc":              "id asc",
	"id desc":             "id desc",
	"expense_date asc":    "expense_date asc",
	"expense_date desc":   "expense_date desc",
	"expense_amount asc":  "cast(expense_amount as real) asc", //金额列是文本，按文本排序会串位
	"expense_amount desc": "cast(expense_amount as real) desc",
	"created_at asc":      "created_at asc",
	"created_at desc":     "created_at desc",
}

const expenseSortDefault = "id asc"

type ExpenseInquiry model.ExpenseInquiry

func (this ExpenseInquiry) Where(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
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
	if !this.ExpenseDateStart.IsZero() {
		tx = tx.Where("expense_date >= ?", this.ExpenseDateStart)
	}
	if !this.ExpenseDateEnd.IsZero() {
		tx = tx.Where("expense_date <= ?", this.ExpenseDateEnd)
	}
	if this.ExpenseAmountMin != nil {
		tx = tx.Where("cast(expense_amount as real) >= ?", this.ExpenseAmountMin.InexactFloat64())
	}
	if this.ExpenseAmountMax != nil {
		tx = tx.Where("cast(expense_amount as real) <= ?", this.ExpenseAmountMax.InexactFloat64())
	}
	if this.CounterpartyLike != "" {
		tx = tx.Where(`counterparty like ? escape '\'`, likeValue(this.CounterpartyLike))
	}
	if this.RemarkLike != "" {
		tx = tx.Where(`remark like ? escape '\'`, likeValue(this.RemarkLike))
	}
	if this.ExpenseTypeLike != "" {
		tx = tx.Where(`expense_type like ? escape '\'`, likeValue(this.ExpenseTypeLike))
	}
	switch this.Deleted {
	case model.DeletedAll:
		tx = tx.Unscoped()
	case model.DeletedOnly:
		tx = tx.Unscoped().Where("deleted_at is not null")
	}
	return tx, nil
}
func (this ExpenseInquiry) Order(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
	return sortOrder(ctx, tx, expenseSortMap, this.Sort, expenseSortDefault)
}
func (this ExpenseInquiry) Limit(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
	return pageLimit(ctx, tx, this.Page, this.PageSize)
}

func NewExpenseInsertHandler(object ...*model.Expense) *util.InsertHandler[model.Expense] {
	handler := util.NewInsertHandler[model.Expense](model.Expense{}.TableName(), object...)
	return handler
}

func NewExpenseUpdateHandler(object *model.Expense) *ExpenseUpdateHandler {
	handler := new(ExpenseUpdateHandler)
	handler.Object = object
	return handler
}

type ExpenseUpdateHandler struct {
	Object *model.Expense
	Count  int64
}

func (this *ExpenseUpdateHandler) Transaction(ctx context.Context, tx *gorm.DB) error {
	if this.Object == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("更新expense，为空")
		return nil
	}

	object := *this.Object
	object.Version = this.Object.Version + 1
	result := tx.Model(&model.Expense{}).
		Where("id = ? and version = ?", this.Object.Id, this.Object.Version).
		Select("*").Omit("id", "created_at").Updates(&object)
	this.Count = result.RowsAffected
	err := result.Error
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("更新expense，异常")
		return errors.Errorf("更新expense，异常: %+v", err)
	}
	if this.Count == 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"expense": this.Object}).Error("更新expense，版本冲突")
		return errors.Errorf("更新expense，版本冲突") //如果版本冲突，回滚整个事务
	}
	this.Object.Version = object.Version
	logrus.WithContext(ctx).WithFields(logrus.Fields{"count": this.Count}).Info("更新expense，完成")
	return nil
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
