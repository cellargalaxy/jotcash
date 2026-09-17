package handler

import (
	"context"
	"net/http"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service"
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

	engine.GET(util.PathPing, util.GinPing)
	engine.POST(util.PathPing, validate, GinPing)
	engine.POST(config.PathExpenseInsert, validate, InsertExpense)
	engine.POST(config.PathExpenseSelect, validate, util.NewGinPost("明细查询", service.SelectExpense))
	engine.POST(config.PathExpenseUpdate, validate, util.NewGinPost("明细编辑", service.UpdateExpense))
	engine.POST(config.PathExpenseDelete, validate, util.NewGinPost("明细删除", service.DeleteExpense))
	engine.POST(config.PathExpenseSwitch, validate, util.NewGinPost("记账币种切换", service.SwitchAccountingCurrency))
	engine.POST(config.PathExpenseDistinct, validate, util.NewGinPost("候选取值", service.SelectExpenseDistinct))
	engine.POST(config.PathOperationLogSelect, validate, util.NewGinPost("审计查看", service.SelectOperationLog))
	engine.POST(config.PathFileMetaSelect, validate, util.NewGinPost("文件列表", service.SelectFileMeta))
	engine.POST(config.PathFileMetaDownload, validate, DownloadFile)
	engine.POST(config.PathChangeToken, validate, util.NewGinPost("更换口令", service.ChangeToken))
	engine.POST(config.PathExportDb, validate, Export)
	engine.POST(config.PathImportDb, validate, Import)

	engine.Use(util.StaticCache)
	engine.StaticFS(util.PathStatic, http.FS(static.StaticFile))
	return engine
}

func validate(ctx *gin.Context) {
	if checkTokenBan(ctx) {
		return
	}
	util.ValidateGin(ctx, config.GetConfig(ctx).ServerToken, new(model.Claims))
	checkTokenFail(ctx)
}
