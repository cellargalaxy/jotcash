package unionpay_screenshot

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/expense"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

//go:embed ocr.py
var ocrPyScript []byte

//go:embed requirements.txt
var ocrRequirements string

const (
	currencyCNY     = "CNY"
	summaryPurchase = "消费"
	summaryRefund   = "退款"
)

var (
	bankNameMap = map[string]string{
		"滙豐":   "汇丰银行",
		"汇丰":   "汇丰银行",
		"招商":   "招商银行",
		"招行":   "招商银行",
		"工商":   "工商银行",
		"工行":   "工商银行",
		"中行":   "中国银行",
		"中国银行": "中国银行",
		"建设":   "建设银行",
		"建行":   "建设银行",
		"交通":   "交通银行",
		"交行":   "交通银行",
		"农业":   "农业银行",
		"农行":   "农业银行",
		"平安":   "平安银行",
		"中信":   "中信银行",
		"广发":   "广发银行",
		"光大":   "光大银行",
		"民生":   "民生银行",
		"浦发":   "浦发银行",
		"兴业":   "兴业银行",
		"邮储":   "邮政储蓄银行",
		"邮政":   "邮政储蓄银行",
		"华夏":   "华夏银行",
	}

	dateRegex           = regexp.MustCompile(`^(\d{1,2})\.(\d{1,2})\s*周[一二三四五六日天—\-]`)
	yearRegex           = regexp.MustCompile(`(\d{4})年`)
	monthSummaryRegex   = regexp.MustCompile(`^\d{1,2}月$`)
	monthHeaderRegex    = regexp.MustCompile(`^(\d{1,2})月$`)
	summaryAmountRegex  = regexp.MustCompile(`^(?:支出|收入)\s*￥`)
	cardRegex           = regexp.MustCompile(`([\p{Han}A-Za-z]+)\[(\d{4})\][］]?\s*·\s*(\d{1,2}:\d{2})`)
	amountRegex         = regexp.MustCompile(`([+\-]?)\s*￥\s*([\d,]+\.\d{2})`)
	amountFallbackRegex = regexp.MustCompile(`^([+\-]?)\s*([\d,]+\.\d{2})$`)

	repaymentKeywords = []string{"还款", "信用卡还款", "转账", "划转"}

	ocrCache      sync.Map
	ocrScriptPath string
	ocrScriptOnce sync.Once
)

func init() {
	expense.Register(new(Parser))
}

type Parser struct {
}

