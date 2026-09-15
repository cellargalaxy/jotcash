package rdb

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/model"
)

func TestExport(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestExpense()
	if _, err := insertExpense(ctx, expense); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	buffer := new(bytes.Buffer)
	if err := Export(ctx, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}
	if buffer.Len() == 0 {
		t.Fatalf("导出内容为空")
	}
	if bytes.Contains(buffer.Bytes(), []byte("SQLite format 3")) {
		t.Errorf("导出的是明文库，加密没生效")
	}
	if bytes.Contains(buffer.Bytes(), []byte(expense.Counterparty)) {
		t.Errorf("导出内容里能直接搜到业务数据，加密没生效")
	}

	if err := Import(ctx, bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("导回异常: %+v", err)
	}
	gormDb, err := Open(ctx)
	if err != nil {
		t.Fatalf("导回后打开数据库异常: %+v", err)
	}
	for i := range migrateModels {
		if !gormDb.Migrator().HasTable(migrateModels[i].TableName()) {
			t.Errorf("导入后应把表结构补齐，缺表: %s", migrateModels[i].TableName())
		}
	}
	gormDb.Close(ctx)

	objects, count, err := selectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}})
	if err != nil {
		t.Fatalf("导回后查询异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || !objects[0].ExpenseAmount.Equal(expense.ExpenseAmount) {
		t.Errorf("导回后的数据不符: count=%d %+v", count, objects)
	}
	//导出审计写在原库上，所以它跟着快照一起被导出、又跟着快照导了回来
	if _, count, _ = selectOperationLog(ctx, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDbExport}}); count != 1 {
		t.Errorf("导出审计应跟着快照一起导回来: count=%d want=1", count)
	}
	if _, count, _ = selectOperationLog(ctx, model.OperationLogInquiry{OperationType: []string{model.OperationTypeDbImport}}); count != 1 {
		t.Errorf("导入审计应落在顶上来的新库里: count=%d want=1", count)
	}
}

// B-2「结构合法」：口令对得上但不是本系统的库，不能拿来把用户数据覆盖成空库
func TestImportWrongSchema(t *testing.T) {
	ctx := newTestCtx(t)
	if _, err := insertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	otherPath := "resource/other.db"
	if err := create(ctx, otherPath, testClientToken); err != nil {
		t.Fatalf("建库异常: %+v", err)
	}
	gormDb, err := open(ctx, otherPath, testClientToken)
	if err != nil {
		t.Fatalf("连库异常: %+v", err)
	}
	//四张表全删掉再建一张别的表，冒充口令对得上但不是本系统的库
	for i := range migrateModels {
		if err = gormDb.Exec("DROP TABLE " + migrateModels[i].TableName()).Error; err != nil {
			t.Fatalf("删表异常: %+v", err)
		}
	}
	if err = gormDb.Exec("CREATE TABLE other(id integer)").Error; err != nil {
		t.Fatalf("建表异常: %+v", err)
	}
	util.CloseDb(ctx, gormDb)
	data, err := util.ReadFile2Data(ctx, otherPath, nil)
	if err != nil {
		t.Fatalf("读库异常: %+v", err)
	}
	util.RemoveFile(ctx, otherPath)

	if err = Import(ctx, bytes.NewReader(data)); err == nil {
		t.Fatalf("结构不合法的库应拒绝导入")
	}
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Errorf("被拒的导入不应影响原库: count=%d", count)
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) > 0 {
		t.Errorf("被拒的导入没清理临时文件: %d", len(files))
	}
}

func TestImportOldDb(t *testing.T) {
	ctx := newTestCtx(t)
	if _, err := insertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	//造一个同口令、表在但少了两列的库文件，冒充旧版本导出的库（一张表都没有的库过不了B-2的结构校验）
	oldPath := "resource/old.db"
	if err := create(ctx, oldPath, testClientToken); err != nil {
		t.Fatalf("建旧库异常: %+v", err)
	}
	gormDb, err := open(ctx, oldPath, testClientToken)
	if err != nil {
		t.Fatalf("连旧库异常: %+v", err)
	}
	for _, column := range []string{"counterparty", "remark"} {
		if err = gormDb.Exec("ALTER TABLE expense DROP COLUMN " + column).Error; err != nil {
			t.Fatalf("删旧库列异常: %+v", err)
		}
	}
	util.CloseDb(ctx, gormDb)
	data, err := util.ReadFile2Data(ctx, oldPath, nil)
	if err != nil {
		t.Fatalf("读旧库异常: %+v", err)
	}
	util.RemoveFile(ctx, oldPath)

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				//建表标记要是在锁外从true翻回false，这里就会撞上缺表
				if _, _, err := selectExpense(ctx, model.ExpenseInquiry{}); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 5; j++ {
			if err := Import(ctx, bytes.NewReader(data)); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("并发导入旧库异常: %+v", err)
	}
}

