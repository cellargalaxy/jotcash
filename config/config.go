package config

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	ListenAddress = ":7678"
	ConfigPath    = "resource/jotcash.yaml"
	DbPath        = "resource/jotcash.db"
	DbBackupPath  = "resource/db_backup"
)

const (
	DbBackupLimit    = 5                  //数据库备份上限
	ExpenseFileLimit = 10 * 1024 * 1024   //明细文件大小上限：10MB
	ImportFileLimit  = 1024 * 1024 * 1024 //数据库文件大小上限：1GB
)

var configService *util.ConfigService[model.Config]

func init() {
	ctx := util.GenCtx()
	configService = util.NewConfigService[model.Config](new(ConfigHandler))
	err := configService.Start(ctx)
	if err != nil {
		panic(err)
	}
}

func GetConfig(ctx context.Context) model.Config {
	return configService.GetConfig(ctx)
}

type ConfigHandler struct {
}

func (this *ConfigHandler) GetPath(ctx context.Context) string {
	return ConfigPath
}
func (this *ConfigHandler) GetDefault(ctx context.Context) string {
	var config model.Config
	serverToken, err := tool.GenToken(ctx, tool.TokenLen)
	if err != nil {
		panic(err)
	}
	config.ServerToken = serverToken
	config.DbBackupLimit = DbBackupLimit
	config.ExpenseFileLimit = ExpenseFileLimit
	config.ImportFileLimit = ImportFileLimit
	text := util.YamlStruct2Str(ctx, config)
	return text
}
func (this *ConfigHandler) Parse(ctx context.Context, text string) (model.Config, error) {
	var config model.Config
	err := util.YamlStr2Struct(ctx, text, &config)
	if err != nil {
		return config, err
	}
	if config.ServerToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("加载配置，后端口令为空")
		return config, errors.Errorf("加载配置，后端口令为空")
	}
	if config.DbBackupLimit <= 0 {
		config.DbBackupLimit = DbBackupLimit
	}
	if config.ExpenseFileLimit <= 0 {
		config.ExpenseFileLimit = ExpenseFileLimit
	}
	if config.ImportFileLimit <= 0 {
		config.ImportFileLimit = ImportFileLimit
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{"config": config}).Info("加载配置")
	return config, nil
}
