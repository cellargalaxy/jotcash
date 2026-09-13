package tool_test

import (
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
)

// Claims用的是go_common的ctx key，db包取口令、gin中间件塞口令都靠这一对
func TestClaims(t *testing.T) {
	ctx := util.GenCtx()
	if tool.GetClaims(ctx) != nil {
		t.Errorf("空ctx应取不到Claims")
	}

	claims := &model.Claims{ClientToken: "test-client-token"}
	ctx = tool.SetClaims(ctx, claims)
	loaded := tool.GetClaims(ctx)
	if loaded == nil || loaded.ClientToken != claims.ClientToken {
		t.Fatalf("Claims读写不一致: %+v", loaded)
	}
	if util.GetCtxValue[*model.Claims](ctx, util.ClaimsKey) == nil {
		t.Errorf("应存在go_common约定的ClaimsKey下")
	}
	if tool.GetClaims(tool.SetClaims(util.GenCtx(), nil)) != nil {
		t.Errorf("SetClaims传nil不应写入ctx")
	}
}