func TestTokenSpecialChar(t *testing.T) {
	newTestDb(t)
	//备份目标库的口令是拼进uri的，这些字符都得经过转义才能原样传过去；空格转义成加号传不过去，已在口令强度里禁掉
	token := "p+w&d=12%34#x?y"
	ctx := newTokenCtx(token)
	if err := create(ctx, config.DbPath, token); err != nil {
		t.Fatalf("建库异常: %+v", err)
	}
	expense := newTestExpense()
	if _, err := insertExpense(ctx, expense); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	buffer := new(bytes.Buffer)
	if err := Export(ctx, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}
	if err := Import(ctx, bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("导出的库用同一口令导不回来: %+v", err)
	}
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 1 {
		t.Errorf("导回后数据不符: count=%d want=1", count)
	}

	newToken := "n+w&d=56%78#z?q"
	if err := ChangeToken(ctx, newToken); err != nil {
		t.Fatalf("换口令异常: %+v", err)
	}
	if err := CheckToken(newTokenCtx(newToken)); err != nil {
		t.Errorf("换成含特殊字符的口令后打不开库: %+v", err)
	}
}

// 卡在第一次写出上不动，把「副本正在流给客户端」这个窗口拉开
type blockWriter struct {
	start chan struct{}
	block chan struct{}
	once  sync.Once
	data  bytes.Buffer
}

func (this *blockWriter) Write(data []byte) (int, error) {
	this.once.Do(func() { close(this.start) })
	<-this.block
	return this.data.Write(data)
}

// 副本落盘之后就与库文件无关了，把它流给客户端这一段不该再占着读锁：
// 否则一个卡在半路的下载就能把换口令这类写锁操作堵死，连带挡住排在写锁后面的所有业务请求
func TestExportNotBlockWriteLock(t *testing.T) {
	ctx := newTestCtx(t)
	if _, err := insertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	writer := &blockWriter{start: make(chan struct{}), block: make(chan struct{})}
	exported := make(chan error, 1)
	go func() { exported <- Export(ctx, writer) }()
	select {
	case <-writer.start:
	case <-time.After(10 * time.Second):
		t.Fatalf("导出迟迟没开始写出")
	}

	//导出正卡在写出这一步，此时换口令必须还能拿到写锁
	changed := make(chan error, 1)
	go func() { changed <- ChangeToken(ctx, "new-client-token-7") }()
	select {
	case err := <-changed:
		if err != nil {
			t.Errorf("换口令异常: %+v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("导出把副本流出去时还攥着读锁，换口令被挡住")
	}

	close(writer.block)
	if err := <-exported; err != nil {
		t.Fatalf("导出异常: %+v", err)
	}
	//读锁虽然提前放了，流出去的仍是换口令之前那份副本，内容不能是空的
	if writer.data.Len() == 0 {
		t.Errorf("导出内容为空")
	}
}

func TestExportWrongToken(t *testing.T) {
	newTestCtx(t)

	buffer := new(bytes.Buffer)
	if err := Export(newTokenCtx("wrong-client-token"), buffer); err == nil {
		t.Errorf("错误口令导出应报错")
	}
}

func TestChangeToken(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestExpense()
	if _, err := insertExpense(ctx, expense); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	if err := ChangeToken(newTokenCtx("wrong-client-token"), "new-client-token-1"); err == nil {
		t.Errorf("旧口令错误应报错")
	}
	if err := ChangeToken(ctx, ""); err == nil {
		t.Errorf("新口令为空应报错")
	}
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Fatalf("失败的换口令不应影响原库: count=%d", count)
	}

	newToken := "new-client-token-1"
	if err := ChangeToken(ctx, newToken); err != nil {
		t.Fatalf("换口令异常: %+v", err)
	}

	if err := CheckToken(ctx); err == nil {
		t.Errorf("换口令后旧口令应打不开库")
	}
	newCtx := newTokenCtx(newToken)
	objects, count, err := selectExpense(newCtx, model.ExpenseInquiry{Id: []int64{expense.Id}})
	if err != nil {
		t.Fatalf("换口令后查询异常: %+v", err)
	}
	if count != 1 || len(objects) != 1 || objects[0].Counterparty != expense.Counterparty {
		t.Errorf("换口令后数据不符: count=%d %+v", count, objects)
	}
	if _, count, _ = selectOperationLog(newCtx, model.OperationLogInquiry{OperationType: []string{model.OperationTypeClientTokenSwap}}); count != 1 {
		t.Errorf("更换口令审计应落在新库里: count=%d want=1", count)
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) > 0 {
		t.Errorf("备份文件没清理: %d", len(files))
	}
}

func TestImportIllegal(t *testing.T) {
	ctx := newTestCtx(t)
	if _, err := insertExpense(ctx, newTestExpense()); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	buffer := new(bytes.Buffer)
	if err := Export(ctx, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}

	if err := Import(newTokenCtx("wrong-client-token"), bytes.NewReader(buffer.Bytes())); err == nil {
		t.Errorf("口令不匹配的库应拒绝导入")
	}
	if err := Import(ctx, bytes.NewReader([]byte("我不是数据库"))); err == nil {
		t.Errorf("非数据库文件应拒绝导入")
	}
	if err := Import(ctx, bytes.NewReader(nil)); err == nil {
		t.Errorf("0字节文件应拒绝导入")
	}
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{}); count != 1 {
		t.Errorf("导入失败不应影响原库: count=%d", count)
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) > 0 {
		t.Errorf("导入失败没清理临时文件: %d", len(files))
	}
}

