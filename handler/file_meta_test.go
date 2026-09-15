package handler_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/rdb"
	"github.com/gin-gonic/gin"
)

type fileMetaResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Object []*model.FileMeta `json:"object"`
		Count  int64             `json:"count"`
	} `json:"data"`
}

// 文件表建库时是空的，测试自己铺三条，其中两条挂在同一次入库审计下
func newTestFileMeta(t *testing.T, clientToken string) int64 {
	t.Helper()
	now := time.Now()
	operationId := util.GenId()
	execTransaction(t, clientToken, db.NewFileMetaInsertHandler(
		&model.FileMeta{Id: util.GenId(), FileHash: "hash-1", FileName: "2609.csv", FileSize: 37, OperationId: operationId, CreatedAt: now.Add(time.Minute)},
		&model.FileMeta{Id: util.GenId(), FileHash: "hash-2", FileName: "2609快照.csv", FileSize: 41, OperationId: operationId, CreatedAt: now.Add(2 * time.Minute)},
		&model.FileMeta{Id: util.GenId(), FileHash: "hash-3", FileName: "2610.csv", FileSize: 53, OperationId: util.GenId(), CreatedAt: now.Add(3 * time.Minute)},
	))
	return operationId
}

func selectFileMeta(t *testing.T, engine *gin.Engine, jwt string, inquiry model.FileMetaInquiry) fileMetaResp {
	t.Helper()
	var resp fileMetaResp
	doRequest(t, engine, newRequest(config.PathFileMetaSelect, jwt, inquiry), &resp)
	return resp
}

func TestSelectFileMeta(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	newTestFileMeta(t, clientToken)

	resp := selectFileMeta(t, engine, jwt, model.FileMetaInquiry{})
	if resp.Code != http.StatusOK {
		t.Fatalf("查询文件应成功: %+v", resp)
	}
	if resp.Data.Count != 3 || len(resp.Data.Object) != 3 {
		t.Fatalf("全量查询不符: count=%d len=%d want=3", resp.Data.Count, len(resp.Data.Object))
	}
	//不传排序时默认最新在前
	if resp.Data.Object[0].FileName != "2610.csv" {
		t.Errorf("默认排序应为最新在前: %+v", resp.Data.Object)
	}
	//D-1：文件列表只读元数据，不带文件内容
	if resp.Data.Object[0].FileSize != 53 || resp.Data.Object[0].FileHash != "hash-3" {
		t.Errorf("元数据字段不符: %+v", resp.Data.Object[0])
	}
}

func TestSelectFileMetaFilter(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	operationId := newTestFileMeta(t, clientToken)

	//D-4：按来源审计筛选
	resp := selectFileMeta(t, engine, jwt, model.FileMetaInquiry{OperationId: []int64{operationId}})
	if resp.Data.Count != 2 {
		t.Errorf("按来源审计筛选不符: %+v", resp.Data)
	}

	//D-4：按文件名筛选
	resp = selectFileMeta(t, engine, jwt, model.FileMetaInquiry{FileNameLike: "2609"})
	if resp.Data.Count != 2 {
		t.Errorf("按文件名模糊筛选不符: %+v", resp.Data)
	}
	resp = selectFileMeta(t, engine, jwt, model.FileMetaInquiry{FileName: []string{"2610.csv"}})
	if resp.Data.Count != 1 {
		t.Errorf("按文件名精确筛选不符: %+v", resp.Data)
	}

	//D-4：按时间筛选
	resp = selectFileMeta(t, engine, jwt, model.FileMetaInquiry{CreatedAtStart: time.Now().Add(150 * time.Second)})
	if resp.Data.Count != 1 || resp.Data.Object[0].FileName != "2610.csv" {
		t.Errorf("按时间区间筛选不符: %+v", resp.Data)
	}

	//D-2：同一内容可被多条元数据引用，按内容哈希能反查
	resp = selectFileMeta(t, engine, jwt, model.FileMetaInquiry{FileHash: []string{"hash-1"}})
	if resp.Data.Count != 1 {
		t.Errorf("按内容哈希筛选不符: %+v", resp.Data)
	}
}

func TestSelectFileMetaPage(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	newTestFileMeta(t, clientToken)

	//count是筛选结果全集，不受分页影响
	resp := selectFileMeta(t, engine, jwt, model.FileMetaInquiry{Page: 1, PageSize: 2, Sort: "file_name asc"})
	if resp.Data.Count != 3 || len(resp.Data.Object) != 2 {
		t.Fatalf("首页不符: count=%d len=%d", resp.Data.Count, len(resp.Data.Object))
	}
	if resp.Data.Object[0].FileName != "2609.csv" {
		t.Errorf("排序应可配: %+v", resp.Data.Object)
	}
	resp = selectFileMeta(t, engine, jwt, model.FileMetaInquiry{Page: 2, PageSize: 2, Sort: "file_name asc"})
	if resp.Data.Count != 3 || len(resp.Data.Object) != 1 {
		t.Errorf("末页不符: count=%d len=%d", resp.Data.Count, len(resp.Data.Object))
	}
}

func TestSelectFileMetaInvalid(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)

	inquiries := map[string]model.FileMetaInquiry{
		"时间区间倒挂":  {CreatedAtStart: time.Now(), CreatedAtEnd: time.Now().Add(-time.Hour)},
		"排序不在白名单": {Sort: "file_name; drop table file_meta"},
	}
	for name, inquiry := range inquiries {
		resp := selectFileMeta(t, engine, jwt, inquiry)
		if resp.Code == http.StatusOK {
			t.Errorf("%s应报错: %+v", name, resp)
		}
	}
}

func TestSelectFileMetaWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	resp := selectFileMeta(t, engine, "", model.FileMetaInquiry{})
	if resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}
