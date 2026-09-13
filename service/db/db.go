package db

import (
	"context"

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