func TestClearBackup(t *testing.T) {
	ctx := newTestCtx(t)
	limit := config.GetConfig(util.GenCtx()).DbBackupLimit

	var backupPaths []string
	for i := 0; i < limit+2; i++ {
		backupPath, err := genBackupPath(ctx)
		if err != nil {
			t.Fatalf("生成备份路径异常: %+v", err)
		}
		if err = util.WriteData2File(ctx, []byte("backup"), backupPath); err != nil {
			t.Fatalf("写备份文件异常: %+v", err)
		}
		backupPaths = append(backupPaths, backupPath)
	}

	if err := ClearBackup(ctx); err != nil {
		t.Fatalf("清理备份异常: %+v", err)
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) != limit {
		t.Errorf("备份保留数量: got=%d want=%d", len(files), limit)
	}
	for i := range backupPaths {
		exist := util.GetPathInfo(ctx, backupPaths[i]) != nil
		if i < 2 && exist {
			t.Errorf("旧备份未清理: %s", backupPaths[i])
		}
		if i >= 2 && !exist {
			t.Errorf("新备份不应清理: %s", backupPaths[i])
		}
	}
}

// 整库覆盖前原库要先留一份，导错了还能手动导回去
func TestImportBackupOrigin(t *testing.T) {
	ctx := newTestCtx(t)
	origin := newTestExpense()
	if _, err := insertExpense(ctx, origin); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}
	buffer := new(bytes.Buffer)
	if err := Export(ctx, buffer); err != nil {
		t.Fatalf("导出异常: %+v", err)
	}

	//再插一笔，这笔只在原库里有，导入之后会被覆盖掉
	lost := newTestExpense()
	if _, err := insertExpense(ctx, lost); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}
	if err := Import(ctx, bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("导入异常: %+v", err)
	}
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{Id: []int64{lost.Id}}); count != 0 {
		t.Fatalf("导入应已整库覆盖: count=%d want=0", count)
	}

	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) != 1 {
		t.Fatalf("备份目录里应只剩原库那一份: got=%d want=1", len(files))
	}
	originPath := filepath.Join(config.DbBackupPath, files[0].Name())
	if err = os.Rename(originPath, config.DbPath); err != nil {
		t.Fatalf("用原库备份回滚异常: %+v", err)
	}
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{Id: []int64{lost.Id}}); count != 1 {
		t.Errorf("回滚后被覆盖掉的数据应回来: count=%d want=1", count)
	}
}

// 后端口令泄露时攻击者能签出合法jwt，但拿自己加密的库也顶不掉原库
func TestImportForeignDb(t *testing.T) {
	ctx := newTestCtx(t)
	origin := newTestExpense()
	if _, err := insertExpense(ctx, origin); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	foreignToken := "foreign-client-token"
	if err := create(ctx, "foreign.db", foreignToken); err != nil {
		t.Fatalf("建外来库异常: %+v", err)
	}
	data, err := util.ReadFile2Data(ctx, "foreign.db", nil)
	if err != nil {
		t.Fatalf("读外来库异常: %+v", err)
	}

	//外来库拿外来口令自己能打开，校验副本这一关拦不住，拦住它的是原库打不开
	if err = Import(newTokenCtx(foreignToken), bytes.NewReader(data)); err == nil {
		t.Errorf("外来库不应能顶掉原库")
	}
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{Id: []int64{origin.Id}}); count != 1 {
		t.Errorf("原库应原封不动: count=%d want=1", count)
	}
	if err = CheckToken(ctx); err != nil {
		t.Errorf("原口令应照常可用: %+v", err)
	}
}

