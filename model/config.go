package model

import (
	"github.com/cellargalaxy/go_common/util"
)

const serverName = "jotcash"

func init() {
	util.Init(serverName)
}

type Config struct {
	ServerToken      string `json:"-" yaml:"server_token"`                        //后端口令
	DbBackupLimit    int    `json:"db_backup_limit" yaml:"db_backup_limit"`       //数据库备份上限
	ExpenseFileLimit int64  `json:"expense_file_limit" yaml:"expense_file_limit"` //明细文件大小上限
	ImportFileLimit  int64  `json:"import_file_limit" yaml:"import_file_limit"`   //数据库文件大小上限
	AmountScale      int32  `json:"amount_scale" yaml:"amount_scale"`             //金额精度，保留多少位小数
	TokenFailLimit   int    `json:"token_fail_limit" yaml:"token_fail_limit"`     //口令失败次数上限，攒够就封禁
	TokenBanMinute   int    `json:"token_ban_minute" yaml:"token_ban_minute"`     //口令封禁时长，分钟；同时是失败计数的有效期
}

func (this Config) String() string {
	return util.JsonStruct2Str(this)
}
