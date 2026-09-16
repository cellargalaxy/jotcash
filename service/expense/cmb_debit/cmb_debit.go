package cmb_debit

import (
	"bytes"
	"context"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/expense"
	"github.com/ledongthuc/pdf"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

const (
	bankName = "招商银行"

	title           = "招商银行交易流水"
	colDate         = "记账日期"
	colCurrency     = "货币"
	colAmount       = "交易金额"
	colBalance      = "联机余额"
	colRemark       = "交易摘要"
	colCounterparty = "对手信息"

	//单元格垂直居中，折行的半截会落在本行上下半行的位置，超过这个距离就不是这一行的
	rowTolerance = 12
	//列名的横坐标就是这一列的左边界，留一点余量吸收排版误差
	columnTolerance = 1
)

var (
	columns = []string{colDate, colCurrency, colAmount, colBalance, colRemark, colCounterparty}

	//中文列名下面还压着一行英文列名，两行逐个对上才是这套模板
	headerFields = []string{
		colDate, colCurrency, colAmount, colBalance, colRemark, colCounterparty,
		"Date", "Currency", "Transaction", "Amount", "Balance", "Transaction", "Type", "Counter", "Party",
	}

	footers = []string{"温馨提示", "交易流水验真", "打印时间"}

	//退款类明细是支出的抵扣，判定排在丢弃判定前面，免得被后面的规则顺手丢掉
	refundWords = []string{"退款", "退货", "退税", "退回", "冲正", "撤销"}
	//入账里只有这些摘要能确认是纯收入，其余入账（含说不清的）一律留下来记成负数支出
	incomeRemarks = []string{"结息", "利息", "转入", "收款", "代付", "代发", "工资"}
	//信用卡还款是账户之间的划转，消费本身在信用卡账单里，留下来就重复记账了
	repaymentWords = []string{"信用卡", "贷记卡"}

	accountRegexp   = regexp.MustCompile(`账号[：:]\s*([0-9*]+)`)
	dateRegexp      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	cardLast4Regexp = regexp.MustCompile(`^\d{4}$`)
)

func init() {
	expense.Register(new(Parser))
}

type Parser struct {
}

type textChunk struct {
	x    float64
	y    float64
	text string
}

type tableRow struct {
	y     float64
	cells []string
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
	if err != nil || !checkContract(text) {
		return false
	}
	_, _, ok := findColumns(extractPageChunks(page))
	return ok
}

// 同一家银行换张卡、导出时多勾一列，表格就换了个样子，认错比不认更糟，所以列名与列序差一点都不认
func checkContract(text string) bool {
	if !strings.Contains(text, title) || !accountRegexp.MatchString(text) {
		return false
	}
	start := strings.Index(text, colDate)
	if start < 0 {
		return false
	}
	fields := strings.Fields(text[start:])
	if len(fields) < len(headerFields) || !slices.Equal(fields[:len(headerFields)], headerFields) {
		return false
	}
	if len(fields) == len(headerFields) {
		return true
	}
	//表头后面必须直接是流水行或页脚，中间多出来的都是没认领的列
	next := fields[len(headerFields)]
	return dateRegexp.MatchString(next) || slices.ContainsFunc(footers, func(footer string) bool {
		return strings.HasPrefix(next, footer)
	})
}

func (this *Parser) Parse(ctx context.Context, data []byte) ([]*model.Expense, error) {
	if !this.Support(ctx, data) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析招商银行借记卡流水，表格与契约不一致")
		return nil, errors.Errorf("解析招商银行借记卡流水，表格与契约不一致")
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析招商银行借记卡流水，打开PDF异常")
		return nil, errors.Errorf("解析招商银行借记卡流水，打开PDF异常: %+v", err)
	}

	cardLast4 := parseCardLast4(extractPageChunks(reader.Page(1)))
	if cardLast4 == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析招商银行借记卡流水，账号非法")
		return nil, errors.Errorf("解析招商银行借记卡流水，账号非法")
	}

	var rows []tableRow
	for pageIndex := 1; pageIndex <= reader.NumPage(); pageIndex++ {
		page := reader.Page(pageIndex)
		if page.V.IsNull() {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex}).Warn("解析招商银行借记卡流水，页面为空")
			return nil, errors.Errorf("解析招商银行借记卡流水，第%d页为空", pageIndex)
		}
		pageRows, ok := parsePageRows(extractPageChunks(page))
		if !ok {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageIndex}).Warn("解析招商银行借记卡流水，页面没有流水表头")
			return nil, errors.Errorf("解析招商银行借记卡流水，第%d页没有流水表头", pageIndex)
		}
		rows = append(rows, pageRows...)
	}

	objects := make([]*model.Expense, 0, len(rows))
	for i := range rows {
		object, err := parseRow(ctx, i+1, cardLast4, rows[i])
		if err != nil {
			return nil, err
		}
		if object == nil {
			continue
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func parseRow(ctx context.Context, rowIndex int, cardLast4 string, row tableRow) (*model.Expense, error) {
	amount, err := decimal.NewFromString(strings.ReplaceAll(rowValue(row, colAmount), ",", ""))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"row": rowIndex, "err": err}).Warn("解析招商银行借记卡流水，交易金额非法")
		return nil, errors.Errorf("解析招商银行借记卡流水，第%d笔，交易金额非法", rowIndex)
	}
	remark := rowValue(row, colRemark)
	counterparty := rowValue(row, colCounterparty)
	if discardable(amount, remark, counterparty) {
		return nil, nil
	}

	expenseDate, err := util.ParseStr2Time(ctx, util.DateLayout_2006_01_02, rowValue(row, colDate), nil)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"row": rowIndex}).Warn("解析招商银行借记卡流水，记账日期非法")
		return nil, errors.Errorf("解析招商银行借记卡流水，第%d笔，记账日期非法", rowIndex)
	}

	object := model.Expense{
		BankName:        bankName,
		CardLast4:       cardLast4,
		ExpenseDate:     expenseDate,
		ExpenseCurrency: rowValue(row, colCurrency),
		//借记卡的扣款是负数，本系统的支出是正数，退款这类入账取反之后正好是负数支出
		ExpenseAmount: amount.Neg(),
		Counterparty:  counterparty,
		Remark:        remark,
	}
	return &object, nil
}

