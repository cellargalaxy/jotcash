package config_test

// 外部消费者验证：以**黑盒**方式（只用导出 API，包名为 config_test）模拟
// §六 L7 的两个唯一调用方——`cmd/jotcash`（读配置）与 `app`（把参数注入 L0~L6），
// 确认包对外契约真的可用。
//
// 另有一条独立校验路径：**绕过 Load**，直接用 yaml.v2 解析仓库里真实提交的
// `configs/jotcash.example.yaml`，逐键与 Config 的 yaml 标签比对——
// 同一实现自己读自己写的样例必然自洽，只有独立解析才能证明样例文件与结构体
// 没有跑偏（改了字段名却忘了改样例，是这类包最典型的腐化方式）。

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/config"
	"gopkg.in/yaml.v2"
)

// exampleConfigPath 样例文件相对本测试文件的路径（本包位于 internal/base/config/）。
const exampleConfigPath = "../../../configs/jotcash.example.yaml"

// TestExampleConfigMatchesStruct 独立解析真实样例文件：键集合必须与 Config 的
// yaml 标签**完全一致**——多一个键说明样例含已废弃项，少一个键说明新字段没写进样例，
// 两者都会让「复制样例即可运行」这条承诺失效。
func TestExampleConfigMatchesStruct(t *testing.T) {
	data, err := os.ReadFile(exampleConfigPath)
	if err != nil {
		t.Fatalf("读取样例文件异常: %+v", err)
	}

	// 解析成 map 而非 Config：解析成 Config 会让未知键被静默丢弃，检不出多余项
	raw := map[string]any{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("样例文件不是合法 YAML: %+v", err)
	}

	// 从结构体反射取 yaml 标签，不手写期望清单——手写清单会与结构体二次漂移
	structType := reflect.TypeOf(config.Config{})
	wantKeys := make([]string, 0, structType.NumField())
	for i := 0; i < structType.NumField(); i++ {
		tag := structType.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			t.Fatalf("字段 %s 缺少 yaml 标签", structType.Field(i).Name)
		}
		wantKeys = append(wantKeys, strings.Split(tag, ",")[0])
	}
	gotKeys := make([]string, 0, len(raw))
	for key := range raw {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(wantKeys)
	sort.Strings(gotKeys)
	if !reflect.DeepEqual(wantKeys, gotKeys) {
		t.Fatalf("样例文件与结构体的键不一致\n样例 %v\n结构 %v", gotKeys, wantKeys)
	}

	// 样例里的 token_secret 必须留空：预填一个值会让所有部署共用同一个密钥，
	// 任何人都能用公开仓库里的这个值伪造出登录令牌
	if secret, _ := raw["token_secret"].(string); strings.TrimSpace(secret) != "" {
		t.Fatalf("样例文件预填了令牌密钥，会导致各部署共用同一密钥: %q", secret)
	}
}

// TestExampleConfigIsUsable 样例文件「复制 → 填密钥 → 启动」这条路径真的能走通。
func TestExampleConfigIsUsable(t *testing.T) {
	data, err := os.ReadFile(exampleConfigPath)
	if err != nil {
		t.Fatalf("读取样例文件异常: %+v", err)
	}
	// 模拟用户复制样例后填入密钥这一个动作
	text := strings.Replace(string(data), `token_secret: ""`, `token_secret: "my-random-secret"`, 1)
	if !strings.Contains(text, "my-random-secret") {
		t.Fatalf("样例文件中未找到 token_secret: \"\" 这一行，用户无从填写密钥")
	}

	path := filepath.Join(t.TempDir(), "jotcash.yaml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatalf("写入配置文件异常: %+v", err)
	}
	loaded, err := config.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("按样例填写后加载失败，「复制样例即可运行」不成立: %+v", err)
	}
	if loaded.TokenSecret != "my-random-secret" {
		t.Fatalf("令牌密钥 = %q", loaded.TokenSecret)
	}
	// 样例中写明的默认值必须与包常量一致，否则文档与实现两套口径
	if loaded.ListenAddress != config.DefaultListenAddress {
		t.Fatalf("样例监听地址 %q 与常量 %q 不一致", loaded.ListenAddress, config.DefaultListenAddress)
	}
	if loaded.SqlitePath != config.DefaultSqlitePath {
		t.Fatalf("样例 SQLite 路径 %q 与常量 %q 不一致", loaded.SqlitePath, config.DefaultSqlitePath)
	}
	if loaded.LogLevel != config.DefaultLogLevel {
		t.Fatalf("样例日志级别 %q 与常量 %q 不一致", loaded.LogLevel, config.DefaultLogLevel)
	}
}