// 四个直接操作库文件的函数，入参不合法时必须在碰盘之前就拦下来
func TestBackupIllegalArgument(t *testing.T) {
	ctx := newTestCtx(t)

	dstPath := "resource/backup.db"
	if err := backup(ctx, "resource/not-exist.db", testClientToken, dstPath, testClientToken); err == nil {
		t.Errorf("来源库文件不存在应报错")
	}
	if err := backup(ctx, config.DbPath, "", dstPath, testClientToken); err == nil {
		t.Errorf("来源口令为空应报错")
	}
	if err := backup(ctx, config.DbPath, testClientToken, dstPath, ""); err == nil {
		t.Errorf("目标口令为空应报错")
	}
	if util.GetPathInfo(ctx, dstPath) != nil {
		t.Errorf("入参不合法时不应留下目标文件: %s", dstPath)
	}

	//目标文件已存在就得拒掉，不能把别人的库覆盖成快照
	if err := util.WriteData2File(ctx, []byte("占位"), dstPath); err != nil {
		t.Fatalf("写占位文件异常: %+v", err)
	}
	if err := backup(ctx, config.DbPath, testClientToken, dstPath, testClientToken); err == nil {
		t.Errorf("目标文件已存在应报错")
	}
	data, err := util.ReadFile2Data(ctx, dstPath, nil)
	if err != nil {
		t.Fatalf("读占位文件异常: %+v", err)
	}
	if string(data) != "占位" {
		t.Errorf("已存在的目标文件被覆盖了: %s", data)
	}
	util.RemoveFile(ctx, dstPath)

	if err := Export(ctx, nil); err == nil {
		t.Errorf("写出目标为空应报错")
	}
	if err := Import(ctx, nil); err == nil {
		t.Errorf("读入来源为空应报错")
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) > 0 {
		t.Errorf("入参不合法的导入导出不应留下临时文件: %d", len(files))
	}
}

// 备份目录里混进子目录时，它既不该被算进保留份数，也不该被当成备份删掉
func TestClearBackupSkipFolder(t *testing.T) {
	ctx := newTestCtx(t)

	var backupPaths []string
	for i := 0; i < 2; i++ {
		backupPath, err := genBackupPath(ctx)
		if err != nil {
			t.Fatalf("生成备份路径异常: %+v", err)
		}
		if err = util.WriteData2File(ctx, []byte("backup"), backupPath); err != nil {
			t.Fatalf("写备份文件异常: %+v", err)
		}
		backupPaths = append(backupPaths, backupPath)
	}
	subPath := filepath.Join(config.DbBackupPath, "sub")
	if err := util.CreateFolderPath(ctx, subPath); err != nil {
		t.Fatalf("建子目录异常: %+v", err)
	}

	//子目录要是被算进来，两份备份就一份都不用删了
	if err := clearBackup(ctx, config.DbBackupPath, 1); err != nil {
		t.Fatalf("清理备份异常: %+v", err)
	}
	if util.GetPathInfo(ctx, backupPaths[0]) != nil {
		t.Errorf("旧备份未清理: %s", backupPaths[0])
	}
	if util.GetPathInfo(ctx, backupPaths[1]) == nil {
		t.Errorf("新备份不应清理: %s", backupPaths[1])
	}
	if util.GetFolderInfo(ctx, subPath) == nil {
		t.Errorf("子目录不应被当成备份删掉: %s", subPath)
	}
}

