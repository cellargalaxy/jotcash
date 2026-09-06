package log

// 本文件验证 A-3 的四条语义：
//   ① 穿透日志级别抑制（最要紧的一条，§三 坑 4 的回归）
//   ② 一次性（含并发）
//   ③ 明文只进专用文件，不进全局日志
//   ④ 红线：哈希与盐无从传入
//
// 用例位于包内（package log）而非 _test 外部包，唯一原因是需要重置
// initPasswordOnce ——它是包级 sync.Once，不重置的话除第一个用例外全部拿不到打印行为。
// 外部黑盒视角的调用方验证在 zz_consumer_check_test.go。

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

// 用例中统一使用的假明文。取一个符合 A-8 策略（长度 ≥ 10、含字母数字符号）的串，
// 与 rule/passwd 将来真实产出的形态一致；且足够独特，便于在日志里精确检索。
const fakePassword = "Init-Pwd-9x7!"

// resetInitPasswordOnce 的测试侧包装：重置一次性开关，让每个用例都能独立观察「第一次调用」的行为。
// 实现在 password.go 里与 initPasswordOnce 的声明贴在一起；
// 外部测试包经 export_test.go 的 ResetInitPasswordOnceForTest 使用同一实现。
func resetOnce(t *testing.T) {
	t.Helper()
	resetInitPasswordOnce()
}

// readInitPasswordLog 读取 A-3 专用日志文件内容，文件不存在时返回空串。
func readInitPasswordLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "log", DefaultServerName, InitPasswordLogFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("读取初始密码日志异常: %+v", err)
	}
	return string(data)
}

// readGlobalLog 读取全局日志内容，文件不存在时返回空串。
func readGlobalLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "log", DefaultServerName, DefaultLogFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("读取全局日志异常: %+v", err)
	}
	return string(data)
}

// TestPrintInitPasswordWritesPlaintext A-3 的基本语义：明文落进专用文件，且带用户名与提示文案。
func TestPrintInitPasswordWritesPlaintext(t *testing.T) {
	dir := chdirTemp(t)
	resetOnce(t)
	if err := Init("info"); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}

	PrintInitPassword("admin", fakePassword)

	text := readInitPasswordLog(t, dir)
	if !strings.Contains(text, fakePassword) {
		t.Fatalf("初始密码未落盘，admin 将无法登录（A-3 唯一交付路径断裂）:\n%s", text)
	}
	if !strings.Contains(text, "admin") {
		t.Fatalf("初始密码日志缺用户名:\n%s", text)
	}
	// 提示文案必须与密码同行出现：读者需当场知道「只打印这一次」
	if !strings.Contains(text, initPasswordLogMsg) {
		t.Fatalf("初始密码日志缺一次性提示文案:\n%s", text)
	}
}

// TestPrintInitPasswordSurvivesLevelSuppression **本包最关键的用例**（§三 坑 4 回归）。
//
// log_level 是部署方可配参数。若 A-3 走全局 logrus 的 Info，配成 error/panic 时明文会被
// 静默丢弃；而 §二 A-3 规定该日志丢失即 admin 永久无法登录、只能重新部署。
// 故这里逐个把全局级别压到最低档，断言明文**仍然**写得出去。
func TestPrintInitPasswordSurvivesLevelSuppression(t *testing.T) {
	// panic 是 logrus 最严格的级别，Info/Warn/Error 全部被抑制
	for _, level := range []string{"error", "fatal", "panic"} {
		t.Run(level, func(t *testing.T) {
			dir := chdirTemp(t)
			resetOnce(t)
			if err := Init(level); err != nil {
				t.Fatalf("初始化异常: %+v", err)
			}

			// 先确认该级别确实会抑制全局 Info——否则本用例证明不了任何事
			const suppressed = "本条全局 Info 应被抑制"
			logrus.WithFields(logrus.Fields{}).Info(suppressed)
			if strings.Contains(readGlobalLog(t, dir), suppressed) {
				t.Fatalf("级别 %s 未抑制全局 Info，用例前提不成立", level)
			}

			PrintInitPassword("admin", fakePassword)

			if text := readInitPasswordLog(t, dir); !strings.Contains(text, fakePassword) {
				t.Fatalf("级别 %s 下初始密码未能落盘——部署方一个配置项就能让 admin 永久无法登录:\n%s", level, text)
			}
		})
	}
}

