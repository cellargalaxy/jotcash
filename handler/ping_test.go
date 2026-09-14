package handler_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	common_model "github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func postPing(t *testing.T, engine *gin.Engine, request *http.Request) common_model.HttpResp {
	t.Helper()
	var resp common_model.HttpResp
	doRequest(t, engine, request, &resp)
	return resp
}

func newPingRequest(jwt string) *http.Request {
	return newRequest(util.PathPing, jwt, nil)
}

func TestPing(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	serverToken := config.GetConfig(util.GenCtx()).ServerToken

	resp := postPing(t, engine, newPingRequest(newJwt(t, serverToken, clientToken, time.Hour)))
	if resp.Code != http.StatusOK {
		t.Fatalf("口令都对应校验通过: %+v", resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok || data["sn"] != util.GetServerName() {
		t.Errorf("响应data不符: %+v", resp.Data)
	}
}

func TestPingWrongToken(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	serverToken := config.GetConfig(util.GenCtx()).ServerToken

	resp := postPing(t, engine, newPingRequest(newJwt(t, serverToken, "wrong-client-token-1", time.Hour)))
	if resp.Code == http.StatusOK {
		t.Errorf("前端口令错应校验不通过: %+v", resp)
	}
	if resp.Msg != "连接数据库，口令错误或数据库文件损坏" {
		t.Errorf("前端口令错的文案不符: %+v", resp)
	}
	//报错归报错，服务的基础信息照样要返回
	if data, ok := resp.Data.(map[string]any); !ok || data["sn"] != util.GetServerName() {
		t.Errorf("校验不通过也应返回服务基础信息: %+v", resp.Data)
	}

	resp = postPing(t, engine, newPingRequest(newJwt(t, "wrong-server-token", clientToken, time.Hour)))
	if resp.Code == http.StatusOK {
		t.Errorf("后端口令错应校验不通过: %+v", resp)
	}
	if strings.Contains(resp.Msg, clientToken) {
		t.Errorf("响应文案泄露前端口令: %+v", resp)
	}
}

func TestPingWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	resp := postPing(t, engine, newPingRequest(""))
	if resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}

// 前端签了jwt但漏掉client_token，是联调最容易出的岔子
func TestPingWithoutClientToken(t *testing.T) {
	engine, _ := newTestEngine(t)

	resp := postPing(t, engine, newPingRequest(newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, "", time.Hour)))
	if resp.Code == http.StatusOK {
		t.Errorf("jwt里没有前端口令应校验不通过: %+v", resp)
	}
}

// header里直接放裸jwt、漏掉Bearer前缀，同上
func TestPingWithoutBearer(t *testing.T) {
	engine, clientToken := newTestEngine(t)

	request := httptest.NewRequest(http.MethodPost, util.PathPing, nil)
	request.Header.Set(util.AuthorizationKey, newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour))
	resp := postPing(t, engine, request)
	if resp.Code != http.StatusUnauthorized {
		t.Errorf("漏掉Bearer前缀应401: %+v", resp)
	}
}

// go_common在改，alg=none这条底线得钉住
func TestPingAlgNoneJwt(t *testing.T) {
	engine, clientToken := newTestEngine(t)

	encode := func(text string) string { return base64.RawURLEncoding.EncodeToString([]byte(text)) }
	jwt := encode(`{"alg":"none","typ":"JWT"}`) + "." + encode(fmt.Sprintf(`{"exp":%d,"client_token":"%s"}`, time.Now().Add(time.Hour).Unix(), clientToken)) + "."
	resp := postPing(t, engine, newPingRequest(jwt))
	if resp.Code == http.StatusOK {
		t.Errorf("alg=none的jwt应校验不通过: %+v", resp)
	}
}

func TestPingExpiredJwt(t *testing.T) {
	engine, clientToken := newTestEngine(t)

	resp := postPing(t, engine, newPingRequest(newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, -time.Minute)))
	if resp.Code == http.StatusOK {
		t.Errorf("过期jwt应校验不通过: %+v", resp)
	}
}

func TestPingTokenNotInLog(t *testing.T) {
	engine, clientToken := newTestEngine(t)

	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() { logrus.SetLevel(logrus.WarnLevel) })
	buffer := catchLog(t)

	resp := postPing(t, engine, newPingRequest(newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)))
	if resp.Code != http.StatusOK {
		t.Fatalf("口令都对应校验通过: %+v", resp)
	}
	if strings.Contains(buffer.String(), clientToken) {
		t.Errorf("日志泄露前端口令: %s", buffer.String())
	}
}

func TestPingGet(t *testing.T) {
	engine, _ := newTestEngine(t)

	writer := httptest.NewRecorder()
	engine.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, util.PathPing, nil))
	if writer.Code != http.StatusOK {
		t.Errorf("存活探针不应被validate拦截: code=%d body=%s", writer.Code, writer.Body.String())
	}
}
