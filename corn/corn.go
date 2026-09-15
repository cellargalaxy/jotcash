package corn

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/service"
	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"
)

func Init(ctx context.Context) error {
	var err error
	object := cron.New()

	_, err = object.AddJob(config.GetConfig(ctx).DbBackupCron, new(BackupDbJob))
	if err != nil {
		return err
	}

	object.Start()
	logrus.WithContext(ctx).WithFields(logrus.Fields{"dbBackupCron": config.GetConfig(ctx).DbBackupCron}).Info("定时任务，备份数据库")
	return nil
}

type BackupDbJob struct {
}

func (this *BackupDbJob) Run() {
	ctx := util.GenCtx()

	err := service.Backup(ctx)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("定时任务，备份数据库异常")
		return
	}
}
