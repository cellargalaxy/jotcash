package corn

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/service"
	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
)

func Init(ctx context.Context) error {
	var err error
	object := cron.New()
	defer object.Start()

	err = object.AddJob(config.GetConfig(ctx).DbBackupCron, new(BackupDbJob))
	if err != nil {
		return err
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"dbBackupCron": config.GetConfig(ctx).DbBackupCron}).Info("定时任务，备份数据库")

	return nil
}

type BackupDbJob struct {
}

func (this *BackupDbJob) Run() {
	ctx := util.GenCtx()

	service.BackupDb(ctx)
}
