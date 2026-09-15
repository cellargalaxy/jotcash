package db

import (
	"os"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	logrus.SetLevel(logrus.WarnLevel)
	code := m.Run()
	//db包的init会在测试二进制的工作目录建库
	os.RemoveAll("resource")
	os.RemoveAll("log")
	os.Exit(code)
}

func TestCheckOperationLogInquiry(t *testing.T) {
	ctx := util.GenCtx()

	inquiry, err := checkOperationLogInquiry(ctx, model.OperationLogInquiry{})
	if err != nil {
		t.Fatalf("空查询条件不应报错: %+v", err)
	}
	if inquiry.PageSize != pageSizeDefault {
		t.Errorf("分页应兜底: got=%d want=%d", inquiry.PageSize, pageSizeDefault)
	}
	if inquiry.Sort != operationLogSortDefault {
		t.Errorf("排序应兜底: got=%s want=%s", inquiry.Sort, operationLogSortDefault)
	}

	if inquiry, _ = checkOperationLogInquiry(ctx, model.OperationLogInquiry{PageSize: pageSizeMax + 1}); inquiry.PageSize != pageSizeMax {
		t.Errorf("分页应封顶: got=%d want=%d", inquiry.PageSize, pageSizeMax)
	}
	if inquiry, _ = checkOperationLogInquiry(ctx, model.OperationLogInquiry{PageSize: 5, Sort: "id asc"}); inquiry.PageSize != 5 || inquiry.Sort != "id asc" {
		t.Errorf("上层传了就不该被覆盖: %+v", inquiry)
	}

	if _, err = checkOperationLogInquiry(ctx, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDataEntry}}); err != nil {
		t.Errorf("枚举内的操作类型不应报错: %+v", err)
	}
	if _, err = checkOperationLogInquiry(ctx, model.OperationLogInquiry{OperationType: []string{"乱填的"}}); err == nil {
		t.Errorf("枚举外的操作类型应报错")
	}
	if _, err = checkOperationLogInquiry(ctx, model.OperationLogInquiry{Result: []string{"乱填的"}}); err == nil {
		t.Errorf("枚举外的操作结果应报错")
	}

	now := time.Now()
	if _, err = checkOperationLogInquiry(ctx, model.OperationLogInquiry{CreatedAtStart: now, CreatedAtEnd: now.Add(-time.Hour)}); err == nil {
		t.Errorf("时间区间倒挂应报错")
	}
	if _, err = checkOperationLogInquiry(ctx, model.OperationLogInquiry{CreatedAtStart: now}); err != nil {
		t.Errorf("只传区间起点不应报错: %+v", err)
	}
}
