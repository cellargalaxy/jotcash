// Package log 提供全仓库日志的统一初始化，并承载 A-3 admin 初始密码的一次性打印。
//
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md` §六 L0 `base/log` 行：
// 「日志；**A-3 的 admin 初始密码一次性打印走此包**——这是全系统唯一把**密码明文
// 写入持久介质**的地方（其余合法出现处见 §九 红线行，均不落盘）」。
//
// 两项职责分文件承载，改动理由互不相干：
//
//	log.go       统一初始化（服务名 + 日志级别），与密码无关
//	password.go  A-3 唯一落盘出口，明文只出现在那一个文件里
//
// 三条硬约束（决定了本包每一个签名的形状）：
//
//  1. **L0 层内零依赖**（§六 L0 表头「仅依赖标准库与第三方库」+ 约定 5）：本包不 import
//     任何 `internal/` 包。故错误用 `github.com/pkg/errors` 而**不用 `base/errs`**、
//     日志级别以 `string` 传入而**不引 `base/config`**（两者都是 L0 兄弟包）。
//     与 `base/config` 的做法一致，理由亦同：初始化发生在启动阶段，没有请求也没有用户，
//     `base/errs` 携带的是面向用户的文案键，套到启动期错误上是错配。
//  2. **纯技术层**（约定 5）：不得放入任何带业务语义（角色、币种、语言、归属）的类型。
//     因此 A-3 的入口收「用户名 + 明文密码」两个 string，**不收 `entity.User`**。
//  3. **不封装日常打点函数**（⚠️ 见下方「为什么不封装」）：全仓库继续直写
//     `logrus.WithContext(ctx).WithFields(...)`，与 `go_common` 及既有兄弟包
//     （`base/config`、`base/idgen`）的写法一致。
//
// # 为什么不封装 Info/Warn/Error
//
// `go_common/util` 的 `paramHook.getCaller`（util/log.go）用**固定的 `skip := 6`**
// 回溯调用栈来填 `caller` 字段。任何一层包装都会让栈深多一帧，使 `caller` 指向
// **包装函数自身**而不是真正的调用点，且该偏移无法从包外修正。实测对照：
//
//	直连 logrus  → caller:".../main.go:27"（正确，指向调用点）
//	经一层封装   → caller:".../main.go:20"（错误，指向封装函数那一行）
//
// 日志定位能力比「统一入口」重要，故本包只做初始化，不做打点。
// 后续任何包若自行封装 logrus，都会重现该问题。
//
// # 调用方
//
//	app             → MustInit(config.LogLevel)  启动装配时初始化一次（约定 8：配置只在 L7 读）
//	service/account → PrintInitPassword(...)     A-1 建 admin 时打印初始密码（§八 A 域）
//
// 另注：§六 L6 `middleware` 行的依赖列含 `base/log`，指的是它挂载「请求日志」中间件——
// 那属 L6 职责，本包不提供；届时可直接用 `util.GinLog`，同样不要再包一层。
package log

import (
	"github.com/cellargalaxy/go_common/util"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultServerName 服务名，即 `go_common/util` 日志落盘路径 `log/<服务名>/log.log`
	// 中的那一段，也是日志里的 `sn` 字段值。
	//
	// 定为包常量而**不做成参数或配置项**：服务名是本仓库的身份，不随部署环境变化。
	// `internal/base/config/config.go` 末尾的反查清单已明确把它排除在部署参数之外
	// （原文「服务名 → `base/log`（走 go_common/util 的 util.Init）」）。
	// 要临时覆盖，用 ServerNameEnvKey 环境变量，见其说明。
	DefaultServerName = "jotcash"

	// ServerNameEnvKey 覆盖服务名的环境变量名，由 `go_common/util` 定义（util/os.go 的
	// `serverNameKey = "server_name"`），此处复述为常量供部署方与文档引用。
	//
	// ⚠️ 它不只是「另一种配置方式」，而是消除随机名日志目录的**唯一**手段：
	// `util` 的包级 `init()` 在 `main` 之前就已按 `GetServerName()` 配好日志，
	// 那时本包的任何代码都还没机会执行，服务名为空于是回退成一个 18 位随机 ID，
	// 于当前工作目录留下 `log/<随机ID>/log.log`。而 `GetEnvString` 第一顺位读环境变量，
	// 因此在**进程启动前**设好它，从第一条日志起就是 `log/jotcash/`。
	//
	// 实测：设 `server_name=jotcash` 启动 → 全程只有 `log/jotcash/`，无随机目录。
	// 不设 → 多一个随机名目录（内容为 init 阶段的少量日志），由 `.gitignore` 的
	// `/log/` 与 `*.log` 覆盖，不入库。
	ServerNameEnvKey = "server_name"

	// DefaultLogFileName 全局日志文件名，落于 `log/<服务名>/` 下。
	// 与 `util.InitDefaultLog` 的默认值保持一致，避免同一份日志出现两种文件名。
	DefaultLogFileName = "log.log"

	// 日志切割参数，取值与 `util.InitDefaultLog`（util/log.go）完全一致，不自行改动：
	// §六 L0 表只把「日志级别」一项列为可配参数，切割策略不在其列，
	// 沿用上游默认值可保证与 `go_common` 其余仓库的运维习惯一致。
	//
	// 语义（lumberjack）：单文件超过 maxSize MB 即切割；最多保留 maxBackups 个旧文件；
	// 旧文件最多保留 maxAge 天。⚠️ 该清理**只作用于已切割的备份文件**，
	// 当前正在写的文件不会被按天数删除（lumberjack 靠文件名里的时间戳识别备份文件）——
	// 这条性质对 A-3 是必要的，见 password.go。
	maxSize    = 1
	maxBackups = 100
	maxAge     = 30
)

// Init 初始化全仓库日志，供 `app` 在启动装配时调用一次。
//
// level 取 logrus 可解析的级别名（panic / fatal / error / warn / info / debug / trace）。
// 传空串按 info 回落，与 `base/config` 的 DefaultLogLevel 一致。
// `base/config.Load` 已把该值规整为 logrus 规范名，故正常路径下此处的解析必然成功；
// 仍做校验是因为本包不能假定调用方一定经过了 `base/config`（约定 8 只规定配置从 L7 注入，
// 未规定注入值必然来自配置文件）。
//
// 级别非法即返回错误，**不静默降级为默认级别**：与 `base/config.checkAndReset` 同一取向——
// 静默降级会造成「配置看似生效实则未生效」，这类问题往往在运行数月后才被发现。
//
// # 为什么必须调两次上游函数
//
// `util.Init(serverName)` 的全部内容只是 `InitOs(serverName)`，即给包级变量
// `defaultServerName` 赋值；而日志**早在 `util` 的包级 `init()` 里就已经配好了**
// （`init()` → `InitDefaultLog()` → `InitLog(GetServerName(), ...)`），
// 那时 `defaultServerName` 尚为空串。所以只调 `util.Init` 会得到一个自相矛盾的状态：
// `util.GetServerName()` 已返回 "jotcash"，但日志仍在往 `log/<随机ID>/` 里写。
//
// 实测（只调 util.Init）：日志字段 `[sn:260906020551358988]`、目录 `log/260906020551358988/`，
// 而 `GetServerName()` == "jotcash" —— 两者不一致即为佐证。
//
// 因此这里在 `util.Init` 之后**再显式调一次 `util.InitLog`**，用确定的服务名与级别
// 重新配置全局 logrus。补调后实测：`[sn:jotcash]`、目录 `log/jotcash/`，
// 与 `.gitignore` 注释所述的 `log/<服务名>/log.log` 形态一致。
//
// 副作用：`util.InitLog` 会重设全局 logrus 的级别、输出与格式化器，并追加一个 hook。
// 故本函数应在启动阶段调用一次即可；重复调用会累加 hook（字段被覆盖为同值，
// 不产生重复字段，但无必要）。
func Init(level string) error {
	logLevel, err := parseLevel(level)
	if err != nil {
		return err
	}

	// 第一步：把服务名写进 util 的包级变量，使 util.GetServerName() 与本包一致。
	// 这也是 prompt 指定参考的那一处用法（survive_monitor 在 model 包的 init 中
	// 调用 util.Init(DefaultServerName)）。
	util.Init(DefaultServerName)

	// 第二步：重新配置日志。缺了这一步，第一步就只是改了个变量而已（见上方说明）。
	util.InitLog(DefaultServerName, DefaultLogFileName, maxSize, maxBackups, maxAge, logLevel)

	logrus.WithFields(logrus.Fields{"serverName": DefaultServerName, "logLevel": logLevel.String()}).Info("初始化日志，完成")
	return nil
}

// MustInit 同 Init，级别非法时直接 panic，供没有错误处理分支的启动路径使用。
//
// 日志是排查一切问题的前提，配错级别就该在启动时立刻暴露，而不是带着一个
// 「看起来配了、实际没生效」的级别跑下去。
func MustInit(level string) {
	if err := Init(level); err != nil {
		panic(err)
	}
}

// parseLevel 解析日志级别，空串回落为 info。
//
// 用 `logrus.ParseLevel` 而非自维护白名单：级别名的取值域归 logrus 所有，
// 自己抄一份必然与它漂移（例如漏掉 warn/warning 这组别名）。
// 与 `base/config.checkAndReset` 里的处理同源，两处口径因此天然一致。
func parseLevel(level string) (logrus.Level, error) {
	if level == "" {
		return logrus.InfoLevel, nil
	}
	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		// 此处不能用 base/errs（L0 兄弟包，会破坏层内零依赖），与 base/config 同样用 pkg/errors
		logrus.WithFields(logrus.Fields{"logLevel": level, "err": err}).Error("初始化日志，日志级别非法")
		return logrus.InfoLevel, errors.Errorf("初始化日志，日志级别非法: %s，可选 panic/fatal/error/warn/info/debug/trace: %+v", level, err)
	}
	return logLevel, nil
}
