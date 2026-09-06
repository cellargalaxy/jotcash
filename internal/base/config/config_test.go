package config

// 单测覆盖 Load 的全部分支与 checkAndReset 的每一条取值口径。
//
// 用**临时目录**而非 testdata 固定文件：本包的核心行为是「按路径读文件并校验」，
// 每个用例需要一份内容不同的配置文件，固化到 testdata 会变成十来个只差一行的样例文件；
// t.TempDir() 由测试框架自动清理，也不会在仓库里留下运行期产物。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig 在临时目录写一份配置文件，返回其路径。
func writeConfig(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jotcash.yaml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatalf("写入临时配置文件异常: %+v", err)
	}
	return path
}

// TestLoadFull 六个字段全部填写时，取值一字不差地落到结构体上，不被默认值覆盖。
func TestLoadFull(t *testing.T) {
	path := writeConfig(t, `
listen_address: "127.0.0.1:18080"
sqlite_path: "/var/lib/jotcash/jotcash.db"
token_secret: "s3cr3t-token-key"
fx_rate_endpoint: "https://fx.example.com/v1/daily"
fx_rate_secret: "fx-access-key"
log_level: "debug"
`)
	config, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("加载配置异常: %+v", err)
	}
	if config.ListenAddress != "127.0.0.1:18080" {
		t.Fatalf("监听地址 = %q", config.ListenAddress)
	}
	if config.SqlitePath != "/var/lib/jotcash/jotcash.db" {
		t.Fatalf("SQLite 路径 = %q", config.SqlitePath)
	}
	if config.TokenSecret != "s3cr3t-token-key" {
		t.Fatalf("令牌密钥 = %q", config.TokenSecret)
	}
	if config.FxRateEndpoint != "https://fx.example.com/v1/daily" {
		t.Fatalf("汇率源端点 = %q", config.FxRateEndpoint)
	}
	if config.FxRateSecret != "fx-access-key" {
		t.Fatalf("汇率源密钥 = %q", config.FxRateSecret)
	}
	if config.LogLevel != "debug" {
		t.Fatalf("日志级别 = %q", config.LogLevel)
	}
}

// TestLoadOnlyRequired 只填必填项时，其余四项落到默认值，汇率源两项保持为空
// （未选型，不能凭空给默认端点）。
func TestLoadOnlyRequired(t *testing.T) {
	path := writeConfig(t, "token_secret: \"only-required\"\n")
	config, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("加载配置异常: %+v", err)
	}
	if config.ListenAddress != DefaultListenAddress {
		t.Fatalf("监听地址未取默认值: %q", config.ListenAddress)
	}
	if config.SqlitePath != DefaultSqlitePath {
		t.Fatalf("SQLite 路径未取默认值: %q", config.SqlitePath)
	}
	if config.LogLevel != DefaultLogLevel {
		t.Fatalf("日志级别未取默认值: %q", config.LogLevel)
	}
	if config.FxRateEndpoint != "" || config.FxRateSecret != "" {
		t.Fatalf("汇率源被凭空填了默认值: endpoint=%q secret=%q", config.FxRateEndpoint, config.FxRateSecret)
	}
}

// TestLoadTrimSpace 各字段的首尾空白被清理——配置文件里对齐缩进或误敲空格很常见，
// 带空白的路径 / 密钥会以「文件不存在」「密码错误」等无关形态失败。
func TestLoadTrimSpace(t *testing.T) {
	path := writeConfig(t, `
listen_address: "  :9090  "
sqlite_path: "  data/x.db  "
token_secret: "  padded-secret  "
fx_rate_endpoint: "  https://fx.example.com  "
fx_rate_secret: "  fx  "
log_level: "  warn  "
`)
	config, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("加载配置异常: %+v", err)
	}
	if config.ListenAddress != ":9090" || config.SqlitePath != "data/x.db" {
		t.Fatalf("空白未清理: address=%q path=%q", config.ListenAddress, config.SqlitePath)
	}
	if config.TokenSecret != "padded-secret" || config.FxRateSecret != "fx" {
		t.Fatalf("密钥空白未清理: token=%q fx=%q", config.TokenSecret, config.FxRateSecret)
	}
	if config.FxRateEndpoint != "https://fx.example.com" {
		t.Fatalf("端点空白未清理: %q", config.FxRateEndpoint)
	}
	// logrus 的规范名把 warn 归一为 warning，避免 base/log 再处理一遍别名
	if config.LogLevel != "warning" {
		t.Fatalf("日志级别未规整为 logrus 规范名: %q", config.LogLevel)
	}
}

