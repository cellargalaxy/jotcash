package log

// 本文件是**全系统唯一把密码明文写入持久介质的地方**（§六 L0 `base/log` 行原文）。
// 明文只出现在这一个文件里，且本文件不打印任何其他内容——
// 「哪一行代码会把密码写进磁盘」这个问题因此有一个单文件的答案，评审与 grep 都只需看一处。
//
// 依据 `doc/decisions/记账系统功能与模型.md`：
//   - §二 A-3：`admin` 的初始密码**仅在初始化创建的那一次**按「内存随机生成 → 打印到日志 →
//     哈希加盐写入」交付，此后不再生成也不再打印。**`admin` 遗忘密码不设计恢复路径**——
//     初始化那一次的日志若丢失，`admin` 将永久无法登录。
//   - §十 流程 1：`admin` 初始化时**从日志读取**一次性打印的初始密码 → 登录 → 强制改密。
//   - §九 红线行：对外出口仅三类，本文件是其中「③ A-3 admin 初始密码日志打印」，
//     也是三类中**唯一落盘**的一类；且 `密码哈希` 与 `盐` **连三类出口都不得出现**。
//
// 正因为它是 admin 唯一的交付路径、且无任何恢复兜底，这条日志「写不出去」的后果不是
// 少一行日志，而是**整个系统无人能登录、只能重新部署**。本文件的全部设计都围绕这一点。

import (
	"sync"

	"github.com/cellargalaxy/go_common/util"
	"github.com/sirupsen/logrus"
)

const (
	// InitPasswordLogFileName A-3 初始密码的专用日志文件名，落于 `log/<服务名>/` 下。
	//
	// 独立成文件而不混入 log.log，有两个理由：
	//  1. §十 流程 1 要求用户去日志里**找**这一次的密码。混在海量业务日志里找一行，
	//     与打开一个只有一行的文件，可靠性差距很大。
	//  2. log.log 的切割阈值只有 1MB（maxSize），业务日志会很快把它轮转掉；
	//     虽然备份文件仍在，但「初始密码可能在第 37 个备份里」不是可接受的交付方式。
	InitPasswordLogFileName = "init_password.log"

	// initPasswordLogMsg 该条日志的固定文案。
	//
	// 写明「一次性」「不再打印」「请立即改密」，因为这条日志的读者是正在部署系统的人，
	// 而 A-3 的关键约束（只打印这一次、丢了就无法恢复）必须与密码本身出现在同一行。
	initPasswordLogMsg = "A-3 管理员初始密码，仅打印这一次，此后不再生成也不再打印；请立即登录并修改密码，日志请妥善保管"
)

// initPasswordOnce 保证明文至多落盘一次，是 A-3「仅在初始化创建的那一次」的结构性兜底。
//
// 语义上的一次性本已由 `service/account` 的 A-1 保证（§六 L5 `service/account` 行：
// 「用户名唯一由 `store` 唯一索引保证，重复初始化天然幂等」）。此处再加一道，是因为
// 本包是明文落盘的**唯一出口**：即便上层将来出现重试、循环或误调用，明文也只可能写一次。
// 这是准则 6「约束优先用结构表达；能用结构兜底的不写成口头约定」在本包的落点。
var initPasswordOnce sync.Once

// resetInitPasswordOnce 把一次性开关归零，**仅供测试**（经 export_test.go 的
// ResetInitPasswordOnceForTest 暴露给外部测试包）。
//
// 放在生产文件而非测试文件里，是因为它必须与 initPasswordOnce 的声明贴在一起：
// 将来若把一次性语义换成别的实现（如原子标志位），改动者一眼就能看到还有这个复位口要一起改。
// 生产路径上不存在任何调用点——这个开关的意义正是不可复位。
func resetInitPasswordOnce() {
	initPasswordOnce = sync.Once{}
}

