package rdb

import (
	"testing"

	"github.com/cellargalaxy/go_common/util"
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

func TestPageLimit(t *testing.T) {
	ctx := newTestCtx(t)

	gormDb, err := Open(ctx)
	if err != nil {
		t.Fatalf("打开数据库异常: %+v", err)
	}
	defer util.CloseDb(ctx, gormDb)

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

// 排序是拼进SQL的，只能走白名单：命中取映射值，落白名单外一律报错
func TestSortOrder(t *testing.T) {
	ctx := newTestCtx(t)

	gormDb, err := Open(ctx)
	if err != nil {
		t.Fatalf("打开数据库异常: %+v", err)
	}
	defer util.CloseDb(ctx, gormDb)

	orderBy := func(tx *gorm.DB) string {
		object, ok := tx.Statement.Clauses["ORDER BY"]
		if !ok {
			return ""
		}
		order, ok := object.Expression.(clause.OrderBy)
		if !ok || len(order.Columns) != 1 {
			return ""
		}
		return order.Columns[0].Column.Name
	}

	//传空走默认排序
	tx, err := sortOrder(ctx, gormDb.Session(&gorm.Session{}), expenseSortMap, "", expenseSortDefault)
	if err != nil {
		t.Fatalf("默认排序异常: %+v", err)
	}
	if got := orderBy(tx); got != expenseSortMap[expenseSortDefault] {
		t.Errorf("默认排序不符: got=%s want=%s", got, expenseSortMap[expenseSortDefault])
	}
	//金额列是文本，白名单里映射成了cast，不能把请求里的原串直接拼进去
	tx, err = sortOrder(ctx, gormDb.Session(&gorm.Session{}), expenseSortMap, "expense_amount desc", expenseSortDefault)
	if err != nil {
		t.Fatalf("金额排序异常: %+v", err)
	}
	if got := orderBy(tx); got != "cast(expense_amount as real) desc" {
		t.Errorf("金额排序未取白名单映射值: got=%s", got)
	}

	//四张表的白名单各管各的，落在别人白名单里的取值也得拒掉
	for _, sortMap := range []map[string]string{expenseSortMap, fileBlobSortMap, fileMetaSortMap, operationLogSortMap} {
		for _, sort := range []string{"bank_name asc", "id asc; drop table expense", "1", " id asc"} {
			if _, ok := sortMap[sort]; ok {
				continue
			}
			if _, err = sortOrder(ctx, gormDb.Session(&gorm.Session{}), sortMap, sort, expenseSortDefault); err == nil {
				t.Errorf("白名单外的排序应报错: %s", sort)
			}
		}
	}
	//默认值本身不在白名单里时也得报错，不能静默不排序
	if _, err = sortOrder(ctx, gormDb.Session(&gorm.Session{}), fileBlobSortMap, "", expenseSortDefault); err == nil {
		t.Errorf("默认排序不在白名单内应报错")
	}
}
