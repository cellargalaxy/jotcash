package db

import (
	"context"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"gorm.io/gorm"
)

type OperationLogInquiry model.OperationLogInquiry

func (this OperationLogInquiry) Where(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if len(this.Id) > 0 {
		tx = tx.Where("id in (?)", this.Id)
	}
	if len(this.OperationType) > 0 {
		tx = tx.Where("operation_type in (?)", this.OperationType)
	}
	if len(this.ObjectType) > 0 {
		tx = tx.Where("object_type in (?)", this.ObjectType)
	}
	if len(this.ObjectId) > 0 {
		tx = tx.Where("object_id in (?)", this.ObjectId)
	}
	if len(this.Result) > 0 {
		tx = tx.Where("result in (?)", this.Result)
	}
	return tx
}
func (this OperationLogInquiry) Order(ctx context.Context, tx *gorm.DB) *gorm.DB {
	tx = tx.Order("id")
	return tx
}
func (this OperationLogInquiry) Limit(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if this.PageSize <= 0 {
		return tx
	}
	offset := (this.Page - 1) * this.PageSize
	tx = tx.Offset(offset).Limit(this.PageSize)
	return tx
}

func NewOperationLogInsertHandler(object ...*model.OperationLog) *util.InsertHandler[model.OperationLog] {
	handler := util.NewInsertHandler[model.OperationLog](model.OperationLog{}.TableName(), object...)
	return handler
}

func NewOperationLogUpdateHandler(object *model.OperationLog) *util.UpdateHandler[model.OperationLog] {
	handler := util.NewUpdateHandler[model.OperationLog](model.OperationLog{}.TableName(), object)
	return handler
}

func NewOperationLogDeleteHandler(inquiry model.OperationLogInquiry) *util.DeleteHandler[model.OperationLog] {
	handler := util.NewDeleteHandler[model.OperationLog](model.OperationLog{}.TableName(), OperationLogInquiry(inquiry))
	return handler
}

func NewOperationLogSelectHandler(inquiry model.OperationLogInquiry) *util.SelectHandler[model.OperationLog] {
	handler := util.NewSelectHandler[model.OperationLog](model.OperationLog{}.TableName(), OperationLogInquiry(inquiry))
	return handler
}

func InsertOperationLog(ctx context.Context, object ...*model.OperationLog) (int64, error) {
	handler := NewOperationLogInsertHandler(object...)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func UpdateOperationLog(ctx context.Context, object *model.OperationLog) (int64, error) {
	handler := NewOperationLogUpdateHandler(object)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func DeleteOperationLog(ctx context.Context, inquiry model.OperationLogInquiry) (int64, error) {
	handler := NewOperationLogDeleteHandler(inquiry)
	err := Transaction(ctx, handler)
	if err != nil {
		return 0, err
	}
	return handler.Count, nil
}

func SelectOperationLog(ctx context.Context, inquiry model.OperationLogInquiry) ([]*model.OperationLog, int64, error) {
	handler := NewOperationLogSelectHandler(inquiry)
	err := Transaction(ctx, handler)
	if err != nil {
		return nil, 0, err
	}
	return handler.Object, handler.Count, nil
}
