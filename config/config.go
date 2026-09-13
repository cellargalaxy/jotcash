package config

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
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
	err := configService.Start(ctx)
	if err != nil {
		panic(err)
	}
	err = configService.LoadConfig(ctx)
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
	config.ServerToken = "" //todo,生成初始后端口令，打印初始的前端口令与后端口令的地方在db包里，db包才是判断数据库文件是否为创建，服务第一次其次的地方
	conf := util.YamlStruct2Str(ctx, config)
	return conf
}
func (this *ConfigHandler) Parse(ctx context.Context, text string) error {
	var config model.Config
	err := util.YamlStr2Struct(ctx, text, &config)
	if err != nil {
		return err
	}
	Config = config
	logrus.WithContext(ctx).WithFields(logrus.Fields{"config": config}).Info("加载配置")
	return nil
}
