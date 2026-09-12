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
//
// decimal.Decimal 字段一律用 varchar 而非 decimal(m,n)/numeric(m,n) 作为 gorm 列类型：
// SQLite 按列类型声明做“类型亲和”（type affinity）判定，decimal/numeric 会落入其 NUMERIC 亲和规则，
// 插入的高精度数字文本会被 SQLite 自动转换成浮点数再存储，导致大数值/高精度小数静默丢精度；
// 只有列类型声明命中 CHAR/CLOB/TEXT 关键字（如 varchar/text）才会取 TEXT 亲和、原样存字符串。
// 已用 model_test.go 的 TestExpenseRoundTrip 实测验证：decimal(20,4) 会把
// "-1234567890123456.7890" 存成浮点数 -1234567890123456.8（丢失全部小数精度），varchar(32) 则精确往返。
type Expense struct {
	// Id 明细ID：唯一标识。取值由应用层调用 github.com/cellargalaxy/go_common/util.GenId() 生成
	// （返回 int64，时间有序、进程内单调递增），不使用数据库自增，因此不带 autoIncrement。
	Id int64 `json:"id" gorm:"column:id;primaryKey"`

	BankName  string `json:"bank_name" gorm:"column:bank_name"`     // 银行名称，解析不出/未填时留空
	CardLast4 string `json:"card_last_4" gorm:"column:card_last_4"` // 卡号后四位，解析不出/未填时留空

	ExpenseDate     time.Time       `json:"expense_date" gorm:"column:expense_date;type:date;index"`               // 支出日期：交易发生日字面值，无时区换算；摊分起始月的依据
	ExpenseCurrency string          `json:"expense_currency" gorm:"column:expense_currency;type:varchar(3);index"` // 支出币种：币种枚举代码，枚举本身不落库（见F-1）
	ExpenseAmount   decimal.Decimal `json:"expense_amount" gorm:"column:expense_amount;type:varchar(32)"`          // 支出金额：允许为0/负（退款、冲正）

	Counterparty string `json:"counterparty" gorm:"column:counterparty"` // 交易对手方，未给出时留空
	Remark       string `json:"remark" gorm:"column:remark"`             // 交易备注，未给出时留空

	ExchangeRate       decimal.Decimal `json:"exchange_rate" gorm:"column:exchange_rate;type:varchar(32)"`                  // 折算汇率：CSV有值优先，无值自动获取；支出币种=记账币种时恒为1；恒>0
	AccountingCurrency string          `json:"accounting_currency" gorm:"column:accounting_currency;type:varchar(3);index"` // 记账币种：新增取请求携带值，编辑既有行保持库内原值，只能经F-4变更
	AccountingAmount   decimal.Decimal `json:"accounting_amount" gorm:"column:accounting_amount;type:varchar(32)"`          // 记账金额 = 支出金额 × 折算汇率，只读因变量

	ExpenseType string `json:"expense_type" gorm:"column:expense_type;index"` // 支出类型：自由字符串（零实体方案），空即未分类，候选下拉来源=运行时distinct(支出类型)

	// 摊分（会计上的“摊销”语义：把一笔一次性支出的核算覆盖范围摊到多个月份，不改变支出金额/支出日期本身）
	AmortizationMonths     int       `json:"amortization_months" gorm:"column:amortization_months"`                     // 摊分月数：默认1，下限1，无上限
	AmortizationStartMonth time.Time `json:"amortization_start_month" gorm:"column:amortization_start_month;type:date"` // 摊分起始月 = 支出日期所属月
	AmortizationEndMonth   time.Time `json:"amortization_end_month" gorm:"column:amortization_end_month;type:date"`     // 摊分结束月 = 起始月 + 摊分月数 − 1

	AuditId int64 `json:"audit_id" gorm:"column:audit_id;index"` // 首次入库所属审计ID：仅新增时赋值，编辑不改动；历史指针，非外键（§8.5）
	FileId  int64 `json:"file_id" gorm:"column:file_id;index"`   // 首次入库所用CSV快照的文件ID：仅新增时赋值，编辑不改动

	Version int `json:"version" gorm:"column:version"` // 乐观锁版本号：仅用于单行编辑接口，不进CSV契约

	CreatedAt time.Time      `json:"created_at" gorm:"column:created_at"`       // 创建时间
	UpdatedAt time.Time      `json:"updated_at" gorm:"column:updated_at"`       // 更新时间
	DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"column:deleted_at;index"` // 删除时间：有值即已删除，终态不可逆，仅软删除（前提7）
}

func (this Expense) String() string {
	data, _ := json.Marshal(this)
	return string(data)
}
