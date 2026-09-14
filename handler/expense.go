package handler

import (
	"net/http"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func InsertExpense(c *gin.Context) {
	logrus.WithContext(c).WithFields(logrus.Fields{"claims": tool.GetClaims(c)}).Info("明细入库")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.ExpenseFileLimit)
	fileHeader, err := c.FormFile(model.ExpenseFileKey)
	if err != nil {
		logrus.WithContext(c).WithFields(logrus.Fields{"err": err}).Error("明细入库，取上传文件异常")
		c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, errors.Errorf("明细入库，取上传文件异常: %+v", err)))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		logrus.WithContext(c).WithFields(logrus.Fields{"err": err}).Error("明细入库，打开上传文件异常")
		c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, errors.Errorf("明细入库，打开上传文件异常: %+v", err)))
		return
	}
	defer util.CloseIo(c, file)
	c.JSON(http.StatusOK, util.NewHttpRespByErr(service.InsertExpense(c, fileHeader.Filename, file)))
}
