package icbc_credit

import (
	"bytes"
	"context"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bojanz/currency"
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/expense"
	"github.com/ledongthuc/pdf"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

const (
	bankName = "工商银行"

	title                  = "中国工商银行信用卡历史明细（电子版）"
	colDate                = "入账日期"
	colCard                = "交易卡号"
	colDirection           = "收支"
	colTxCurrency          = "交易币种"
	colTxAmount            = "交易金额"
	colPostCurrency        = "入账币种"
	colPostAmount          = "入账金额"
	colBalance             = "账户余额"
	colCounterpartyName    = "对方户名"
	colCounterpartyAccount = "对方账号"
	colSummary             = "摘要"
	colPlace               = "交易场所"

	directionOut = "借"
	directionIn  = "贷"

	//入账日期与交易时间之间没有分隔符
	dateTimeLayout = "2006-01-0215:04:05"
)

var (
	columns = []string{
		colDate,
		colCard,
		colDirection,
		colTxCurrency,
		colTxAmount,
		colPostCurrency,
		colPostAmount,
		colBalance,
		colSummary,
		colPlace,
	}

	columnsWithCounterparty = []string{
		colDate,
		colCard,
		colDirection,
		colTxCurrency,
		colTxAmount,
		colPostCurrency,
		colPostAmount,
		colBalance,
		colCounterpartyName,
		colCounterpartyAccount,
		colSummary,
		colPlace,
	}

	supportedColumns = [][]string{columns, columnsWithCounterparty}

	//合计行一出现，本页明细就结束了
	footers = []string{"本页支出算术合计", "本页收入算术合计", "本页交易笔数", "下单时间"}

	//贷方里只有这几个摘要能确认是还款，其余贷方（退货、冲正、说不清的）一律留下来记成负数支出
	repaymentSummaries = []string{"转帐", "转账", "还款"}

	//部分导出模板中包含对方户名与对方账号，若未打印则单元格为空，纯文本中该两列缺失
	knownSummaries = []string{
		"消费", "退货", "转帐", "转账", "还款", "贷款利息", "结息", "利息", "年费", "冲正", "分期", "违约金", "手续费",
	}

	currencyCodes = map[string]string{
		"人民币":   "CNY",
		"美元":    "USD",
		"港币":    "HKD",
		"港元":    "HKD",
		"欧元":    "EUR",
		"英镑":    "GBP",
		"日元":    "JPY",
		"澳元":    "AUD",
		"澳大利亚元": "AUD",
		"加元":    "CAD",
		"加拿大元":  "CAD",
		"新加坡元":  "SGD",
		"瑞士法郎":  "CHF",
	}

	dateRegexp      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)
	cardLast4Regexp = regexp.MustCompile(`^\d{4}$`)
)

func init() {
	expense.Register(new(Parser))
}

type Parser struct {
}

func (this *Parser) Support(ctx context.Context, data []byte) bool {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return false
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || reader.NumPage() == 0 {
		return false
	}
	page := reader.Page(1)
	if page.V.IsNull() {
		return false
	}
	text, err := page.GetPlainText(nil)
	if err != nil {
		return false
	}
	return checkContract(text)
}

// 同一家银行换张卡、导出时多勾一列，表格就换了个样子，认错比不认更糟，所以列名与列序差一点都不认
func checkContract(text string) bool {
	return matchColumns(text) != nil
}

func matchColumns(text string) []string {
	if !strings.Contains(text, title) || !strings.Contains(text, "起止日期：") {
		return nil
	}
	if !strings.Contains(text, "卡号:") && !strings.Contains(text, "卡号：") {
		return nil
	}
	start := strings.Index(text, colDate)
	if start < 0 {
		return nil
	}
	fields := strings.Fields(text[start:])
	for _, cols := range supportedColumns {
		if len(fields) < len(cols) || !slices.Equal(fields[:len(cols)], cols) {
			continue
		}
		if len(fields) == len(cols) {
			return cols
		}
		//表头后面必须直接是明细行或页脚，中间多出来的都是没认领的列
		next := fields[len(cols)]
		if dateRegexp.MatchString(next) || slices.ContainsFunc(footers, func(footer string) bool {
			return strings.HasPrefix(next, footer)
		}) {
			return cols
		}
	}
	return nil
}

