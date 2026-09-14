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
	logrus.WithContext(c).WithFields(logrus.Fields{"claims": util.GetClaims[*model.Claims](c)}).Info("Ping")
	err := db.CheckToken(c)
	c.JSON(http.StatusOK, util.NewHttpRespByErr(model.PingData{Ip: util.GetIP(), ServerName: util.GetServerName(), Timestamp: time.Now().Unix()}, err))
}
