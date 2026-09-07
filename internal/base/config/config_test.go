package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// writeConf 把配置文本写入临时目录里的文件，返回其路径。
func writeConf(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jotcash.yaml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatalf("写入临时配置文件异常: %+v", err)
	}
	return path
}

// examplePath 定位随仓库提交的配置样例。测试的工作目录是包目录
// internal/base/config，故上溯三级到仓库根。
func examplePath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", ExamplePath)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("定位配置样例异常: %s: %+v", path, err)
	}
	return path
}

// TestExampleYamlContract 用与编码不同的路径反向核验「样例文件即字段契约」：
// 逐字段断言样例取值等于文档写明的默认值。任何一个 yaml tag 拼错、
// 任何一个默认值与样例注释不符，本用例都会红。
func TestExampleYamlContract(t *testing.T) {
	data, err := os.ReadFile(examplePath(t))
	if err != nil {
		t.Fatalf("读取配置样例异常: %+v", err)
	}

	var conf Config
	// 用严格模式解析样例：样例里出现结构体未定义的键，说明二者已脱节。
	if err := yaml.UnmarshalStrict(data, &conf); err != nil {
		t.Fatalf("解析配置样例异常，样例与结构体已脱节: %+v", err)
	}

	if conf.ListenAddress != DefaultListenAddress {
		t.Errorf("样例 listen_address，期望 %s，实际 %s", DefaultListenAddress, conf.ListenAddress)
	}
	if conf.SqlitePath != DefaultSqlitePath {
		t.Errorf("样例 sqlite_path，期望 %s，实际 %s", DefaultSqlitePath, conf.SqlitePath)
	}
	if conf.LogLevel != DefaultLogLevel {
		t.Errorf("样例 log_level，期望 %s，实际 %s", DefaultLogLevel, conf.LogLevel)
	}
	// 样例的三个密钥/端点必须留空——它们含敏感信息，样例文件绝不能带真值入库。
	if conf.TokenSecret != "" {
		t.Errorf("样例 token_secret 必须留空，实际 %q", conf.TokenSecret)
	}
	if conf.FxRateEndpoint != "" {
		t.Errorf("样例 fx_rate_endpoint 必须留空，实际 %q", conf.FxRateEndpoint)
	}
	if conf.FxRateSecret != "" {
		t.Errorf("样例 fx_rate_secret 必须留空，实际 %q", conf.FxRateSecret)
	}
}

// TestLoadExampleRejectedOnlyByTokenSecret 断言直接加载样例文件会失败，
// 且**恰好只因** token_secret 留空——即样例其余各项本身都是合法取值。
func TestLoadExampleRejectedOnlyByTokenSecret(t *testing.T) {
	_, err := Load(examplePath(t))
	if err == nil {
		t.Fatal("加载配置样例，期望因 token_secret 为空而失败，实际成功")
	}
	if !strings.Contains(err.Error(), "token_secret") {
		t.Fatalf("加载配置样例，期望报错指向 token_secret，实际: %+v", err)
	}

	// 把样例原文里的 token_secret 填上，其余一字不改，应当加载成功。
	data, err := os.ReadFile(examplePath(t))
	if err != nil {
		t.Fatalf("读取配置样例异常: %+v", err)
	}
	text := strings.Replace(string(data), `token_secret: ""`, `token_secret: "abc"`, 1)
	if text == string(data) {
		t.Fatal("配置样例中未找到 token_secret 空值行，样例格式已变更")
	}

	conf, err := Load(writeConf(t, text))
	if err != nil {
		t.Fatalf("加载配置，样例仅补齐 token_secret 后应当成功，实际: %+v", err)
	}
	if conf.ListenAddress != DefaultListenAddress || conf.SqlitePath != DefaultSqlitePath ||
		conf.LogLevel != DefaultLogLevel || conf.TokenSecret != "abc" {
		t.Errorf("加载配置，取值与样例不一致: %s", conf)
	}
	if conf.FxRateEndpoint != "" || conf.FxRateSecret != "" {
		t.Errorf("加载配置，汇率源两项应保持为空: %s", conf)
	}
}

