package rdb

import (
	"context"
	"fmt"

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
	"updated_at asc":      "updated_at asc",
	"updated_at desc":     "updated_at desc",
}

const expenseSortDefault = "id asc"

var expenseDistinctMap = map[string]string{
	"bank_name":           "bank_name",
	"card_last_4":         "card_last_4",
	"expense_currency":    "expense_currency",
	"accounting_currency": "accounting_currency",
	"expense_type":        "expense_type",
}

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
	if len(this.AccountingCurrencyNot) > 0 {
		tx = tx.Where("accounting_currency not in (?)", this.AccountingCurrencyNot)
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
	Object   *model.Expense
	Unscoped bool
	Count    int64
}

func (this *ExpenseUpdateHandler) Exec(ctx context.Context, tx *gorm.DB) error {
	if this.Object == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("更新expense，为空")
		return nil
	}

	object := *this.Object
	object.Version = this.Object.Version + 1
	tx = tx.Model(&model.Expense{})
	if this.Unscoped {
		tx = tx.Unscoped()
	}
	result := tx.Where("id = ? and version = ?", this.Object.Id, this.Object.Version).
		Select("*").Omit("id", "created_at", "deleted_at").Updates(&object)
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
	inquiry.Deleted = model.DeletedNo
	inquiry.Page, inquiry.PageSize = 0, 0
	handler := util.NewDeleteHandler[model.Expense](model.Expense{}.TableName(), ExpenseInquiry(inquiry))
	return handler
}

func NewExpenseSelectHandler(inquiry model.ExpenseInquiry) *util.SelectHandler[model.Expense] {
	handler := util.NewSelectHandler[model.Expense](model.Expense{}.TableName(), ExpenseInquiry(inquiry))
	return handler
}

func NewExpenseDistinctHandler(inquiry model.ExpenseDistinctInquiry) *ExpenseDistinctHandler {
	handler := new(ExpenseDistinctHandler)
	handler.Inquiry = inquiry
	return handler
}

type ExpenseDistinctHandler struct {
	Inquiry model.ExpenseDistinctInquiry
	Object  []string
	Count   int64
}

func (this *ExpenseDistinctHandler) Exec(ctx context.Context, tx *gorm.DB) error {
	column := expenseDistinctMap[this.Inquiry.Field]
	if column == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"field": this.Inquiry.Field}).Error("查询expense候选，不在白名单内")
		return errors.Errorf("查询expense候选，不在白名单内: %s", this.Inquiry.Field)
	}

	tx, err := ExpenseInquiry(model.ExpenseInquiry{Deleted: this.Inquiry.Deleted}).Where(ctx, tx.Model(&model.Expense{}))
	if err != nil {
		return err
	}
	//空串不是候选，列为NULL的老行也一并被这条比较筛掉
	err = tx.Where(fmt.Sprintf("%s != ''", column)).Distinct().Order(column).Pluck(column, &this.Object).Error
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("查询expense候选，异常")
		return errors.Errorf("查询expense候选，异常: %+v", err)
	}
	this.Count = int64(len(this.Object))
	logrus.WithContext(ctx).WithFields(logrus.Fields{"count": this.Count}).Info("查询expense候选，完成")
	return nil
}
