package model

import (
	"github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/go_common/util"
)

type Claims struct {
	model.Claims
	ClientToken string `json:"client_token,omitempty"`
}

func (this Claims) String() string {
	this.ClientToken = ""
	return util.JsonStruct2Str(this)
}
