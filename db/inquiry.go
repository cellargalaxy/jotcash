package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var likeReplacer = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// 模糊匹配的值来自用户输入，%与_要当普通字符，转义后SQL里跟上escape '\'
func likeValue(value string) string {
	return fmt.Sprintf("%%%s%%", likeReplacer.Replace(value))
}

func pageLimit(ctx context.Context, tx *gorm.DB, page, pageSize int) (*gorm.DB, error) {
	if pageSize <= 0 {
		return tx, nil
	}
	if page < 1 {
		page = 1
	}
	return tx.Offset((page - 1) * pageSize).Limit(pageSize), nil
}

func sortOrder(ctx context.Context, tx *gorm.DB, sortMap map[string]string, sort, defaultSort string) (*gorm.DB, error) {
	if sort == "" {
		sort = defaultSort
	}
	order := sortMap[sort]
	if order == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"sort": sort}).Error("排序，不在白名单内")
		return tx, errors.Errorf("排序，不在白名单内: %s", sort)
	}
	return tx.Order(order), nil
}
