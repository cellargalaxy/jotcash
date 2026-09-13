package model

import (
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// decimal.Decimal只能声明为varchar：SQLite按声明类型做亲和判定，decimal/numeric落到NUMERIC亲和，会把数字文本转成浮点数丢精度
type Expense struct {
	Id                     int64           `json:"id" gorm:"column:id;primaryKey;autoIncrement:false"`
	BankName               string          `json:"bank_name" gorm:"column:bank_name;index"`
	CardLast4              string          `json:"card_last_4" gorm:"column:card_last_4;index"`
	ExpenseDate            time.Time       `json:"expense_date" gorm:"column:expense_date;index"`
	ExpenseCurrency        string          `json:"expense_currency" gorm:"column:expense_currency;index"`
	ExpenseAmount          decimal.Decimal `json:"expense_amount" gorm:"column:expense_amount;type:varchar(32)"`
	Counterparty           string          `json:"counterparty" gorm:"column:counterparty"`
	Remark                 string          `json:"remark" gorm:"column:remark"`
	ExchangeRate           decimal.Decimal `json:"exchange_rate" gorm:"column:exchange_rate;type:varchar(32)"`
	AccountingCurrency     string          `json:"accounting_currency" gorm:"column:accounting_currency;index"`
	AccountingAmount       decimal.Decimal `json:"accounting_amount" gorm:"column:accounting_amount;type:varchar(32)"`
	ExpenseType            string          `json:"expense_type" gorm:"column:expense_type;index"`
	AmortizationMonths     int             `json:"amortization_months" gorm:"column:amortization_months;index"`
	AmortizationStartMonth time.Time       `json:"amortization_start_month" gorm:"column:amortization_start_month"`
	AmortizationEndMonth   time.Time       `json:"amortization_end_month" gorm:"column:amortization_end_month"`
	OperationId            int64           `json:"operation_id" gorm:"column:operation_id;index"`
	FileId                 int64           `json:"file_id" gorm:"column:file_id;index"`
	Version                int             `json:"version" gorm:"column:version;index"`
	CreatedAt              time.Time       `json:"created_at" gorm:"column:created_at"`
	UpdatedAt              time.Time       `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt              gorm.DeletedAt  `json:"deleted_at" gorm:"column:deleted_at;index"`
}

func (this Expense) String() string {
	return util.JsonStruct2Str(this)
}
func (this Expense) TableName() string {
	return "expense"
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
	Page               int      `json:"page"`
	PageSize           int      `json:"page_size"`
}

func (this ExpenseInquiry) String() string {
	return util.JsonStruct2Str(this)
}
