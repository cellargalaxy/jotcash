package model

import (
	"github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/go_common/util"
)

type Claims struct {
	model.Claims
	ClientToken string `json:"client_token,omitempty"` //前端口令，jwt载荷要带上它，db层靠它开库
}

// 口令不进日志：String()抹掉，json.Marshal（jwt签发用的就是它）照常带上
func (this Claims) String() string {
	this.ClientToken = ""
	return util.JsonStruct2Str(this)
}
