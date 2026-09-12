// Package model 定义记账系统的全部实体 struct。
//
// 依据：doc/decisions/记账系统功能与模型.md（对话4 合并定稿）。
// 约束：本包不承载任何业务逻辑（校验、计算、派生规则等一律不在此实现），
// 仅描述实体字段与持久化/序列化标签；全部代码层级（接口层/服务层/持久化层）
// 统一复用本包内的 struct，不再按层派生 PO/BO/DTO 等变体。
package model

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Expense 支出明细：全模型唯一的事实实体，只记支出（对应文档 §四.1，共 21 项属性）
type Expense struct {
	Id int `json:"id" gorm:"column:id;primaryKey;autoIncrement"` // 明细ID，唯一标识

	BankName     string `json:"bank_name" gorm:"column:bank_name"`           // 银行名称，解析不出/未填时留空
	CardLastFour string `json:"card_last_four" gorm:"column:card_last_four"` // 卡号后四位，解析不出/未填时留空

	ExpenseDate     time.Time       `json:"expense_date" gorm:"column:expense_date;type:date;index"`               // 支出日期：交易发生日字面值，无时区换算；摊分起始月的依据
	ExpenseCurrency string          `json:"expense_currency" gorm:"column:expense_currency;type:varchar(3);index"` // 支出币种：币种枚举代码，枚举本身不落库（见F-1）
	ExpenseAmount   decimal.Decimal `json:"expense_amount" gorm:"column:expense_amount;type:decimal(20,4)"`        // 支出金额：允许为0/负（退款、冲正）

	Counterparty string `json:"counterparty" gorm:"column:counterparty"` // 交易对手方，未给出时留空
	Remark       string `json:"remark" gorm:"column:remark"`             // 交易备注，未给出时留空

	ExchangeRate       decimal.Decimal `json:"exchange_rate" gorm:"column:exchange_rate;type:decimal(20,8)"`                // 折算汇率：CSV有值优先，无值自动获取；支出币种=记账币种时恒为1；恒>0
	AccountingCurrency string          `json:"accounting_currency" gorm:"column:accounting_currency;type:varchar(3);index"` // 记账币种：新增取请求携带值，编辑既有行保持库内原值，只能经F-4变更
	AccountingAmount   decimal.Decimal `json:"accounting_amount" gorm:"column:accounting_amount;type:decimal(20,4)"`        // 记账金额 = 支出金额 × 折算汇率，只读因变量

	ExpenseType string `json:"expense_type" gorm:"column:expense_type;index"` // 支出类型：自由字符串（零实体方案），空即未分类，候选下拉来源=运行时distinct(支出类型)

	InstallmentMonths     int       `json:"installment_months" gorm:"column:installment_months"`                     // 摊分月数：默认1，下限1，无上限
	InstallmentStartMonth time.Time `json:"installment_start_month" gorm:"column:installment_start_month;type:date"` // 摊分起始月 = 支出日期所属月
	InstallmentEndMonth   time.Time `json:"installment_end_month" gorm:"column:installment_end_month;type:date"`     // 摊分结束月 = 起始月 + 摊分月数 − 1

	AuditId int `json:"audit_id" gorm:"column:audit_id;index"` // 首次入库所属审计ID：仅新增时赋值，编辑不改动；历史指针，非外键（§8.5）
	FileId  int `json:"file_id" gorm:"column:file_id;index"`   // 首次入库所用CSV快照的文件ID：仅新增时赋值，编辑不改动

	Version int `json:"version" gorm:"column:version"` // 乐观锁版本号：仅用于单行编辑接口，不进CSV契约

	CreatedAt time.Time      `json:"created_at" gorm:"column:created_at"`       // 创建时间
	UpdatedAt time.Time      `json:"updated_at" gorm:"column:updated_at"`       // 更新时间
	DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"column:deleted_at;index"` // 删除时间：有值即已删除，终态不可逆，仅软删除（前提7）
}

func (this Expense) String() string {
	data, _ := json.Marshal(this)
	return string(data)
}
