package db

import (
	"context"
	"io"

	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/tool"
)

func CheckToken(ctx context.Context) error {
	return db.CheckToken(ctx)
}

func ChangeToken(ctx context.Context, newToken string) error {
	err := tool.CheckToken(ctx, newToken)
	if err != nil {
		return err
	}
	return db.ChangeToken(ctx, newToken)
}

func Export(ctx context.Context, writer io.Writer) error {
	return db.Export(ctx, writer)
}

func Import(ctx context.Context, reader io.Reader) error {
	return db.Import(ctx, reader)
}
