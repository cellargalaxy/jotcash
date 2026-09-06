package log_test

// 外部消费者验证：以**黑盒**方式（只用导出 API，包名为 log_test）模拟本包的两个唯一调用方——
// `app`（启动装配时按 `base/config` 的 LogLevel 初始化）与 `service/account`
// （A-1 建 admin 时一次性打印初始密码），确认包对外契约真的可用。
//
// 与包内测试的分工：password_test.go 站在包内、会重置 sync.Once 以逐条验证语义；
// 本文件不碰任何内部状态，只按真实调用顺序走一遍，因此**全程只有一次**打印机会——
// 这恰好与生产路径一致（进程生命周期内 A-1 只发生一次）。
//
// 唯一的例外是开头那次 `log.ResetInitPasswordOnceForTest()`：它不改变本文件的黑盒立场，
// 只是把测试二进制内被前序用例消耗掉的一次性开关归零，从而还原「进程内第一次调用」
// 这一生产前提。理由详见该调用处与 export_test.go 的说明。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/internal/base/log"
	"github.com/sirupsen/logrus"
)

// TestAppStartupThenInitAdminFlow 按 §七 登记的启动时序走一遍：
// config.Load（此处以字面量代替）→ log.MustInit(级别) → service/account 的 A-1 打印初始密码。
//
// 级别刻意取 error：这是部署方完全可能配出来的值，而 A-3 的明文必须照样落盘。
// 若本用例通过，说明「一个配置项就能让 admin 永久无法登录」这个陷阱已被堵住。
func TestAppStartupThenInitAdminFlow(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// 复位一次性开关，模拟「进程内第一次且唯一一次」的生产时序。
	//
	// 必需，否则本用例的结果取决于**是否与包内用例同批运行**：
	// 单独 -run 时通过，全量 go test 时因 initPasswordOnce 已被 password_test.go 用掉，
	// PrintInitPassword 会走「重复调用」分支、专用日志文件根本不被创建，用例失败。
	// 这是测试二进制内的状态泄漏，与生产行为无关（生产中 A-1 一辈子只发生一次）。
	// 钩子经 export_test.go 暴露，业务流程本身仍只用导出 API，黑盒视角不受影响。
	log.ResetInitPasswordOnceForTest()

	// —— app：拿到 base/config 的 LogLevel 后初始化日志 ——
	// 这里的字符串形态与 internal/base/config 的 zz_consumer_check_test.go 中
	// `initLog := func(level string)` 桩函数逐字对齐（config 已把 warn 规整为 warning）
	const logLevelFromConfig = "error"
	log.MustInit(logLevelFromConfig)

	if got := util.GetServerName(); got != log.DefaultServerName {
		t.Fatalf("服务名 = %q，期望 %q", got, log.DefaultServerName)
	}
	if got := logrus.GetLevel(); got != logrus.ErrorLevel {
		t.Fatalf("全局级别 = %v，期望 error", got)
	}

	// —— service/account：A-1 系统初始化建 admin，一次性打印初始密码 ——
	// 明文由 rule/passwd 生成（A-2），此处以等价形态的假值代替
	const username = "admin"
	const plain = "Consumer-Pwd-42!"
	log.PrintInitPassword(username, plain)

	// A-3 的交付路径必须成立：明文在专用文件里
	pwdPath := filepath.Join(dir, "log", log.DefaultServerName, log.InitPasswordLogFileName)
	pwdData, err := os.ReadFile(pwdPath)
	if err != nil {
		t.Fatalf("初始密码日志不存在，§十 流程 1「从日志读取初始密码」走不通: %+v", err)
	}
	if !strings.Contains(string(pwdData), plain) {
		t.Fatalf("初始密码日志中无明文:\n%s", pwdData)
	}
	if !strings.Contains(string(pwdData), username) {
		t.Fatalf("初始密码日志中无用户名:\n%s", pwdData)
	}

	// 明文不得进入全局日志（那份文件会被业务日志轮转，也最常被整体分享）
	globalPath := filepath.Join(dir, "log", log.DefaultServerName, log.DefaultLogFileName)
	if globalData, err := os.ReadFile(globalPath); err == nil {
		if strings.Contains(string(globalData), plain) {
			t.Fatalf("明文进入了全局日志:\n%s", globalData)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("读取全局日志异常: %+v", err)
	}

	// 两份日志同处 log/<服务名>/ 下，运维只需关注一个目录
	entries, err := os.ReadDir(filepath.Join(dir, "log", log.DefaultServerName))
	if err != nil {
		t.Fatalf("读取日志目录异常: %+v", err)
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	if !names[log.InitPasswordLogFileName] {
		t.Fatalf("日志目录下缺 %s，实际为 %v", log.InitPasswordLogFileName, names)
	}
}

// TestAppRejectsBadLevel `app` 若拿到非法级别，必须当场拿到 error 而不是带着
// 「看似配了、实则没生效」的级别继续启动。
func TestAppRejectsBadLevel(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := log.Init("verbose"); err == nil {
		t.Fatalf("非法级别竟未报错，app 会带着未生效的日志配置启动")
	}
}

// TestServerNameEnvKeyMatchesUpstream 本包复述的环境变量名必须与 `go_common/util`
// 真正读取的那个键一致——写错一个字母，「消除随机名日志目录」的手段就失效了，
// 而这种失效是静默的（日志照写，只是目录名是随机 ID）。
//
// 通过实际设置该环境变量并观察 util.GetServerName() 的返回来验证，
// 而不是硬编码比对字符串：后者只能证明常量等于它自己。
func TestServerNameEnvKeyMatchesUpstream(t *testing.T) {
	t.Chdir(t.TempDir())

	const want = "jotcash-env-probe"
	t.Setenv(log.ServerNameEnvKey, want)

	// util.GetServerName() = GetEnvString(serverNameKey, defaultServerName)，
	// 环境变量为第一顺位。若键名写错，这里会拿到 DefaultServerName。
	if got := util.GetServerName(); got != want {
		t.Fatalf("设置环境变量 %s=%q 后 util.GetServerName() = %q；"+
			"键名与上游不一致，无法用它消除随机名日志目录", log.ServerNameEnvKey, want, got)
	}
}
