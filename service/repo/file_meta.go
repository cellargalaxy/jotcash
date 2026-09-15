package repo

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/rdb"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
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

func DownloadFile(ctx context.Context, id int64) (*model.FileMeta, []byte, error) {
	transaction, err := rdb.NewTransaction(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer transaction.Close(ctx)

	downloadHandler := rdb.NewFileDownloadHandler(id)
	err = transaction.AddCommit(downloadHandler).Exec(ctx)
	if err != nil {
		return nil, nil, err
	}
	if downloadHandler.FileMeta == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"id": id}).Warn("文件下载，文件不存在")
		return nil, nil, errors.Errorf("文件下载，文件不存在: %d", id)
	}
	if downloadHandler.FileBlob == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"id": id, "fileHash": downloadHandler.FileMeta.FileHash}).Error("文件下载，文件内容缺失")
		return nil, nil, errors.Errorf("文件下载，文件内容缺失: %d", id)
	}
	fileHash := util.EnSha256Hex(string(downloadHandler.FileBlob.FileData))
	if fileHash != downloadHandler.FileBlob.FileHash {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"id": id, "fileHash": fileHash, "want": downloadHandler.FileBlob.FileHash}).Error("文件下载，文件数据已损坏")
		return nil, nil, errors.Errorf("文件下载，文件数据已损坏: %d", id)
	}
	return downloadHandler.FileMeta, downloadHandler.FileBlob.FileData, nil
}

func checkFileMetaInquiry(ctx context.Context, inquiry model.FileMetaInquiry) (model.FileMetaInquiry, error) {
	err := checkTimeRange(ctx, inquiry.CreatedAtStart, inquiry.CreatedAtEnd)
	if err != nil {
		return inquiry, err
	}
	//db层默认id asc，文件列表要的是最新在前
	if inquiry.Sort == "" {
		inquiry.Sort = fileMetaSortDefault
	}
	return inquiry, nil
}
