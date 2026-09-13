package db

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"gorm.io/gorm"
)

type FileMetaInquiry model.FileMetaInquiry

func (this FileMetaInquiry) Where(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if len(this.Id) > 0 {
		tx = tx.Where("id in (?)", this.Id)
	}
	if len(this.FileHash) > 0 {
		tx = tx.Where("file_hash in (?)", this.FileHash)
	}
	if len(this.OperationId) > 0 {
		tx = tx.Where("operation_id in (?)", this.OperationId)
	}
	return tx
}
func (this FileMetaInquiry) Order(ctx context.Context, tx *gorm.DB) *gorm.DB {
	tx = tx.Order("id")
	return tx
}
func (this FileMetaInquiry) Limit(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if this.PageSize <= 0 {
		return tx
	}
	offset := (this.Page - 1) * this.PageSize
	tx = tx.Offset(offset).Limit(this.PageSize)
	return tx
}

func NewFileMetaInsertHandler(object ...*model.FileMeta) *util.InsertHandler[model.FileMeta] {
	handler := util.NewInsertHandler[model.FileMeta](model.FileMeta{}.TableName(), object...)
	return handler
}

func NewFileMetaUpdateHandler(object *model.FileMeta) *util.UpdateHandler[model.FileMeta] {
	handler := util.NewUpdateHandler[model.FileMeta](model.FileMeta{}.TableName(), object)
	return handler
}

func NewFileMetaDeleteHandler(inquiry model.FileMetaInquiry) *util.DeleteHandler[model.FileMeta] {
	handler := util.NewDeleteHandler[model.FileMeta](model.FileMeta{}.TableName(), FileMetaInquiry(inquiry))
	return handler
}

func NewFileMetaSelectHandler(inquiry model.FileMetaInquiry) *util.SelectHandler[model.FileMeta] {
	handler := util.NewSelectHandler[model.FileMeta](model.FileMeta{}.TableName(), FileMetaInquiry(inquiry))
	return handler
}

func InsertFileMeta(ctx context.Context, object ...*model.FileMeta) (int64, error) {
	handler := NewFileMetaInsertHandler(object...)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func UpdateFileMeta(ctx context.Context, object *model.FileMeta) (int64, error) {
	handler := NewFileMetaUpdateHandler(object)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func DeleteFileMeta(ctx context.Context, inquiry model.FileMetaInquiry) (int64, error) {
	handler := NewFileMetaDeleteHandler(inquiry)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func SelectFileMeta(ctx context.Context, inquiry model.FileMetaInquiry) ([]*model.FileMeta, int64, error) {
	handler := NewFileMetaSelectHandler(inquiry)
	err := Transaction(ctx, handler)
	if err != nil {
		return nil, 0, err
	}
	return handler.Object, handler.Count, nil
}
