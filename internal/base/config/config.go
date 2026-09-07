// Package config 承载 jotcash 的部署相关参数（L0 基础层）。
//
// 边界（依据 doc/decisions/仓库代码包的层级设计与划分.md）：
//   - 本包**只装部署参数**，共 5 类：监听地址、SQLite 文件路径、令牌密钥、
//     汇率源端点与密钥、日志级别。业务阈值（令牌 12 小时、连续失败 5 次、
//     锁定 10 分钟、Argon2 参数、汇率 8 位小数、本位币 2 位定点、密码长度与
//     字符类别要求）一律写死在各自的包内，不在此配置——它们是业务规则而非
//     部署差异，安全参数尤其不应存在可被调低的入口。
//   - 本位币与界面语言是**用户级**设置，按账户存于库中，不进本包。
//   - 本包属 L0 且**层内零依赖**：不 import 仓库内任何其他包（含 base/errs
//     与 base/log）。校验失败一律返回 error，由 cmd 打印后退出——此刻
//     base/log 尚未按级别初始化，本包无处可记。
//   - 按约定 8，本包**仅被 app 与 cmd import**。Go 的可见性机制无法强制这一点，
//     靠评审与 import 检查兜底。
package config

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const (
	// EnvPath 是指定配置文件路径的环境变量名。
	// 路径确定顺序：命令行参数 > 环境变量 jotcash_config > DefaultPath。
	EnvPath = "jotcash_config"

	// DefaultPath 是配置文件的默认路径（相对二进制的工作目录）。
	// 真实配置含令牌密钥，已被 .gitignore 忽略；随仓库提交的是 ExamplePath。
	DefaultPath = "configs/jotcash.yaml"

	// ExamplePath 是随仓库提交的配置样例路径，仅用于在报错时指引部署者复制。
	ExamplePath = "configs/jotcash.example.yaml"

	// DefaultListenAddress 不绑定具体网卡，容器与局域网均可访问。
	// 只允许本机访问时填 127.0.0.1:4747；IPv6 填 [::1]:4747。
	DefaultListenAddress = ":4747"

	// DefaultSqlitePath 是 SQLite 数据文件的默认路径。运行时同目录还会产生
	// -wal 与 -shm 两个副产品文件，备份时需一并考虑。
	// 该目录**不由本包创建**——按样例约定，由程序在初始化数据库阶段创建。
	DefaultSqlitePath = "data/jotcash.db"

	// DefaultLogLevel 是默认日志级别。
	DefaultLogLevel = "info"
)

// Config 是部署参数的唯一载体。
//
// 字段与 configs/jotcash.example.yaml 的键名逐一对应，样例文件即本结构体的
// 契约来源，单测会加载该文件反向核验，避免二者日后各走各路。
type Config struct {
	// ListenAddress HTTP 监听地址，必须带端口。
	ListenAddress string `yaml:"listen_address"`

	// SqlitePath SQLite 数据文件路径。
	SqlitePath string `yaml:"sqlite_path"`

	// TokenSecret 登录令牌签名密钥，**必填**。令牌是无状态的，其不可伪造性
	// 完全依赖此密钥，故留空即拒绝启动。修改此值会使所有已签发的令牌立即失效。
	TokenSecret string `yaml:"token_secret"`

	// FxRateEndpoint 汇率源服务端点。汇率源尚未选定，允许留空，
	// 且**不做任何形态校验**——端点的具体形态由未来选定的汇率源决定。
	FxRateEndpoint string `yaml:"fx_rate_endpoint"`

	// FxRateSecret 汇率源访问密钥。是否需要密钥同样由汇率源决定，允许留空。
	FxRateSecret string `yaml:"fx_rate_secret"`

	// LogLevel 日志级别，取值为 logrus 的 7 档：
	// panic / fatal / error / warn / info / debug / trace。
	LogLevel string `yaml:"log_level"`
}

