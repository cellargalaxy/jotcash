package service

import (
	"context"

	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/db"
)

func ChangeToken(ctx context.Context, request model.ChangeTokenRequest) (any, error) {
	return nil, db.ChangeToken(ctx, request.NewToken)
}
