package db

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/sirupsen/logrus"
)

func newTestDb(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
}

func newTokenCtx(clientToken string) context.Context {
	return util.SetClaims(util.GenCtx(), &model.Claims{ClientToken: clientToken})
}

func catchLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := new(bytes.Buffer)
	origin := logrus.StandardLogger().Out
	originLevel := logrus.GetLevel()
	logrus.SetOutput(buffer)
	//要捞的建库口令是Info级，TestMain把全局级别压到了Warn，捞的这段得临时放开
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(origin)
		logrus.SetLevel(originLevel)
	})
	return buffer
}

func findLogField(text, key string) string {
	index := strings.Index(text, "["+key+":")
	if index < 0 {
		return ""
	}
	text = text[index+len(key)+2:]
	index = strings.Index(text, "]")
	if index < 0 {
		return ""
	}
	return text[:index]
}

func TestCreate(t *testing.T) {
	newTestDb(t)
	ctx := util.GenCtx()
	serverToken := config.GetConfig(util.GenCtx()).ServerToken

	buffer := catchLog(t)
	if err := Create(ctx); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}
	if util.GetPathInfo(ctx, config.DbPath) == nil {
		t.Fatalf("初始化后库文件应存在: %s", config.DbPath)
	}
	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("初始前端口令没有打印: %s", buffer.String())
	}
	if findLogField(buffer.String(), "serverToken") != serverToken {
		t.Errorf("初始后端口令没有打印: %s", buffer.String())
	}
	if err := tool.CheckToken(ctx, clientToken); err != nil {
		t.Errorf("生成的初始口令不满足强度: %+v", err)
	}

	tokenCtx := newTokenCtx(clientToken)
	gormDb, err := db.Open(tokenCtx)
	if err != nil {
		t.Fatalf("用初始口令打开库异常: %+v", err)
	}
	for _, object := range []interface{}{&model.Expense{}, &model.OperationLog{}, &model.FileMeta{}, &model.FileBlob{}} {
		if !gormDb.Migrator().HasTable(object) {
			t.Errorf("表未建出来: %T", object)
		}
	}
	util.CloseDb(tokenCtx, gormDb)
	objects, count, err := SelectOperationLog(tokenCtx, model.OperationLogInquiry{OperationType: []string{model.OperationTypeSystemInit}})
	if err != nil {
		t.Fatalf("查询系统初始化审计异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || objects[0].Result != model.ResultSuccess {
		t.Errorf("系统初始化审计不符: count=%d %+v", count, objects)
	}

	buffer2 := catchLog(t)
	if err = Create(ctx); err != nil {
		t.Fatalf("重复初始化异常: %+v", err)
	}
	if findLogField(buffer2.String(), "clientToken") != "" {
		t.Errorf("库已存在时不应再打印初始口令: %s", buffer2.String())
	}
	if _, count, _ = SelectOperationLog(tokenCtx, model.OperationLogInquiry{}); count != 1 {
		t.Errorf("重复初始化不应再记审计: count=%d", count)
	}
}

func TestCreateEmptyDbFile(t *testing.T) {
	newTestDb(t)
	ctx := util.GenCtx()
	if err := util.WriteData2File(ctx, nil, config.DbPath); err != nil {
		t.Fatalf("写0字节库文件异常: %+v", err)
	}

	buffer := catchLog(t)
	if err := Create(ctx); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}
	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("0字节库文件是残骸，应当重新初始化: %s", buffer.String())
	}
	if err := CheckToken(newTokenCtx(clientToken)); err != nil {
		t.Errorf("重新初始化后的口令应能打开库: %+v", err)
	}
	if err := CheckToken(newTokenCtx("wrong-client-token")); err == nil {
		t.Errorf("重新初始化后其他口令不应能打开库")
	}
}

// 锁从db包迁到这一层之后，整库覆盖与并发读写的互斥就压在dbLock上，这里钉住它
func TestConcurrentImport(t *testing.T) {
	newTestDb(t)
	ctx := util.GenCtx()

	buffer := catchLog(t)
	if err := Create(ctx); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}
	clientToken := findLogField(buffer.String(), "clientToken")
	if clientToken == "" {
		t.Fatalf("没捞到初始前端口令: %s", buffer.String())
	}
	tokenCtx := newTokenCtx(clientToken)

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if _, _, err := SelectOperationLog(tokenCtx, model.OperationLogInquiry{}); err != nil {
					errs <- err
					return
				}
				if _, _, err := SelectExpense(tokenCtx, model.ExpenseInquiry{}); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 2; j++ {
			writer := new(bytes.Buffer)
			if err := Export(tokenCtx, writer); err != nil {
				errs <- err
				return
			}
			if err := Import(tokenCtx, bytes.NewReader(writer.Bytes())); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("并发读写异常: %+v", err)
	}
	if _, _, err := SelectOperationLog(tokenCtx, model.OperationLogInquiry{}); err != nil {
		t.Errorf("并发之后查询异常: %+v", err)
	}
}
