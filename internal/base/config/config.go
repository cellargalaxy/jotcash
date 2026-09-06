// Package config 提供部署参数的加载与校验，是全仓库唯一的配置读取入口。
//
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md` §六 L0 `base/config` 行：
// 「**仅部署相关参数**：监听地址、SQLite 文件路径、令牌密钥、汇率源端点与密钥、日志级别。
// **业务阈值不进配置**（令牌 12h 写死在 `token`、失败 5 次与锁定 10 分钟写死在
// `service/auth`、Argon2 参数写死在 `base/pwdhash`——终版 A-5/A-8 把它们定义为规则）；
// **仅被 `app` / `cmd` import**（约定 8）」。
//
// 四条硬约束（决定了本包每一个签名的形状）：
//
//  1. **字段只有这 5 类**（§十 约定 8 复述了同一份清单）。任何「顺手加个开关」的字段都
//     不属本包——加进来即等于给业务阈值开了可被部署方调低的口子，A-5/A-8/T36 的
//     安全语义会被配置文件绕过。反查清单见本文件末尾的注释。
//  2. **L0 层内零依赖**（§六 L0 表头「仅依赖标准库与第三方库」+ 约定 5）：本包不 import
//     任何 `internal/` 包。因此错误用 `github.com/pkg/errors` 而**不用 `base/errs`**
//     （那是 L0 兄弟包），日志直接用 `logrus` 而**不用 `base/log`**（同理）。
//     这也符合语义：配置加载发生在启动阶段，没有请求也没有用户，`base/errs` 的五档
//     （用户输入错误 / 唯一冲突 / 归属越权 / 未登录 / 系统错误）携带的是**面向用户的
//     文案键**，供 `middleware` 经 `i18n` 渲染，套到启动期错误上是错配。
//  3. **配置只在 L7 读**（约定 8）：L0~L6 所需部署参数由 `app` 注入，因此本包**不提供
//     包级全局变量**——只提供 `Load` 返回值。全局单例会让任何包 import 后直接读到配置，
//     把 §四 R3 登记的残留（「`base/config` 是普通包，任何包 import 得到」）放大；
//     返回值形态让「谁拿到了配置」在函数签名上可见。
//  4. **只读、一次性**：`Load` 无写盘、无建目录、无守护协程。不做热更新是刻意的——
//     数据流是「`app` 读一次 → 注入 L0~L6」，配置改了没有回路能把新值送进已注入的
//     `token`（密钥）与 `store/sqlite`（已按旧路径开的写连接单例），热更新只会制造
//     「改了没生效」的假象；且这 6 个参数本身都要重启才能生效，令牌密钥热改更等于
//     当场作废全部登录态，而 §五 3 规定令牌失效只有 `User.登录态基线时间` 一条路径。
//
// 调用方恰为 2 个（§六 L7 表 `app` 行与 `cmd/jotcash` 行的依赖列）：
//
//	cmd/jotcash → Load        main：读配置 → 交给 app
//	app         → Config 字段  装配时把参数注入 L0~L6（向 token 注入密钥、
//	                          按路径构造 store/sqlite、按端点与密钥注册 fxrate/<源>）
package config

import (
	"context"
	"net"
	"strings"

	"github.com/cellargalaxy/go_common/util"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultConfigPath 默认配置文件路径。
	//
	// 相对工作目录而非绝对路径：§十 目录树把 `configs/` 标注为「配置样例，运行时加载
	// （唯一非 embed 的外部资源）」，与 `go_common/util` 把日志落到相对目录
	// `log/<服务名>/log.log` 的取向一致，二进制与配置同级摆放即可运行。
	DefaultConfigPath = "configs/jotcash.yaml"

	// ExampleConfigPath 配置样例文件路径，随仓库提交。
	//
	// 它是「配置样例」这一目录职责的落地：真实配置含令牌密钥，不入库
	// （见 `.gitignore` 的 `/configs/jotcash.yaml`），样例带注释与默认值供复制。
	// 出现在错误提示里，让「配置文件不存在」这条错误自带下一步动作。
	ExampleConfigPath = "configs/jotcash.example.yaml"

	// ConfigPathEnvKey 指定配置文件路径的环境变量名。
	//
	// 命名沿用 `go_common/util` 的既有口径（`util/os.go` 的 `serverNameKey = "server_name"`）：
	// 全小写下划线；加 `jotcash_` 前缀避免与同机其他服务的环境变量撞名。
	//
	// 只有「配置文件路径」这一项支持环境变量，**逐字段覆盖不做**：承载只要求「装部署参数」，
	// 未要求多来源合并；逐字段覆盖会引出「文件与环境变量冲突时以谁为准」的第二套口径，
	// 属未定事项，不脑补。
	ConfigPathEnvKey = "jotcash_config"

	// DefaultListenAddress 默认监听地址。
	//
	// 需求与分层文档均未规定端口，取一个不与常见服务冲突的四位端口作为默认值
	// （同作者其余仓库亦为四位端口的习惯：survive_monitor `:4343`、server_center `:7557`）。
	// 默认不绑定具体网卡（形如 `:4747` 而非 `127.0.0.1:4747`），因为个人自建系统常跑在
	// 容器或 NAS 里，绑死回环会让宿主机与局域网都访问不到；要收窄成仅本机访问，
	// 在配置文件里写全 `127.0.0.1:4747` 即可。
	DefaultListenAddress = ":4747"

	// DefaultSqlitePath 默认 SQLite 数据文件路径。
	//
	// 单独放 `data/` 而不与配置同目录：§六 L4 `store/sqlite` 定了「单写 + WAL」，
	// WAL 模式下同目录还会产生 `-wal`、`-shm` 两个副产品文件，与 `configs/` 混放会让
	// 「哪些该备份、哪些该入库」难以分辨。`.gitignore` 已忽略 `/data/`。
	DefaultSqlitePath = "data/jotcash.db"

	// DefaultLogLevel 默认日志级别。
	//
	// 取 info 而非 debug：A-3 的 admin 初始密码一次性打印走 `base/log`，是全系统唯一把
	// 密码明文写入持久介质的地方（§六 L0 `base/log` 行），默认放大日志量只会让这条
	// 关键信息更难被发现，也更容易被无意间分享出去。
	DefaultLogLevel = "info"

	// maskedSecret 密钥在日志中的替代文本，见 Config.String。
	maskedSecret = "******"
)

// Config 部署参数全集，字段与 §六 L0 `base/config` 行列出的 5 类参数一一对应
// （汇率源的「端点与密钥」各占一个字段，故为 6 个字段）。
//
// yaml 标签用于反序列化配置文件，json 标签用于 String() 的日志输出。
// 两套标签取同名，保证配置文件里的键名与日志里的键名一致，排查时无需在两套命名间换算。
//
// 值类型（非指针）传递：6 个字段全为 string，复制成本可忽略，且值语义天然阻止
// 「`app` 注入后某个下游又改回来」——配置在启动后即为不可变事实。
type Config struct {
	// ListenAddress HTTP 服务监听地址，形如 `:4747` 或 `127.0.0.1:4747`，由 `app` 用于启动服务。
	ListenAddress string `yaml:"listen_address" json:"listen_address"`

	// SqlitePath SQLite 数据文件路径，由 `app` 注入 `store/sqlite`（§六 L4：文件路径由 app 注入）。
	SqlitePath string `yaml:"sqlite_path" json:"sqlite_path"`

	// TokenSecret 登录令牌签名密钥，由 `app` 注入 `token`
	// （§六 L3 `token` 行：「无状态令牌，12h 写死，**仅密钥由 app 注入**」）。
	//
	// ⚠️ 该值绝不可进日志：String() 已做掩码，新增日志点请一律走 String()。
	TokenSecret string `yaml:"token_secret" json:"token_secret"`

	// FxRateEndpoint 汇率源服务端点，由 `app` 注入 `fxrate/<源>`（§六 L4：端点与密钥由 app 注入）。
	FxRateEndpoint string `yaml:"fx_rate_endpoint" json:"fx_rate_endpoint"`

	// FxRateSecret 汇率源访问密钥，由 `app` 注入 `fxrate/<源>`。
	//
	// ⚠️ 同 TokenSecret，不可进日志。
	FxRateSecret string `yaml:"fx_rate_secret" json:"fx_rate_secret"`

	// LogLevel 日志级别，由 `app` 注入 `base/log`。取值为 logrus 可解析的级别名，
	// 经 Load 校验并规整为小写规范形式（见 checkAndReset）。
	LogLevel string `yaml:"log_level" json:"log_level"`
}

// String 返回可直接进日志的配置文本，**两个密钥字段被掩码**。
//
// 依据 §九 红线行的取向：密码与密钥类数据只在受限范围内流转，A-3 的初始密码打印是
// 全系统唯一的落盘出口（在 `base/log`），本包不属于任何出口，因此密钥绝不能出现在
// 日志里——配置日志通常是排障时第一份被贴出来分享的材料。
//
// 掩码而非整字段隐去，是为了保留「密钥是否已配置」这一排障真正需要的信号：
// 启动失败最常见的原因就是密钥没填，全部隐去会让日志无法区分「填了」与「没填」。
//
// 实现上复制值后就地改写：接收者为**值类型**，改写只作用于副本，调用方持有的配置不受影响；
// 复制整个结构（而非另写一个影子结构逐字段搬运）保证将来新增字段会自动出现在日志里，
// 不会因为漏改一处而静默少打。
func (config Config) String() string {
	config.TokenSecret = maskSecret(config.TokenSecret)
	config.FxRateSecret = maskSecret(config.FxRateSecret)
	return util.JsonStruct2String(config)
}

// GetConfigPath 按优先级确定配置文件路径：显式入参 → 环境变量 ConfigPathEnvKey → DefaultConfigPath。
//
// 入参优先于环境变量，是因为入参来自 `cmd/jotcash` 的命令行参数，属「本次运行的明确指定」；
// 环境变量属部署环境的默认设定，理应可被单次运行覆盖。
//
// 三处取值一律 TrimSpace：环境变量常经 shell、.env 文件、容器编排配置传入而带首尾空白，
// 带空白的路径会以「配置文件不存在」的形式失败，排查时极难看出问题出在一个空格上
// （`go_common/util` 的 GetEnvInt/GetEnvBool 出于同样理由也做了 TrimSpace）。
func GetConfigPath(path string) string {
	if trimmed := strings.TrimSpace(path); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(util.GetEnvString(ConfigPathEnvKey, "")); trimmed != "" {
		return trimmed
	}
	return DefaultConfigPath
}

// Load 加载并校验配置，是本包唯一的入口，供 `cmd/jotcash` 在 main 中调用一次。
//
// path 为空时按 GetConfigPath 的优先级回落。流程：定位路径 → 存在性检查 → 读取文本 →
// 反序列化 → 填默认值 → 校验，任一步失败即返回错误。
//
// 失败时返回**零值** Config：部署参数不合法就该启动失败，而不是带着半套配置跑起来。
// 若调用方漏判 error，拿到零值会在监听地址为空时当场失败，而拿到「半填默认值的配置」
// 则可能真的把服务拉起来——比如令牌密钥为空却仍在签发令牌，那是安全问题。
func Load(ctx context.Context, path string) (Config, error) {
	var config Config
	path = GetConfigPath(path)

	// 必须先判存在再读：`util.OpenReadFile` 在文件不存在时会**创建**该文件
	// （util/io.go 用 os.O_CREATE 打开），绕过判存直接读会在磁盘上留下一个空文件，
	// 让「配置到底有没有」这件事在第二次启动时变得更含糊。
	// GetFileInfo 对目录返回 nil（util/io.go：IsDir 时返回 nil），因此「路径指向目录」
	// 这一情况也在此处一并被挡住。
	if util.GetFileInfo(ctx, path) == nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"path": path}).Error("加载配置，配置文件不存在")
		return Config{}, errors.Errorf("加载配置，配置文件不存在或不是普通文件: %s，可复制样例 %s 后按需修改", path, ExampleConfigPath)
	}

	text, err := util.ReadFile2String(ctx, path, "")
	if err != nil {
		// util 已打日志，此处只补业务语境，不重复打
		return Config{}, errors.Errorf("加载配置，读取配置文件异常: %s: %+v", path, err)
	}

	// 空文件单独拦截：`util.ReadFile2String` 对空文件只返回空串不报错，
	// 若放它过去，最终会以「令牌密钥为空」的形式报错，把排查方向指向密钥而不是空文件。
	if strings.TrimSpace(text) == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"path": path}).Error("加载配置，配置文件为空")
		return Config{}, errors.Errorf("加载配置，配置文件为空: %s，可复制样例 %s 后按需修改", path, ExampleConfigPath)
	}

	// YamlString2Struct 内部已把 yaml.v2 对非指针目标的 panic 兜成 error 并记日志，
	// 此处只补路径语境后透传。
	if err := util.YamlString2Struct(ctx, text, &config); err != nil {
		return Config{}, errors.Errorf("加载配置，解析配置文件异常: %s: %+v", path, err)
	}

	if err := config.checkAndReset(ctx); err != nil {
		return Config{}, err
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{"path": path, "config": config.String()}).Info("加载配置，完成")
	return config, nil
}

// checkAndReset 填充默认值并校验，是本包全部取值口径的唯一落点。
//
// 校验强度按「这个参数填错了会不会静默产生错误行为」分档，不搞一视同仁：
//
//	必填     token_secret —— 空密钥等于令牌无签名，任何人都能伪造登录态
//	填默认+校验形状 listen_address / log_level —— 有合理默认值，但写错了要当场报
//	填默认   sqlite_path —— 有合理默认值，形状无从校验（任何字符串都可能是合法路径）
//	不校验   fx_rate_endpoint / fx_rate_secret —— 口径未定，见下方注释
//
// 一旦不合法立即返回错误，不静默降级为默认值：静默降级会让「配置看似生效实则未生效」，
// 这类问题在运行数月后才被发现的代价远高于启动失败。
func (config *Config) checkAndReset(ctx context.Context) error {
	// —— 监听地址：空则取默认，非空则校验形状 ——
	config.ListenAddress = strings.TrimSpace(config.ListenAddress)
	if config.ListenAddress == "" {
		config.ListenAddress = DefaultListenAddress
	}
	// 用 net.SplitHostPort 而非自己找冒号：IPv6 字面量形如 `[::1]:4747`，
	// 手写切分会把 `::1` 里的冒号当成分隔符。校验端口非空是必要的补充——
	// SplitHostPort 对 `:`（端口为空）不报错，但 net.Listen 会失败在启动那一刻。
	_, port, err := net.SplitHostPort(config.ListenAddress)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"listenAddress": config.ListenAddress, "err": err}).Error("加载配置，监听地址非法")
		return errors.Errorf("加载配置，监听地址非法: %s，应形如 :4747 或 127.0.0.1:4747: %+v", config.ListenAddress, err)
	}
	if strings.TrimSpace(port) == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"listenAddress": config.ListenAddress}).Error("加载配置，监听地址缺少端口")
		return errors.Errorf("加载配置，监听地址缺少端口: %s，应形如 :4747 或 127.0.0.1:4747", config.ListenAddress)
	}

	// —— SQLite 文件路径：空则取默认 ——
	// 不建目录、不校验可写：建库与建目录属 `store/sqlite`（§十一 阶段 5），
	// 本包只负责把参数搬过来。此处若顺手建目录，`Load` 就不再是只读操作，
	// 单测与只读环境（如仅做配置校验的容器探针）都会被迫产生副作用。
	config.SqlitePath = strings.TrimSpace(config.SqlitePath)
	if config.SqlitePath == "" {
		config.SqlitePath = DefaultSqlitePath
	}

	// —— 令牌密钥：必填 ——
	// 唯一的必填项。依据 §二 A-5「签发起 12 小时绝对过期」与 §五 3：令牌是无状态的，
	// 其不可伪造性完全依赖签名密钥，空密钥直接击穿 A-6「除登录页与登录接口外，
	// 所有页面与接口强制校验」。
	//
	// 只校验非空、**不校验长度**：需求与分层文档均未规定密钥长度或生成方式，
	// 凭空定一个下限属于脑补规则。要加下限时在此补一条即可。
	config.TokenSecret = strings.TrimSpace(config.TokenSecret)
	if config.TokenSecret == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("加载配置，令牌密钥为空")
		return errors.Errorf("加载配置，令牌密钥（token_secret）不可为空，请在配置文件中填写一个随机字符串")
	}

	// —— 汇率源端点与密钥：只去空白，不校验 ——
	// §四 明确「`fxrate/<源>` 的具体包名随汇率源选定后确定」，即汇率源尚未选型：
	// 是否需要密钥、端点是 URL 还是主机名，全都取决于选定的源。此处若写死校验口径
	// （例如要求必须是 http(s) URL），就是替一个还没做的选型下结论。
	// 由 `fxrate/<源>` 在实现时自行校验——它才知道自己需要什么形状的端点。
	config.FxRateEndpoint = strings.TrimSpace(config.FxRateEndpoint)
	config.FxRateSecret = strings.TrimSpace(config.FxRateSecret)

	// —— 日志级别：空则取默认，非空则交 logrus 解析 ——
	// 用 logrus.ParseLevel 而非自己维护白名单：级别名的取值域归 logrus 所有，
	// 自己抄一份必然与它漂移（例如漏掉 warn/warning 这组别名）。
	config.LogLevel = strings.TrimSpace(config.LogLevel)
	if config.LogLevel == "" {
		config.LogLevel = DefaultLogLevel
	}
	level, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"logLevel": config.LogLevel, "err": err}).Error("加载配置，日志级别非法")
		return errors.Errorf("加载配置，日志级别非法: %s，可选 panic/fatal/error/warn/info/debug/trace: %+v", config.LogLevel, err)
	}
	// 规整为 logrus 的规范名（小写，warn → warning）：让 `base/log` 拿到的一定是
	// 可解析且唯一的形式，无需再做一遍大小写与别名处理。
	config.LogLevel = level.String()

	return nil
}

// maskSecret 把非空密钥替换为固定掩码，空值保持为空（用于区分「填了」与「没填」）。
// 掩码为定长常量，不随原值长度变化——长度本身也是信息，不该泄漏给日志读者。
func maskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	return maskedSecret
}

// —— 明确不属本包的参数（反查清单，防止后人「顺手加个配置项」）——
//
// 依据 §十 约定 8「业务阈值一律写死在对应包」，以下均**不得**出现在 Config 中：
//
//	令牌有效期 12h                → `token`（§六 L3）
//	连续失败 5 次 / 锁定 10 分钟   → `service/auth`（§二 A-8）
//	Argon2 内存 / 迭代 / 并行度    → `base/pwdhash` 包内常量（T36）
//	折算汇率 8 位小数              → `base/decimal`（T39）
//	本位币存储 2 位定点            → `base/decimal`（T35）
//	密码长度 ≥ 10 与字符类别要求   → `rule/passwd`（§二 A-8）
//
// 另有几项形似配置但另有归属，同样不进本包：
//
//	服务名                        → `base/log`（走 go_common/util 的 util.Init）
//	日志切割大小 / 个数 / 天数     → L0 表只列「日志级别」一项，不扩
//	本位币 / 界面语言              → **用户级**配置，落 `User` 实体（§二 I-2 / K-1）
//	语言目录 / 迁移脚本 / 前端产物  → 一律 embed，没有路径可配（约定 4「三 embed 一运行时」）
