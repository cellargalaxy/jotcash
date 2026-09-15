package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/model"
)

func TestChangeTokenReq(t *testing.T) {
	req := model.ChangeTokenReq{NewToken: "secret-new-token"}

	//新口令是请求体里真要传的，序列化不能丢
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("序列化异常: %+v", err)
	}
	var loaded model.ChangeTokenReq
	if err = json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("反序列化异常: %+v", err)
	}
	if loaded.NewToken != req.NewToken {
		t.Errorf("NewToken往返不一致: got=%s want=%s", loaded.NewToken, req.NewToken)
	}
	//但打日志的String()要把它抹掉
	if strings.Contains(req.String(), "secret-new-token") {
		t.Errorf("NewToken不应出现在String()里: %s", req.String())
	}
}
