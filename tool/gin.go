package tool

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
)

func GetClaims(ctx context.Context) *model.Claims {
	return util.GetCtxValue[*model.Claims](ctx, util.ClaimsKey)
}
func SetClaims(ctx context.Context, claims *model.Claims) context.Context {
	if claims == nil {
		return ctx
	}
	return util.SetCtxValue(ctx, util.ClaimsKey, claims)
}