// TestCmdAndAppFlow 模拟 §六 L7 的两个调用方：
// `cmd/jotcash` 读配置 → `app` 取字段注入 L0~L6（向 token 注入密钥、按路径构造
// store/sqlite、按端点与密钥注册 fxrate/<源>、把级别交给 base/log）。
func TestCmdAndAppFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jotcash.yaml")
	text := `
listen_address: "127.0.0.1:18081"
sqlite_path: "data/app.db"
token_secret: "app-token-secret"
fx_rate_endpoint: "https://fx.example.com/v1"
fx_rate_secret: "app-fx-secret"
log_level: "warn"
`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatalf("写入配置文件异常: %+v", err)
	}

	// —— cmd/jotcash：读配置 ——
	conf, err := config.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("cmd 读配置异常: %+v", err)
	}

	// —— app：把参数注入各层（此处以桩函数代替尚未实现的包）——
	var injectedTokenSecret, injectedSqlitePath, injectedLogLevel string
	var injectedFxEndpoint, injectedFxSecret string
	injectToken := func(secret string) { injectedTokenSecret = secret }
	buildStore := func(path string) { injectedSqlitePath = path }
	registerFxRate := func(endpoint, secret string) { injectedFxEndpoint, injectedFxSecret = endpoint, secret }
	initLog := func(level string) { injectedLogLevel = level }

	injectToken(conf.TokenSecret)
	buildStore(conf.SqlitePath)
	registerFxRate(conf.FxRateEndpoint, conf.FxRateSecret)
	initLog(conf.LogLevel)

	if injectedTokenSecret != "app-token-secret" {
		t.Fatalf("注入 token 的密钥 = %q", injectedTokenSecret)
	}
	if injectedSqlitePath != "data/app.db" {
		t.Fatalf("注入 store/sqlite 的路径 = %q", injectedSqlitePath)
	}
	if injectedFxEndpoint != "https://fx.example.com/v1" || injectedFxSecret != "app-fx-secret" {
		t.Fatalf("注入 fxrate 的端点与密钥 = %q / %q", injectedFxEndpoint, injectedFxSecret)
	}
	// warn 已被规整为 logrus 规范名 warning，base/log 无需再处理别名
	if injectedLogLevel != "warning" {
		t.Fatalf("注入 base/log 的级别 = %q", injectedLogLevel)
	}

	// 值语义：配置被下游取用后，调用方持有的副本不受影响
	if conf.TokenSecret != "app-token-secret" {
		t.Fatalf("配置在注入过程中被改写: %+v", conf)
	}

	// app 记录启动日志时用 String()，密钥不得落盘
	if strings.Contains(conf.String(), "app-token-secret") || strings.Contains(conf.String(), "app-fx-secret") {
		t.Fatalf("启动日志会泄漏密钥: %s", conf.String())
	}
}

// TestLoadFailureStopsStartup 配置不合法时 cmd 必须拿到 error 并终止启动，
// 而不是拿着半套配置继续跑（尤其不能在密钥为空的情况下把服务拉起来）。
func TestLoadFailureStopsStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jotcash.yaml")
	if err := os.WriteFile(path, []byte("listen_address: \":4747\"\n"), 0600); err != nil {
		t.Fatalf("写入配置文件异常: %+v", err)
	}
	conf, err := config.Load(context.Background(), path)
	if err == nil {
		t.Fatalf("缺令牌密钥竟未报错，服务会在无签名密钥的情况下启动")
	}
	if conf != (config.Config{}) {
		t.Fatalf("失败时返回了非零值配置，漏判 error 的调用方可能据此启动: %+v", conf)
	}
}
