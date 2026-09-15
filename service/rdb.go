package service

import (
	"context"

	"github.com/cellargalaxy/jotcash/service/repo"
)

func Backup(ctx context.Context) error {
	err := repo.Backup(ctx)
	//备份失败多半是盘满，清理照样要做，正好给下一轮腾地方
	eee := repo.ClearBackup(ctx)
	if err != nil {
		return err
	}
	return eee
}
