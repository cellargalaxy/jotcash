package db

import (
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestLikeValue(t *testing.T) {
	for value, want := range map[string]string{
		"马逊":    `%马逊%`,
		"100%退": `%100\%退%`,
		"a_b":   `%a\_b%`,
		`c\d`:   `%c\\d%`,
		"":      `%%`,
	} {
		if got := likeValue(value); got != want {
			t.Errorf("likeValue(%q): got=%s want=%s", value, got, want)
		}
	}
}

// 页码从1开始，传0或负数按第1页算，不能拼出负的OFFSET
func TestPageLimit(t *testing.T) {
	ctx := newTestCtx(t)

	gormDb, err := Open(ctx)
	if err != nil {
		t.Fatalf("打开数据库异常: %+v", err)
	}
	defer Close(ctx, gormDb)

	for _, page := range []int{0, -1, 1} {
		tx, err := pageLimit(ctx, gormDb.Session(&gorm.Session{}), page, 10)
		if err != nil {
			t.Fatalf("分页异常: %+v", err)
		}
		limit, ok := tx.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
		if !ok {
			t.Fatalf("分页未生效: page=%d", page)
		}
		if limit.Offset != 0 || limit.Limit == nil || *limit.Limit != 10 {
			t.Errorf("page=%d 分页不符: offset=%d limit=%v", page, limit.Offset, limit.Limit)
		}
	}

	tx, err := pageLimit(ctx, gormDb.Session(&gorm.Session{}), 1, 0)
	if err != nil {
		t.Fatalf("分页异常: %+v", err)
	}
	if _, ok := tx.Statement.Clauses["LIMIT"]; ok {
		t.Errorf("pageSize<=0不应分页")
	}
}
