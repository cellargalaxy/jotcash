package handler

import (
	"net/http"
	"time"

	"github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/service/db"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func Ping(c *gin.Context) {
	ctx := c.Request.Context()
	logrus.WithContext(ctx).WithFields(logrus.Fields{"claims": util.GetClaims[*model.Claims](ctx)}).Info("Ping")
	err := db.CheckToken(ctx)
	c.JSON(http.StatusOK, util.NewHttpRespByErr(model.PingData{Ip: util.GetIP(), ServerName: util.GetServerName(), Timestamp: time.Now().Unix()}, err))
}
