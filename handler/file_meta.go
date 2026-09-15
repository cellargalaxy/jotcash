package handler

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/repo"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func DownloadFile(c *gin.Context) {
	ctx := c.Request.Context()

	var req model.FileDownloadReq
	err := c.ShouldBindJSON(&req)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("文件下载，请求参数解析异常")
		c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, errors.Errorf("文件下载，请求参数解析异常: %+v", err)))
		return
	}
	fileMeta, fileData, err := repo.DownloadFile(ctx, req.Id)
	if err != nil {
		c.JSON(http.StatusOK, util.NewHttpRespByErr(nil, err))
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fileMeta.FileName, url.QueryEscape(fileMeta.FileName)))
	c.Data(http.StatusOK, "application/octet-stream", fileData)
}
