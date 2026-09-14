package model_test

import (
	"os"
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
)

func TestMain(m *testing.M) {
	code := m.Run()
	os.RemoveAll("log")
	os.Exit(code)
}

func TestConfig(t *testing.T) {
	config := model.Config{ServerToken: "secret-server-token"}
	if strings.Contains(config.String(), "secret-server-token") {
		t.Errorf("ServerToken不应出现在序列化结果中: %s", config.String())
	}
	if util.GetServerName() != "jotcash" {
		t.Errorf("服务名不符: %s", util.GetServerName())
	}
}
