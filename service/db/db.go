package db

import (
	"context"
	"io"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
)

func CheckToken(ctx context.Context) error {
	return db.CheckToken(ctx)
}

func ChangeToken(ctx context.Context, newToken string) error {
	err := tool.CheckToken(ctx, newToken)
	if err != nil {
		return err
	}
	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeClientTokenSwap,
		Summary:       "数据库已用新口令重新加密",
		Result:        model.ResultSuccess,
	}
	return db.ChangeToken(ctx, newToken, db.NewOperationLogInsertHandler(&operationLog))
}

func Export(ctx context.Context, writer io.Writer) error {
	operationLog := model.OperationLog{
		Id:            util.GenId(),
		OperationType: model.OperationTypeDbExport,
		Summary:       "导出加密数据库快照",
		Result:        model.ResultSuccess,
	}
	return db.Export(ctx, writer, db.NewOperationLogInsertHandler(&operationLog))
}
