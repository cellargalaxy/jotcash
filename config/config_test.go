package config_test

import (
	"os"
	"strings"
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
	//首次启动写出去的就是这份默认配置，它必须自己解析得回来
	object, err := handler.Parse(ctx, handler.GetDefault(ctx))
	if err != nil {
		t.Fatalf("默认配置解析异常: %+v", err)
	}
	if err = tool.CheckToken(ctx, object.ServerToken); err != nil {
		t.Errorf("默认配置里的后端口令不满足强度: %+v", err)
	}
	if object.DbBackupLimit <= 0 || object.ExpenseFileLimit <= 0 || object.ImportFileLimit <= 0 || object.AmountScale <= 0 || object.TokenFailLimit <= 0 || object.TokenBanMinute <= 0 {
		t.Errorf("默认配置的限额项不应为零值: %+v", object)
	}
	if strings.Contains(object.String(), object.ServerToken) {
		t.Errorf("后端口令不应出现在配置日志里: %s", object.String())
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
	if object.DbBackupLimit <= 0 || object.ExpenseFileLimit <= 0 || object.ImportFileLimit <= 0 || object.TokenFailLimit <= 0 || object.TokenBanMinute <= 0 {
		t.Errorf("缺省项未兜底: %+v", object)
	}
	//负数与0一样是非法值，同样得被兜底顶掉，否则备份会被清空、上传会被全拒
	negative, err := handler.Parse(ctx, "server_token: abcdefghijk1\ndb_backup_limit: -1\nexpense_file_limit: -1\nimport_file_limit: -1\n")
	if err != nil {
		t.Fatalf("解析配置异常: %+v", err)
	}
	if negative.DbBackupLimit != object.DbBackupLimit || negative.ExpenseFileLimit != object.ExpenseFileLimit || negative.ImportFileLimit != object.ImportFileLimit {
		t.Errorf("负数项未兜底: %+v", negative)
	}
	//填了合法值就不能被默认值顶掉
	custom, err := handler.Parse(ctx, "server_token: abcdefghijk1\ndb_backup_limit: 9\nexpense_file_limit: 11\nimport_file_limit: 13\namount_scale: 4\ntoken_fail_limit: 3\ntoken_ban_minute: 7\n")
	if err != nil {
		t.Fatalf("解析配置异常: %+v", err)
	}
	if custom.DbBackupLimit != 9 || custom.ExpenseFileLimit != 11 || custom.ImportFileLimit != 13 || custom.AmountScale != 4 || custom.TokenFailLimit != 3 || custom.TokenBanMinute != 7 {
		t.Errorf("填了的配置项被默认值顶掉: %+v", custom)
	}
	//金额精度与限额项的兜底判据不同：0是「按整数记账」这个合法口径，只有负数才该被顶掉
	defaultObject, err := handler.Parse(ctx, handler.GetDefault(ctx))
	if err != nil {
		t.Fatalf("解析默认配置异常: %+v", err)
	}
	scale, err := handler.Parse(ctx, "server_token: abcdefghijk1\namount_scale: 0\n")
	if err != nil {
		t.Fatalf("解析配置异常: %+v", err)
	}
	if scale.AmountScale != 0 {
		t.Errorf("金额精度为0应原样保留: got=%d want=0", scale.AmountScale)
	}
	if scale, err = handler.Parse(ctx, "server_token: abcdefghijk1\namount_scale: -1\n"); err != nil {
		t.Fatalf("解析配置异常: %+v", err)
	}
	if scale.AmountScale != defaultObject.AmountScale {
		t.Errorf("金额精度为负未兜底: got=%d want=%d", scale.AmountScale, defaultObject.AmountScale)
	}
	if _, err = handler.Parse(ctx, "server_token: \"\"\n"); err == nil {
		t.Errorf("后端口令为空应报错")
	}
	if _, err = handler.Parse(ctx, "server_token: [ban"); err == nil {
		t.Errorf("非法yaml应报错")
	}
}
