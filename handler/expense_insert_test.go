package handler_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	common_model "github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/gin-gonic/gin"
)

const testCsvHeader = "银行名称,卡号后四位,支出日期,支出币种,支出金额,交易对手方,交易备注,折算汇率,支出类型\n"

func insertExpense(t *testing.T, engine *gin.Engine, jwt, filename, csv string) common_model.HttpResp {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	if filename != "" {
		part, err := writer.CreateFormFile(model.ExpenseFileKey, filename)
		if err != nil {
			t.Fatalf("构造上传表单异常: %+v", err)
		}
		if _, err = part.Write([]byte(csv)); err != nil {
			t.Fatalf("写上传表单异常: %+v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭上传表单异常: %+v", err)
	}
	request := httptest.NewRequest(http.MethodPost, model.PathExpenseInsert, body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if jwt != "" {
		request.Header.Set(util.AuthorizationKey, util.BearerKey+" "+jwt)
	}
	var resp common_model.HttpResp
	doRequest(t, engine, request, &resp)
	return resp
}

func TestInsertExpense(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)

	csv := testCsvHeader +
		"招商银行,6789,2026-01-02,CNY,100.50,亚马逊,买书,,购物\n" +
		"中国银行,4321,2026-03-04,USD,-20.25,苹果,退款,7.1234,数码\n"
	resp := insertExpense(t, engine, jwt, "2609.csv", csv)
	if resp.Code != http.StatusOK {
		t.Fatalf("入库应成功: %+v", resp)
	}

	list := selectExpense(t, engine, jwt, model.ExpenseInquiry{Sort: "expense_date asc"})
	if list.Data.Count != 2 {
		t.Fatalf("应入库2笔: %+v", list.Data)
	}
	first, second := list.Data.Object[0], list.Data.Object[1]

	//同币种的折算汇率恒为1，记账金额=支出金额×折算汇率
	if !first.ExchangeRate.Equal(decimalOf(t, "1")) || !first.AccountingAmount.Equal(first.ExpenseAmount) {
		t.Errorf("同币种的折算不符: rate=%s amount=%s", first.ExchangeRate, first.AccountingAmount)
	}
	//CSV里给了汇率就用CSV的
	if !second.ExchangeRate.Equal(decimalOf(t, "7.1234")) {
		t.Errorf("应采用CSV里的折算汇率: %s", second.ExchangeRate)
	}
	if !second.AccountingAmount.Equal(second.ExpenseAmount.Mul(second.ExchangeRate)) {
		t.Errorf("记账金额应等于支出金额×折算汇率: %s", second.AccountingAmount)
	}
	//记账币种取jwt里携带的那个
	if second.AccountingCurrency != testAccountingCurrency {
		t.Errorf("记账币种应取jwt携带值: %s", second.AccountingCurrency)
	}
	//摊分月数默认1，起始月=支出日期所属月，结束月=起始月+月数-1
	if first.AmortizationMonths != 1 {
		t.Errorf("摊分月数应默认1: %d", first.AmortizationMonths)
	}
	ctx := util.GenCtx()
	startMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, first.AmortizationStartMonth, nil)
	endMonth := util.Time2Str(ctx, util.DateLayout_2006_01_02, first.AmortizationEndMonth, nil)
	if startMonth != "2026-01-01" || endMonth != "2026-01-01" {
		t.Errorf("摊分起止月不符: %s %s", startMonth, endMonth)
	}
	//可选列有值就带上，金额往返不丢精度
	if first.BankName != "招商银行" || first.Counterparty != "亚马逊" || first.ExpenseType != "购物" {
		t.Errorf("可选列没落库: %+v", first)
	}
	if !second.ExpenseAmount.Equal(decimalOf(t, "-20.25")) {
		t.Errorf("负数金额不符: %s", second.ExpenseAmount)
	}
}

// 一条「数据入库」审计 + 一份CSV存档，明细的来源字段都指向它们
func TestInsertExpenseArchive(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)

	csv := testCsvHeader + "招商银行,6789,2026-01-02,CNY,100.50,亚马逊,买书,,购物\n"
	if resp := insertExpense(t, engine, jwt, "2609.csv", csv); resp.Code != http.StatusOK {
		t.Fatalf("入库应成功: %+v", resp)
	}

	logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDataEntry}})
	if logs.Data.Count != 1 {
		t.Fatalf("应记一条数据入库审计: %+v", logs.Data)
	}
	if !strings.Contains(logs.Data.Object[0].Summary, "2609.csv") {
		t.Errorf("审计摘要应带来源文件名: %+v", logs.Data.Object[0])
	}

	files := selectFileMeta(t, engine, jwt, model.FileMetaInquiry{OperationId: []int64{logs.Data.Object[0].Id}})
	if files.Data.Count != 1 {
		t.Fatalf("应存档一份CSV: %+v", files.Data)
	}
	if files.Data.Object[0].FileName != "2609.csv" || files.Data.Object[0].FileSize != int64(len(csv)) {
		t.Errorf("存档的文件名与大小不符: %+v", files.Data.Object[0])
	}

	list := selectExpense(t, engine, jwt, model.ExpenseInquiry{})
	if list.Data.Count != 1 {
		t.Fatalf("应入库1笔: %+v", list.Data)
	}
	if list.Data.Object[0].OperationId != logs.Data.Object[0].Id || list.Data.Object[0].FileId != files.Data.Object[0].Id {
		t.Errorf("明细的审计ID与文件ID应指向本次入库: %+v", list.Data.Object[0])
	}

	//存档的是上传的原样CSV，不加工
	blobs, _, err := db.SelectFileBlob(newTokenCtx(clientToken), model.FileBlobInquiry{FileHash: []string{files.Data.Object[0].FileHash}})
	if err != nil || len(blobs) != 1 {
		t.Fatalf("取存档内容异常: err=%+v len=%d", err, len(blobs))
	}
	if string(blobs[0].FileData) != csv {
		t.Errorf("存档内容应与上传的一致: %s", blobs[0].FileData)
	}
}

