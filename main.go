package main

import (
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/handler"
)

func main() {
	ctx := util.GenCtx()
	err := handler.Init(ctx)
	if err != nil {
		panic(err)
	}
}
