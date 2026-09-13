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
	ConfigPath = "resource/jotcash.yaml"
	DbPath     = "resource/jotcash.db"
)

var Config model.Config
var configService *util.ConfigService

func init() {
	ctx := util.GenCtx()
	configService = util.NewConfigService(new(ConfigHandler))
	err := configService.LoadConfig(ctx)
	if err != nil {
		panic(err)
	}
	err = configService.Start(ctx)
	if err != nil {
		panic(err)
	}
}

type ConfigHandler struct {
}

func (this *ConfigHandler) GetPath(ctx context.Context) string {
	return ConfigPath
}
func (this *ConfigHandler) GetDefault(ctx context.Context) string {
	var config model.Config
	config.DbPath = DbPath
	serverToken, err := tool.GenToken(ctx, tool.TokenLen)
	if err != nil {
		panic(err)
	}
	config.ServerToken = serverToken
	conf := util.YamlStruct2Str(ctx, config)
	return conf
}
func (this *ConfigHandler) Parse(ctx context.Context, text string) error {
	var config model.Config
	err := util.YamlStr2Struct(ctx, text, &config)
	if err != nil {
		return err
	}
	if config.DbPath == "" {
		config.DbPath = DbPath
	}
	//后端口令是jwt签名密钥，空值等于谁都能伪造jwt，宁可起不来也不能带着空口令跑
	if config.ServerToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"path": ConfigPath}).Error("加载配置，后端口令为空")
		return errors.Errorf("加载配置，后端口令为空")
	}
	Config = config
	logrus.WithContext(ctx).WithFields(logrus.Fields{"config": config}).Info("加载配置")
	return nil
}
