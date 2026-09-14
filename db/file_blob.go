package db

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var fileBlobSortMap = map[string]string{
	"file_hash asc":  "file_hash asc",
	"file_hash desc": "file_hash desc",
}

const fileBlobSortDefault = "file_hash asc"

type FileBlobInquiry model.FileBlobInquiry

func (this FileBlobInquiry) Where(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
	if len(this.FileHash) > 0 {
		tx = tx.Where("file_hash in (?)", this.FileHash)
	}
	return tx, nil
}
func (this FileBlobInquiry) Order(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
	return sortOrder(ctx, tx, fileBlobSortMap, this.Sort, fileBlobSortDefault)
}
func (this FileBlobInquiry) Limit(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
	return pageLimit(ctx, tx, this.Page, this.PageSize)
}

func NewFileBlobInsertHandler(object ...*model.FileBlob) *FileBlobInsertHandler {
	handler := new(FileBlobInsertHandler)
	handler.Object = object
	return handler
}

type FileBlobInsertHandler struct {
	Object []*model.FileBlob
	Count  int64
}

func (this *FileBlobInsertHandler) Transaction(ctx context.Context, tx *gorm.DB) error {
	if len(this.Object) == 0 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Warn("插入file_blob，为空")
		return nil
	}

	result := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(this.Object, util.DbBatchSize)
	this.Count = result.RowsAffected
	err := result.Error
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("插入file_blob，异常")
		return errors.Errorf("插入file_blob，异常: %+v", err)
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"count": this.Count}).Info("插入file_blob，完成")
	return nil
}

func NewFileBlobUpdateHandler(object *model.FileBlob) *util.UpdateHandler[model.FileBlob] {
	handler := util.NewUpdateHandler[model.FileBlob](model.FileBlob{}.TableName(), object)
	return handler
}

func NewFileBlobDeleteHandler(inquiry model.FileBlobInquiry) *util.DeleteHandler[model.FileBlob] {
	inquiry.Page, inquiry.PageSize = 0, 0
	handler := util.NewDeleteHandler[model.FileBlob](model.FileBlob{}.TableName(), FileBlobInquiry(inquiry))
	return handler
}

func NewFileBlobSelectHandler(inquiry model.FileBlobInquiry) *util.SelectHandler[model.FileBlob] {
	handler := util.NewSelectHandler[model.FileBlob](model.FileBlob{}.TableName(), FileBlobInquiry(inquiry))
	return handler
}

func InsertFileBlob(ctx context.Context, object ...*model.FileBlob) (int64, error) {
	handler := NewFileBlobInsertHandler(object...)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func UpdateFileBlob(ctx context.Context, object *model.FileBlob) (int64, error) {
	handler := NewFileBlobUpdateHandler(object)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func DeleteFileBlob(ctx context.Context, inquiry model.FileBlobInquiry) (int64, error) {
	handler := NewFileBlobDeleteHandler(inquiry)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func SelectFileBlob(ctx context.Context, inquiry model.FileBlobInquiry) ([]*model.FileBlob, int64, error) {
	handler := NewFileBlobSelectHandler(inquiry)
	err := Transaction(ctx, handler)
	if err != nil {
		return nil, 0, err
	}
	return handler.Object, handler.Count, nil
}