type OcrBlock struct {
	Text  string  `json:"text"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Score float64 `json:"score"`
}

func (this *Parser) Support(ctx context.Context, data []byte) bool {
	if !isImage(data) {
		return false
	}
	blocks, err := runOcr(ctx, data)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("识别云闪付截图，OCR识别异常")
		return false
	}
	if len(blocks) == 0 {
		return false
	}
	return checkContract(blocks)
}

func (this *Parser) Parse(ctx context.Context, data []byte) ([]*model.Expense, error) {
	if !isImage(data) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析云闪付截图明细，非图片格式")
		return nil, errors.Errorf("解析云闪付截图明细，非图片格式")
	}

	blocks, err := runOcr(ctx, data)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Warn("解析云闪付截图明细，OCR识别异常")
		return nil, errors.Errorf("解析云闪付截图明细，OCR识别异常: %+v", err)
	}

	if !checkContract(blocks) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析云闪付截图明细，页面与契约不一致")
		return nil, errors.Errorf("解析云闪付截图明细，页面与契约不一致")
	}

	expenses, err := parseBlocks(ctx, blocks)
	if err != nil {
		return nil, err
	}
	if len(expenses) == 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("解析云闪付截图明细，未识别到有效交易")
		return nil, errors.Errorf("解析云闪付截图明细，未识别到有效交易")
	}
	return expenses, nil
}

func isImage(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	if bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
		return true
	}
	if bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return true
	}
	return false
}

func checkContract(blocks []OcrBlock) bool {
	var hasRecordTitle bool
	var hasUnionPay bool
	var hasFilterAll bool
	var hasFilterType bool

	for i := range blocks {
		if blocks[i].Y > 800 {
			continue
		}
		t := strings.TrimSpace(blocks[i].Text)
		if strings.Contains(t, "交易记录") {
			hasRecordTitle = true
		}
		if strings.Contains(t, "银联交易") || strings.Contains(t, "云闪付") {
			hasUnionPay = true
		}
		if t == "全部" || strings.Contains(t, "全部") {
			hasFilterAll = true
		}
		if strings.Contains(t, "支出") || strings.Contains(t, "收入") {
			hasFilterType = true
		}
	}
	return hasRecordTitle && hasUnionPay && hasFilterAll && hasFilterType
}

func parseBlocks(ctx context.Context, rawBlocks []OcrBlock) ([]*model.Expense, error) {
	blocks := make([]OcrBlock, len(rawBlocks))
	copy(blocks, rawBlocks)

	slices.SortFunc(blocks, func(a, b OcrBlock) int {
		if a.Y != b.Y {
			if a.Y < b.Y {
				return -1
			}
			return 1
		}
		if a.X < b.X {
			return -1
		}
		return 1
	})

	stmtYear := time.Now().Year()
	for i := range blocks {
		if blocks[i].Y > 800 {
			break
		}
		matches := yearRegex.FindStringSubmatch(blocks[i].Text)
		if len(matches) >= 2 {
			if y, err := strconv.Atoi(matches[1]); err == nil {
				stmtYear = y
				break
			}
		}
	}

	var curYear, curMonth, curDay int
	curYear = stmtYear

	var expenses []*model.Expense

	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		t := strings.TrimSpace(b.Text)

		if dm := dateRegex.FindStringSubmatch(t); len(dm) >= 3 {
			m, _ := strconv.Atoi(dm[1])
			d, _ := strconv.Atoi(dm[2])
			curMonth = m
			curDay = d
			curYear = stmtYear
			if curMonth == 12 && stmtYear > 0 {
				// 若账单年为1月，而出现12月流水，归为上一年
				for j := range blocks {
					if blocks[j].Y > 800 {
						break
					}
					if strings.Contains(blocks[j].Text, "1月") {
						curYear = stmtYear - 1
						break
					}
				}
			}
			continue
		}

		if monthSummaryRegex.MatchString(t) || summaryAmountRegex.MatchString(t) {
			continue
		}

		cm := cardRegex.FindStringSubmatch(t)
		if len(cm) < 4 {
			continue
		}

		rawBank := cm[1]
		cardLast4 := cm[2]
		timeStr := cm[3]

		timeParts := strings.Split(timeStr, ":")
		if len(timeParts) < 2 {
			continue
		}
		hour, _ := strconv.Atoi(timeParts[0])
		minute, _ := strconv.Atoi(timeParts[1])

		if curMonth == 0 || curDay == 0 {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"block": t}).Warn("解析云闪付截图明细，支出日期缺失")
			return nil, errors.Errorf("解析云闪付截图明细，支出日期缺失")
		}
		expenseDate := time.Date(curYear, time.Month(curMonth), curDay, hour, minute, 0, 0, time.Local)

		// 检查卡行附近的状态标签（退款、冻结、解冻）
		var tag string
		for j := max(0, i-3); j < min(len(blocks), i+4); j++ {
			if math.Abs(blocks[j].Y-b.Y) < 60 && blocks[j].X >= 800 {
				txt := strings.TrimSpace(blocks[j].Text)
				if txt == "退款" || txt == "冻结" || txt == "解冻" {
					tag = txt
					break
				}
			}
		}

		// 冻结与解冻为预授权临时变动，不计入实际支出
		if tag == "冻结" || tag == "解冻" {
			continue
		}

		// 向上回溯同一笔交易的商户与金额
		var counterparties []OcrBlock
		var amount decimal.Decimal
		var hasAmount bool
		var isNegative bool
		var hasExplicitSign bool

		for k := i - 1; k >= 0; k-- {
			prev := blocks[k]
			if prev.Y < b.Y-160 {
				break
			}
			if cardRegex.MatchString(prev.Text) {
				break
			}
			if dateRegex.MatchString(prev.Text) {
				break
			}
			prevText := strings.TrimSpace(prev.Text)
			if prevText == "退款" || prevText == "冻结" || prevText == "解冻" {
				continue
			}

			if am := amountRegex.FindStringSubmatch(prevText); len(am) >= 3 && !hasAmount {
				sign := am[1]
				valStr := strings.ReplaceAll(am[2], ",", "")
				d, err := decimal.NewFromString(valStr)
				if err == nil {
					amount = d
					hasAmount = true
					hasExplicitSign = (sign != "")
					isNegative = (sign == "-")
				}
				continue
			}

			if !hasAmount && prev.X >= 700 {
				if fam := amountFallbackRegex.FindStringSubmatch(prevText); len(fam) >= 3 {
					sign := fam[1]
					valStr := strings.ReplaceAll(fam[2], ",", "")
					d, err := decimal.NewFromString(valStr)
					if err == nil {
						amount = d
						hasAmount = true
						hasExplicitSign = (sign != "")
						isNegative = (sign == "-")
					}
					continue
				}
			}

			if prev.X < 700 {
				counterparties = append(counterparties, prev)
			}
		}

		if !hasAmount {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"block": t, "date": expenseDate}).Warn("解析云闪付截图明细，支出金额缺失")
			return nil, errors.Errorf("解析云闪付截图明细，支出金额缺失")
		}

		slices.SortFunc(counterparties, func(p1, p2 OcrBlock) int {
			if p1.X < p2.X {
				return -1
			}
			return 1
		})

		var cpBuilder strings.Builder
		for cpIdx := range counterparties {
			cpBuilder.WriteString(strings.TrimSpace(counterparties[cpIdx].Text))
		}
		counterparty := cpBuilder.String()
		if counterparty == "" {
			counterparty = "云闪付消费"
		}

		// 检查信用卡还款或账户互转安全过滤
		if isRepayment(counterparty) {
			continue
		}

		var remark string
		var expenseAmount decimal.Decimal
		if tag == "退款" || (hasExplicitSign && !isNegative) {
			expenseAmount = amount.Abs().Neg()
			remark = summaryRefund
		} else {
			expenseAmount = amount.Abs()
			remark = summaryPurchase
		}

		bankName := normalizeBank(rawBank)

		expenses = append(expenses, &model.Expense{
			BankName:        bankName,
			CardLast4:       cardLast4,
			ExpenseDate:     expenseDate,
			ExpenseCurrency: currencyCNY,
			ExpenseAmount:   expenseAmount,
			Counterparty:    counterparty,
			Remark:          remark,
		})
	}

	return expenses, nil
}

func isRepayment(text string) bool {
	for i := range repaymentKeywords {
		if strings.Contains(text, repaymentKeywords[i]) {
			return true
		}
	}
	return false
}

func normalizeBank(raw string) string {
	raw = strings.TrimSpace(raw)
	for k, v := range bankNameMap {
		if strings.Contains(raw, k) {
			return v
		}
	}
	if strings.HasSuffix(raw, "银行") {
		return raw
	}
	return raw + "银行"
}

func runOcr(ctx context.Context, data []byte) ([]OcrBlock, error) {
	hash := util.EnSha256Hex(string(data))
	if val, ok := ocrCache.Load(hash); ok {
		return val.([]OcrBlock), nil
	}

	pythonPath, err := getPythonPath()
	if err != nil {
		return nil, err
	}
	scriptPath, err := getOcrScriptPath()
	if err != nil {
		return nil, err
	}

	_, version, _ := strings.Cut(strings.TrimSpace(ocrRequirements), "==")
	cmd := exec.CommandContext(ctx, pythonPath, scriptPath, version)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, errors.Errorf("执行本地OCR异常: %v, %s", err, stderr.String())
	}

	var blocks []OcrBlock
	if err := json.Unmarshal(stdout.Bytes(), &blocks); err != nil {
		return nil, errors.Errorf("解析OCR输出JSON异常: %v", err)
	}

	ocrCache.Store(hash, blocks)
	return blocks, nil
}

func getPythonPath() (string, error) {
	if env := os.Getenv("JOTCASH_PYTHON"); env != "" {
		if fi, err := os.Stat(env); err == nil && !fi.IsDir() {
			return env, nil
		}
	}
	if p, err := exec.LookPath("python3"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("python"); err == nil {
		return p, nil
	}
	return "", errors.Errorf("未找到本地 python3 解释器")
}

func getOcrScriptPath() (string, error) {
	var err error
	ocrScriptOnce.Do(func() {
		_, callerFile, _, ok := runtime.Caller(0)
		if ok {
			candidate := filepath.Join(filepath.Dir(callerFile), "ocr.py")
			if fi, e := os.Stat(candidate); e == nil && !fi.IsDir() {
				ocrScriptPath = candidate
				return
			}
		}
		workingCandidate := "service/expense/unionpay_screenshot/ocr.py"
		if fi, e := os.Stat(workingCandidate); e == nil && !fi.IsDir() {
			abs, e := filepath.Abs(workingCandidate)
			if e == nil {
				ocrScriptPath = abs
				return
			}
		}
		tmpFile := filepath.Join(os.TempDir(), "jotcash_unionpay_ocr.py")
		if writeErr := os.WriteFile(tmpFile, ocrPyScript, 0755); writeErr != nil {
			err = errors.Errorf("释放临时OCR脚本异常: %v", writeErr)
			return
		}
		ocrScriptPath = tmpFile
	})
	if err != nil {
		return "", err
	}
	if ocrScriptPath == "" {
		return "", errors.Errorf("未找到有效OCR脚本路径")
	}
	return ocrScriptPath, nil
}
