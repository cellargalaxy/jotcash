package handler_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	common_model "github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const testNewToken = "new-client-token-1"

func changeToken(t *testing.T, engine *gin.Engine, jwt, newToken string) common_model.HttpResp {
	t.Helper()
	var resp common_model.HttpResp
	doRequest(t, engine, newRequest(model.PathChangeToken, jwt, model.ChangeTokenRequest{NewToken: newToken}), &resp)
	return resp
}

func TestChangeToken(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	serverToken := config.GetConfig().ServerToken

	resp := changeToken(t, engine, newJwt(t, serverToken, clientToken, time.Hour), testNewToken)
	if resp.Code != http.StatusOK {
		t.Fatalf("更换口令应成功: %+v", resp)
	}

	//A-5：换完之后旧口令打不开，新口令能打开
	if ping := postPing(t, engine, newPingRequest(newJwt(t, serverToken, clientToken, time.Hour))); ping.Code == http.StatusOK {
		t.Errorf("旧口令不应还能打开: %+v", ping)
	}
	newJwtToken := newJwt(t, serverToken, testNewToken, time.Hour)
	if ping := postPing(t, engine, newPingRequest(newJwtToken)); ping.Code != http.StatusOK {
		t.Fatalf("新口令应能打开: %+v", ping)
	}

	//A-5：审计写进新库；§四.2 变更内容不记口令值
	logs := selectOperationLog(t, engine, newJwtToken, model.OperationLogInquiry{OperationType: []string{model.OperationTypeClientTokenSwap}})
	if logs.Data.Count != 1 {
		t.Fatalf("新库里应有一条更换口令审计: %+v", logs.Data)
	}
	if logs.Data.Object[0].Changes != "" {
		t.Errorf("更换口令不应记变更内容: %+v", logs.Data.Object[0])
	}
	if strings.Contains(logs.Data.Object[0].String(), testNewToken) || strings.Contains(logs.Data.Object[0].String(), clientToken) {
		t.Errorf("审计泄露口令: %+v", logs.Data.Object[0])
	}
	//系统初始化那条是旧库里的，换口令是整库改密不是重建，历史审计要还在
	if all := selectOperationLog(t, engine, newJwtToken, model.OperationLogInquiry{}); all.Data.Count != 2 {
		t.Errorf("历史审计应随库保留: %+v", all.Data)
	}
}

// A-6：长度≥12且不得纯数字或纯字母，不合规的连库都不该动
func TestChangeTokenWeak(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	serverToken := config.GetConfig().ServerToken
	jwt := newJwt(t, serverToken, clientToken, time.Hour)

	for _, weak := range []string{"", "short-1", "123456789012", "abcdefghijkl"} {
		if resp := changeToken(t, engine, jwt, weak); resp.Code == http.StatusOK {
			t.Errorf("弱口令应被拒: token=%s resp=%+v", weak, resp)
		}
	}
	//任一步失败原库原封不动
	if ping := postPing(t, engine, newPingRequest(jwt)); ping.Code != http.StatusOK {
		t.Errorf("被拒之后原口令应照常可用: %+v", ping)
	}
}

func TestChangeTokenWrongOldToken(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	serverToken := config.GetConfig().ServerToken

	resp := changeToken(t, engine, newJwt(t, serverToken, "wrong-client-token-1", time.Hour), testNewToken)
	if resp.Code == http.StatusOK {
		t.Errorf("旧口令错应换不了: %+v", resp)
	}
	//原库不动：原口令照常可用，新口令打不开
	if ping := postPing(t, engine, newPingRequest(newJwt(t, serverToken, clientToken, time.Hour))); ping.Code != http.StatusOK {
		t.Errorf("失败后原口令应照常可用: %+v", ping)
	}
	if ping := postPing(t, engine, newPingRequest(newJwt(t, serverToken, testNewToken, time.Hour))); ping.Code == http.StatusOK {
		t.Errorf("失败后新口令不应能打开: %+v", ping)
	}
}

// NewGinPost会把请求体打进日志，新口令绝不能跟着进去
func TestChangeTokenNotInLog(t *testing.T) {
	engine, clientToken := newTestEngine(t)

	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() { logrus.SetLevel(logrus.WarnLevel) })
	buffer := catchLog(t)

	resp := changeToken(t, engine, newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour), testNewToken)
	if resp.Code != http.StatusOK {
		t.Fatalf("更换口令应成功: %+v", resp)
	}
	if strings.Contains(buffer.String(), testNewToken) || strings.Contains(buffer.String(), clientToken) {
		t.Errorf("日志泄露口令: %s", buffer.String())
	}
}

func TestChangeTokenWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	var resp common_model.HttpResp
	doRequest(t, engine, newRequest(model.PathChangeToken, "", model.ChangeTokenRequest{NewToken: testNewToken}), &resp)
	if resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}