// PrintInitPassword 把 A-3 的管理员初始密码一次性打印到独立日志文件，
// 供 `service/account` 在 A-1 系统初始化建 `admin` 时调用。
//
// 入参只有用户名与明文密码两个 string：
//   - **不收 `entity.User`**：一是约定 5 规定 `base/` 不得引入业务语义类型（本包也不 import
//     `entity`，那会破坏 L0 层内零依赖与纯技术层定位）；二是 `User` 上带着 `密码哈希` 与
//     `盐`，而 §九 红线行要求这两项连出口都不许出现——签名里根本没有它们，
//     就不存在「传进来了但忘了过滤」的可能。这与 `dto` 用「字段根本不存在」表达
//     前提 6 是同一手法（准则 6：结构缺席 > 运行时忽略）。
//   - 明文以 string 传入、用后即弃，本函数不留存、不返回、不写入任何其他介质。
//
// 至多生效一次；重复调用只记一条不含明文的警告。
//
// # 为什么不走全局 logrus
//
// 日志级别是部署方可配的（`configs/jotcash.example.yaml` 的 `log_level`）。若这条日志走
// 全局 logrus 的 Info，那么把级别配成 error/warn 时它会被**静默丢弃**——实测在
// ErrorLevel 下 Info 与 Warn 均无任何输出。结合 A-3「日志丢失则 admin 永久无法登录」，
// 那等于把「系统无人能登录」做成了一个可被无意勾中的配置项。
//
// 备选方案「改用 Error 级打印」也被否：能穿透多数级别，但把一次正常的初始化事件记为
// 错误会污染「有无 ERROR」这一最常用的巡检信号，且 panic/fatal 级仍会吞掉它。
//
// 故改用 `util.CreateLog` 建**独立日志器**：级别在包内写死为 Info，部署方无法调低；
// 输出走自己的文件与自己的 lumberjack 句柄，与全局日志完全解耦。
// 实测：全局级别为 error 时，log.log 中无明文，init_password.log 中正常写入。
//
// 另经核实（lumberjack v2.0.0 源码 oldLogFiles）：maxAge/maxBackups 的清理只作用于
// **带时间戳的已切割备份文件**，当前正在写的 init_password.log 不会被按天数删除——
// 初始密码日志不会在 30 天后自动消失。但备份文件仍会被清理，且磁盘丢失等情形不在
// 本包能力范围，留存责任仍在部署方（§二 A-3 已显式接受该风险）。
func PrintInitPassword(username, password string) {
	// 空明文视为调用方缺陷：记录一条**不含明文**的错误后返回，不落任何密码。
	//
	// 不 panic 的理由：A-1 初始化此刻正处在建账户的流程中（§六 L5 `service/account` 行），
	// 崩掉会让初始化半途而废、留下一个不完整的库；而 A-2 已规定初始密码由
	// `rule/passwd` 随机生成并符合 A-8 策略（长度 ≥ 10），空值在正常路径上不会出现。
	// 也不消耗 initPasswordOnce——否则一次误调用会把真正那次打印永久堵死。
	if password == "" {
		logrus.WithFields(logrus.Fields{"username": username}).Error("打印管理员初始密码，密码为空，未打印")
		return
	}

	printed := false
	initPasswordOnce.Do(func() {
		printed = true

		// 独立日志器：级别写死 Info，不受全局级别影响（理由见上方函数注释）。
		// 与全局日志同处 `log/<服务名>/` 目录下，运维只需关注一个目录。
		passwordLog := util.CreateLog(DefaultServerName, InitPasswordLogFileName, maxSize, maxBackups, maxAge, logrus.InfoLevel)

		// 明文进入日志的唯一一行。
		// 字段名 password 直白不做混淆：这条日志的存在本身就是设计意图，
		// 遮掩字段名只会让部署者找不到它，反而违背 §十 流程 1。
		passwordLog.WithFields(logrus.Fields{
			"username": username,
			"password": password,
		}).Info(initPasswordLogMsg)
	})

	if !printed {
		// 重复调用：只记不含明文的警告。
		// 记而不静默，是因为这意味着上层可能绕过了 A-1 的幂等前提，值得在日志里留痕。
		logrus.WithFields(logrus.Fields{"username": username}).Warn("打印管理员初始密码，已打印过，本次忽略")
	}
}