func (this *Parser) Parse(ctx context.Context, data []byte) ([]*model.Expense, error) {
	if !this.Support(ctx, data) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析工商银行信用卡明细，表格与契约不一致")
		return nil, errors.Errorf("解析工商银行信用卡明细，表格与契约不一致")
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析工商银行信用卡明细，打开PDF异常")
		return nil, errors.Errorf("解析工商银行信用卡明细，打开PDF异常: %+v", err)
	}

	var objects []*model.Expense
	for pageIndex := 1; pageIndex <= reader.NumPage(); pageIndex++ {
		page := reader.Page(pageIndex)
		if page.V.IsNull() {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex}).Warn("解析工商银行信用卡明细，页面为空")
			return nil, errors.Errorf("解析工商银行信用卡明细，第%d页为空", pageIndex)
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex, "err": err}).Warn("解析工商银行信用卡明细，读取页面文本异常")
			return nil, errors.Errorf("解析工商银行信用卡明细，第%d页读取页面文本异常: %+v", pageIndex, err)
		}
		cols := matchColumns(text)
		if cols == nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex}).Warn("解析工商银行信用卡明细，页面没有明细表头")
			return nil, errors.Errorf("解析工商银行信用卡明细，第%d页没有明细表头", pageIndex)
		}
		chunks, ok := splitChunks(text)
		if !ok {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex}).Warn("解析工商银行信用卡明细，页面没有明细表头")
			return nil, errors.Errorf("解析工商银行信用卡明细，第%d页没有明细表头", pageIndex)
		}
		for i := range chunks {
			object, err := parseChunk(ctx, cols, pageIndex, i+1, chunks[i])
			if err != nil {
				return nil, err
			}
			if object == nil {
				continue
			}
			objects = append(objects, object)
		}
	}
	return objects, nil
}

// 表头与页脚之间按日期行切出每一笔，折行的单元格会留在上一笔里
func splitChunks(text string) ([][]string, bool) {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}

	start := slices.Index(lines, colPlace)
	if start < 0 {
		return nil, false
	}

	var chunks [][]string
	var chunk []string
	for _, line := range lines[start+1:] {
		if slices.ContainsFunc(footers, func(footer string) bool { return strings.HasPrefix(line, footer) }) {
			break
		}
		if dateRegexp.MatchString(line) {
			if len(chunk) > 0 {
				chunks = append(chunks, chunk)
			}
			chunk = []string{line}
			continue
		}
		if len(chunk) > 0 {
			chunk = append(chunk, line)
		}
	}
	if len(chunk) > 0 {
		chunks = append(chunks, chunk)
	}
	return chunks, true
}

func normalizeChunk(cols []string, chunk []string) ([]string, error) {
	if len(cols) == len(columns) {
		if len(chunk) < len(columns) {
			return nil, errors.Errorf("明细列缺失")
		}
		//交易场所折行时多出来的行都是它的后半截
		if len(chunk) > len(columns) {
			chunk = append(chunk[:len(columns)-1:len(columns)-1], strings.Join(chunk[len(columns)-1:], ""))
		}
		return chunk, nil
	}

	// 12列表头：对方户名与对方账号未打印时单元格为空，纯文本中该两列缺失
	if len(chunk) < len(columns) {
		return nil, errors.Errorf("明细列缺失")
	}
	if len(chunk) == len(columns) {
		return []string{
			chunk[0], chunk[1], chunk[2], chunk[3], chunk[4],
			chunk[5], chunk[6], chunk[7], "", "", chunk[8], chunk[9],
		}, nil
	}
	if len(chunk) >= len(columnsWithCounterparty) {
		if len(chunk) > len(columnsWithCounterparty) {
			chunk = append(chunk[:len(columnsWithCounterparty)-1:len(columnsWithCounterparty)-1], strings.Join(chunk[len(columnsWithCounterparty)-1:], ""))
		}
		return chunk, nil
	}
	// 长度介于10与12之间（如11）：判断第9项是否为摘要
	if slices.Contains(knownSummaries, chunk[8]) {
		return []string{
			chunk[0], chunk[1], chunk[2], chunk[3], chunk[4],
			chunk[5], chunk[6], chunk[7], "", "", chunk[8], strings.Join(chunk[9:], ""),
		}, nil
	}
	return []string{
		chunk[0], chunk[1], chunk[2], chunk[3], chunk[4],
		chunk[5], chunk[6], chunk[7], chunk[8], chunk[9], chunk[10], "",
	}, nil
}