// 内容寻址：同一份CSV传两次，FileMeta每次都加，FileBlob同内容只存一份
func TestInsertExpenseSameContent(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)

	csv := testCsvHeader + "招商银行,6789,2026-01-02,CNY,100.50,亚马逊,买书,1,购物\n"
	for i := 0; i < 2; i++ {
		if resp := insertExpense(t, engine, jwt, "2609.csv", csv); resp.Code != http.StatusOK {
			t.Fatalf("第%d次入库应成功: %+v", i+1, resp)
		}
	}

	files := selectFileMeta(t, engine, jwt, model.FileMetaInquiry{})
	if files.Data.Count != 2 {
		t.Fatalf("两次入库应有2条文件元数据: %+v", files.Data)
	}
	if files.Data.Object[0].FileHash != files.Data.Object[1].FileHash {
		t.Errorf("同内容的哈希应相同: %+v", files.Data.Object)
	}
	blobs, count, err := db.SelectFileBlob(newTokenCtx(clientToken), model.FileBlobInquiry{})
	if err != nil {
		t.Fatalf("查文件内容异常: %+v", err)
	}
	if count != 1 || len(blobs) != 1 {
		t.Errorf("同内容只该存一份，FileBlob应为1条: count=%d", count)
	}

	if list := selectExpense(t, engine, jwt, model.ExpenseInquiry{}); list.Data.Count != 2 {
		t.Errorf("应入库2笔: %+v", list.Data)
	}
}

// 按列名匹配不依赖顺序，可选列缺失按空
func TestInsertExpenseColumnOrder(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)

	csv := "支出金额,支出币种,支出日期\n100.50,CNY,2026-01-02\n"
	if resp := insertExpense(t, engine, jwt, "2609.csv", csv); resp.Code != http.StatusOK {
		t.Fatalf("只带必填列、顺序打乱也应成功: %+v", resp)
	}
	list := selectExpense(t, engine, jwt, model.ExpenseInquiry{})
	if list.Data.Count != 1 {
		t.Fatalf("应入库1笔: %+v", list.Data)
	}
	if list.Data.Object[0].BankName != "" || list.Data.Object[0].Counterparty != "" {
		t.Errorf("缺失的可选列应为空: %+v", list.Data.Object[0])
	}
}

func TestInsertExpenseInvalid(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)

	cases := map[string]string{
		"未知列":    "支出日期,支出币种,支出金额,乱七八糟\n2026-01-02,CNY,1,x\n",
		"缺必填列":   "支出日期,支出币种\n2026-01-02,CNY\n",
		"重复列":    "支出日期,支出币种,支出金额,支出金额\n2026-01-02,CNY,1,2\n",
		"只有表头":   testCsvHeader,
		"支出日期非法": testCsvHeader + ",,2026/01/02,CNY,1,,,,\n",
		"支出金额非法": testCsvHeader + ",,2026-01-02,CNY,abc,,,,\n",
		"支出币种为空": testCsvHeader + ",,2026-01-02,,1,,,,\n",
		"支出币种非法": testCsvHeader + ",,2026-01-02,XYZ,1,,,1,\n",
		"折算汇率非正": testCsvHeader + ",,2026-01-02,USD,1,,,0,\n",
		"折算汇率非法": testCsvHeader + ",,2026-01-02,USD,1,,,abc,\n",
	}
	for name, csv := range cases {
		if resp := insertExpense(t, engine, jwt, "2609.csv", csv); resp.Code == http.StatusOK {
			t.Errorf("%s应报错: %+v", name, resp)
		}
	}
	//报错的入库不能留下任何痕迹
	if list := selectExpense(t, engine, jwt, model.ExpenseInquiry{}); list.Data.Count != 0 {
		t.Errorf("报错的入库不应落库: %+v", list.Data)
	}
	if logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDataEntry}}); logs.Data.Count != 0 {
		t.Errorf("报错的入库不应记审计: %+v", logs.Data)
	}
	if files := selectFileMeta(t, engine, jwt, model.FileMetaInquiry{}); files.Data.Count != 0 {
		t.Errorf("报错的入库不应存档: %+v", files.Data)
	}
}

func TestInsertExpenseMissingField(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	csv := testCsvHeader + "招商银行,6789,2026-01-02,CNY,100.50,亚马逊,买书,,购物\n"

	jwt, err := tool.EnJwt(util.GenCtx(), config.GetConfig().ServerToken, clientToken, "", time.Hour)
	if err != nil {
		t.Fatalf("签发jwt异常: %+v", err)
	}
	if resp := insertExpense(t, engine, jwt, "2609.csv", csv); resp.Code == http.StatusOK {
		t.Errorf("jwt里没带记账币种应报错: %+v", resp)
	}
	if resp := insertExpense(t, engine, newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour), "", csv); resp.Code == http.StatusOK {
		t.Errorf("没带文件应报错: %+v", resp)
	}
}

func TestInsertExpenseWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	csv := testCsvHeader + "招商银行,6789,2026-01-02,CNY,100.50,亚马逊,买书,,购物\n"
	if resp := insertExpense(t, engine, "", "2609.csv", csv); resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}
