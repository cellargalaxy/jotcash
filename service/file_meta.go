package service

import (
	"context"

	common_model "github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/service/db"
)

func SelectFileMeta(ctx context.Context, inquiry model.FileMetaInquiry) (any, error) {
	objects, count, err := db.SelectFileMeta(ctx, inquiry)
	if err != nil {
		return nil, err
	}
	return common_model.HttpData{Object: objects, Count: count}, nil
}
