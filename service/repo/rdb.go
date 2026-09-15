package repo

import (
	"context"
	"io"

	"github.com/cellargalaxy/jotcash/rdb"
	"github.com/cellargalaxy/jotcash/tool"
)

func CheckToken(ctx context.Context) error {
	return rdb.CheckToken(ctx)
}

func ChangeToken(ctx context.Context, newToken string) error {
	err := tool.CheckToken(ctx, newToken)
	if err != nil {
		return err
	}
	return rdb.ChangeToken(ctx, newToken)
}

func Export(ctx context.Context, writer io.Writer) error {
	return rdb.Export(ctx, writer)
}

func Import(ctx context.Context, reader io.Reader) error {
	return rdb.Import(ctx, reader)
}

func Backup(ctx context.Context) error {
	return rdb.Backup(ctx)
}

func ClearBackup(ctx context.Context) error {
	return rdb.ClearBackup(ctx)
}