// TestResolvePath 覆盖「命令行参数 > 环境变量 > 默认路径」三级优先级。
func TestResolvePath(t *testing.T) {
	cases := []struct {
		name    string
		cliPath string
		envPath string
		expect  string
	}{
		{"命令行参数优先于环境变量", "/cli.yaml", "/env.yaml", "/cli.yaml"},
		{"命令行参数为空则取环境变量", "", "/env.yaml", "/env.yaml"},
		{"命令行参数全空白视为未传", "   ", "/env.yaml", "/env.yaml"},
		{"两者皆空取默认路径", "", "", DefaultPath},
		{"环境变量全空白视为未设置", "", "  ", DefaultPath},
		{"命令行参数首尾空白被忽略", " /cli.yaml ", "", "/cli.yaml"},
		{"环境变量首尾空白被忽略", "", " /env.yaml ", "/env.yaml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Setenv 由 testing 负责在用例结束时还原，天然禁止并行，无竞态。
			t.Setenv(EnvPath, c.envPath)
			if path := ResolvePath(c.cliPath); path != c.expect {
				t.Errorf("定位配置路径，期望 %s，实际 %s", c.expect, path)
			}
		})
	}
}

// TestLoadFillDefault 断言只填必填项时，其余各项回填为文档默认值。
func TestLoadFillDefault(t *testing.T) {
	conf, err := Load(writeConf(t, "token_secret: \"s3cr3t\"\n"))
	if err != nil {
		t.Fatalf("加载配置异常: %+v", err)
	}
	if conf.ListenAddress != DefaultListenAddress {
		t.Errorf("listen_address 默认值，期望 %s，实际 %s", DefaultListenAddress, conf.ListenAddress)
	}
	if conf.SqlitePath != DefaultSqlitePath {
		t.Errorf("sqlite_path 默认值，期望 %s，实际 %s", DefaultSqlitePath, conf.SqlitePath)
	}
	if conf.LogLevel != DefaultLogLevel {
		t.Errorf("log_level 默认值，期望 %s，实际 %s", DefaultLogLevel, conf.LogLevel)
	}
	// 汇率源两项无默认值，必须保持为空，不能被凭空填上。
	if conf.FxRateEndpoint != "" || conf.FxRateSecret != "" {
		t.Errorf("汇率源两项应保持为空，实际 endpoint=%q secret=%q",
			conf.FxRateEndpoint, conf.FxRateSecret)
	}
}

// TestLoadNormalize 覆盖取值规整：结构性字段去空白、日志级别转小写、
// 而密钥必须原样保留。
func TestLoadNormalize(t *testing.T) {
	text := "listen_address: \"  127.0.0.1:8080  \"\n" +
		"sqlite_path: \"  data/x.db  \"\n" +
		"token_secret: \"  keep me  \"\n" +
		"fx_rate_endpoint: \"  https://fx.example.com  \"\n" +
		"fx_rate_secret: \"  fx keep  \"\n" +
		"log_level: \"  DEBUG  \"\n"
	conf, err := Load(writeConf(t, text))
	if err != nil {
		t.Fatalf("加载配置异常: %+v", err)
	}

	if conf.ListenAddress != "127.0.0.1:8080" {
		t.Errorf("listen_address 应去首尾空白，实际 %q", conf.ListenAddress)
	}
	if conf.SqlitePath != "data/x.db" {
		t.Errorf("sqlite_path 应去首尾空白，实际 %q", conf.SqlitePath)
	}
	if conf.FxRateEndpoint != "https://fx.example.com" {
		t.Errorf("fx_rate_endpoint 应去首尾空白，实际 %q", conf.FxRateEndpoint)
	}
	if conf.LogLevel != "debug" {
		t.Errorf("log_level 应转小写并去空白，实际 %q", conf.LogLevel)
	}
	// 密钥是不透明字节串：静默裁剪会让签名结果取决于用户看不见的字符。
	if conf.TokenSecret != "  keep me  " {
		t.Errorf("token_secret 必须原样保留，实际 %q", conf.TokenSecret)
	}
	if conf.FxRateSecret != "  fx keep  " {
		t.Errorf("fx_rate_secret 必须原样保留，实际 %q", conf.FxRateSecret)
	}
}

