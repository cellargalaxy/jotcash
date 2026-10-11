package hsbc_pulse_credit

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/md5"
	"crypto/rc4"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/expense"
	"github.com/ledongthuc/pdf"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

const (
	bankName = "汇丰银行"

	titleKeyword1 = "HSBC"
	titleKeyword2 = "Pulse"
	titleKeyword3 = "Dual Currency"

	colPostDate    = "Post date"
	colTransDate   = "Trans date"
	colDescription = "Description of transaction"
	colAmount      = "Amount"

	subAccountHKD = "HKD Sub-account"
	subAccountRMB = "RMB Sub-account"
	currencyHKD   = "HKD"
	currencyRMB   = "CNY"

	summaryPurchase = "消费"
	summaryRefund   = "退款"

	flagCR = "CR"
)

var (
	repaymentWords = []string{"PAYMENT", "AUTOPAY", "TRANSFER", "还款", "缴费"}

	monthMap = map[string]time.Month{
		"JAN": time.January,
		"FEB": time.February,
		"MAR": time.March,
		"APR": time.April,
		"MAY": time.May,
		"JUN": time.June,
		"JUL": time.July,
		"AUG": time.August,
		"SEP": time.September,
		"OCT": time.October,
		"NOV": time.November,
		"DEC": time.December,
	}

	dateMonthRegexp = regexp.MustCompile(`^(\d{1,2})\s*(JAN|FEB|MAR|APR|MAY|JUN|JUL|AUG|SEP|OCT|NOV|DEC)$`)
	cardLast4Regexp = regexp.MustCompile(`^\d{4}$`)
	card16Regexp    = regexp.MustCompile(`(?i)(?:Account\s*number\s*)?(\d{16})`)
	stmtDateRegexp  = regexp.MustCompile(`(\d{1,2})\s*(JAN|FEB|MAR|APR|MAY|JUN|JUL|AUG|SEP|OCT|NOV|DEC)\s*(\d{4})`)

	objHeaderRegexp = regexp.MustCompile(`(\d+)\s+(\d+)\s+obj`)
	passwordPad     = []byte{
		0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41,
		0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
		0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80,
		0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
	}
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

type txRow struct {
	y         float64
	postDate  string
	transDate string
	desc      string
	amount    string
	isCR      bool
}

func (this *Parser) Support(ctx context.Context, data []byte) bool {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return false
	}
	cleanedPdf, err := preprocessPdf(data)
	if err != nil {
		return false
	}
	reader, err := pdf.NewReader(bytes.NewReader(cleanedPdf), int64(len(cleanedPdf)))
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
	return checkContract(text, reader)
}

func checkContract(page1Text string, reader *pdf.Reader) bool {
	if !strings.Contains(page1Text, titleKeyword1) ||
		!strings.Contains(page1Text, titleKeyword2) ||
		!strings.Contains(page1Text, titleKeyword3) {
		return false
	}
	if !stmtDateRegexp.MatchString(page1Text) {
		return false
	}

	var hasSubAccount, hasTableHeaders bool
	for p := 1; p <= reader.NumPage(); p++ {
		text, err := reader.Page(p).GetPlainText(nil)
		if err != nil {
			continue
		}
		ltNoSpace := strings.ReplaceAll(text, " ", "")
		if strings.Contains(ltNoSpace, "HKDSub-account") || strings.Contains(ltNoSpace, "RMBSub-account") ||
			strings.Contains(ltNoSpace, "Amount(HKD)") || strings.Contains(ltNoSpace, "Amount(CNY)") ||
			strings.Contains(text, subAccountHKD) || strings.Contains(text, subAccountRMB) {
			hasSubAccount = true
		}
		if (strings.Contains(ltNoSpace, "Postdate") || strings.Contains(ltNoSpace, "PostDate")) &&
			(strings.Contains(ltNoSpace, "Transdate") || strings.Contains(ltNoSpace, "TransDate")) &&
			strings.Contains(ltNoSpace, "Descriptionoftransaction") {
			hasTableHeaders = true
		}
	}
	return hasSubAccount && hasTableHeaders
}

