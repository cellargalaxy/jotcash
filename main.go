package main

import (
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/corn"
	"github.com/cellargalaxy/jotcash/handler"
)

func main() {
	ctx := util.GenCtx()
	err := corn.Init(ctx)
	if err != nil {
		panic(err)
	}
	err = handler.Init(ctx)
	if err != nil {
		panic(err)
	}
}
