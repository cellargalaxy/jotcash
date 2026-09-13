package model

import (
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type Expense struct {
	Id                     int64           `json:"id"`
	BankName               string          `json:"bank_name"`
	CardLast4              string          `json:"card_last_4"`
	ExpenseDate            time.Time       `json:"expense_date"`
	ExpenseCurrency        string          `json:"expense_currency"`
	ExpenseAmount          decimal.Decimal `json:"expense_amount"`
	Counterparty           string          `json:"counterparty"`
	Remark                 string          `json:"remark"`
	ExchangeRate           decimal.Decimal `json:"exchange_rate"`
	AccountingCurrency     string          `json:"accounting_currency"`
	AccountingAmount       decimal.Decimal `json:"accounting_amount"`
	ExpenseType            string          `json:"expense_type"`
	AmortizationMonths     int             `json:"amortization_months"`
	AmortizationStartMonth time.Time       `json:"amortization_start_month"`
	AmortizationEndMonth   time.Time       `json:"amortization_end_month"`
	OperationId            int64           `json:"operation_id"`
	FileId                 int64           `json:"file_id"`
	Version                int             `json:"version"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
	DeletedAt              gorm.DeletedAt  `json:"deleted_at"`
}

func (this Expense) String() string {
	return util.JsonStruct2Str(this)
}
func (this FileMeta) TableName() string {
	return "" //todo
}

type ExpenseInquiry struct {
	Id                 []int64  `json:"id"`
	BankName           []string `json:"bank_name"`
	CardLast4          []string `json:"card_last_4"`
	ExpenseCurrency    []string `json:"expense_currency"`
	AccountingCurrency []string `json:"accounting_currency"`
	ExpenseType        []string `json:"expense_type"`
	AmortizationMonths []int    `json:"amortization_months"`
	OperationId        []int64  `json:"operation_id"`
	FileId             []int64  `json:"file_id"`
	Version            []int    `json:"version"`
}

func (this ExpenseInquiry) String() string {
	return util.JsonStruct2Str(this)
}