// 只有能严格判定的信用卡还款与纯入账收入才丢，说不清的一律留着，宁可多记也不能漏记
func discardable(amount decimal.Decimal, remark, counterparty string) bool {
	content := remark + counterparty
	if slices.ContainsFunc(refundWords, func(word string) bool { return strings.Contains(content, word) }) {
		return false
	}
	if strings.Contains(content, "还款") && slices.ContainsFunc(repaymentWords, func(word string) bool { return strings.Contains(content, word) }) {
		return true
	}
	if amount.IsNegative() {
		return false
	}
	//对手信息是自由文本，撞上收入词的商户名不少，所以入账只认银行填的摘要
	return slices.ContainsFunc(incomeRemarks, func(word string) bool { return strings.Contains(remark, word) })
}

// 列名在契约里的下标就是这一行单元格的下标
func rowValue(row tableRow, name string) string {
	i := slices.Index(columns, name)
	if i < 0 || i >= len(row.cells) {
		return ""
	}
	return strings.TrimSpace(row.cells[i])
}

func parseCardLast4(chunks []textChunk) string {
	_, headerY, ok := findColumns(chunks)
	if !ok {
		return ""
	}
	for i := range chunks {
		//账号印在表头上方的账户信息里，表格内的对手信息不算数
		if chunks[i].y <= headerY {
			continue
		}
		matches := accountRegexp.FindStringSubmatch(chunks[i].text)
		if len(matches) < 2 || len(matches[1]) < 4 {
			continue
		}
		last4 := matches[1][len(matches[1])-4:]
		if cardLast4Regexp.MatchString(last4) {
			return last4
		}
	}
	return ""
}

