package config

import (
	"context"
	"sync"

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
	DbBackupLimit = 5

	ExpenseFileLimit = 10 * 1024 * 1024   //明细文件上限10MB
	ImportFileLimit  = 1024 * 1024 * 1024 //数据库文件上限1GB
)

var configService *util.ConfigService
var confLock sync.RWMutex
var conf model.Config

func init() {
	ctx := util.GenCtx()
	configService = util.NewConfigService(new(ConfigHandler))
	err := configService.Start(ctx)
	if err != nil {
		panic(err)
	}
}

func GetConfig() model.Config {
	confLock.RLock()
	defer confLock.RUnlock()
	return conf
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
	text := util.YamlStruct2Str(ctx, config)
	return text
}
func (this *ConfigHandler) Parse(ctx context.Context, text string) error {
	var config model.Config
	err := util.YamlStr2Struct(ctx, text, &config)
	if err != nil {
		return err
	}
	//后端口令是jwt签名密钥，空值等于谁都能伪造jwt，宁可起不来也不能带着空口令跑
	if config.ServerToken == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"path": ConfigPath}).Error("加载配置，后端口令为空")
		return errors.Errorf("加载配置，后端口令为空")
	}
	if config.DbBackupLimit <= 0 {
		config.DbBackupLimit = DbBackupLimit
	}

	confLock.Lock()
	defer confLock.Unlock()
	conf = config

	logrus.WithContext(ctx).WithFields(logrus.Fields{"config": config}).Info("加载配置")
	return nil
}
