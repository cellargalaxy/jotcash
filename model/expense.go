// Package model 定义记账系统的全部实体 struct，仅描述字段与持久化/序列化标签，不含业务逻辑。
package model

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Expense 支出明细：全模型唯一的事实实体，只记支出。
//
// decimal.Decimal 字段用 varchar 而非 decimal(m,n)：SQLite 按声明类型做亲和判定，decimal/numeric
// 会被当成 NUMERIC 亲和，插入的高精度数字文本会被自动转成浮点数丢精度；varchar 才是 TEXT 亲和，原样存字符串。
type Expense struct {
	Id int64 `json:"id" gorm:"column:id;primaryKey"` // 由 go_common/util.GenId() 生成，不用数据库自增

	BankName  string `json:"bank_name" gorm:"column:bank_name"`
	CardLast4 string `json:"card_last_4" gorm:"column:card_last_4"`

	ExpenseDate     time.Time       `json:"expense_date" gorm:"column:expense_date;type:date;index"` // 字面值，无时区换算
	ExpenseCurrency string          `json:"expense_currency" gorm:"column:expense_currency;type:varchar(3);index"`
	ExpenseAmount   decimal.Decimal `json:"expense_amount" gorm:"column:expense_amount;type:varchar(32)"` // 允许为0/负

	Counterparty string `json:"counterparty" gorm:"column:counterparty"`
	Remark       string `json:"remark" gorm:"column:remark"`

	ExchangeRate       decimal.Decimal `json:"exchange_rate" gorm:"column:exchange_rate;type:varchar(32)"`                  // 支出币种=记账币种时恒为1，恒>0
	AccountingCurrency string          `json:"accounting_currency" gorm:"column:accounting_currency;type:varchar(3);index"` // 编辑既有行不可直接改，只能经币种切换变更
	AccountingAmount   decimal.Decimal `json:"accounting_amount" gorm:"column:accounting_amount;type:varchar(32)"`          // = 支出金额 × 折算汇率，只读

	ExpenseType string `json:"expense_type" gorm:"column:expense_type;index"` // 空即未分类

	AmortizationMonths     int       `json:"amortization_months" gorm:"column:amortization_months"`                     // 下限1
	AmortizationStartMonth time.Time `json:"amortization_start_month" gorm:"column:amortization_start_month;type:date"` // = 支出日期所属月
	AmortizationEndMonth   time.Time `json:"amortization_end_month" gorm:"column:amortization_end_month;type:date"`     // = 起始月 + 摊分月数 − 1

	AuditId int64 `json:"audit_id" gorm:"column:audit_id;index"` // 仅新增时赋值，编辑不改动
	FileId  int64 `json:"file_id" gorm:"column:file_id;index"`   // 仅新增时赋值，编辑不改动

	Version int `json:"version" gorm:"column:version"` // 乐观锁

	CreatedAt time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"column:deleted_at;index"` // 有值即已删除，不可恢复
}

func (this Expense) String() string {
	data, _ := json.Marshal(this)
	return string(data)
}
