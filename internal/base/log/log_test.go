package log

// 本文件验证「统一初始化」这一职责：级别解析、服务名生效、日志真的落到 log/<服务名>/。
// A-3 相关用例在 password_test.go。
//
// ⚠️ 用例设计上的一条硬约束：`util.InitLog` 改的是**全局** logrus，且日志落盘路径是
// **相对当前工作目录**的。因此凡会产生落盘的用例，一律先 chdir 到 t.TempDir()，
// 避免在仓库目录里留下 log/ 产物（仓库内 internal/base/config/log/、internal/base/idgen/log/
// 那些随机名目录就是没有这么做的后果）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/sirupsen/logrus"
)

// chdirTemp 把工作目录切到用例私有临时目录，返回该目录；用例结束自动切回。
// 保证日志落盘不污染仓库工作区。
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// t.Chdir 由 Go 1.24 引入，测试结束自动恢复原工作目录
	t.Chdir(dir)
	return dir
}

// TestParseLevel 级别解析：合法值逐一覆盖、别名、空串回落、非法值报错。
func TestParseLevel(t *testing.T) {
	// 合法级别逐个覆盖，含 warn/warning 这组别名——这正是「不自维护白名单」的收益
	oks := map[string]logrus.Level{
		"":        logrus.InfoLevel, // 空串回落，与 config.DefaultLogLevel 一致
		"panic":   logrus.PanicLevel,
		"fatal":   logrus.FatalLevel,
		"error":   logrus.ErrorLevel,
		"warn":    logrus.WarnLevel,
		"warning": logrus.WarnLevel,
		"info":    logrus.InfoLevel,
		"debug":   logrus.DebugLevel,
		"trace":   logrus.TraceLevel,
	}
	for level, want := range oks {
		got, err := parseLevel(level)
		if err != nil {
			t.Fatalf("级别 %q 应合法，却报错: %+v", level, err)
		}
		if got != want {
			t.Fatalf("级别 %q 解析为 %v，期望 %v", level, got, want)
		}
	}

	// 非法值必须报错，且**不得静默降级**为默认级别
	for _, level := range []string{"verbose", "INFO ", "warnning", "0", "无"} {
		if _, err := parseLevel(level); err == nil {
			t.Fatalf("级别 %q 非法却未报错，配置会看似生效实则未生效", level)
		}
	}
}

// TestInitRejectsInvalidLevel 级别非法时 Init 返回错误，且不改动全局级别。
func TestInitRejectsInvalidLevel(t *testing.T) {
	chdirTemp(t)

	if err := Init("info"); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}
	before := logrus.GetLevel()

	if err := Init("verbose"); err == nil {
		t.Fatalf("级别非法却未报错")
	}
	// 失败不应产生副作用：级别必须还是上一次的有效值
	if logrus.GetLevel() != before {
		t.Fatalf("级别非法的 Init 改动了全局级别: %v → %v", before, logrus.GetLevel())
	}
}

// TestInitSetsServerNameAndLogPath 初始化后服务名生效，且日志确实落到 log/<服务名>/log.log。
//
// 这是 §三 坑 1 的回归用例：只调 util.Init 时，GetServerName() 会返回 jotcash，
// 但日志仍写在 log/<随机ID>/ 下。断言必须同时覆盖「服务名」与「落盘位置」两侧，
// 只查前者的话，那个 bug 照样能通过。
func TestInitSetsServerNameAndLogPath(t *testing.T) {
	dir := chdirTemp(t)

	if err := Init("info"); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}

	if got := util.GetServerName(); got != DefaultServerName {
		t.Fatalf("服务名 = %q，期望 %q", got, DefaultServerName)
	}
	if got := logrus.GetLevel(); got != logrus.InfoLevel {
		t.Fatalf("全局级别 = %v，期望 info", got)
	}

	// 打一条可识别的日志，验证它落进了 log/jotcash/log.log
	const mark = "用例校验落盘位置的标记串"
	logrus.WithFields(logrus.Fields{"case": "logPath"}).Info(mark)

	path := filepath.Join(dir, "log", DefaultServerName, DefaultLogFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("日志未落到 %s（坑 1 回归）: %+v", path, err)
	}
	text := string(data)
	if !strings.Contains(text, mark) {
		t.Fatalf("日志文件中找不到标记串，落盘未生效:\n%s", text)
	}
	// sn 字段必须是服务名而非随机 ID
	if !strings.Contains(text, "[sn:"+DefaultServerName+"]") {
		t.Fatalf("日志的 sn 字段不是服务名（坑 1 回归）:\n%s", text)
	}
}

// TestInitAppliesLevel 级别真的被应用：低于阈值的日志不落盘。
func TestInitAppliesLevel(t *testing.T) {
	dir := chdirTemp(t)

	if err := Init("error"); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}

	const infoMark = "本条 Info 不应出现"
	const errMark = "本条 Error 应当出现"
	logrus.WithFields(logrus.Fields{}).Info(infoMark)
	logrus.WithFields(logrus.Fields{}).Error(errMark)

	data, err := os.ReadFile(filepath.Join(dir, "log", DefaultServerName, DefaultLogFileName))
	if err != nil {
		t.Fatalf("读取日志异常: %+v", err)
	}
	text := string(data)
	if strings.Contains(text, infoMark) {
		t.Fatalf("级别 error 下 Info 仍落盘，级别未生效")
	}
	if !strings.Contains(text, errMark) {
		t.Fatalf("级别 error 下 Error 未落盘")
	}
}

// TestMustInit 正常值不 panic、非法值 panic。
func TestMustInit(t *testing.T) {
	chdirTemp(t)

	MustInit("info") // 不应 panic

	func() {
		defer func() {
			if recover() == nil {
				t.Fatalf("MustInit 遇非法级别未 panic")
			}
		}()
		MustInit("verbose")
	}()
}

// TestInitIsIdempotentForLevel 重复初始化不报错，且级别以最后一次为准。
// （不建议重复调用，但要保证行为可预期：不 panic、不残留旧级别）
func TestInitIsIdempotentForLevel(t *testing.T) {
	chdirTemp(t)

	for _, level := range []string{"info", "debug", "warn"} {
		if err := Init(level); err != nil {
			t.Fatalf("重复初始化（%s）异常: %+v", level, err)
		}
	}
	if got := logrus.GetLevel(); got != logrus.WarnLevel {
		t.Fatalf("级别 = %v，期望 warning（以最后一次为准）", got)
	}
}
