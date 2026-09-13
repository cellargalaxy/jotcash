package model

import (
	"github.com/cellargalaxy/go_common/util"
)

type Config struct {
	DbPath      string `json:"db_path" yaml:"db_path"`
	ServerToken string `json:"-" yaml:"server_token"` //后端口令
}

func (this Config) String() string {
	return util.JsonStruct2Str(this)
}
