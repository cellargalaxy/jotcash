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
	"github.com/cellargalaxy/jotcash/service/repo"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func SelectExpense(ctx context.Context, inquiry model.ExpenseInquiry) (any, error) {
	objects, count, err := db.SelectExpense(ctx, inquiry)
	if err != nil {
		return nil, err
	}
	return common_model.HttpData{Object: objects, Count: count}, nil
}

func DeleteExpense(ctx context.Context, inquiry model.ExpenseInquiry) (any, error) {
	count, err := db.DeleteExpense(ctx, inquiry)
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
	data, err := io.ReadAll(reader)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("明细入库，读取上传文件异常")
		return nil, errors.Errorf("明细入库，读取上传文件异常: %+v", err)
	}
	if filename == "" {
		filename = fmt.Sprintf("%d.csv", util.GenId())
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
	err = db.InsertExpense(ctx, operationId, expenses, &fileMeta, &fileBlob)
	if err != nil {
		return nil, err
	}
	return common_model.HttpData{Object: operationId, Count: int64(len(expenses))}, nil
}