// ResolvePath 按「命令行参数 > 环境变量 jotcash_config > DefaultPath」定出配置
// 文件路径。cliPath 由 cmd 自己的 flag 提供——flag 的定义权在 cmd，本包不注册
// 任何全局 flag，以免污染 go test 的 flag 集。
//
// 本函数是纯函数：只读环境变量，不访问文件系统，也不判断文件是否存在。
// 与 Load 分开而不合并，是为了让调用方能拿到**实际生效的路径**用于日志与报错，
// 不必从错误文本里反解。
func ResolvePath(cliPath string) string {
	// 首尾空白多来自 shell、.env 文件与容器编排的参数传递，无语义，一律忽略；
	// 否则 " configs/jotcash.yaml" 会被当成一个确实不存在的路径而报文件不存在。
	if path := strings.TrimSpace(cliPath); path != "" {
		return path
	}
	if path := strings.TrimSpace(os.Getenv(EnvPath)); path != "" {
		return path
	}
	return DefaultPath
}

// Load 从 path 读取并解析配置：读文件 → 严格解析 → 回填默认值 → 校验。
// 任一步失败均返回中文错误，错误文本带上路径或键名，可直接呈现给部署者。
//
// 三点刻意的选择：
//   - 文件不存在即报错，**不自动落盘一份默认配置**：自动生成的文件其
//     token_secret 必为空，随即被必填校验拒绝，等于用一次静默写盘换一个更晚
//     更绕的报错。故直接报错，并在错误里给出 cp 命令与环境变量名。
//   - 用 yaml.UnmarshalStrict 而非 Unmarshal：未知键直接报错。代价是将来删键时
//     旧配置文件会启动失败；收益是键名拼错（如 sqlitepath）不再静默退回默认值。
//   - 不 panic：启动失败如何呈现（打日志、退出码）属 cmd 的决定。
func Load(path string) (*Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("加载配置，配置文件路径为空")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("加载配置，配置文件不存在: %s。"+
				"请复制样例后填写: cp %s %s；或用环境变量 %s 指定其他路径",
				path, ExamplePath, DefaultPath, EnvPath)
		}
		return nil, fmt.Errorf("加载配置，读取配置文件异常: %s: %+v", path, err)
	}

	var conf Config
	// UnmarshalStrict：出现未知键或重复键时报错，不静默丢弃。
	if err := yaml.UnmarshalStrict(data, &conf); err != nil {
		return nil, fmt.Errorf("加载配置，解析配置文件异常: %s: %+v", path, err)
	}

	conf.fillDefault()
	if err := conf.check(path); err != nil {
		return nil, err
	}
	return &conf, nil
}

// fillDefault 为空值字段回填默认值，并规整那些空白字符无语义的字段。
//
// 空白处理分两类，这是本包唯一需要区别对待的地方：
//   - 结构性取值（监听地址、SQLite 路径、日志级别、汇率源端点）：**去首尾空白后
//     存储**。这些位置的空白从无语义，" :4747 " 只会让 net.Listen 报一个难懂的错。
//   - 两个密钥（TokenSecret、FxRateSecret）：**判空时去空白、存储时原样保留**。
//     密钥是不透明字节串，静默裁剪会让签名结果取决于用户看不见的字符，
//     且同一份配置在不同实现下裁剪口径未必一致；但全空白显然是没填，
//     必须按未填拒绝——这一判定在 check 里做，此处不动其值。
func (this *Config) fillDefault() {
	this.ListenAddress = strings.TrimSpace(this.ListenAddress)
	if this.ListenAddress == "" {
		this.ListenAddress = DefaultListenAddress
	}

	this.SqlitePath = strings.TrimSpace(this.SqlitePath)
	if this.SqlitePath == "" {
		this.SqlitePath = DefaultSqlitePath
	}

	// 汇率源端点允许为空，只做规整不回填默认值——汇率源尚未选定，无默认值可言。
	this.FxRateEndpoint = strings.TrimSpace(this.FxRateEndpoint)

	// 日志级别统一转小写，"INFO" 与 "Info" 都应被接受，避免部署者因大小写卡住。
	this.LogLevel = strings.ToLower(strings.TrimSpace(this.LogLevel))
	if this.LogLevel == "" {
		this.LogLevel = DefaultLogLevel
	}
}

