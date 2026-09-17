package service

import (
	"context"
	"fmt"
	"io"

	common_model "github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/expense"
	_ "github.com/cellargalaxy/jotcash/service/expense/base_csv"
	_ "github.com/cellargalaxy/jotcash/service/expense/cmb_debit"
	_ "github.com/cellargalaxy/jotcash/service/expense/icbc_credit"
	"github.com/cellargalaxy/jotcash/service/repo"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

func SelectExpenseDistinct(ctx context.Context, inquiry model.ExpenseDistinctInquiry) (any, error) {
	objects, count, err := repo.SelectExpenseDistinct(ctx, inquiry)
	if err != nil {
		return nil, err
	}
	return common_model.HttpData{Object: objects, Count: count}, nil
}

func UpdateExpense(ctx context.Context, req model.Expense) (any, error) {
	objects, _, err := repo.SelectExpense(ctx, model.ExpenseInquiry{Id: []int64{req.Id}, Deleted: model.DeletedAll})
	if err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"id": req.Id}).Warn("明细编辑，明细不存在")
		return nil, errors.Errorf("明细编辑，明细不存在: %d", req.Id)
	}
	before := objects[0]
	if before.DeletedAt.Valid {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"id": req.Id}).Warn("明细编辑，已删除明细不可编辑")
		return nil, errors.Errorf("明细编辑，已删除明细不可编辑: %d", req.Id)
	}
	if before.Version != req.Version {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"id": req.Id, "version": req.Version, "dbVersion": before.Version}).Warn("明细编辑，数据已落后")
		return nil, errors.Errorf("明细编辑，数据已落后，请刷新页面重新加载")
	}

	object := req
	object.Id, object.Version = before.Id, before.Version
	object.AccountingCurrency = before.AccountingCurrency
	object.OperationId, object.FileId = before.OperationId, before.FileId
	object.CreatedAt, object.UpdatedAt, object.DeletedAt = before.CreatedAt, before.UpdatedAt, before.DeletedAt
	//汇率置零即交给自动获取：支出日期或支出币种变了要按新值重取，手填了新汇率则以手填值为准
	if !object.ExpenseDate.Equal(before.ExpenseDate) || object.ExpenseCurrency != before.ExpenseCurrency {
		if object.ExchangeRate.Equal(before.ExchangeRate) {
			object.ExchangeRate = decimal.Zero
		}
	}
	err = expense.Derive(ctx, &object)
	if err != nil {
		return nil, errors.Errorf("明细编辑，%s", err)
	}

	err = repo.UpdateExpense(ctx, before, &object)
	if err != nil {
		return nil, err
	}
	return common_model.HttpData{Object: &object, Count: 1}, nil
}

func SwitchAccountingCurrency(ctx context.Context, req model.CurrencySwitchReq) (any, error) {
	err := expense.CheckCurrency(ctx, model.CsvAccountingCurrency, req.AccountingCurrency)
	if err != nil {
		return nil, err
	}
	objects, _, err := repo.SelectExpense(ctx, model.ExpenseInquiry{AccountingCurrencyNot: []string{req.AccountingCurrency}, Deleted: model.DeletedAll})
	if err != nil {
		return nil, err
	}

	var switched model.CurrencySwitchResult
	for i := range objects {
		object := *objects[i]
		object.AccountingCurrency = req.AccountingCurrency
		object.ExchangeRate = decimal.Zero
		err = expense.Derive(ctx, &object)
		if err == nil {
			err = repo.SwitchAccountingCurrency(ctx, &object)
		}
		if err != nil {
			switched.Failed++
			continue
		}
		switched.Done++
	}

	var result string
	switch {
	case switched.Failed == 0:
		result = model.ResultSuccess
	case switched.Done == 0:
		result = model.ResultFailure
	default:
		result = model.ResultPartial
	}
	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeCurrencySwitch,
		Summary:       fmt.Sprintf("切换记账币种为 %s，成功 %d 笔，失败 %d 笔", req.AccountingCurrency, switched.Done, switched.Failed),
		Result:        result,
	}
	err = repo.InsertOperationLog(ctx, &operationLog)
	if err != nil {
		return nil, err
	}
	return common_model.HttpData{Object: switched, Count: switched.Done}, nil
}

func DeleteExpense(ctx context.Context, inquiry model.ExpenseInquiry) (any, error) {
	count, err := repo.DeleteExpense(ctx, inquiry)
	if err != nil {
		return nil, err
	}
	return common_model.HttpData{Count: count}, nil
}

func InsertExpense(ctx context.Context, filename string, reader io.Reader) (any, error) {
	var accountingCurrency string
	claims := util.GetClaims[*model.Claims](ctx)
	if claims != nil {
		accountingCurrency = claims.AccountingCurrency
	}
	if accountingCurrency == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("明细入库，记账币种为空")
		return nil, errors.Errorf("明细入库，记账币种为空")
	}
	if filename == "" {
		filename = fmt.Sprintf("%d.csv", util.GetLogId(ctx))
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("明细入库，读取上传文件异常")
		return nil, errors.Errorf("明细入库，读取上传文件异常: %+v", err)
	}

	expenses, err := expense.Parse(ctx, data, accountingCurrency)
	if err != nil {
		return nil, err
	}

	operationId, fileId := util.GenId(), util.GenId()
	for i := range expenses {
		expenses[i].OperationId, expenses[i].FileId = operationId, fileId
	}
	fileMeta := model.FileMeta{Id: fileId, FileHash: util.EnSha256Hex(string(data)), FileName: filename, FileSize: int64(len(data)), OperationId: operationId}
	fileBlob := model.FileBlob{FileHash: fileMeta.FileHash, FileData: data}
	err = repo.InsertExpense(ctx, operationId, expenses, &fileMeta, &fileBlob)
	if err != nil {
		return nil, err
	}
	return common_model.HttpData{Object: operationId, Count: int64(len(expenses))}, nil
}
