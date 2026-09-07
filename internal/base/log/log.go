// Package log 承载 jotcash 的日志能力（L0 基础层）。
//
// # 为什么需要本包
//
// 仓库统一使用 github.com/cellargalaxy/go_common/util 的日志实现（底层是
// logrus + lumberjack + nested formatter），日志经 stdout 与
// log/<服务名>/log.log 双写。但该库有三处行为必须由本包收口，否则线上会
// 静默出错——三处均已实测复现，详见各函数注释：
//
//   - util 包自带 init()，在**被 import 的那一刻**就跑 InitDefaultLog()，
//     此时服务名尚未设置，日志会落到 log/<随机18位ID>/log.log 且 sn 字段为
//     该随机值。仅调 util.Init(服务名) **不足以**纠正——它只赋值服务名变量，
//     不重做日志。故 Init 必须「设服务名 + 重做日志」两步齐做。
//   - 重复初始化会让日志字段注入 hook 累积（logrus.AddHook 是 append，无
//     去重），而该 hook 每条日志都要 runtime.Caller 走栈，累积即成倍开销。
//     故重做前先清空 hook，保证 Init 幂等。
//   - 日志级别会门控输出。A-3 的 admin 初始密码若按普通级别打印，运维把
//     log_level 调成 error 就**永久丢失**，而终版 A-3 明写「不设计恢复路径，
//     唯一出路是重新部署」。故 PrintAdminInitPassword 必须绕过级别门控。
//
// # 密码红线
//
// 「记账系统功能与模型」A-3 规定 admin 初始密码按「内存随机生成 → 打印到
// 日志 → 哈希加盐写入」交付。本包的 PrintAdminInitPassword 是**全系统唯一
// 把密码明文写入持久介质**的地方（分层方案 §九 红线行出口 ③），因此：
//
//   - 该函数只做「接收 → 打印」，不存储、不返回、不二次加工明文；
//   - 除该函数外，本包任何代码不接触密码；
//   - log/<服务名>/log.log 因此含明文，其存放与分享范围需受控——这是终版
//     前提 1「假定无人能直接读写数据库」下已显式接受的代价。
//
// # 不提供日志门面
//
// 本包**不**封装 Info/Warn/Error 等函数，全仓库直接用
// logrus.WithContext(ctx).WithFields(...) 打日志，与 go_common 生态一致。
// 原因是 go_common 注入 caller 字段时按**硬编码的栈深度**回溯，任何封装层都
// 会让 caller 指向封装函数自身而非真实调用点（已实测），等于废掉日志里最有用
// 的定位字段。
//
// # 依赖边界
//
// 本包属 L0 且层内零依赖：不 import 仓库内任何其他包（含 base/config 与
// base/errs）。日志级别由 app 以字符串入参注入——按约定 8，base/config 仅被
// app 与 cmd import，本包不得直接读配置。
package log

import (
	"context"
	"strings"
	"sync"

	"github.com/cellargalaxy/go_common/util"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultServerName 是本服务的服务名，决定两件事：日志目录
	// log/jotcash/ 与每条日志的 sn 字段值。
	//
	// 它不进 configs：配置文件只装 5 类部署参数（样例文件已写死这一点），
	// 服务名不在其中；且它是纯技术标识，符合「base/ 不放业务语义」。
	//
	// 环境变量 server_name 优先于本常量（go_common 的既有机制）。**部署时
	// 建议置 server_name=jotcash**：Go 的包初始化顺序不可绕，util 包在被
	// import 时就会打出前几行日志，那一刻本包的 Init 还没机会执行，于是必然
	// 先产生一个 log/<随机ID>/log.log 游离目录。只有在进程启动前用环境变量
	// 把服务名交给它，才能让这几行日志也落到 log/jotcash/ 下。
	DefaultServerName = "jotcash"

	// DefaultLevel 是默认日志级别，与 configs/jotcash.example.yaml 的
	// log_level 默认值一致。
	DefaultLevel = "info"

	// 日志文件的切分与留存参数，沿用 go_common 的 InitDefaultLog 口径，
	// 不自定义——保持与生态内其它服务同一套运维口径。
	// 文件名留空即 log.log，路径由 go_common 拼为 log/<服务名>/log.log。
	logFilename   = ""
	logMaxSizeMB  = 1   // 单文件超过 1MB 即切分
	logMaxBackups = 100 // 最多保留 100 个旧文件
	logMaxAgeDay  = 30  // 旧文件最多保留 30 天
)

// levelLock 保护 PrintAdminInitPassword 里「临时抬级别 → 打印 → 复原」这段
// 读改写序列。logrus 的级别本身是原子变量，但本包这段逻辑跨三次访问，
// 并发下可能被另一次调用把级别复原成中间值。
//
// A-1 系统初始化只会调用一次，正常不存在并发；加锁是为了让该函数在被误用
// （如单测并行）时也不会把全局级别改坏——这是全进程共享的状态，改坏的后果
// 是后续日志整体丢失，代价远大于一把锁。
var levelLock sync.Mutex

