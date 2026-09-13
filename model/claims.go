package model

import "github.com/cellargalaxy/go_common/model"

type Claims struct {
	model.Claims
	ClientToken string `json:"-"` //前端口令
}