// TestLoadAcceptListenAddress 覆盖样例注释列出的三种合法监听地址写法。
func TestLoadAcceptListenAddress(t *testing.T) {
	for _, address := range []string{":4747", "127.0.0.1:4747", "[::1]:4747", "0.0.0.0:80"} {
		t.Run(address, func(t *testing.T) {
			text := fmt.Sprintf("listen_address: %q\ntoken_secret: \"s\"\n", address)
			conf, err := Load(writeConf(t, text))
			if err != nil {
				t.Fatalf("加载配置，期望接受 %s，实际: %+v", address, err)
			}
			if conf.ListenAddress != address {
				t.Errorf("listen_address，期望 %s，实际 %s", address, conf.ListenAddress)
			}
		})
	}
}

// TestLoadAcceptLogLevel 断言 7 档级别全部被接受，且能转成对应的 logrus.Level。
// warning 是 logrus 对 warn 的别名，一并覆盖以确认口径与 base/log 完全同源。
func TestLoadAcceptLogLevel(t *testing.T) {
	cases := map[string]logrus.Level{
		"panic":   logrus.PanicLevel,
		"fatal":   logrus.FatalLevel,
		"error":   logrus.ErrorLevel,
		"warn":    logrus.WarnLevel,
		"warning": logrus.WarnLevel,
		"info":    logrus.InfoLevel,
		"debug":   logrus.DebugLevel,
		"trace":   logrus.TraceLevel,
	}
	for level, expect := range cases {
		t.Run(level, func(t *testing.T) {
			text := fmt.Sprintf("token_secret: \"s\"\nlog_level: %q\n", level)
			conf, err := Load(writeConf(t, text))
			if err != nil {
				t.Fatalf("加载配置，期望接受 log_level=%s，实际: %+v", level, err)
			}
			if got := conf.GetLogrusLevel(); got != expect {
				t.Errorf("日志级别转换，期望 %v，实际 %v", expect, got)
			}
		})
	}
}

// TestGetLogrusLevelFallback 断言零值或非法级别退回默认级别，而不返回越界值。
func TestGetLogrusLevelFallback(t *testing.T) {
	if got := (Config{}).GetLogrusLevel(); got != logrus.InfoLevel {
		t.Errorf("零值配置的日志级别，期望 %v，实际 %v", logrus.InfoLevel, got)
	}
	if got := (Config{LogLevel: "verbose"}).GetLogrusLevel(); got != logrus.InfoLevel {
		t.Errorf("非法级别应退回默认，期望 %v，实际 %v", logrus.InfoLevel, got)
	}
}

// TestLoadReject 覆盖全部拒绝面，并断言错误文本含可定位的键名或路径。
func TestLoadReject(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		expect string // 错误文本必须包含的定位信息
	}{
		{"令牌密钥缺失", "log_level: \"info\"\n", "token_secret"},
		{"令牌密钥为空串", "token_secret: \"\"\n", "token_secret"},
		{"令牌密钥全空白", "token_secret: \"   \"\n", "token_secret"},
		{"日志级别非法", "token_secret: \"s\"\nlog_level: \"verbose\"\n", "log_level"},
		{"监听地址缺少端口", "token_secret: \"s\"\nlisten_address: \"4747\"\n", "listen_address"},
		{"监听地址端口段为空", "token_secret: \"s\"\nlisten_address: \"127.0.0.1:\"\n", "listen_address"},
		{"监听地址形态非法", "token_secret: \"s\"\nlisten_address: \"a:b:c\"\n", "listen_address"},
		{"SQLite路径显式空白", "token_secret: \"s\"\nsqlite_path: \"   \"\n", ""}, // 空白被规整后回填默认值，此例应通过，见下方单独处理
		{"未知键", "token_secret: \"s\"\nsqlitepath: \"x.db\"\n", "解析配置文件异常"},
		{"YAML语法错误", "token_secret: \"s\"\n  bad_indent: 1\n", "解析配置文件异常"},
	}
	for _, c := range cases {
		if c.expect == "" {
			continue // 该行为在 TestLoadBlankSqlitePathFallsBackToDefault 中单独断言
		}
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(writeConf(t, c.text))
			if err == nil {
				t.Fatalf("加载配置，期望失败，实际成功")
			}
			if !strings.Contains(err.Error(), c.expect) {
				t.Errorf("加载配置，错误文本应含 %q，实际: %+v", c.expect, err)
			}
		})
	}
}

