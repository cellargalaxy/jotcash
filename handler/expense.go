package handler

import (
	"net/http"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service"
	"github.com/cellargalaxy/jotcash/service/repo"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type expenseSelectWriter struct {
	c       *gin.Context
	written bool
}

func (this *expenseSelectWriter) Write(data []byte) (int, error) {
	if !this.written {
		this.written = true
		this.c.Header("Content-Type", "application/json; charset=utf-8")
	}
	return this.c.Writer.Write(data)
}

func SelectExpense(c *gin.Context) {
	ctx := c.Request.Context()

	var req model.ExpenseInquiry
	err := c.ShouldBindJSON(&req)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("明细查询，请求参数解析异常")
		c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, err))
		return
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"req": req}).Info("明细查询")

	writer := &expenseSelectWriter{c: c}
	err = repo.SelectExpenseStream(ctx, req, writer)
	if err == nil {
		return
	}
	if writer.written {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("明细查询，已开始流式写出，改不回错误响应")
		return
	}
	c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, err))
}

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
