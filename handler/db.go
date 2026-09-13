package handler

import (
	"fmt"
	"net/http"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/service/db"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// 导出走的是二进制流，出错要能改回JSON响应，所以响应头拖到真正写第一个字节时再设
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
	logrus.WithContext(c).WithFields(logrus.Fields{"claims": tool.GetClaims(c)}).Info("数据库导出")
	writer := &dbFileWriter{c: c}
	err := db.Export(c, writer)
	if err == nil {
		return
	}
	if writer.written {
		logrus.WithContext(c).WithFields(logrus.Fields{"err": err}).Error("数据库导出，已开始写出，改不回错误响应")
		return
	}
	c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, err))
}
