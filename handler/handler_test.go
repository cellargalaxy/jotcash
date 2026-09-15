package handler_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/handler"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/rdb"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const testAccountingCurrency = "CNY"

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	logrus.SetLevel(logrus.WarnLevel)
	code := m.Run()
	//配置文件会落在测试二进制的原始工作目录，不清掉会污染代码目录
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

func catchLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := new(bytes.Buffer)
	origin := logrus.StandardLogger().Out
	originLevel := logrus.GetLevel()
	logrus.SetOutput(buffer)
	//要捞的建库口令是Info级，TestMain把全局级别压到了Warn，捞的这段得临时放开
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(origin)
		logrus.SetLevel(originLevel)
	})
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

// 服务起来就建库，口令由建库那一步生成，测试从日志里把它捞出来当这条链路的前端口令
func newTestEngine(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	t.Chdir(t.TempDir())

	buffer := new(bytes.Buffer)
	origin, originLevel := logrus.StandardLogger().Out, logrus.GetLevel()
	logrus.SetOutput(buffer)
	//建库口令是Info级，TestMain把全局级别压到了Warn，只放开捞口令这一小段，别把整条用例的日志都吞进buffer
	logrus.SetLevel(logrus.InfoLevel)
	err := rdb.Create(util.GenCtx())
	logrus.SetOutput(origin)
	logrus.SetLevel(originLevel)
	if err != nil {
		t.Fatalf("建测试库异常: %+v", err)
	}

	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("建库口令没有打印: %s", buffer.String())
	}
	handler.ResetValidateBan()
	return handler.NewEngine(util.GenCtx()), clientToken
}

// /db的静态增删查改已经收归/service/db，测试夹具要铺数据就自己开一次事务
func execTransaction(t *testing.T, clientToken string, handlers ...util.TransactionHandler) {
	t.Helper()
	ctx := newTokenCtx(clientToken)
	transaction, err := rdb.NewTransaction(ctx)
	if err != nil {
		t.Fatalf("开事务异常: %+v", err)
	}
	defer transaction.Close(ctx)
	if err = transaction.AddCommit(handlers...).Exec(ctx); err != nil {
		t.Fatalf("事务异常: %+v", err)
	}
}

// 每个请求都自带口令与记账币种，两样都签进jwt
func newJwt(t *testing.T, serverToken, clientToken string, expire time.Duration) string {
	t.Helper()
	return newJwtByCurrency(t, serverToken, clientToken, testAccountingCurrency, expire)
}

func newJwtByCurrency(t *testing.T, serverToken, clientToken, accountingCurrency string, expire time.Duration) string {
	t.Helper()
	ctx := util.GenCtx()
	now := time.Now()
	var claims model.Claims
	claims.IssuedAt = now.Unix()
	claims.ExpiresAt = now.Add(expire).Unix()
	claims.Ip = util.GetIP()
	claims.ServerName = util.GetServerName()
	claims.LogId = util.GetLogId(ctx)
	claims.ClientToken = clientToken
	claims.AccountingCurrency = accountingCurrency
	jwt, err := util.EnJwt(ctx, serverToken, claims)
	if err != nil {
		t.Fatalf("签发jwt异常: %+v", err)
	}
	return jwt
}

func newRequest(path, jwt string, object any) *http.Request {
	var body io.Reader
	if object != nil {
		body = strings.NewReader(util.JsonStruct2Str(object))
	}
	request := httptest.NewRequest(http.MethodPost, path, body)
	if jwt != "" {
		request.Header.Set(util.AuthorizationKey, util.BearerKey+" "+jwt)
	}
	return request
}

func doRequest(t *testing.T, engine *gin.Engine, request *http.Request, resp any) {
	t.Helper()
	writer := httptest.NewRecorder()
	engine.ServeHTTP(writer, request)
	if writer.Code != http.StatusOK {
		t.Fatalf("http状态码不符: got=%d want=%d", writer.Code, http.StatusOK)
	}
	err := util.JsonStr2Struct(writer.Body.String(), resp)
	if err != nil {
		t.Fatalf("响应体解析异常: body=%s err=%+v", writer.Body.String(), err)
	}
}
