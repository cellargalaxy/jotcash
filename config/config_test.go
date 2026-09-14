package config_test

import (
	"os"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/tool"
)

func TestMain(m *testing.M) {
	code := m.Run()
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

func TestConfigDefault(t *testing.T) {
	ctx := util.GenCtx()

	if err := tool.CheckToken(ctx, config.GetConfig(ctx).ServerToken); err != nil {
		t.Errorf("默认后端口令不满足强度: %+v", err)
	}

	handler := new(config.ConfigHandler)
	if handler.GetPath(ctx) != config.ConfigPath {
		t.Errorf("配置路径不符: %s", handler.GetPath(ctx))
	}
	if handler.GetDefault(ctx) == handler.GetDefault(ctx) {
		t.Errorf("默认配置里的后端口令应随机生成")
	}
}

func TestConfigParse(t *testing.T) {
	ctx := util.GenCtx()
	handler := new(config.ConfigHandler)

	object, err := handler.Parse(ctx, "server_token: abcdefghijk1\n")
	if err != nil {
		t.Fatalf("解析配置异常: %+v", err)
	}
	if object.ServerToken != "abcdefghijk1" {
		t.Errorf("后端口令解析不符: %s", object.ServerToken)
	}
	//没填的项要用默认值兜底，不能留零值
	if object.DbBackupLimit <= 0 || object.ExpenseFileLimit <= 0 || object.ImportFileLimit <= 0 {
		t.Errorf("缺省项未兜底: %+v", object)
	}
	if _, err = handler.Parse(ctx, "server_token: \"\"\n"); err == nil {
		t.Errorf("后端口令为空应报错")
	}
	if _, err = handler.Parse(ctx, "server_token: [ban"); err == nil {
		t.Errorf("非法yaml应报错")
	}
}
