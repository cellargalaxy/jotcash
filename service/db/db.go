package db

import (
	"context"

	"github.com/cellargalaxy/jotcash/db"
)

func CheckToken(ctx context.Context) error {
	return db.CheckToken(ctx)
}
