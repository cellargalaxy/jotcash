// Package parser 是浏览器与服务端之间的明细契约，也是各识别器的注册表。
//
// 识别器编译成 js/wasm 跑在浏览器里，所以这个包与各识别器只能引用纯计算的依赖：
// go_common/util、model、config 这些服务端包都带着系统调用与初始化副作用，编不进 wasm，一个都不能进来。
// 服务端只引用本包的 Expense 去反序列化上传的明细，不引用任何识别器。
package parser

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

// 支出日期在契约里是不带时区的墙钟时间：浏览器的时区跟服务端未必一致，带着时区传就会让日期整天漂移
const DateLayout = "2006-01-02 15:04:05"

// 识别器解析日期统一用东八区，与服务端 go_common/util 的 E8Loc 一致；序列化只取墙钟，不做时区换算
var Loc = time.FixedZone("UTC+8", 8*3600)

type Parser interface {
	Support(ctx context.Context, data []byte) bool
	Parse(ctx context.Context, data []byte) ([]*Expense, error)
}

// Expense 是识别器解析出的一笔明细，也是上传接口 expense 字段里的数组元素。
// 只有文件里能读到的字段；汇率、记账金额、摊销起止月这些附加数据一律由服务端填充
type Expense struct {
	BankName           string          `json:"bank_name"`
	CardLast4          string          `json:"card_last_4"`
	ExpenseDate        time.Time       `json:"expense_date"`
	ExpenseCurrency    string          `json:"expense_currency"`
	ExpenseAmount      decimal.Decimal `json:"expense_amount"`
	Counterparty       string          `json:"counterparty"`
	Remark             string          `json:"remark"`
	ExchangeRate       decimal.Decimal `json:"exchange_rate"`       //0表示交给服务端按支出日期去取
	AccountingCurrency string          `json:"accounting_currency"` //空表示用请求带的记账币种
	ExpenseType        string          `json:"expense_type"`
	AmortizationMonths int             `json:"amortization_months"` //0表示交给服务端取默认值
}

// 只为了把 ExpenseDate 换成墙钟字符串，其余字段沿用默认的序列化
type expenseJson struct {
	*expenseAlias
	ExpenseDate string `json:"expense_date"`
}

type expenseAlias Expense

func (this Expense) MarshalJSON() ([]byte, error) {
	return json.Marshal(expenseJson{expenseAlias: (*expenseAlias)(&this), ExpenseDate: this.ExpenseDate.Format(DateLayout)})
}

func (this *Expense) UnmarshalJSON(data []byte) error {
	object := expenseJson{expenseAlias: (*expenseAlias)(this)}
	err := json.Unmarshal(data, &object)
	if err != nil {
		return err
	}
	this.ExpenseDate, err = time.ParseInLocation(DateLayout, object.ExpenseDate, Loc)
	if err != nil {
		return errors.Errorf("支出日期非法: %s", object.ExpenseDate)
	}
	return nil
}

var parsers = map[string]Parser{}

func Register(name string, parser Parser) {
	parsers[name] = parser
}

// 已注册的识别器名，排好序，页面下拉与契约测试拿它对账
func Names() []string {
	names := make([]string, 0, len(parsers))
	for name := range parsers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// 识别器由用户手选，严格按所选识别器的口径解析：内容跟定义差一点都不解析，解析不出明细也报错，
// 让页面提醒用户检查是否选错了识别器，而不是悄悄入库一个空批次
func Parse(ctx context.Context, parserName string, data []byte) ([]*Expense, error) {
	parser, ok := parsers[parserName]
	if !ok {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"parser": parserName}).Warn("解析明细，识别器不存在")
		return nil, errors.Errorf("解析明细，识别器不存在: %s", parserName)
	}
	if !parser.Support(ctx, data) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"parser": parserName, "len": len(data)}).Warn("解析明细，文件内容与所选识别器不符")
		return nil, errors.Errorf("解析明细，文件内容与所选识别器不符")
	}
	objects, err := parser.Parse(ctx, data)
	if err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"parser": parserName}).Warn("解析明细，没有解析到明细")
		return nil, errors.Errorf("解析明细，没有解析到明细")
	}
	return objects, nil
}

func ParseTime(layout, value string) (time.Time, error) {
	return time.ParseInLocation(layout, value, Loc)
}
