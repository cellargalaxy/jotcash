package repo

import (
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
)

func TestCheckFileMetaInquiry(t *testing.T) {
	ctx := util.GenCtx()

	inquiry, err := checkFileMetaInquiry(ctx, model.FileMetaInquiry{})
	if err != nil {
		t.Fatalf("空查询条件不应报错: %+v", err)
	}
	if inquiry.PageSize != pageSizeDefault {
		t.Errorf("分页应兜底: got=%d want=%d", inquiry.PageSize, pageSizeDefault)
	}
	if inquiry.Sort != fileMetaSortDefault {
		t.Errorf("排序应兜底: got=%s want=%s", inquiry.Sort, fileMetaSortDefault)
	}

	if inquiry, _ = checkFileMetaInquiry(ctx, model.FileMetaInquiry{PageSize: pageSizeMax + 1}); inquiry.PageSize != pageSizeMax {
		t.Errorf("分页应封顶: got=%d want=%d", inquiry.PageSize, pageSizeMax)
	}
	if inquiry, _ = checkFileMetaInquiry(ctx, model.FileMetaInquiry{PageSize: 5, Sort: "id asc"}); inquiry.PageSize != 5 || inquiry.Sort != "id asc" {
		t.Errorf("上层传了就不该被覆盖: %+v", inquiry)
	}

	now := time.Now()
	if _, err = checkFileMetaInquiry(ctx, model.FileMetaInquiry{CreatedAtStart: now, CreatedAtEnd: now.Add(-time.Hour)}); err == nil {
		t.Errorf("时间区间倒挂应报错")
	}
	if _, err = checkFileMetaInquiry(ctx, model.FileMetaInquiry{CreatedAtStart: now}); err != nil {
		t.Errorf("只传区间起点不应报错: %+v", err)
	}
}
