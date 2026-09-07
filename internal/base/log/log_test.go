package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/sirupsen/logrus"
)

// TestMain 在全部用例跑完后清掉「游离日志目录」。
//
// 该目录的成因见 log.go 中 DefaultServerName 的注释：go_common 的 util 包在被
// import 时其 init() 就会打日志，那一刻服务名还是空的，于是日志落到
// log/<随机18位ID>/log.log。这早于本包的 Init、也早于 TestMain，进程内无法
// 阻止；而此时的工作目录正是包目录，故每跑一次 go test 就会在
// internal/base/log/ 下留一个游离目录。
//
// 各用例本身已用 t.Chdir 切到临时目录，不会新增污染；这里只负责收尾。
// .gitignore 的 *.log 已能保证它不入库，清理是为了不让源码目录变脏。
//
// 删除做了两道收窄，确保只删这一类产物：只认「全为数字且恰 18 位」的子目录
// （即 go_common 的 ID 形态），随后用 os.Remove 删父目录——它只在目录为空时
// 成功，故任何非预期内容都会让父目录留下来，不会被连带删除。
func TestMain(m *testing.M) {
	code := m.Run()
	clearStrayLogDir()
	os.Exit(code)
}

func clearStrayLogDir() {
	entries, err := os.ReadDir("log")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !isGeneratedID(entry.Name()) {
			continue
		}
		_ = os.RemoveAll(filepath.Join("log", entry.Name()))
	}
	// 仅当目录已空时才会成功，避免误删他人放在此处的内容。
	_ = os.Remove("log")
}