func parseChunk(ctx context.Context, cols []string, pageIndex, chunkIndex int, chunk []string) (*model.Expense, error) {
	chunk, err := normalizeChunk(cols, chunk)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex, "chunk": util.JsonStruct2Str(chunk)}).Warn("解析工商银行信用卡明细，明细列缺失")
		return nil, errors.Errorf("解析工商银行信用卡明细，第%d页第%d笔，明细列缺失", pageIndex, chunkIndex)
	}

	direction := chunkValue(cols, chunk, colDirection)
	summary := chunkValue(cols, chunk, colSummary)
	if direction == directionIn && slices.Contains(repaymentSummaries, summary) {
		return nil, nil
	}

	expenseDate, err := parseDate(ctx, chunkValue(cols, chunk, colDate))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex, "chunk": chunkIndex}).Warn("解析工商银行信用卡明细，支出日期非法")
		return nil, errors.Errorf("解析工商银行信用卡明细，第%d页第%d笔，支出日期非法", pageIndex, chunkIndex)
	}
	expenseCurrency, err := parseCurrency(chunkValue(cols, chunk, colTxCurrency))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex, "chunk": chunkIndex}).Warn("解析工商银行信用卡明细，支出币种非法")
		return nil, errors.Errorf("解析工商银行信用卡明细，第%d页第%d笔，%s", pageIndex, chunkIndex, err)
	}
	expenseAmount, err := decimal.NewFromString(strings.ReplaceAll(chunkValue(cols, chunk, colTxAmount), ",", ""))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex, "chunk": chunkIndex, "err": err}).Warn("解析工商银行信用卡明细，支出金额非法")
		return nil, errors.Errorf("解析工商银行信用卡明细，第%d页第%d笔，支出金额非法", pageIndex, chunkIndex)
	}
	switch direction {
	case directionOut:
		expenseAmount = expenseAmount.Abs()
	case directionIn:
		expenseAmount = expenseAmount.Abs().Neg()
	default:
		logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex, "chunk": chunkIndex, "direction": direction}).Warn("解析工商银行信用卡明细，收支方向非法")
		return nil, errors.Errorf("解析工商银行信用卡明细，第%d页第%d笔，收支方向非法: %s", pageIndex, chunkIndex, direction)
	}
	cardLast4 := parseCardLast4(chunkValue(cols, chunk, colCard))
	if cardLast4 == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex, "chunk": chunkIndex}).Warn("解析工商银行信用卡明细，交易卡号非法")
		return nil, errors.Errorf("解析工商银行信用卡明细，第%d页第%d笔，交易卡号非法", pageIndex, chunkIndex)
	}

	object := model.Expense{
		BankName:        bankName,
		CardLast4:       cardLast4,
		ExpenseDate:     expenseDate,
		ExpenseCurrency: expenseCurrency,
		ExpenseAmount:   expenseAmount,
		Counterparty:    chunkValue(cols, chunk, colPlace),
		Remark:          summary,
	}
	return &object, nil
}

// 表头已经与契约对齐，列名在契约里的下标就是这一笔的下标
func chunkValue(cols []string, chunk []string, name string) string {
	i := slices.Index(cols, name)
	if i < 0 || i >= len(chunk) {
		return ""
	}
	return strings.TrimSpace(chunk[i])
}

func parseDate(ctx context.Context, value string) (time.Time, error) {
	if len(value) == len(dateTimeLayout) {
		return util.ParseStr2Time(ctx, dateTimeLayout, value, nil)
	}
	if len(value) == len(util.DateLayout_2006_01_02_15_04_05) {
		return util.ParseStr2Time(ctx, util.DateLayout_2006_01_02_15_04_05, value, nil)
	}
	return util.ParseStr2Time(ctx, util.DateLayout_2006_01_02, value, nil)
}

func parseCurrency(value string) (string, error) {
	code, ok := currencyCodes[value]
	if ok {
		return code, nil
	}
	code = strings.ToUpper(value)
	if currency.IsValid(code) {
		return code, nil
	}
	return "", errors.Errorf("支出币种非法: %s", value)
}

func parseCardLast4(cardNo string) string {
	if len(cardNo) < 4 {
		return ""
	}
	last4 := cardNo[len(cardNo)-4:]
	if !cardLast4Regexp.MatchString(last4) {
		return ""
	}
	return last4
}
