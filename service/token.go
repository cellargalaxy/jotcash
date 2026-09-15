package service

import (
	"context"

	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/repo"
)

func ChangeToken(ctx context.Context, request model.ChangeTokenReq) (any, error) {
	return nil, repo.ChangeToken(ctx, request.NewToken)
}
