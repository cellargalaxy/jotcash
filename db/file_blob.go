package db

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
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

func NewFileBlobInsertHandler(object ...*model.FileBlob) *util.InsertHandler[model.FileBlob] {
	handler := util.NewInsertHandler[model.FileBlob](model.FileBlob{}.TableName(), object...)
	handler = handler.Clauses(clause.OnConflict{DoNothing: true})
	return handler
}

func NewFileBlobUpdateHandler(object *model.FileBlob) *util.UpdateHandler[model.FileBlob] {
	handler := util.NewUpdateHandler[model.FileBlob](model.FileBlob{}.TableName(), object)
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

func SelectFileBlob(ctx context.Context, inquiry model.FileBlobInquiry) ([]*model.FileBlob, int64, error) {
	handler := NewFileBlobSelectHandler(inquiry)
	err := Transaction(ctx, handler)
	if err != nil {
		return nil, 0, err
	}
	return handler.Object, handler.Count, nil
}
