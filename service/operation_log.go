package service

import (
	"context"

	common_model "github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/db"
)

func SelectOperationLog(ctx context.Context, inquiry model.OperationLogInquiry) (any, error) {
	objects, count, err := db.SelectOperationLog(ctx, inquiry)
	if err != nil {
		return nil, err
	}
	return common_model.HttpData{Object: objects, Count: count}, nil
}