func (this *Parser) Parse(ctx context.Context, data []byte) ([]*model.Expense, error) {
	if !this.Support(ctx, data) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析汇丰银行信用卡明细，表格与契约不一致")
		return nil, errors.Errorf("解析汇丰银行信用卡明细，表格与契约不一致")
	}

	cleanedPdf, err := preprocessPdf(data)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析汇丰银行信用卡明细，预处理PDF异常")
		return nil, errors.Errorf("解析汇丰银行信用卡明细，预处理PDF异常: %+v", err)
	}

	reader, err := pdf.NewReader(bytes.NewReader(cleanedPdf), int64(len(cleanedPdf)))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析汇丰银行信用卡明细，打开PDF异常")
		return nil, errors.Errorf("解析汇丰银行信用卡明细，打开PDF异常: %+v", err)
	}

	if reader.NumPage() == 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析汇丰银行信用卡明细，页面为空")
		return nil, errors.Errorf("解析汇丰银行信用卡明细，页面为空")
	}

	stmtYear, stmtMonth, err := extractStatementDate(reader)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析汇丰银行信用卡明细，账单日期非法")
		return nil, errors.Errorf("解析汇丰银行信用卡明细，%s", err)
	}

	var allExpenses []*model.Expense
	var lastCardLast4 string

	for p := 1; p <= reader.NumPage(); p++ {
		page := reader.Page(p)
		if page.V.IsNull() {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": p}).Warn("解析汇丰银行信用卡明细，页面为空")
			return nil, errors.Errorf("解析汇丰银行信用卡明细，第%d页为空", p)
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": p, "err": err}).Warn("解析汇丰银行信用卡明细，读取页面文本异常")
			return nil, errors.Errorf("解析汇丰银行信用卡明细，第%d页读取页面文本异常: %+v", p, err)
		}

		pageCardLast4 := ""
		if m := card16Regexp.FindStringSubmatch(text); len(m) == 2 {
			pageCardLast4 = m[1][len(m[1])-4:]
			lastCardLast4 = pageCardLast4
		} else {
			pageCardLast4 = lastCardLast4
		}

		chunks := extractPageChunks(page)
		if len(chunks) == 0 {
			continue
		}

		pageExpenses, err := parsePageTransactions(ctx, p, chunks, stmtYear, stmtMonth, pageCardLast4)
		if err != nil {
			return nil, err
		}
		allExpenses = append(allExpenses, pageExpenses...)
	}

	return allExpenses, nil
}

func extractStatementDate(reader *pdf.Reader) (int, time.Month, error) {
	for p := 1; p <= reader.NumPage(); p++ {
		text, err := reader.Page(p).GetPlainText(nil)
		if err != nil {
			continue
		}
		if m := stmtDateRegexp.FindStringSubmatch(text); len(m) == 4 {
			year, _ := strconv.Atoi(m[3])
			mon := monthMap[strings.ToUpper(m[2])]
			if year > 2000 && mon > 0 {
				return year, mon, nil
			}
		}
	}
	return 0, 0, errors.Errorf("未能从对账单中解析出账单日期")
}