// TestLoadFileNotExist 文件不存在时报错，且**不得创建该文件**
// （util.OpenReadFile 会创建文件，故 Load 必须先判存在）。
func TestLoadFileNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yaml")
	if _, err := Load(context.Background(), path); err == nil {
		t.Fatalf("文件不存在竟未报错")
	} else if !strings.Contains(err.Error(), ExampleConfigPath) {
		t.Fatalf("错误提示未指向样例文件: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Load 竟创建了配置文件，破坏只读语义: %v", err)
	}
}

// TestLoadPathIsDir 路径指向目录时报错（GetFileInfo 对目录返回 nil）。
func TestLoadPathIsDir(t *testing.T) {
	if _, err := Load(context.Background(), t.TempDir()); err == nil {
		t.Fatalf("路径为目录竟未报错")
	}
}

// TestLoadEmptyFile 空文件单独成一条错误，不被误报成「令牌密钥为空」。
func TestLoadEmptyFile(t *testing.T) {
	for _, text := range []string{"", "   \n\t\n"} {
		_, err := Load(context.Background(), writeConfig(t, text))
		if err == nil {
			t.Fatalf("空文件竟未报错，内容=%q", text)
		}
		if !strings.Contains(err.Error(), "配置文件为空") {
			t.Fatalf("空文件错误被误报成其他原因: %v", err)
		}
	}
}

// TestLoadInvalidYaml 非法 YAML 报错且不 panic（util.YamlString2Struct 已兜住 yaml.v2 的 panic）。
func TestLoadInvalidYaml(t *testing.T) {
	path := writeConfig(t, "token_secret: \"unclosed\nlisten_address: [1, 2\n")
	if _, err := Load(context.Background(), path); err == nil {
		t.Fatalf("非法 YAML 竟未报错")
	}
}

// TestLoadTokenSecretRequired 令牌密钥是唯一必填项：缺失、空串、纯空白一律报错。
// 空密钥等于令牌无签名，会直接击穿 A-6 的强制校验。
func TestLoadTokenSecretRequired(t *testing.T) {
	cases := map[string]string{
		"缺失":  "log_level: \"info\"\n",
		"空串":  "token_secret: \"\"\n",
		"纯空白": "token_secret: \"   \"\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			config, err := Load(context.Background(), writeConfig(t, text))
			if err == nil {
				t.Fatalf("令牌密钥为空竟未报错")
			}
			if !strings.Contains(err.Error(), "token_secret") {
				t.Fatalf("错误提示未点明字段名: %v", err)
			}
			// 失败时必须返回零值，避免调用方漏判 error 后拿半套配置把服务拉起来
			if config != (Config{}) {
				t.Fatalf("失败时返回了非零值配置: %+v", config)
			}
		})
	}
}

// TestLoadInvalidLogLevel 日志级别非法即报错，不静默退回默认值
// （静默退回会让「配置看似生效实则未生效」）。
func TestLoadInvalidLogLevel(t *testing.T) {
	path := writeConfig(t, "token_secret: \"s\"\nlog_level: \"verbose\"\n")
	if _, err := Load(context.Background(), path); err == nil {
		t.Fatalf("非法日志级别竟未报错")
	}
}

// TestLoadListenAddress 监听地址的形状校验：合法形态放行，缺端口 / 无冒号一律报错。
func TestLoadListenAddress(t *testing.T) {
	valid := []string{":4747", "127.0.0.1:8080", "0.0.0.0:80", "[::1]:4747", "localhost:9000"}
	for _, address := range valid {
		path := writeConfig(t, "token_secret: \"s\"\nlisten_address: \""+address+"\"\n")
		config, err := Load(context.Background(), path)
		if err != nil {
			t.Fatalf("合法监听地址 %q 被拒: %+v", address, err)
		}
		if config.ListenAddress != address {
			t.Fatalf("监听地址被改写: %q → %q", address, config.ListenAddress)
		}
	}
	// 缺端口的 ":" 必须被挡住：net.SplitHostPort 对它不报错，但 net.Listen 会失败在启动那一刻
	invalid := []string{"127.0.0.1", ":", "127.0.0.1:", "::1:4747"}
	for _, address := range invalid {
		path := writeConfig(t, "token_secret: \"s\"\nlisten_address: \""+address+"\"\n")
		if _, err := Load(context.Background(), path); err == nil {
			t.Fatalf("非法监听地址 %q 竟被放行", address)
		}
	}
}