// TestLoadBlankSqlitePathFallsBackToDefault 明确 sqlite_path 配成空白时的行为：
// 与「未配置」等同，回填默认路径，而不是报错——空白在路径位置无语义。
func TestLoadBlankSqlitePathFallsBackToDefault(t *testing.T) {
	conf, err := Load(writeConf(t, "token_secret: \"s\"\nsqlite_path: \"   \"\n"))
	if err != nil {
		t.Fatalf("加载配置异常: %+v", err)
	}
	if conf.SqlitePath != DefaultSqlitePath {
		t.Errorf("sqlite_path 空白应回填默认值 %s，实际 %q", DefaultSqlitePath, conf.SqlitePath)
	}
}

// TestLoadPathError 覆盖路径本身的两种错误：文件不存在、路径为空。
func TestLoadPathError(t *testing.T) {
	t.Run("文件不存在", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.yaml")
		_, err := Load(path)
		if err == nil {
			t.Fatal("加载配置，期望因文件不存在而失败，实际成功")
		}
		// 报错必须给出可操作的下一步：复制样例、或用环境变量指定路径。
		for _, want := range []string{path, ExamplePath, EnvPath} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("加载配置，错误文本应含 %q，实际: %+v", want, err)
			}
		}
	})
	t.Run("路径为空", func(t *testing.T) {
		if _, err := Load("   "); err == nil {
			t.Fatal("加载配置，期望因路径为空而失败，实际成功")
		}
	})
}

// TestStringMaskSecret 断言 String 脱敏：密钥原文不得出现在输出中。
func TestStringMaskSecret(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef" // 48 字符
	const fxSecret = "fx-secret-value"
	text := fmt.Sprintf("token_secret: %q\nfx_rate_secret: %q\n"+
		"fx_rate_endpoint: \"https://fx.example.com\"\n", token, fxSecret)
	conf, err := Load(writeConf(t, text))
	if err != nil {
		t.Fatalf("加载配置异常: %+v", err)
	}

	// 值与指针两条打印路径都要脱敏：值接收器使 Config 与 *Config 同时受保护。
	for name, output := range map[string]string{
		"String()": conf.String(),
		"%v(值)":    fmt.Sprintf("%v", *conf),
		"%+v(指针)":  fmt.Sprintf("%+v", conf),
		"%s(值)":    fmt.Sprintf("%s", *conf),
	} {
		if strings.Contains(output, token) {
			t.Errorf("%s 泄露了 token_secret: %s", name, output)
		}
		if strings.Contains(output, fxSecret) {
			t.Errorf("%s 泄露了 fx_rate_secret: %s", name, output)
		}
		// 长度信息应保留，便于部署者确认密钥确实读进来了。
		if !strings.Contains(output, "已设置(48字符)") {
			t.Errorf("%s 应含 token_secret 的长度提示: %s", name, output)
		}
		// 端点不是密钥，应当照实输出，便于排查连不上汇率源的问题。
		if !strings.Contains(output, "https://fx.example.com") {
			t.Errorf("%s 应含 fx_rate_endpoint 原值: %s", name, output)
		}
	}
}

// TestStringUnsetSecret 断言未设置与全空白的密钥都呈现为「未设置」，
// 与 check 的判空口径一致。
func TestStringUnsetSecret(t *testing.T) {
	output := Config{TokenSecret: "", FxRateSecret: "   "}.String()
	if strings.Count(output, "未设置") != 2 {
		t.Errorf("空密钥与全空白密钥均应显示未设置，实际: %s", output)
	}
}
