package handler_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	common_model "github.com/cellargalaxy/go_common/model"
	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/rdb"
	"github.com/gin-gonic/gin"
)

func exportDb(t *testing.T, engine *gin.Engine, jwt string) *httptest.ResponseRecorder {
	t.Helper()
	writer := httptest.NewRecorder()
	engine.ServeHTTP(writer, newRequest(config.PathExportDb, jwt, nil))
	return writer
}

func TestExportDb(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)

	writer := exportDb(t, engine, jwt)
	if writer.Code != http.StatusOK {
		t.Fatalf("导出应成功: code=%d body=%s", writer.Code, writer.Body.String())
	}
	if !strings.HasPrefix(writer.Header().Get("Content-Disposition"), "attachment;") {
		t.Errorf("应是附件下载: %s", writer.Header().Get("Content-Disposition"))
	}
	if writer.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("Content-Type不符: %s", writer.Header().Get("Content-Type"))
	}
	if writer.Body.Len() == 0 {
		t.Fatalf("导出内容为空")
	}

	//§一前提2：导出的是加密态，明文内容不该在字节流里裸奔
	if strings.Contains(writer.Body.String(), "系统初始化，创建加密数据库") {
		t.Errorf("导出内容不应是明文可读的")
	}
	//H-2 / §九不变式3：口令不进导出
	if strings.Contains(writer.Body.String(), clientToken) {
		t.Errorf("导出内容泄露前端口令")
	}

	//B-1 + C-1：导出是唯一要记审计的读操作，且这条记录就在导出的快照里
	logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDbExport}})
	if logs.Data.Count != 1 {
		t.Fatalf("导出应记一条审计: %+v", logs.Data)
	}
	if logs.Data.Object[0].Changes != "" {
		t.Errorf("导出不应记变更内容: %+v", logs.Data.Object[0])
	}
}

// 快照必须是能真正打开的一致性副本，而不只是一堆字节
func TestExportDbUsable(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	newTestFileMeta(t, clientToken)

	writer := exportDb(t, engine, jwt)
	if writer.Code != http.StatusOK {
		t.Fatalf("导出应成功: code=%d", writer.Code)
	}

	//B-2/B-3：导出的库能用同一口令原样导回来，导回后数据与审计都在
	if err := db.Import(newTokenCtx(clientToken), strings.NewReader(writer.Body.String())); err != nil {
		t.Fatalf("导出的库应能导回: %+v", err)
	}
	if files := selectFileMeta(t, engine, jwt, model.FileMetaInquiry{}); files.Data.Count != 3 {
		t.Errorf("导回后文件元数据不符: %+v", files.Data)
	}
	logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDbExport}})
	if logs.Data.Count != 1 {
		t.Errorf("快照里应带着自己那条导出审计: %+v", logs.Data)
	}
}

func TestExportDbWrongToken(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	serverToken := config.GetConfig(util.GenCtx()).ServerToken

	//口令错时要能改回JSON报错，而不是吐半个文件
	writer := exportDb(t, engine, newJwt(t, serverToken, "wrong-client-token-1", time.Hour))
	var resp common_model.HttpResp
	if err := util.JsonStr2Struct(writer.Body.String(), &resp); err != nil {
		t.Fatalf("口令错时应返回JSON: body=%s err=%+v", writer.Body.String(), err)
	}
	if resp.Code == http.StatusOK {
		t.Errorf("口令错应导出失败: %+v", resp)
	}
	if writer.Header().Get("Content-Disposition") != "" {
		t.Errorf("失败时不该带附件响应头: %s", writer.Header().Get("Content-Disposition"))
	}
	//失败的导出不该留下审计
	jwt := newJwt(t, serverToken, clientToken, time.Hour)
	if logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDbExport}}); logs.Data.Count != 0 {
		t.Errorf("失败的导出不应记审计: %+v", logs.Data)
	}
}

func TestExportDbWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	writer := exportDb(t, engine, "")
	var resp common_model.HttpResp
	if err := util.JsonStr2Struct(writer.Body.String(), &resp); err != nil {
		t.Fatalf("没带jwt应返回JSON: body=%s err=%+v", writer.Body.String(), err)
	}
	if resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}

func TestExportDbNoBackupResidue(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)

	exportDb(t, engine, jwt)
	files, err := util.ListFile(util.GenCtx(), config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) > 0 {
		t.Errorf("导出用的临时快照没清理: %d", len(files))
	}
}

func newImportRequest(t *testing.T, jwt string, data []byte) *http.Request {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	if data != nil {
		part, err := writer.CreateFormFile(config.ImportFileKey, "jotcash.db")
		if err != nil {
			t.Fatalf("构造上传表单异常: %+v", err)
		}
		if _, err = part.Write(data); err != nil {
			t.Fatalf("写上传表单异常: %+v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭上传表单异常: %+v", err)
	}
	request := httptest.NewRequest(http.MethodPost, config.PathImportDb, body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if jwt != "" {
		request.Header.Set(util.AuthorizationKey, util.BearerKey+" "+jwt)
	}
	return request
}

func importDb(t *testing.T, engine *gin.Engine, jwt string, data []byte) common_model.HttpResp {
	t.Helper()
	var resp common_model.HttpResp
	doRequest(t, engine, newImportRequest(t, jwt, data), &resp)
	return resp
}

func TestImportDb(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	newTestFileMeta(t, clientToken)

	snapshot := exportDb(t, engine, jwt).Body.Bytes()

	//快照之后再加一条，导入要把它整库覆盖掉
	newTestOperationLog(t, clientToken)
	if logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDataEntry}}); logs.Data.Count != 1 {
		t.Fatalf("快照后应多出一条审计: %+v", logs.Data)
	}

	if resp := importDb(t, engine, jwt, snapshot); resp.Code != http.StatusOK {
		t.Fatalf("导入应成功: %+v", resp)
	}

	//B-2：整库覆盖，快照之后的数据没了，快照里的数据都在
	if logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDataEntry}}); logs.Data.Count != 0 {
		t.Errorf("快照之后的数据应被整库覆盖掉: %+v", logs.Data)
	}
	if files := selectFileMeta(t, engine, jwt, model.FileMetaInquiry{}); files.Data.Count != 3 {
		t.Errorf("快照里的数据应还在: %+v", files.Data)
	}
	//B-2：审计落新库
	logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDbImport}})
	if logs.Data.Count != 1 {
		t.Fatalf("导入应记一条审计且落在新库里: %+v", logs.Data)
	}
	if logs.Data.Object[0].Changes != "" {
		t.Errorf("导入不应记变更内容: %+v", logs.Data.Object[0])
	}
}

func TestImportDbIllegal(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig(util.GenCtx()).ServerToken, clientToken, time.Hour)
	newTestFileMeta(t, clientToken)

	//没带文件 / 非数据库文件 / 0字节，都要被拒
	if resp := importDb(t, engine, jwt, nil); resp.Code == http.StatusOK {
		t.Errorf("没带文件应报错: %+v", resp)
	}
	if resp := importDb(t, engine, jwt, []byte("我不是数据库")); resp.Code == http.StatusOK {
		t.Errorf("非数据库文件应报错: %+v", resp)
	}
	if resp := importDb(t, engine, jwt, []byte{}); resp.Code == http.StatusOK {
		t.Errorf("0字节文件应报错: %+v", resp)
	}

	//原库原封不动，且失败的导入不留审计
	if files := selectFileMeta(t, engine, jwt, model.FileMetaInquiry{}); files.Data.Count != 3 {
		t.Errorf("被拒的导入不应影响原库: %+v", files.Data)
	}
	if logs := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDbImport}}); logs.Data.Count != 0 {
		t.Errorf("失败的导入不应记审计: %+v", logs.Data)
	}
}

func TestImportDbWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	if resp := importDb(t, engine, "", []byte("whatever")); resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}
