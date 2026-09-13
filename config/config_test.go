package config_test

import (
	"testing"

	"github.com/cellargalaxy/jotcash/config"
)

// 没有配置文件时要退回默认库路径，而不是留空导致db包拼出非法DSN
func TestConfigDefault(t *testing.T) {
	if config.Config.DbPath != config.DefaultDbPath {
		t.Errorf("默认库路径不符: got=%s want=%s", config.Config.DbPath, config.DefaultDbPath)
	}
}
