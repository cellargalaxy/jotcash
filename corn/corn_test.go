package corn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	logrus.SetLevel(logrus.WarnLevel)
	code := m.Run()
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

// 配置里的默认表达式解析不了，服务一起来就会panic，所以它得跟着用例一起被钉住
func TestInit(t *testing.T) {
	ctx := util.GenCtx()
	if config.GetConfig(ctx).DbBackupCron == "" {
		t.Fatalf("备份表达式不应为空")
	}
	if err := Init(ctx); err != nil {
		t.Fatalf("定时任务启动异常: %+v", err)
	}
}

func TestInitIllegalCron(t *testing.T) {
	object := cron.New(cron.WithSeconds())

	//robfig/cron是「秒 分 时 日 月 周」六段，五段的标准crontab在这里是非法的
	for _, dbBackupCron := range []string{"", "0 4 * * *", "每天4点"} {
		if _, err := object.AddJob(dbBackupCron, new(BackupDbJob)); err == nil {
			t.Errorf("非法表达式应报错: %s", dbBackupCron)
		}
	}
}

// 定时任务这一侧自始至终没有口令，备份仍要落得下来
func TestBackupDbJob(t *testing.T) {
	ctx := util.GenCtx()
	if util.GetFileInfo(ctx, config.DbPath) == nil {
		t.Fatalf("库文件应已由建库流程建出来: %s", config.DbPath)
	}

	new(BackupDbJob).Run()

	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) != 1 {
		t.Fatalf("备份目录里应有一份备份: got=%d want=1", len(files))
	}
	dbData, err := util.ReadFile2Data(ctx, config.DbPath, nil)
	if err != nil {
		t.Fatalf("读库文件异常: %+v", err)
	}
	backupData, err := util.ReadFile2Data(ctx, filepath.Join(config.DbBackupPath, files[0].Name()), nil)
	if err != nil {
		t.Fatalf("读备份文件异常: %+v", err)
	}
	if len(dbData) != len(backupData) {
		t.Errorf("备份与库文件大小不符: db=%d backup=%d", len(dbData), len(backupData))
	}
}