// check 校验配置的合法性。path 只用于错误文本定位，不参与判定。
func (this *Config) check(path string) error {
	// 监听地址必须带端口。net.SplitHostPort 正好覆盖样例的三种写法
	// （:4747、127.0.0.1:4747、[::1]:4747），而漏了冒号的 "4747" 被拒。
	// 不额外要求端口为数字——net.Listen 同样接受 http 这类服务名。
	port, err := listenPort(this.ListenAddress)
	if err != nil {
		return fmt.Errorf("加载配置，listen_address 非法: %s: %s: %+v",
			path, this.ListenAddress, err)
	}
	if port == "" {
		return fmt.Errorf("加载配置，listen_address 缺少端口: %s: %s。"+
			"形如 %s / 127.0.0.1:4747 / [::1]:4747",
			path, this.ListenAddress, DefaultListenAddress)
	}

	// 令牌密钥必填：令牌无状态，其不可伪造性完全依赖此密钥，留空即拒绝启动。
	// 判空前去空白，"   " 这种显然是没填。
	if strings.TrimSpace(this.TokenSecret) == "" {
		return fmt.Errorf("加载配置，token_secret 为空: %s。"+
			"令牌是无状态的，其不可伪造性完全依赖此密钥，"+
			"请生成一个随机值填入: openssl rand -base64 48", path)
	}

	// 日志级别交给 logrus 校验：本包认可的取值与 base/log 能接受的取值因此
	// 天然是同一个集合，不会出现两套白名单各自漂移。
	if _, err := logrus.ParseLevel(this.LogLevel); err != nil {
		return fmt.Errorf("加载配置，log_level 非法: %s: %s。"+
			"可选 panic / fatal / error / warn / info / debug / trace",
			path, this.LogLevel)
	}

	// SQLite 路径经 fillDefault 后不可能为空，此处兜住"显式配成空白"的情形。
	// 只校验取值形态，**不校验目录是否存在、也不创建目录**——按样例约定，
	// 目录由程序在初始化数据库阶段创建，职责属 store/sqlite。
	if this.SqlitePath == "" {
		return fmt.Errorf("加载配置，sqlite_path 为空: %s", path)
	}

	// 汇率源端点与密钥**不做任何校验**：汇率源尚未选定，是否需要密钥、
	// 端点的具体形态均由该汇率源决定，此处任何校验都是凭空猜测。
	return nil
}

// listenPort 取出监听地址的端口段。仅在地址形态非法时返回 error。
func listenPort(address string) (string, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	return port, nil
}

// GetLogrusLevel 返回已校验的日志级别对应的 logrus.Level，供 app 传给 base/log。
//
// 仅在 Load 成功返回的 Config 上调用才有意义：此时级别必然合法。零值 Config
// 或手工构造的非法级别一律退回 DefaultLogLevel，保证本方法不返回 error、
// 也不让调用方拿到一个越界的级别值。
func (this Config) GetLogrusLevel() logrus.Level {
	level, err := logrus.ParseLevel(this.LogLevel)
	if err != nil {
		level, _ = logrus.ParseLevel(DefaultLogLevel)
	}
	return level
}

// String 输出**脱敏**后的配置，两个密钥只显示「已设置(N字符)/未设置」。
//
// fmt 对 %v、%s、%+v 都会走 Stringer，因此 logrus.Fields{"conf": conf} 与
// fmt.Sprintf("%+v", conf) 均自动脱敏；用值接收器可使 Config 与 *Config 同时
// 受保护。已知边界：yaml.Marshal 不走 Stringer，仍会输出明文——那是"写出配置
// 文件"的正当用途，本包不提供该能力，也不应用于日志。
func (this Config) String() string {
	return fmt.Sprintf("{listen_address:%s sqlite_path:%s token_secret:%s "+
		"fx_rate_endpoint:%s fx_rate_secret:%s log_level:%s}",
		this.ListenAddress, this.SqlitePath, maskSecret(this.TokenSecret),
		this.FxRateEndpoint, maskSecret(this.FxRateSecret), this.LogLevel)
}

// maskSecret 把密钥脱敏为「已设置(N字符)」或「未设置」。
// 只暴露长度不暴露内容：长度足以让部署者确认"密钥确实读进来了"，
// 又不足以推出密钥本身。全空白按未设置呈现，与 check 的判定口径一致。
func maskSecret(secret string) string {
	if strings.TrimSpace(secret) == "" {
		return "未设置"
	}
	return fmt.Sprintf("已设置(%d字符)", len([]rune(secret)))
}
