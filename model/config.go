package model

import (
	"github.com/cellargalaxy/go_common/util"
)

const DefaultServerName = "jotcash"

func init() {
	util.Init(DefaultServerName)
}

type Config struct {
	ServerToken   string `json:"-" yaml:"server_token"`
	DbBackupLimit int    `json:"db_backup_limit" yaml:"db_backup_limit"`
}

func (this Config) String() string {
	return util.JsonStruct2Str(this)
}
