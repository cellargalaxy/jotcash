package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/model"
)

// 口令要随jwt载荷走，所以json.Marshal带上它；但String()（日志用）必须抹掉
func TestClaims(t *testing.T) {
	claims := model.Claims{ClientToken: "secret-client-token"}
	claims.LogId = 123

	data, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("序列化异常: %+v", err)
	}
	if !strings.Contains(string(data), "secret-client-token") {
		t.Errorf("jwt载荷要带上ClientToken: %s", string(data))
	}
	var loaded model.Claims
	if err = json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("反序列化异常: %+v", err)
	}
	if loaded.ClientToken != claims.ClientToken || loaded.LogId != claims.LogId {
		t.Errorf("往返不一致: %+v", loaded)
	}
	if strings.Contains(claims.String(), "secret-client-token") {
		t.Errorf("ClientToken不应出现在String()里: %s", claims.String())
	}
	if !strings.Contains(claims.String(), "123") {
		t.Errorf("其余字段应正常输出: %s", claims.String())
	}
}
