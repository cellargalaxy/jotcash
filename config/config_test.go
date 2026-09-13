package config_test

import (
	"os"
	"testing"

	"github.com/cellargalaxy/jotcash/config"
)

func TestMain(m *testing.M) {
	code := m.Run()
	//init时会把默认配置落到相对路径下，测试产物不留在仓库里
	os.RemoveAll("resource")
	os.Exit(code)
}

// 没有配置文件时要退回默认库路径，而不是留空导致db包拼出非法DSN
func TestConfigDefault(t *testing.T) {
	if config.Config.DbPath != config.DbPath {
		t.Errorf("默认库路径不符: got=%s want=%s", config.Config.DbPath, config.DbPath)
	}
}
