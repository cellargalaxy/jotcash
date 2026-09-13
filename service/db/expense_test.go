package db

import (
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/shopspring/decimal"
)

func TestCheckExpenseInquiry(t *testing.T) {
	ctx := util.GenCtx()

	inquiry, err := checkExpenseInquiry(ctx, model.ExpenseInquiry{})
	if err != nil {
		t.Fatalf("空查询条件不应报错: %+v", err)
	}
	if inquiry.PageSize != pageSizeDefault {
		t.Errorf("分页应兜底: got=%d want=%d", inquiry.PageSize, pageSizeDefault)
	}
	if inquiry.Sort != expenseSortDefault {
		t.Errorf("排序应兜底: got=%s want=%s", inquiry.Sort, expenseSortDefault)
	}
	if inquiry.Deleted != model.DeletedNo {
		t.Errorf("默认应只查未删除: %d", inquiry.Deleted)
	}

	if inquiry, _ = checkExpenseInquiry(ctx, model.ExpenseInquiry{PageSize: pageSizeMax + 1}); inquiry.PageSize != pageSizeMax {
		t.Errorf("分页应封顶: got=%d want=%d", inquiry.PageSize, pageSizeMax)
	}
	if inquiry, _ = checkExpenseInquiry(ctx, model.ExpenseInquiry{PageSize: 5, Sort: "id asc"}); inquiry.PageSize != 5 || inquiry.Sort != "id asc" {
		t.Errorf("上层传了就不该被覆盖: %+v", inquiry)
	}

	for _, deleted := range []int{model.DeletedNo, model.DeletedAll, model.DeletedOnly} {
		if _, err = checkExpenseInquiry(ctx, model.ExpenseInquiry{Deleted: deleted}); err != nil {
			t.Errorf("枚举内的删除筛选不应报错: deleted=%d err=%+v", deleted, err)
		}
	}
	if _, err = checkExpenseInquiry(ctx, model.ExpenseInquiry{Deleted: 99}); err == nil {
		t.Errorf("枚举外的删除筛选应报错")
	}

	now := time.Now()
	if _, err = checkExpenseInquiry(ctx, model.ExpenseInquiry{ExpenseDateStart: now, ExpenseDateEnd: now.Add(-time.Hour)}); err == nil {
		t.Errorf("日期区间倒挂应报错")
	}
	//金额允许0与负数，只传一端不算倒挂
	min := decimal.RequireFromString("-1")
	max := decimal.RequireFromString("-100")
	if _, err = checkExpenseInquiry(ctx, model.ExpenseInquiry{ExpenseAmountMin: &min, ExpenseAmountMax: &max}); err == nil {
		t.Errorf("金额区间倒挂应报错")
	}
	if _, err = checkExpenseInquiry(ctx, model.ExpenseInquiry{ExpenseAmountMin: &max}); err != nil {
		t.Errorf("只传金额下限不应报错: %+v", err)
	}
	zero := decimal.Zero
	if _, err = checkExpenseInquiry(ctx, model.ExpenseInquiry{ExpenseAmountMin: &zero, ExpenseAmountMax: &zero}); err != nil {
		t.Errorf("上下限相等不算倒挂: %+v", err)
	}
}
