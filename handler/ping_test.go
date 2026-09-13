package handler_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	common_model "github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/handler"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	logrus.SetLevel(logrus.WarnLevel)
	code := m.Run()
	//db包的init会在测试二进制的原始工作目录建库，不清掉会污染代码目录
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

func catchLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := new(bytes.Buffer)
	origin := logrus.StandardLogger().Out
	logrus.SetOutput(buffer)
	t.Cleanup(func() { logrus.SetOutput(origin) })
	return buffer
}

func findLogField(text, key string) string {
	index := strings.Index(text, "["+key+":")
	if index < 0 {
		return ""
	}
	text = text[index+len(key)+2:]
	index = strings.Index(text, "]")
	if index < 0 {
		return ""
	}
	return text[:index]
}

// 初始前端口令只在建库那一次进日志，测试从日志里捞
func newTestEngine(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	t.Chdir(t.TempDir())

	ctx := util.GenCtx()
	buffer := catchLog(t)
	err := db.Create(ctx)
	if err != nil {
		t.Fatalf("建测试库异常: %+v", err)
	}
	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("没捞到初始前端口令: %s", buffer.String())
	}
	return handler.NewEngine(ctx), clientToken
}

func newJwt(t *testing.T, serverToken, clientToken string, expire time.Duration) string {
	t.Helper()
	jwt, err := tool.EnJwt(util.GenCtx(), serverToken, clientToken, expire)
	if err != nil {
		t.Fatalf("签发jwt异常: %+v", err)
	}
	return jwt
}

func postPing(t *testing.T, engine *gin.Engine, request *http.Request) common_model.HttpResp {
	t.Helper()
	writer := httptest.NewRecorder()
	engine.ServeHTTP(writer, request)
	if writer.Code != http.StatusOK {
		t.Fatalf("http状态码不符: got=%d want=%d", writer.Code, http.StatusOK)
	}
	var resp common_model.HttpResp
	err := util.JsonStr2Struct(writer.Body.String(), &resp)
	if err != nil {
		t.Fatalf("响应体解析异常: body=%s err=%+v", writer.Body.String(), err)
	}
	return resp
}

func newPingRequest(jwt string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, util.PathPing, nil)
	if jwt != "" {
		request.Header.Set(util.AuthorizationKey, util.BearerKey+" "+jwt)
	}
	return request
}

func TestPing(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	serverToken := config.GetConfig().ServerToken

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
	serverToken := config.GetConfig().ServerToken

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

func TestPingExpiredJwt(t *testing.T) {
	engine, clientToken := newTestEngine(t)

	resp := postPing(t, engine, newPingRequest(newJwt(t, config.GetConfig().ServerToken, clientToken, -time.Minute)))
	if resp.Code == http.StatusOK {
		t.Errorf("过期jwt应校验不通过: %+v", resp)
	}
}

func TestPingTokenNotInLog(t *testing.T) {
	engine, clientToken := newTestEngine(t)

	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() { logrus.SetLevel(logrus.WarnLevel) })
	buffer := catchLog(t)

	resp := postPing(t, engine, newPingRequest(newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)))
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
