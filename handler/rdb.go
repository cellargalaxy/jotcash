package handler

import (
	"fmt"
	"net/http"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/service/repo"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type dbFileWriter struct {
	c       *gin.Context
	written bool
}

func (this *dbFileWriter) Write(data []byte) (int, error) {
	if !this.written {
		this.written = true
		filename := fmt.Sprintf("jotcash-%d.db", util.GenId())
		this.c.Header("Content-Type", "application/octet-stream")
		this.c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}
	return this.c.Writer.Write(data)
}

func Export(c *gin.Context) {
	ctx := c.Request.Context()

	writer := &dbFileWriter{c: c}
	err := repo.Export(ctx, writer)
	if err == nil {
		return
	}
	if writer.written {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("数据库导出，已开始写出，改不回错误响应")
		return
	}
	c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, err))
}

func Import(c *gin.Context) {
	ctx := c.Request.Context()

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.GetConfig(ctx).ImportFileLimit)
	fileHeader, err := c.FormFile(config.ImportFileKey)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("数据库导入，取上传文件异常")
		c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, errors.Errorf("数据库导入，取上传文件异常: %+v", err)))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("数据库导入，打开上传文件异常")
		c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, errors.Errorf("数据库导入，打开上传文件异常: %+v", err)))
		return
	}
	defer util.CloseIo(ctx, file)
	c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, repo.Import(ctx, file)))
}
