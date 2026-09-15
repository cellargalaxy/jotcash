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
	if inquiry.PageSize != 0 {
		t.Errorf("不传分页应原样留空，前端导出csv靠它拉全量: got=%d want=0", inquiry.PageSize)
	}
	if inquiry.Sort != fileMetaSortDefault {
		t.Errorf("排序应兜底: got=%s want=%s", inquiry.Sort, fileMetaSortDefault)
	}

	if inquiry, _ = checkFileMetaInquiry(ctx, model.FileMetaInquiry{PageSize: 100000}); inquiry.PageSize != 100000 {
		t.Errorf("分页不应再封顶: got=%d want=100000", inquiry.PageSize)
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