// ParseLevel 把配置里的日志级别文本解析为 logrus 级别。
//
// 取值为 logrus 的 7 档：panic / fatal / error / warn / info / debug / trace
// （warning 是 warn 的别名，一并接受）。大小写不敏感。
//
// 两条边界：
//   - 空串（含纯空白）→ 默认级别 info。空值意味着部署者没配，取默认是对的。
//   - 非法值 → **返回错误**，不静默退回默认级别。写错的级别若被静默忽略，
//     部署者会以为调整已生效，而实际日志量与预期不符，排查时无从下手。
//
// 必须先 TrimSpace 再交给 logrus：logrus.ParseLevel 不接受带首尾空白的
// " info "（已实测报错），而这类空白常来自 shell、.env 与容器编排的参数传递。
//
// 供 base/config 在**读配置阶段**提前校验 log_level，把非法级别暴露在配置
// 校验环节，而不是等到日志初始化才失败。
func ParseLevel(level string) (logrus.Level, error) {
	level = strings.TrimSpace(level)
	if level == "" {
		level = DefaultLevel
	}
	return logrus.ParseLevel(level)
}

// Init 按给定级别初始化全局日志：stdout 与 log/<服务名>/log.log 双写。
//
// level 取 ParseLevel 认可的取值，空串取默认级别 info，非法值返回错误——由
// app 据此让启动失败，而不是带着一个没生效的级别继续跑。
//
// 四个步骤缺一不可：
//  1. 解析级别。放在最前，非法值不产生任何副作用（不改级别、不动 hook、
//     不建日志文件），保证失败时全局状态与调用前一致。
//  2. util.Init 落服务名。它只赋值服务名变量，**不重做日志**，故必须有第 4 步。
//  3. 清空 logrus 的 hook。go_common 每次初始化都 AddHook 一个字段注入 hook，
//     而 AddHook 是 append 无去重：util 包 init() 已挂了一个，本函数若直接
//     初始化就变成两个（已实测 1→2）。该 hook 每条日志都要 runtime.Caller
//     走栈找调用点，是日志的主要开销，挂两个等于每行日志走栈两遍。清空后
//     再由第 4 步挂回唯一一个，Init 因此**幂等**，可安全重复调用。
//  4. util.InitLog 重做日志。这一步才真正让服务名生效——日志路径与 sn 字段
//     都是在这里按当前服务名重新确定的。
//
// 已知边界：本函数无法回收 util 包 init() 阶段可能已创建的
// log/<随机ID>/log.log。那几行日志早于本函数执行，属包初始化顺序问题，
// 只能靠部署时置环境变量 server_name 规避（见 DefaultServerName）。
// .gitignore 的 /log/ 与 *.log 已能覆盖正常与游离两种产物。
func Init(level string) error {
	parsed, err := ParseLevel(level)
	if err != nil {
		return err
	}

	util.Init(DefaultServerName)

	// 必须在 InitLog 之前清空：InitLog 内部会 AddHook。
	logrus.StandardLogger().ReplaceHooks(logrus.LevelHooks{})

	util.InitLog(DefaultServerName, logFilename, logMaxSizeMB, logMaxBackups, logMaxAgeDay, parsed)
	return nil
}

// PrintAdminInitPassword 把 A-1 系统初始化时生成的 admin 初始密码打印到日志。
//
// 这是 A-3 交付链「内存随机生成 → 打印到日志 → 哈希加盐写入」的中间一环，
// 也是**全系统唯一把密码明文写入持久介质**的地方（分层方案 §九 红线行出口
// ③）。本函数只打印，不存储、不返回明文。
//
// 为什么要临时抬级别：日志级别会门控输出，log_level=error 时 info 与 warn
// 都被丢弃（已实测）。而终版 A-3 明写 admin 遗忘密码「不设计恢复路径……
// 初始化那一次的日志若丢失，admin 将永久无法登录，唯一出路是重新部署服务
// 从头初始化」。也就是说这一行日志丢了，整个系统就进不去了——它的重要性与
// 部署者配的日志级别无关，不能被级别吞掉。故当前级别低于 warn 时临时抬到
// warn，打完立即 defer 复原，不影响其余日志的级别口径。
//
// 用 warn 而不用 info 或 error：info 会被 error/warn 级别吞掉；error 会让
// 这条「正常交付」混进错误告警与错误统计里，污染监控。
//
// 「仅打印一次」由调用方 A-1 保证：A-1 靠 User.用户名 的唯一索引使重复初始化
// 天然幂等（分层方案 service/account 行），本函数不加 sync.Once——那会静默
// 吞掉第二次打印，把上游的重复调用 bug 藏起来，也让单测无法重复验证。
//
// password 为空时**不打印明文行**，改打一条 error 告警：这必然是上游 bug，
// 静默打印一个空密码会让部署者以为密码就是空的，从此拿不到真实密码。
func PrintAdminInitPassword(ctx context.Context, username, password string) {
	if password == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"username": username}).
			Error("打印管理员初始密码，密码为空")
		return
	}

	levelLock.Lock()
	defer levelLock.Unlock()

	// 级别数值越小越严重（panic=0 < fatal < error < warn < info < debug < trace），
	// 故「低于 warn」意味着 warn 会被丢弃，需要临时抬级别。
	if old := logrus.GetLevel(); old < logrus.WarnLevel {
		logrus.SetLevel(logrus.WarnLevel)
		defer logrus.SetLevel(old)
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{
		"username": username,
		"password": password,
	}).Warn("系统初始化，管理员初始密码仅在此打印一次，请立即登录并修改密码")
}
