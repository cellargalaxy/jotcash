package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/static"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func Init(ctx context.Context) error {
	engine := NewEngine(ctx)
	err := engine.Run(config.ListenAddress)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("API启动，异常")
		return errors.Errorf("API启动，异常: %+v", err)
	}
	return nil
}

func NewEngine(ctx context.Context) *gin.Engine {
	engine := gin.Default()
	engine.Use(util.GinLog)

	engine.GET(util.PathPing, util.Ping)
	engine.POST(util.PathPing, validate, Ping)

	engine.Use(staticCache)
	engine.StaticFS(util.PathStatic, http.FS(static.StaticFile))
	return engine
}

func staticCache(c *gin.Context) {
	if strings.HasPrefix(c.Request.RequestURI, util.PathStatic) {
		c.Header("Cache-Control", "max-age=86400")
	}
}

func validate(ctx *gin.Context) {
	util.ValidateGin(ctx, config.GetConfig().ServerToken, new(model.Claims))
}
