package model

import (
	"github.com/cellargalaxy/go_common/util"
)

type ChangeTokenReq struct {
	NewToken string `json:"new_token"`
}

func (this ChangeTokenReq) String() string {
	this.NewToken = ""
	return util.JsonStruct2Str(this)
}