// TestGetConfigPath 路径三级优先级：显式入参 → 环境变量 → 默认值，且各级都做 TrimSpace。
func TestGetConfigPath(t *testing.T) {
	// 未设环境变量时取默认值
	t.Setenv(ConfigPathEnvKey, "")
	if got := GetConfigPath(""); got != DefaultConfigPath {
		t.Fatalf("未设环境变量时未取默认值: %q", got)
	}
	if got := GetConfigPath("   "); got != DefaultConfigPath {
		t.Fatalf("纯空白入参未回落默认值: %q", got)
	}
	// 环境变量优先于默认值
	t.Setenv(ConfigPathEnvKey, "  /etc/jotcash/from-env.yaml  ")
	if got := GetConfigPath(""); got != "/etc/jotcash/from-env.yaml" {
		t.Fatalf("未取环境变量或未清理空白: %q", got)
	}
	// 显式入参优先于环境变量（本次运行的明确指定应可覆盖部署环境的默认设定）
	if got := GetConfigPath("  /tmp/from-arg.yaml  "); got != "/tmp/from-arg.yaml" {
		t.Fatalf("入参未覆盖环境变量: %q", got)
	}
}

// TestLoadUsesEnvPath Load 的 path 为空时确实走环境变量指定的文件。
func TestLoadUsesEnvPath(t *testing.T) {
	path := writeConfig(t, "token_secret: \"from-env-file\"\n")
	t.Setenv(ConfigPathEnvKey, path)
	config, err := Load(context.Background(), "")
	if err != nil {
		t.Fatalf("按环境变量加载配置异常: %+v", err)
	}
	if config.TokenSecret != "from-env-file" {
		t.Fatalf("未读到环境变量指向的配置: %q", config.TokenSecret)
	}
}

// TestConfigString 日志文本里两个密钥必须被掩码，其余字段照常可见。
// 配置日志通常是排障时第一份被贴出来分享的材料，密钥泄漏风险最高。
func TestConfigString(t *testing.T) {
	config := Config{
		ListenAddress:  ":4747",
		SqlitePath:     "data/jotcash.db",
		TokenSecret:    "super-secret-token",
		FxRateEndpoint: "https://fx.example.com",
		FxRateSecret:   "super-secret-fx",
		LogLevel:       "info",
	}
	text := config.String()
	if strings.Contains(text, "super-secret-token") || strings.Contains(text, "super-secret-fx") {
		t.Fatalf("密钥明文出现在日志文本中: %s", text)
	}
	if !strings.Contains(text, maskedSecret) {
		t.Fatalf("密钥未被掩码: %s", text)
	}
	// 非敏感字段必须保留，否则日志失去排障价值
	for _, want := range []string{":4747", "data/jotcash.db", "https://fx.example.com", "info"} {
		if !strings.Contains(text, want) {
			t.Fatalf("日志文本缺少字段 %q: %s", want, text)
		}
	}
	// String() 是值接收者，不得改动调用方持有的配置
	if config.TokenSecret != "super-secret-token" || config.FxRateSecret != "super-secret-fx" {
		t.Fatalf("String() 竟改写了原配置: %+v", config)
	}
	// 未配置的密钥保持为空，保留「填了 / 没填」的区分度
	empty := Config{LogLevel: "info"}
	if strings.Contains(empty.String(), maskedSecret) {
		t.Fatalf("未配置的密钥被掩码，无法区分是否已配置: %s", empty.String())
	}
}

// TestLoadRejectsBusinessThreshold 反查约定 8：业务阈值不得进配置。
// 配置文件里写了这些键也不会被读进 Config——yaml.v2 默认忽略未知键，
// 本用例锁住这一行为，防止后人「顺手加个字段」把安全参数变成可被部署方调低的开关。
func TestLoadRejectsBusinessThreshold(t *testing.T) {
	path := writeConfig(t, `
token_secret: "s"
token_ttl: "24h"
login_fail_limit: 999
lock_duration: "1s"
argon2_memory: 8
password_min_length: 1
fx_rate_scale: 2
`)
	config, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("含未知键的配置加载异常: %+v", err)
	}
	// 结构体只有 6 个字段，未知键无处安放；逐字段确认取值只来自这 6 项
	want := Config{
		ListenAddress: DefaultListenAddress,
		SqlitePath:    DefaultSqlitePath,
		TokenSecret:   "s",
		LogLevel:      DefaultLogLevel,
	}
	if config != want {
		t.Fatalf("业务阈值键竟影响了配置取值:\n实际 %+v\n期望 %+v", config, want)
	}
}
