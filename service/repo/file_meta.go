package repo

import (
	"context"

	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/rdb"
)

const fileMetaSortDefault = "created_at desc"

func SelectFileMeta(ctx context.Context, inquiry model.FileMetaInquiry) ([]*model.FileMeta, int64, error) {
	inquiry, err := checkFileMetaInquiry(ctx, inquiry)
	if err != nil {
		return nil, 0, err
	}

	transaction, err := rdb.NewTransaction(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer transaction.Close(ctx)

	fileMetaHandler := rdb.NewFileMetaSelectHandler(inquiry)
	err = transaction.AddCommit(fileMetaHandler).Exec(ctx)
	if err != nil {
		return nil, 0, err
	}
	return fileMetaHandler.Object, fileMetaHandler.Count, nil
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