func parsePageTransactions(ctx context.Context, pageNum int, chunks []textChunk, stmtYear int, stmtMonth time.Month, cardLast4 string) ([]*model.Expense, error) {
	lines := groupChunksByLine(chunks, 2.5)
	currencyCode := ""
	for _, line := range lines {
		lt := lineToText(line)
		if strings.Contains(lt, subAccountRMB) || strings.Contains(lt, "Amount (CNY)") || strings.Contains(lt, "Amount(CNY)") {
			currencyCode = currencyRMB
			break
		} else if strings.Contains(lt, subAccountHKD) || strings.Contains(lt, "Amount (HKD)") || strings.Contains(lt, "Amount(HKD)") {
			currencyCode = currencyHKD
			break
		}
	}

	if currencyCode == "" {
		return nil, nil
	}

	var tableHeaderY float64
	for _, line := range lines {
		lt := lineToText(line)
		ltNoSpace := strings.ReplaceAll(lt, " ", "")
		if (strings.Contains(ltNoSpace, "Postdate") || strings.Contains(ltNoSpace, "PostDate")) &&
			(strings.Contains(ltNoSpace, "Transdate") || strings.Contains(ltNoSpace, "TransDate")) {
			tableHeaderY = line[0].y
			break
		}
	}

	if tableHeaderY == 0 {
		return nil, nil
	}

	var tableFooterY float64
	for _, line := range lines {
		if line[0].y >= tableHeaderY {
			continue
		}
		lt := lineToText(line)
		ltNoSpace := strings.ReplaceAll(lt, " ", "")
		if strings.Contains(ltNoSpace, "meansCredittransaction") ||
			strings.Contains(ltNoSpace, "REWARDCASHSUMMARY") ||
			strings.Contains(ltNoSpace, "TRANSACTIONSUMMARY") ||
			strings.Contains(ltNoSpace, "FEESANDCHARGES") ||
			strings.Contains(ltNoSpace, "Minimumpayment") ||
			strings.Contains(ltNoSpace, "Informationformaking") ||
			strings.Contains(ltNoSpace, "Forimportantinformation") ||
			strings.Contains(ltNoSpace, "lostcardreporting") {
			tableFooterY = line[0].y
			break
		}
	}

	var rawRows []txRow
	for _, line := range lines {
		y := line[0].y
		if y >= tableHeaderY || (tableFooterY > 0 && y <= tableFooterY) {
			continue
		}

		slices.SortFunc(line, func(a, b textChunk) int {
			return int(math.Round((a.x - b.x) * 10))
		})

		var postDateB, transDateB, descB, amtB, crB strings.Builder
		var lastDescX float64
		for _, c := range line {
			if c.x < 96 {
				postDateB.WriteString(c.text)
			} else if c.x < 132 {
				transDateB.WriteString(c.text)
			} else if c.x < 460 {
				if descB.Len() > 0 && c.x-lastDescX > 5 {
					descB.WriteString(" ")
				}
				descB.WriteString(c.text)
				lastDescX = c.x + float64(len(c.text))*4
			} else if c.x < 555 {
				amtB.WriteString(c.text)
			} else {
				crB.WriteString(c.text)
			}
		}

		pDate := strings.TrimSpace(postDateB.String())
		tDate := strings.TrimSpace(transDateB.String())
		desc := strings.TrimSpace(descB.String())
		amt := strings.TrimSpace(amtB.String())
		cr := strings.TrimSpace(crB.String())

		if strings.Contains(desc, "PREVIOUS BALANCE") || strings.Contains(desc, "Sub-account") {
			continue
		}

		rawRows = append(rawRows, txRow{
			y:         y,
			postDate:  pDate,
			transDate: tDate,
			desc:      desc,
			amount:    amt,
			isCR:      strings.Contains(cr, flagCR),
		})
	}

	var txList []txRow
	for _, r := range rawRows {
		isDateRow := isTxDate(r.postDate) || isTxDate(r.transDate)
		if isDateRow && r.amount != "" {
			txList = append(txList, r)
		} else if len(txList) > 0 && r.desc != "" {
			last := &txList[len(txList)-1]
			last.desc = strings.TrimSpace(last.desc + " " + r.desc)
		}
	}

	var expenses []*model.Expense
	for i, tx := range txList {
		isRepayment := tx.isCR && slices.ContainsFunc(repaymentWords, func(word string) bool {
			return strings.Contains(strings.ToUpper(tx.desc), word)
		})
		if isRepayment {
			continue
		}

		dateStr := tx.transDate
		if !isTxDate(dateStr) {
			dateStr = tx.postDate
		}
		expDate, err := parseTxDate(dateStr, stmtYear, stmtMonth)
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageNum, "index": i + 1, "date": dateStr}).Warn("解析汇丰银行信用卡明细，支出日期非法")
			return nil, errors.Errorf("解析汇丰银行信用卡明细，第%d页第%d笔，支出日期非法: %s", pageNum, i+1, dateStr)
		}

		amtDec, err := decimal.NewFromString(strings.ReplaceAll(tx.amount, ",", ""))
		if err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageNum, "index": i + 1, "amount": tx.amount, "err": err}).Warn("解析汇丰银行信用卡明细，支出金额非法")
			return nil, errors.Errorf("解析汇丰银行信用卡明细，第%d页第%d笔，支出金额非法: %s", pageNum, i+1, tx.amount)
		}

		var remark string
		if tx.isCR {
			amtDec = amtDec.Abs().Neg()
			remark = summaryRefund
		} else {
			amtDec = amtDec.Abs()
			remark = summaryPurchase
		}

		if cardLast4 == "" {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"page": pageNum, "index": i + 1}).Warn("解析汇丰银行信用卡明细，交易卡号非法")
			return nil, errors.Errorf("解析汇丰银行信用卡明细，第%d页第%d笔，交易卡号非法", pageNum, i+1)
		}

		exp := &model.Expense{
			BankName:        bankName,
			CardLast4:       cardLast4,
			ExpenseDate:     expDate,
			ExpenseCurrency: currencyCode,
			ExpenseAmount:   amtDec,
			Counterparty:    tx.desc,
			Remark:          remark,
		}
		expenses = append(expenses, exp)
	}

	return expenses, nil
}

