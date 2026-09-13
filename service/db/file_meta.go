package db

import (
	"context"

	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
)

const fileMetaSortDefault = "created_at desc"

func SelectFileMeta(ctx context.Context, inquiry model.FileMetaInquiry) ([]*model.FileMeta, int64, error) {
	inquiry, err := checkFileMetaInquiry(ctx, inquiry)
	if err != nil {
		return nil, 0, err
	}
	return db.SelectFileMeta(ctx, inquiry)
}

func checkFileMetaInquiry(ctx context.Context, inquiry model.FileMetaInquiry) (model.FileMetaInquiry, error) {
	err := checkTimeRange(ctx, inquiry.CreatedAtStart, inquiry.CreatedAtEnd)
	if err != nil {
		return inquiry, err
	}
	inquiry.PageSize = checkPageSize(inquiry.PageSize)
	//db层默认id asc，文件列表要的是最新在前
	if inquiry.Sort == "" {
		inquiry.Sort = fileMetaSortDefault
	}
	return inquiry, nil
}
