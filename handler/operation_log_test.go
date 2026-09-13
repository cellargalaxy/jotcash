package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/gin-gonic/gin"
)

type operationLogResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Object []*model.OperationLog `json:"object"`
		Count  int64                 `json:"count"`
	} `json:"data"`
}

func newTokenCtx(clientToken string) context.Context {
	return tool.SetClaims(util.GenCtx(), &model.Claims{ClientToken: clientToken})
}

// 建库时已写过一条「系统初始化」，这里再补两条，凑出可分页可筛选的数据
func newTestOperationLog(t *testing.T, clientToken string) {
	t.Helper()
	now := time.Now()
	_, err := db.InsertOperationLog(newTokenCtx(clientToken),
		&model.OperationLog{Id: util.GenId(), OperationType: model.OperationTypeDataEntry, Summary: "入库 37 笔，来源 2609.csv", Result: model.ResultSuccess, CreatedAt: now.Add(time.Minute)},
		&model.OperationLog{Id: util.GenId(), OperationType: model.OperationTypeExpenseEdit, Summary: "编辑明细", Result: model.ResultFailure, CreatedAt: now.Add(2 * time.Minute)},
	)
	if err != nil {
		t.Fatalf("插入测试审计异常: %+v", err)
	}
}

func selectOperationLog(t *testing.T, engine *gin.Engine, jwt string, inquiry model.OperationLogInquiry) operationLogResp {
	t.Helper()
	var resp operationLogResp
	doRequest(t, engine, newRequest(model.PathOperationLogSelect, jwt, inquiry), &resp)
	return resp
}

func TestSelectOperationLog(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	newTestOperationLog(t, clientToken)

	resp := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{})
	if resp.Code != http.StatusOK {
		t.Fatalf("查询审计应成功: %+v", resp)
	}
	if resp.Data.Count != 3 || len(resp.Data.Object) != 3 {
		t.Fatalf("全量查询不符: count=%d len=%d want=3", resp.Data.Count, len(resp.Data.Object))
	}
	//不传排序时默认最新在前
	if resp.Data.Object[0].OperationType != model.OperationTypeExpenseEdit || resp.Data.Object[2].OperationType != model.OperationTypeSystemInit {
		t.Errorf("默认排序应为最新在前: %+v", resp.Data.Object)
	}
}

func TestSelectOperationLogFilter(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	newTestOperationLog(t, clientToken)

	//C-3：按操作类型筛选
	resp := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDataEntry}})
	if resp.Data.Count != 1 || resp.Data.Object[0].Summary != "入库 37 笔，来源 2609.csv" {
		t.Errorf("按操作类型筛选不符: %+v", resp.Data)
	}

	//C-3：按时间区间筛选
	resp = selectOperationLog(t, engine, jwt, model.OperationLogInquiry{CreatedAtStart: time.Now().Add(90 * time.Second)})
	if resp.Data.Count != 1 || resp.Data.Object[0].OperationType != model.OperationTypeExpenseEdit {
		t.Errorf("按时间区间筛选不符: %+v", resp.Data)
	}

	resp = selectOperationLog(t, engine, jwt, model.OperationLogInquiry{Result: []string{model.ResultFailure}})
	if resp.Data.Count != 1 {
		t.Errorf("按操作结果筛选不符: %+v", resp.Data)
	}
}

func TestSelectOperationLogPage(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	newTestOperationLog(t, clientToken)

	//count是筛选结果全集，不受分页影响
	resp := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{Page: 1, PageSize: 2, Sort: "created_at asc"})
	if resp.Data.Count != 3 || len(resp.Data.Object) != 2 {
		t.Fatalf("首页不符: count=%d len=%d", resp.Data.Count, len(resp.Data.Object))
	}
	if resp.Data.Object[0].OperationType != model.OperationTypeSystemInit {
		t.Errorf("排序应可配: %+v", resp.Data.Object)
	}
	resp = selectOperationLog(t, engine, jwt, model.OperationLogInquiry{Page: 2, PageSize: 2, Sort: "created_at asc"})
	if resp.Data.Count != 3 || len(resp.Data.Object) != 1 {
		t.Errorf("末页不符: count=%d len=%d", resp.Data.Count, len(resp.Data.Object))
	}
}

func TestSelectOperationLogInvalid(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)

	//非枚举值要报错，而不是静默返回空列表
	inquiries := map[string]model.OperationLogInquiry{
		"操作类型非法":  {OperationType: []string{"乱填的"}},
		"操作结果非法":  {Result: []string{"乱填的"}},
		"时间区间倒挂":  {CreatedAtStart: time.Now(), CreatedAtEnd: time.Now().Add(-time.Hour)},
		"排序不在白名单": {Sort: "summary; drop table operation_log"},
	}
	for name, inquiry := range inquiries {
		resp := selectOperationLog(t, engine, jwt, inquiry)
		if resp.Code == http.StatusOK {
			t.Errorf("%s应报错: %+v", name, resp)
		}
	}
}

// NewGinPost用的是ShouldBindJSON，空body报的是EOF，前端不带筛选条件时也得传{}
func TestSelectOperationLogEmptyBody(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)

	var resp operationLogResp
	doRequest(t, engine, newRequest(model.PathOperationLogSelect, jwt, nil), &resp)
	if resp.Code == http.StatusOK {
		t.Errorf("空body应报错: %+v", resp)
	}
}

func TestSelectOperationLogWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	resp := selectOperationLog(t, engine, "", model.OperationLogInquiry{})
	if resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}