func isTxDate(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	return dateMonthRegexp.MatchString(s)
}

func parseTxDate(s string, stmtYear int, stmtMonth time.Month) (time.Time, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	m := dateMonthRegexp.FindStringSubmatch(s)
	if len(m) < 3 {
		return time.Time{}, errors.Errorf("无法解析日期: %s", s)
	}
	day, err := strconv.Atoi(m[1])
	if err != nil || day < 1 || day > 31 {
		return time.Time{}, errors.Errorf("日非法: %s", m[1])
	}
	mon, ok := monthMap[m[2]]
	if !ok {
		return time.Time{}, errors.Errorf("月非法: %s", m[2])
	}

	year := stmtYear
	if mon > stmtMonth && (mon-stmtMonth > 6) {
		year--
	}
	return time.Date(year, mon, day, 0, 0, 0, 0, time.Local), nil
}

func lineToText(line []textChunk) string {
	slices.SortFunc(line, func(a, b textChunk) int {
		return int(math.Round((a.x - b.x) * 10))
	})
	var b strings.Builder
	var lastX float64
	for i, c := range line {
		if i > 0 && c.x-lastX > 4 {
			b.WriteString(" ")
		}
		b.WriteString(c.text)
		lastX = c.x + float64(len(c.text))*4
	}
	return b.String()
}