// isGeneratedID 判断目录名是否为 go_common 生成的 18 位数字 ID。
func isGeneratedID(name string) bool {
	if len(name) != 18 {
		return false
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Init 与 PrintAdminInitPassword 改动的是 logrus 的**全局单例**（级别、输出、
// hook）。故每个会改动全局状态的用例都用本函数在临时目录里隔离，并在结束时
// 复原级别与 hook，避免用例间相互污染，也避免在仓库根产生 log/ 目录。
//
// 切换工作目录是必须的：go_common 按相对路径 log/<服务名>/log.log 落盘，
// 不切目录就会写到包目录 internal/base/log/ 下。
func isolate(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	level := logrus.GetLevel()
	hooks := logrus.LevelHooks{}
	for lv, list := range logrus.StandardLogger().Hooks {
		hooks[lv] = append(hooks[lv], list...)
	}
	output := logrus.StandardLogger().Out
	t.Cleanup(func() {
		logrus.SetLevel(level)
		logrus.StandardLogger().ReplaceHooks(hooks)
		logrus.SetOutput(output)
	})
	return dir
}

// logPath 返回本服务日志文件的相对路径。服务名可被环境变量 server_name 覆盖，
// 故用 util.GetServerName() 取实际生效值，而不是直接拼 DefaultServerName——
// 否则在设了该环境变量的机器上跑测试会找错文件。
func logPath() string {
	return filepath.Join("log", util.GetServerName(), "log.log")
}

// readLog 读出已落盘的日志内容。文件不存在时返回空串，交由调用方断言。
func readLog(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(logPath())
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("读取日志文件异常: %+v", err)
	}
	return string(data)
}

// countHooks 返回单个级别上挂着的 hook 数量（各级别数量一致，取最大值即可）。
// 用于验证 Init 的幂等性。
func countHooks() int {
	most := 0
	for _, list := range logrus.StandardLogger().Hooks {
		if len(list) > most {
			most = len(list)
		}
	}
	return most
}

// TestParseLevel 断言 7 档级别（含 warning 别名）与大小写、空白、空串的处理，
// 以及非法值必须报错。取值域与 configs/jotcash.example.yaml 的 log_level 注释
// 逐一对应。
func TestParseLevel(t *testing.T) {
	accept := map[string]logrus.Level{
		"panic":   logrus.PanicLevel,
		"fatal":   logrus.FatalLevel,
		"error":   logrus.ErrorLevel,
		"warn":    logrus.WarnLevel,
		"warning": logrus.WarnLevel, // logrus 对 warn 的别名
		"info":    logrus.InfoLevel,
		"debug":   logrus.DebugLevel,
		"trace":   logrus.TraceLevel,

		// 大小写不敏感，避免部署者因大小写卡住
		"INFO":  logrus.InfoLevel,
		"Debug": logrus.DebugLevel,

		// 首尾空白无语义，多来自 shell、.env 与容器编排的参数传递。
		// logrus.ParseLevel 本身**不**接受带空白的值，故本包必须先 TrimSpace。
		" info ":  logrus.InfoLevel,
		"\tdebug": logrus.DebugLevel,

		// 空值意味着没配，取默认级别
		"":    logrus.InfoLevel,
		"   ": logrus.InfoLevel,
	}
	for level, expect := range accept {
		t.Run("接受"+level, func(t *testing.T) {
			got, err := ParseLevel(level)
			if err != nil {
				t.Fatalf("解析日志级别，期望接受 %q，实际: %+v", level, err)
			}
			if got != expect {
				t.Errorf("解析日志级别 %q，期望 %v，实际 %v", level, expect, got)
			}
		})
	}

	// 非法值必须报错，不得静默退回默认级别——否则部署者以为改生效了。
	for _, level := range []string{"verbose", "INF", "warnning", "0", "true"} {
		t.Run("拒绝"+level, func(t *testing.T) {
			if _, err := ParseLevel(level); err == nil {
				t.Errorf("解析日志级别，期望拒绝 %q，实际接受", level)
			}
		})
	}

	// 默认级别常量本身必须合法，且与 configs 样例的默认值一致
	if _, err := logrus.ParseLevel(DefaultLevel); err != nil {
		t.Errorf("默认日志级别常量 %q 非法: %+v", DefaultLevel, err)
	}
	if DefaultLevel != "info" {
		t.Errorf("默认日志级别，期望 info（对齐 configs 样例），实际 %s", DefaultLevel)
	}
}

// TestInitSetsLevelAndServerName 断言 Init 三件事同时成立：级别生效、
// 服务名生效（sn 字段）、日志真实落到 log/<服务名>/log.log。
//
// 这是对 go_common「只调 util.Init 不重做日志则服务名不生效」的回归——若
// Init 漏掉重做日志那一步，sn 会是随机 18 位数字，本用例即失败。
func TestInitSetsLevelAndServerName(t *testing.T) {
	isolate(t)

	if err := Init("debug"); err != nil {
		t.Fatalf("初始化日志异常: %+v", err)
	}
	if got := logrus.GetLevel(); got != logrus.DebugLevel {
		t.Fatalf("日志级别，期望 %v，实际 %v", logrus.DebugLevel, got)
	}

	logrus.WithContext(util.GenCtx()).Info("初始化日志，测试落盘")

	text := readLog(t)
	if text == "" {
		t.Fatalf("日志文件未创建或为空: %s", logPath())
	}
	if !strings.Contains(text, "初始化日志，测试落盘") {
		t.Errorf("日志内容缺少测试行: %s", text)
	}
	// sn 字段必须是服务名而非随机 ID
	if want := "sn:" + util.GetServerName(); !strings.Contains(text, want) {
		t.Errorf("日志缺少服务名字段 %s: %s", want, text)
	}
}

// TestInitDefaultLevel 断言空串取默认级别 info。
func TestInitDefaultLevel(t *testing.T) {
	isolate(t)

	logrus.SetLevel(logrus.PanicLevel) // 先设成一个明显不同的值
	if err := Init(""); err != nil {
		t.Fatalf("初始化日志异常: %+v", err)
	}
	if got := logrus.GetLevel(); got != logrus.InfoLevel {
		t.Errorf("空级别应取默认 %v，实际 %v", logrus.InfoLevel, got)
	}
}

// TestInitInvalidLevel 断言非法级别返回错误，且**不产生任何副作用**：
// 全局级别不变、日志文件不创建。非法配置应让 app 启动失败，而不是带着一个
// 半初始化的日志继续跑。
func TestInitInvalidLevel(t *testing.T) {
	isolate(t)

	logrus.SetLevel(logrus.ErrorLevel)
	if err := Init("verbose"); err == nil {
		t.Fatalf("初始化日志，期望拒绝非法级别 verbose，实际接受")
	}
	if got := logrus.GetLevel(); got != logrus.ErrorLevel {
		t.Errorf("非法级别不应改动全局级别，期望 %v，实际 %v", logrus.ErrorLevel, got)
	}
	if _, err := os.Stat(logPath()); err == nil {
		t.Errorf("非法级别不应创建日志文件: %s", logPath())
	}
}

// TestInitIdempotentHooks 断言 Init 幂等：重复调用后字段注入 hook 恒为 1 个。
//
// 这是对「logrus.AddHook 是 append 无去重」的回归。该 hook 每条日志都要
// runtime.Caller 走栈找调用点，是日志的主要开销；若 Init 漏掉清空 hook 这一
// 步，调 3 次就变成 4 个 hook（util 包 init() 自带 1 个），每行日志走栈 4 遍。
func TestInitIdempotentHooks(t *testing.T) {
	isolate(t)

	for i := 1; i <= 3; i++ {
		if err := Init("info"); err != nil {
			t.Fatalf("第 %d 次初始化日志异常: %+v", i, err)
		}
		if got := countHooks(); got != 1 {
			t.Fatalf("第 %d 次初始化后 hook 数，期望 1，实际 %d", i, got)
		}
	}

	// 幂等不能以牺牲字段为代价：清空后必须挂回唯一一个可用的 hook
	logrus.WithContext(util.GenCtx()).Info("初始化日志，幂等后仍应有完整字段")
	text := readLog(t)
	for _, field := range []string{"logid:", "sn:", "caller:"} {
		if !strings.Contains(text, field) {
			t.Errorf("日志缺少字段 %s: %s", field, text)
		}
	}
}

// TestInitKeepsCallerAtCallSite 断言 caller 字段指向**真实调用点**（本测试
// 文件），而不是 logrus 内部或本包内部。
//
// 这条性质是「本包不封装日志门面」的收益所在：go_common 按硬编码的栈深度回溯
// 取 caller，任何封装层都会让它指向封装函数自身，从此日志无法定位到业务代码。
func TestInitKeepsCallerAtCallSite(t *testing.T) {
	isolate(t)

	if err := Init("info"); err != nil {
		t.Fatalf("初始化日志异常: %+v", err)
	}
	logrus.WithContext(util.GenCtx()).Info("初始化日志，测试调用点")

	text := readLog(t)
	if !strings.Contains(text, "log_test.go") {
		t.Errorf("caller 未指向调用点所在的测试文件: %s", text)
	}
	if strings.Contains(text, "sirupsen/logrus") {
		t.Errorf("caller 指向了 logrus 内部而非业务代码: %s", text)
	}
}

// TestPrintAdminInitPasswordAtErrorLevel 是 A-3 的生命线用例：
// log_level=error 时，初始密码行**仍必须落盘**，且打完级别必须复原。
//
// 依据终版 A-3：admin 遗忘密码不设计恢复路径，初始化那一次的日志若丢失，
// admin 将永久无法登录。故这一行不能被部署者配的日志级别吞掉。
func TestPrintAdminInitPasswordAtErrorLevel(t *testing.T) {
	isolate(t)

	if err := Init("error"); err != nil {
		t.Fatalf("初始化日志异常: %+v", err)
	}

	const password = "Init-Pwd-1234!"
	PrintAdminInitPassword(util.GenCtx(), "admin", password)

	text := readLog(t)
	if !strings.Contains(text, password) {
		t.Fatalf("error 级别下初始密码行被吞掉，日志内容: %s", text)
	}
	if !strings.Contains(text, "admin") {
		t.Errorf("初始密码行缺少用户名: %s", text)
	}

	// 级别必须复原，否则后续 info/warn 日志会意外全量输出
	if got := logrus.GetLevel(); got != logrus.ErrorLevel {
		t.Errorf("打印后日志级别未复原，期望 %v，实际 %v", logrus.ErrorLevel, got)
	}
	// 复原生效的实证：这条 info 应被吞掉
	logrus.WithContext(util.GenCtx()).Info("这条不应落盘")
	if strings.Contains(readLog(t), "这条不应落盘") {
		t.Errorf("级别复原后 info 仍被输出，级别门控失效")
	}
}

// TestPrintAdminInitPasswordAtDebugLevel 断言当前级别已高于 warn 时不误改级别，
// 密码行正常落盘。
func TestPrintAdminInitPasswordAtDebugLevel(t *testing.T) {
	isolate(t)

	if err := Init("debug"); err != nil {
		t.Fatalf("初始化日志异常: %+v", err)
	}

	const password = "Init-Pwd-5678!"
	PrintAdminInitPassword(util.GenCtx(), "admin", password)

	if !strings.Contains(readLog(t), password) {
		t.Errorf("debug 级别下初始密码行未落盘")
	}
	if got := logrus.GetLevel(); got != logrus.DebugLevel {
		t.Errorf("不应改动已足够低的级别，期望 %v，实际 %v", logrus.DebugLevel, got)
	}
}

// TestPrintAdminInitPasswordEmpty 断言空密码不打印明文行，改打 error 告警。
// 空密码必然是上游 bug，静默打印会让部署者以为密码就是空的。
func TestPrintAdminInitPasswordEmpty(t *testing.T) {
	isolate(t)

	if err := Init("info"); err != nil {
		t.Fatalf("初始化日志异常: %+v", err)
	}
	PrintAdminInitPassword(util.GenCtx(), "admin", "")

	text := readLog(t)
	if !strings.Contains(text, "密码为空") {
		t.Errorf("空密码应记 error 告警: %s", text)
	}
	if strings.Contains(text, "仅在此打印一次") {
		t.Errorf("空密码不应走正常打印分支: %s", text)
	}
}

// TestDefaultServerName 断言服务名常量与仓库既有约定一致：
// .gitignore 的 /log/ 注释、configs 样例与 doc 的目录树都以 jotcash 为服务名。
func TestDefaultServerName(t *testing.T) {
	if DefaultServerName != "jotcash" {
		t.Errorf("服务名，期望 jotcash，实际 %s", DefaultServerName)
	}
}