// 表头那一行的列名横坐标就是各列的左边界，换个页边距、换张卡都能跟着走，不写死坐标
func findColumns(chunks []textChunk) ([]float64, float64, bool) {
	lines := make(map[float64][]textChunk)
	for i := range chunks {
		lines[chunks[i].y] = append(lines[chunks[i].y], chunks[i])
	}
	var headerY float64
	var headerBounds []float64
	for y := range lines {
		line := lines[y]
		slices.SortFunc(line, func(a, b textChunk) int { return int(math.Round(a.x - b.x)) })
		texts := make([]string, 0, len(line))
		bounds := make([]float64, 0, len(line))
		for i := range line {
			texts = append(texts, strings.TrimSpace(line[i].text))
			bounds = append(bounds, line[i].x)
		}
		//续页的表头会重复出现，取最上面那一行，免得取到哪一行要看map的心情
		if slices.Equal(texts, columns) && (headerBounds == nil || y > headerY) {
			headerY, headerBounds = y, bounds
		}
	}
	return headerBounds, headerY, headerBounds != nil
}

// 表头以下按日期单元格切行，其余单元格投给纵坐标最近的那一行，折行的半截自然并回原单元格
func parsePageRows(chunks []textChunk) ([]tableRow, bool) {
	bounds, headerY, ok := findColumns(chunks)
	if !ok {
		return nil, false
	}

	slices.SortFunc(chunks, func(a, b textChunk) int {
		if a.y != b.y {
			return int(math.Round(b.y - a.y))
		}
		return int(math.Round(a.x - b.x))
	})

	var rows []tableRow
	for i := range chunks {
		if chunks[i].y >= headerY || columnIndex(bounds, chunks[i].x) != 0 {
			continue
		}
		if dateRegexp.MatchString(strings.TrimSpace(chunks[i].text)) {
			rows = append(rows, tableRow{y: chunks[i].y, cells: make([]string, len(columns))})
		}
	}

	for i := range chunks {
		if chunks[i].y >= headerY {
			continue
		}
		column := columnIndex(bounds, chunks[i].x)
		if column < 0 {
			continue
		}
		row := nearestRow(rows, chunks[i].y)
		if row == nil {
			continue
		}
		row.cells[column] += strings.TrimSpace(chunks[i].text)
	}
	return rows, true
}

func columnIndex(bounds []float64, x float64) int {
	for i := len(bounds) - 1; i >= 0; i-- {
		if x >= bounds[i]-columnTolerance {
			return i
		}
	}
	return -1
}

// 够不着任何一行的是页脚与提示文案，直接扔掉
func nearestRow(rows []tableRow, y float64) *tableRow {
	var nearest *tableRow
	distance := float64(rowTolerance)
	for i := range rows {
		if math.Abs(rows[i].y-y) <= distance {
			distance = math.Abs(rows[i].y - y)
			nearest = &rows[i]
		}
	}
	return nearest
}

func extractPageChunks(page pdf.Page) []textChunk {
	var chunks []textChunk
	var curFont pdf.Font
	var curX, curY float64

	pdf.Interpret(page.V.Key("Contents"), func(stk *pdf.Stack, op string) {
		args := popArgs(stk)
		switch op {
		case "Tf":
			if len(args) == 2 {
				curFont = page.Font(args[0].Name())
			}
		case "Tm":
			if len(args) == 6 {
				curX = args[4].Float64()
				curY = args[5].Float64()
			}
		case "Td":
			if len(args) == 2 {
				curX += args[0].Float64()
				curY += args[1].Float64()
			}
		case "Tj":
			if len(args) == 1 && curFont.Encoder() != nil {
				chunks = append(chunks, textChunk{x: curX, y: curY, text: curFont.Encoder().Decode(args[0].RawString())})
			}
		case "TJ":
			if len(args) == 1 && curFont.Encoder() != nil {
				var buffer bytes.Buffer
				for i := 0; i < args[0].Len(); i++ {
					value := args[0].Index(i)
					if value.Kind() == pdf.String {
						buffer.WriteString(curFont.Encoder().Decode(value.RawString()))
					}
				}
				chunks = append(chunks, textChunk{x: curX, y: curY, text: buffer.String()})
			}
		}
	})
	return chunks
}

func popArgs(stk *pdf.Stack) []pdf.Value {
	args := make([]pdf.Value, stk.Len())
	for i := len(args) - 1; i >= 0; i-- {
		args[i] = stk.Pop()
	}
	return args
}
