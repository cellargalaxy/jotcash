package handler

import (
	"net/http"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/repo"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func GinPing(c *gin.Context) {
	ctx := c.Request.Context()

	logrus.WithContext(ctx).WithFields(logrus.Fields{"claims": util.GetClaims[*model.Claims](ctx)}).Info("Ping")
	err := repo.CheckToken(ctx)
	c.JSON(http.StatusOK, util.NewHttpRespByErr(util.NewPingData(), err))
}
