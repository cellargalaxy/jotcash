package handler

import (
	"net/http"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/service"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func InsertExpense(c *gin.Context) {
	ctx := c.Request.Context()

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.GetConfig(ctx).ExpenseFileLimit)
	fileHeader, err := c.FormFile(config.ExpenseFileKey)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("明细入库，取上传文件异常")
		c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, errors.Errorf("明细入库，取上传文件异常: %+v", err)))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("明细入库，打开上传文件异常")
		c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, errors.Errorf("明细入库，打开上传文件异常: %+v", err)))
		return
	}
	defer util.CloseIo(ctx, file)
	c.JSON(http.StatusOK, util.NewHttpRespByErr(service.InsertExpense(ctx, fileHeader.Filename, file)))
}