// TestPrintInitPasswordNotInGlobalLog 明文不得进入全局日志：那是会被业务日志迅速轮转掉的文件，
// 也是排障时最常被整体分享出去的文件。
func TestPrintInitPasswordNotInGlobalLog(t *testing.T) {
	dir := chdirTemp(t)
	resetOnce(t)
	if err := Init("trace"); err != nil { // trace 放行一切级别，最容易漏进全局日志
		t.Fatalf("初始化异常: %+v", err)
	}

	PrintInitPassword("admin", fakePassword)

	if text := readGlobalLog(t, dir); strings.Contains(text, fakePassword) {
		t.Fatalf("明文进入了全局日志:\n%s", text)
	}
	// 同时确认它确实写进了专用文件（否则上面的断言可以靠「根本没打印」蒙过）
	if text := readInitPasswordLog(t, dir); !strings.Contains(text, fakePassword) {
		t.Fatalf("明文未写入专用文件:\n%s", text)
	}
}

// TestPrintInitPasswordOnlyOnce 一次性：连续多次调用，明文只出现一次。
func TestPrintInitPasswordOnlyOnce(t *testing.T) {
	dir := chdirTemp(t)
	resetOnce(t)
	if err := Init("info"); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}

	PrintInitPassword("admin", fakePassword)
	PrintInitPassword("admin", fakePassword)
	PrintInitPassword("admin", "另一个明文-A1b2c3!")

	text := readInitPasswordLog(t, dir)
	if got := strings.Count(text, fakePassword); got != 1 {
		t.Fatalf("明文出现 %d 次，期望 1 次:\n%s", got, text)
	}
	// 第二个明文属于「重复调用」，一个字都不该落盘
	if strings.Contains(text, "另一个明文") {
		t.Fatalf("重复调用竟落了第二个明文:\n%s", text)
	}
}

// TestPrintInitPasswordOnlyOnceConcurrent 并发下同样只打印一次（配合 -race 运行）。
func TestPrintInitPasswordOnlyOnceConcurrent(t *testing.T) {
	dir := chdirTemp(t)
	resetOnce(t)
	if err := Init("info"); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			PrintInitPassword("admin", fakePassword)
		}()
	}
	wg.Wait()

	text := readInitPasswordLog(t, dir)
	if got := strings.Count(text, fakePassword); got != 1 {
		t.Fatalf("并发 %d 次调用后明文出现 %d 次，期望 1 次:\n%s", n, got, text)
	}
}

// TestPrintInitPasswordEmpty 空明文：不落盘、不消耗一次性开关。
//
// 后者是要点——若空明文把开关用掉了，一次误调用就会让真正那次打印永久失效，
// 而那意味着 admin 无法登录。
func TestPrintInitPasswordEmpty(t *testing.T) {
	dir := chdirTemp(t)
	resetOnce(t)
	if err := Init("info"); err != nil {
		t.Fatalf("初始化异常: %+v", err)
	}

	PrintInitPassword("admin", "")
	if text := readInitPasswordLog(t, dir); strings.TrimSpace(text) != "" {
		t.Fatalf("空明文竟产生了初始密码日志:\n%s", text)
	}

	// 开关未被消耗：随后的正常调用仍应打印
	PrintInitPassword("admin", fakePassword)
	if text := readInitPasswordLog(t, dir); !strings.Contains(text, fakePassword) {
		t.Fatalf("空明文调用消耗了一次性开关，真正的密码再也打印不出来:\n%s", text)
	}
}

// TestNoHashOrSaltInSignature §九 红线「`密码哈希` 与 `盐` 连三类出口都不得出现」的结构性验证。
//
// 用反射核对函数签名：两个入参、均为 string、无返回值。签名一旦被改成收结构体
// （例如为了「方便」改收 entity.User），本用例即失败——这正是要拦住的那种改动。
func TestNoHashOrSaltInSignature(t *testing.T) {
	fn := reflect.TypeOf(PrintInitPassword)
	if fn.NumIn() != 2 {
		t.Fatalf("PrintInitPassword 入参个数 = %d，期望 2（用户名 + 明文）；"+
			"若改为接收结构体，密码哈希与盐会随之进入本出口，违反 §九 红线", fn.NumIn())
	}
	for i := 0; i < fn.NumIn(); i++ {
		if fn.In(i).Kind() != reflect.String {
			t.Fatalf("第 %d 个入参类型为 %s，期望 string", i, fn.In(i))
		}
	}
	if fn.NumOut() != 0 {
		t.Fatalf("PrintInitPassword 返回值个数 = %d，期望 0（明文不得被回传）", fn.NumOut())
	}
}