func groupChunksByLine(chunks []textChunk, tolerance float64) [][]textChunk {
	slices.SortFunc(chunks, func(a, b textChunk) int {
		if math.Abs(a.y-b.y) > tolerance {
			if a.y < b.y {
				return 1
			}
			return -1
		}
		return int(math.Round((a.x - b.x) * 10))
	})

	var lines [][]textChunk
	for _, c := range chunks {
		if len(lines) == 0 {
			lines = append(lines, []textChunk{c})
			continue
		}
		lastLine := &lines[len(lines)-1]
		if math.Abs((*lastLine)[0].y-c.y) <= tolerance {
			*lastLine = append(*lastLine, c)
		} else {
			lines = append(lines, []textChunk{c})
		}
	}
	return lines
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
			if len(args) == 1 {
				text := args[0].RawString()
				if curFont.Encoder() != nil {
					text = curFont.Encoder().Decode(text)
				}
				chunks = append(chunks, textChunk{x: curX, y: curY, text: text})
			}
		case "TJ":
			if len(args) == 1 {
				var buffer bytes.Buffer
				for i := 0; i < args[0].Len(); i++ {
					value := args[0].Index(i)
					if value.Kind() == pdf.String {
						text := value.RawString()
						if curFont.Encoder() != nil {
							text = curFont.Encoder().Decode(text)
						}
						buffer.WriteString(text)
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

func preprocessPdf(data []byte) ([]byte, error) {
	pdfBytes := slices.Clone(data)

	trailerIdx := bytes.LastIndex(pdfBytes, []byte("trailer"))
	if trailerIdx >= 0 && bytes.Contains(pdfBytes[trailerIdx:], []byte("/Encrypt")) {
		var err error
		pdfBytes, err = decryptPdf(pdfBytes)
		if err != nil {
			return nil, err
		}
	}

	pdfBytes = stripInlineImagesFromPdf(pdfBytes)
	return pdfBytes, nil
}

func decryptPdf(data []byte) ([]byte, error) {
	trailerIdx := bytes.LastIndex(data, []byte("trailer"))
	if trailerIdx < 0 {
		return nil, fmt.Errorf("missing trailer")
	}

	idIdx := bytes.Index(data[trailerIdx:], []byte("/ID"))
	if idIdx < 0 {
		return nil, fmt.Errorf("missing /ID in trailer")
	}
	idSection := data[trailerIdx+idIdx:]
	idStart := bytes.IndexByte(idSection, '<')
	idEnd := bytes.IndexByte(idSection[idStart+1:], '>')
	if idStart < 0 || idEnd < 0 {
		return nil, fmt.Errorf("malformed /ID in trailer")
	}
	idHex := string(idSection[idStart+1 : idStart+1+idEnd])
	idBytes := make([]byte, len(idHex)/2)
	for i := 0; i < len(idBytes); i++ {
		b, _ := strconv.ParseUint(idHex[i*2:i*2+2], 16, 8)
		idBytes[i] = byte(b)
	}

	encIdx := bytes.Index(data[trailerIdx:], []byte("/Encrypt"))
	encSection := data[trailerIdx+encIdx:]
	encParts := strings.Fields(string(encSection[:bytes.IndexByte(encSection, '\n')]))
	if len(encParts) < 4 {
		return nil, fmt.Errorf("malformed /Encrypt reference")
	}
	encObjId, _ := strconv.Atoi(encParts[1])
	encGenId, _ := strconv.Atoi(encParts[2])

	encTarget := []byte(fmt.Sprintf("%d %d obj", encObjId, encGenId))
	objPos := bytes.Index(data, encTarget)
	if objPos < 0 {
		return nil, fmt.Errorf("encrypt object not found")
	}
	endObjPos := bytes.Index(data[objPos:], []byte("endobj"))
	encObjContent := data[objPos : objPos+endObjPos]

	oRaw := extractPdfString(encObjContent, []byte("/O"))
	pVal := extractPdfInt(encObjContent, []byte("/P"))
	pBytes := []byte{byte(uint32(pVal)), byte(uint32(pVal) >> 8), byte(uint32(pVal) >> 16), byte(uint32(pVal) >> 24)}

	h := md5.New()
	h.Write(passwordPad)
	h.Write(oRaw)
	h.Write(pBytes)
	h.Write(idBytes)
	masterKey := h.Sum(nil)[:5]

	res := slices.Clone(data)
	matches := objHeaderRegexp.FindAllSubmatchIndex(data, -1)
	for _, m := range matches {
		objId, _ := strconv.Atoi(string(data[m[2]:m[3]]))
		genId, _ := strconv.Atoi(string(data[m[4]:m[5]]))
		objStart := m[0]
		endobjPos := bytes.Index(data[objStart:], []byte("endobj"))
		if endobjPos < 0 {
			continue
		}
		endobjPos += objStart

		streamIdx := bytes.Index(data[objStart:endobjPos], []byte("stream"))
		if streamIdx < 0 {
			continue
		}
		streamIdx += objStart

		streamDataStart := streamIdx + 6
		if bytes.HasPrefix(data[streamDataStart:], []byte("\r\n")) {
			streamDataStart += 2
		} else if bytes.HasPrefix(data[streamDataStart:], []byte("\n")) {
			streamDataStart += 1
		}

		endstreamIdx := bytes.Index(data[streamDataStart:endobjPos], []byte("endstream"))
		if endstreamIdx < 0 {
			continue
		}
		streamDataEnd := streamDataStart + endstreamIdx
		for streamDataEnd > streamDataStart && (data[streamDataEnd-1] == '\r' || data[streamDataEnd-1] == '\n') {
			streamDataEnd--
		}

		h = md5.New()
		h.Write(masterKey)
		h.Write([]byte{byte(objId), byte(objId >> 8), byte(objId >> 16), byte(genId), byte(genId >> 8)})
		objKey := h.Sum(nil)[:10]

		c, _ := rc4.NewCipher(objKey)
		encData := data[streamDataStart:streamDataEnd]
		decData := make([]byte, len(encData))
		c.XORKeyStream(decData, encData)
		copy(res[streamDataStart:streamDataEnd], decData)
	}

	tIdx := bytes.LastIndex(res, []byte("trailer"))
	eIdx := bytes.Index(res[tIdx:], []byte("/Encrypt"))
	if eIdx >= 0 {
		nlIdx := bytes.IndexByte(res[tIdx+eIdx:], '\n')
		for i := 0; i < nlIdx; i++ {
			res[tIdx+eIdx+i] = ' '
		}
	}

	return res, nil
}

func extractPdfString(content, key []byte) []byte {
	idx := bytes.Index(content, key)
	if idx < 0 {
		return nil
	}
	parenStart := bytes.IndexByte(content[idx:], '(')
	if parenStart < 0 {
		return nil
	}
	start := idx + parenStart + 1
	var res []byte
	depth := 1
	for i := start; i < len(content); i++ {
		c := content[i]
		if c == '\\' && i+1 < len(content) {
			i++
			next := content[i]
			switch next {
			case 'n':
				res = append(res, '\n')
			case 'r':
				res = append(res, '\r')
			case 't':
				res = append(res, '\t')
			case 'b':
				res = append(res, '\b')
			case 'f':
				res = append(res, '\f')
			case '(', ')', '\\':
				res = append(res, next)
			default:
				if next >= '0' && next <= '7' {
					oct := int(next - '0')
					if i+1 < len(content) && content[i+1] >= '0' && content[i+1] <= '7' {
						i++
						oct = oct*8 + int(content[i]-'0')
						if i+1 < len(content) && content[i+1] >= '0' && content[i+1] <= '7' {
							i++
							oct = oct*8 + int(content[i]-'0')
						}
					}
					res = append(res, byte(oct))
				} else {
					res = append(res, next)
				}
			}
			continue
		}
		if c == '(' {
			depth++
			res = append(res, c)
		} else if c == ')' {
			depth--
			if depth == 0 {
				break
			}
			res = append(res, c)
		} else {
			res = append(res, c)
		}
	}
	return res
}

func extractPdfInt(content, key []byte) int64 {
	idx := bytes.Index(content, key)
	if idx < 0 {
		return 0
	}
	f := strings.Fields(string(content[idx:]))
	if len(f) < 2 {
		return 0
	}
	v, _ := strconv.ParseInt(f[1], 10, 64)
	return v
}

func stripInlineImagesFromPdf(data []byte) []byte {
	res := slices.Clone(data)
	matches := objHeaderRegexp.FindAllSubmatchIndex(data, -1)
	for _, m := range matches {
		objStart := m[0]
		endobjPos := bytes.Index(data[objStart:], []byte("endobj"))
		if endobjPos < 0 {
			continue
		}
		endobjPos += objStart

		streamIdx := bytes.Index(data[objStart:endobjPos], []byte("stream"))
		if streamIdx < 0 {
			continue
		}
		streamIdx += objStart

		streamDataStart := streamIdx + 6
		if bytes.HasPrefix(data[streamDataStart:], []byte("\r\n")) {
			streamDataStart += 2
		} else if bytes.HasPrefix(data[streamDataStart:], []byte("\n")) {
			streamDataStart += 1
		}

		endstreamIdx := bytes.Index(data[streamDataStart:endobjPos], []byte("endstream"))
		if endstreamIdx < 0 {
			continue
		}
		streamDataEnd := streamDataStart + endstreamIdx
		for streamDataEnd > streamDataStart && (data[streamDataEnd-1] == '\r' || data[streamDataEnd-1] == '\n') {
			streamDataEnd--
		}

		rawStream := data[streamDataStart:streamDataEnd]
		zr, err := zlib.NewReader(bytes.NewReader(rawStream))
		if err != nil {
			continue
		}
		decomp, err := io.ReadAll(zr)
		zr.Close()
		if err != nil {
			continue
		}

		if bytes.Contains(decomp, []byte("BI")) {
			cleaned := stripInlineImages(decomp)
			if len(cleaned) != len(decomp) {
				var buf bytes.Buffer
				zw := zlib.NewWriter(&buf)
				zw.Write(cleaned)
				zw.Close()
				recomp := buf.Bytes()
				if len(recomp) <= len(rawStream) {
					copy(res[streamDataStart:], recomp)
					for i := len(recomp); i < len(rawStream); i++ {
						res[streamDataStart+i] = ' '
					}
				}
			}
		}
	}
	return res
}

func stripInlineImages(decomp []byte) []byte {
	res := make([]byte, 0, len(decomp))
	pos := 0
	for {
		biIdx := bytes.Index(decomp[pos:], []byte("\nBI\n"))
		if biIdx < 0 {
			biIdx = bytes.Index(decomp[pos:], []byte("\r\nBI\r\n"))
		}
		if biIdx < 0 {
			res = append(res, decomp[pos:]...)
			break
		}
		actualBi := pos + biIdx
		res = append(res, decomp[pos:actualBi]...)

		idIdx := bytes.Index(decomp[actualBi:], []byte("\nID"))
		if idIdx < 0 {
			idIdx = bytes.Index(decomp[actualBi:], []byte("\r\nID"))
		}
		if idIdx < 0 {
			res = append(res, decomp[actualBi:]...)
			break
		}
		actualId := actualBi + idIdx

		eiIdx := bytes.Index(decomp[actualId:], []byte("EI Q"))
		if eiIdx < 0 {
			eiIdx = bytes.Index(decomp[actualId:], []byte("\nEI"))
		}
		if eiIdx < 0 {
			eiIdx = bytes.Index(decomp[actualId:], []byte("\r\nEI"))
		}
		if eiIdx < 0 {
			res = append(res, decomp[actualBi:]...)
			break
		}
		actualEi := actualId + eiIdx
		if bytes.HasPrefix(decomp[actualEi:], []byte("EI Q")) {
			pos = actualEi + 2
		} else if bytes.HasPrefix(decomp[actualEi:], []byte("\r\nEI")) {
			pos = actualEi + 4
		} else {
			pos = actualEi + 3
		}
	}
	return res
}
