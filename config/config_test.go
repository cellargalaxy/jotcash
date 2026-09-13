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
	//init时会把默认配置落到相对路径下，测试产物不留在仓库里
	os.RemoveAll("resource")
	os.Exit(code)
}

// 没有配置文件时按默认值生成一份：后端口令要随机生成且满足强度
func TestConfigDefault(t *testing.T) {
	ctx := util.GenCtx()

	if err := tool.CheckToken(ctx, config.Config.ServerToken); err != nil {
		t.Errorf("默认后端口令不满足强度: %+v", err)
	}

	handler := new(config.ConfigHandler)
	if handler.GetPath(ctx) != config.ConfigPath {
		t.Errorf("配置路径不符: %s", handler.GetPath(ctx))
	}
	//两次生成的默认配置里，后端口令不能是同一个
	if handler.GetDefault(ctx) == handler.GetDefault(ctx) {
		t.Errorf("默认配置里的后端口令应随机生成")
	}
}

// 配置文件被改坏时的兜底：后端口令为空直接报错
func TestConfigParse(t *testing.T) {
	ctx := util.GenCtx()
	origin := config.Config
	t.Cleanup(func() { config.Config = origin })

	handler := new(config.ConfigHandler)
	if err := handler.Parse(ctx, "server_token: abcdefghijk1\n"); err != nil {
		t.Fatalf("解析配置异常: %+v", err)
	}
	if config.Config.ServerToken != "abcdefghijk1" {
		t.Errorf("后端口令解析不符: %s", config.Config.ServerToken)
	}
	if err := handler.Parse(ctx, "server_token: \"\"\n"); err == nil {
		t.Errorf("后端口令为空应报错")
	}
	if err := handler.Parse(ctx, "server_token: [ban"); err == nil {
		t.Errorf("非法yaml应报错")
	}
}
