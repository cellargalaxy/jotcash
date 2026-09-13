package model

import (
	"github.com/cellargalaxy/go_common/util"
)

type ChangeTokenRequest struct {
	NewToken string `json:"new_token"`
}

func (this ChangeTokenRequest) String() string {
	this.NewToken = ""
	return util.JsonStruct2Str(this)
}
