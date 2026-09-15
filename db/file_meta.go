package db

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"gorm.io/gorm"
)

var fileMetaSortMap = map[string]string{
	"id asc":          "id asc",
	"id desc":         "id desc",
	"file_name asc":   "file_name asc",
	"file_name desc":  "file_name desc",
	"created_at asc":  "created_at asc",
	"created_at desc": "created_at desc",
}

const fileMetaSortDefault = "id asc"

type FileMetaInquiry model.FileMetaInquiry

func (this FileMetaInquiry) Where(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
	if len(this.Id) > 0 {
		tx = tx.Where("id in (?)", this.Id)
	}
	if len(this.FileHash) > 0 {
		tx = tx.Where("file_hash in (?)", this.FileHash)
	}
	if len(this.FileName) > 0 {
		tx = tx.Where("file_name in (?)", this.FileName)
	}
	if len(this.OperationId) > 0 {
		tx = tx.Where("operation_id in (?)", this.OperationId)
	}
	if this.FileNameLike != "" {
		tx = tx.Where(`file_name like ? escape '\'`, likeValue(this.FileNameLike))
	}
	if !this.CreatedAtStart.IsZero() {
		tx = tx.Where("created_at >= ?", this.CreatedAtStart)
	}
	if !this.CreatedAtEnd.IsZero() {
		tx = tx.Where("created_at <= ?", this.CreatedAtEnd)
	}
	return tx, nil
}
func (this FileMetaInquiry) Order(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
	return sortOrder(ctx, tx, fileMetaSortMap, this.Sort, fileMetaSortDefault)
}
func (this FileMetaInquiry) Limit(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
	return pageLimit(ctx, tx, this.Page, this.PageSize)
}

func NewFileMetaInsertHandler(object ...*model.FileMeta) *util.InsertHandler[model.FileMeta] {
	handler := util.NewInsertHandler[model.FileMeta](model.FileMeta{}.TableName(), object...)
	return handler
}

func NewFileMetaUpdateHandler(object *model.FileMeta) *util.UpdateHandler[model.FileMeta] {
	handler := util.NewUpdateHandler[model.FileMeta](model.FileMeta{}.TableName(), object)
	return handler
}

func NewFileMetaSelectHandler(inquiry model.FileMetaInquiry) *util.SelectHandler[model.FileMeta] {
	handler := util.NewSelectHandler[model.FileMeta](model.FileMeta{}.TableName(), FileMetaInquiry(inquiry))
	return handler
}