// 三个动库文件的公开入口都从ctx取口令，取不到就得在碰盘之前报错
func TestBackupWithoutClaims(t *testing.T) {
	ctx := newTestCtx(t)
	expense := newTestExpense()
	if _, err := insertExpense(ctx, expense); err != nil {
		t.Fatalf("插入异常: %+v", err)
	}

	blankCtx := util.GenCtx()
	buffer := new(bytes.Buffer)
	if err := Export(blankCtx, buffer); err == nil {
		t.Errorf("ctx无口令时导出应报错")
	}
	if buffer.Len() > 0 {
		t.Errorf("被拦下的导出不应写出内容: %d", buffer.Len())
	}
	if err := Import(blankCtx, bytes.NewReader(nil)); err == nil {
		t.Errorf("ctx无口令时导入应报错")
	}
	if err := ChangeToken(blankCtx, "new-client-token-5"); err == nil {
		t.Errorf("ctx无口令时换口令应报错")
	}

	//三次都没走到动库文件那一步：原口令照常可用，备份目录里也不该多出东西
	if _, count, _ := selectExpense(ctx, model.ExpenseInquiry{Id: []int64{expense.Id}}); count != 1 {
		t.Errorf("被拦下的操作不应影响原库: count=%d want=1", count)
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) > 0 {
		t.Errorf("被拦下的操作不应留下临时文件: %d", len(files))
	}
}

func TestClearBackupIllegalArgument(t *testing.T) {
	ctx := newTestCtx(t)

	if err := clearBackup(ctx, "", config.GetConfig(ctx).DbBackupLimit); err == nil {
		t.Errorf("备份目录为空应报错")
	}
	//保留0份等于把备份全删光，配置兜底之外再拦一道
	if err := clearBackup(ctx, config.DbBackupPath, 0); err == nil {
		t.Errorf("保留数量为0应报错")
	}
	if err := clearBackup(ctx, config.DbBackupPath, -1); err == nil {
		t.Errorf("保留数量为负应报错")
	}

	//没到上限就一份都不该删
	backupPath, err := genBackupPath(ctx)
	if err != nil {
		t.Fatalf("生成备份路径异常: %+v", err)
	}
	if err = util.WriteData2File(ctx, []byte("backup"), backupPath); err != nil {
		t.Fatalf("写备份文件异常: %+v", err)
	}
	if err = ClearBackup(ctx); err != nil {
		t.Fatalf("清理备份异常: %+v", err)
	}
	if util.GetPathInfo(ctx, backupPath) == nil {
		t.Errorf("没到上限的备份不应被清掉: %s", backupPath)
	}
}

// 导出/导入/换口令的口令与库文件守卫：它们挡的是越权，公开入口取不到空口令，只能直接打私有函数
func TestBackupGuard(t *testing.T) {
	ctx := newTestCtx(t)
	buffer := new(bytes.Buffer)

	if backupPath, err := export(ctx, "resource/not-exist.db", testClientToken); err == nil {
		t.Errorf("库文件不存在时导出应报错: %s", backupPath)
	}
	if backupPath, err := export(ctx, config.DbPath, ""); err == nil {
		t.Errorf("口令为空时导出应报错: %s", backupPath)
	}
	//写出目标为空的守卫挪到了公开入口上，被它拦下时连副本都不该产出
	if err := Export(ctx, nil); err == nil {
		t.Errorf("写出目标为空时导出应报错")
	}
	if err := Export(newTokenCtx("wrong-client-token"), buffer); err == nil {
		t.Errorf("错误口令导出应报错")
	}
	if buffer.Len() > 0 {
		t.Errorf("被守卫拦下的导出不应写出内容: %d", buffer.Len())
	}

	if err := import_(ctx, "resource/not-exist.db", testClientToken, bytes.NewReader(nil)); err == nil {
		t.Errorf("库文件不存在时导入应报错")
	}
	if err := import_(ctx, config.DbPath, "", bytes.NewReader(nil)); err == nil {
		t.Errorf("口令为空时导入应报错")
	}

	if err := changeToken(ctx, "resource/not-exist.db", testClientToken, "new-client-token-4"); err == nil {
		t.Errorf("库文件不存在时换口令应报错")
	}
	if err := changeToken(ctx, config.DbPath, "", "new-client-token-4"); err == nil {
		t.Errorf("旧口令为空时换口令应报错")
	}
	if err := changeToken(ctx, config.DbPath, testClientToken, ""); err == nil {
		t.Errorf("新口令为空时换口令应报错")
	}

	if gormDb, err := open(ctx, config.DbPath, ""); err == nil {
		util.CloseDb(ctx, gormDb)
		t.Errorf("口令为空时开库应报错")
	}

	//守卫拦下的这几次都没碰过库，原口令与数据都得原样在
	if err := CheckToken(ctx); err != nil {
		t.Errorf("原口令应照常可用: %+v", err)
	}
	files, err := util.ListFile(ctx, config.DbBackupPath)
	if err != nil {
		t.Fatalf("读备份目录异常: %+v", err)
	}
	if len(files) > 0 {
		t.Errorf("被守卫拦下的操作不应留下临时文件: %d", len(files))
	}
}
