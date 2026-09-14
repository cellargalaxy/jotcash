package tool_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
)

const testServerToken = "test-server-token-1"

func TestJwtClientToken(t *testing.T) {
	ctx := util.GenCtx()

	token, err := tool.EnJwt(ctx, testServerToken, "test-client-token-1", "CNY", time.Hour)
	if err != nil {
		t.Fatalf("签发异常: %+v", err)
	}
	claims, err := tool.DeJwt(ctx, testServerToken, token)
	if err != nil {
		t.Fatalf("校验异常: %+v", err)
	}
	if claims.ClientToken != "test-client-token-1" {
		t.Fatalf("前端口令没跟着jwt回来: %+v", claims)
	}
}

func TestJwtServerToken(t *testing.T) {
	ctx := util.GenCtx()

	token, err := tool.EnJwt(ctx, testServerToken, "test-client-token-1", "CNY", time.Hour)
	if err != nil {
		t.Fatalf("签发异常: %+v", err)
	}
	if _, err = tool.DeJwt(ctx, "other-server-token", token); err == nil {
		t.Errorf("换一把后端口令应验不过")
	}
	if _, err = tool.DeJwt(ctx, testServerToken, "not-a-jwt"); err == nil {
		t.Errorf("非法jwt应验不过")
	}
	if _, err = tool.EnJwt(ctx, "", "test-client-token-1", "CNY", time.Hour); err == nil {
		t.Errorf("后端口令为空应拒绝签发")
	}
	if _, err = tool.DeJwt(ctx, "", token); err == nil {
		t.Errorf("后端口令为空应拒绝校验")
	}
}

func TestJwtExpire(t *testing.T) {
	ctx := util.GenCtx()

	token, err := tool.EnJwt(ctx, testServerToken, "test-client-token-1", "CNY", -time.Minute)
	if err != nil {
		t.Fatalf("签发异常: %+v", err)
	}
	if _, err = tool.DeJwt(ctx, testServerToken, token); err == nil {
		t.Errorf("过期jwt应验不过")
	}
}

func TestJwtClaimsLog(t *testing.T) {
	claims := model.Claims{ClientToken: "secret-client-token"}
	claims.LogId = 123

	if strings.Contains(claims.String(), "secret-client-token") {
		t.Errorf("ClientToken不应出现在String()里: %s", claims.String())
	}
	if !strings.Contains(claims.String(), "123") {
		t.Errorf("其余字段应正常输出: %s", claims.String())
	}
}
