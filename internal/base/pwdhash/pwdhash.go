// Package pwdhash 承载 jotcash 的密码哈希与密码学安全随机源（L0 基础层）。
//
// # 两件承载
//
// 依据 doc/decisions/仓库代码包的层级设计与划分.md 的 L0 表 base/pwdhash 行，
// 本包恰承载两件事，且各有一条不可让渡的口径：
//
//   - **Argon2id 哈希与校验**（T36 定案）。盐**独立列存**——终版
//     「记账系统功能与模型」§四 的 User.盐 是单列属性，故 Hash 返回
//     「哈希列 + 盐列」两个值，**不用 PHC 自包含哈希串格式**（那种格式把盐
//     内嵌进哈希串，与单列属性的模型直接冲突）。
//   - **密码学安全随机串**，只供 rule/passwd 采样初始密码（A-2）。
//
// # 参数写死在包内，不进配置
//
// Argon2 的内存/迭代/并行度写死为包内常量并带版本前缀（同表 base/config 行：
// 「业务阈值不进配置……Argon2 参数写死在 base/pwdhash」；configs/jotcash.example.yaml
// 亦已明写这一点）。理由是它们属安全规则而非部署差异——留一个可配置入口，
// 等于留一个能把代价参数调到 0 的入口。
//
// 并行度尤其**不能**取 runtime.NumCPU()：该参数参与哈希计算，实测同一(明文,盐)
// 下 threads=4 与 threads=2 的派生密钥不同。一旦随机器核数浮动，开发机（14 核）
// 写入的哈希在 4 核服务器上将永远校验不通过，且表现为「全部用户密码突然全错」
// 这种极难定位的故障。这是 alexedwards/argon2id 等流行封装库的默认值
// （DefaultParams.Parallelism = runtime.NumCPU()）踩不得的原因之一。
//
// # 为什么不引封装库
//
// 算法本体下沉给 golang.org/x/crypto/argon2（Go 官方扩展库，是该算法在 Go 生态的
// 规范实现，各封装库亦以它为底座），本包不自行实现 Argon2、BLAKE2b 与拒绝采样。
// 但不引 alexedwards/argon2id 这类便捷封装：它们只产出盐内嵌的 PHC 串，与上文
// 「盐独立列存」的模型口径冲突，照用反而要「先拼串再拆盐」，比直接调 IDKey 更绕。
//
// # 版本前缀的作用
//
// 哈希列形态为 "argon2id_v1$<base64 派生密钥>"。带版本是为了让代价参数可升级：
// 升级 = 新增一个版本条目，存量哈希凭自身前缀仍查到当年的参数并正常校验，
// **无需数据迁移**。Verify 因此按「哈希自带的版本」取参数，而不是一律用当前版本。
//
// # 密码红线
//
// §九 红线行规定密码值（明文、哈希、盐）的可见范围，本包是「内存流转白名单」
// 四包之一（负责 Argon2id 哈希与校验）。因此定死：
//
//   - 明文只以入参形式出现在 Hash / Verify 的调用栈内，不写入任何包级变量；
//   - 本包所有日志字段**只记长度、版本与错误原因**，绝不记明文、哈希与盐
//     （有回归用例 TestNoPasswordInLog 抓取日志逐条断言）；
//   - 本包不落盘、不进审计、不读写配置。A-3 的 admin 初始密码打印是唯一落盘
//     明文处，落在 base/log，**不在本包**。
//
// # 依赖边界
//
// 本包属 L0 且**层内零依赖**：不 import 仓库内任何其他包（含 errs / log / idgen）。
// 特别地，按分层方案 §三 问题 2 的修复口径，base/idgen 的随机性**不**取自本包，
// 本包的随机串只供 rule/passwd——两包之间无依赖边，L0 层内零依赖因此名副其实。
//
// 层内零依赖的直接后果是本包**不能** import base/errs，故对外只返回标准 error
// （沿用仓库统一的 github.com/pkg/errors）。「五档」错误映射由 L5 的 service/auth
// （A-4/A-8）与 service/account（A-2/A-3/A-7）完成，它们 import base/errs 无障碍。
package pwdhash

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/argon2"
)

// Version 为当前哈希版本，写入哈希列的前缀。
//
// 带版本前缀是为了让 Argon2 代价参数将来可升级：升级 = 新增一个版本条目，
// 存量哈希凭自身前缀仍能查到当年的参数并正常校验，无需数据迁移。
const Version = "argon2id_v1"

// versionSeparator 分隔哈希列中的版本前缀与 base64 派生密钥。
// 选 '$' 而非 base64 字母表内的字符，保证切分不会误伤密钥本体。
const versionSeparator = "$"

// param 为一个哈希版本对应的 Argon2id 代价参数。
//
// 这些参数写死在包内、不进配置（同文档 §六 base/pwdhash 行、约定 8，
// configs/jotcash.example.yaml 亦明写「Argon2 密码哈希参数一律写死在代码里」）——
// 它们是安全规则而非部署差异，不应存在可被调低的入口。
type param struct {
	time    uint32 // 迭代次数，即对内存的遍历轮数
	memory  uint32 // 内存占用，单位 KiB
	threads uint8  // 并行度（lane 数）
	saltLen uint32 // 盐长度，单位字节
	keyLen  uint32 // 派生密钥长度，单位字节
}

// paramTable 为「版本 → 参数」表。
//
// 取值为 RFC 9106 §4 的第二推荐档（time=3、memory=64 MiB、threads=4），
// 亦即 golang.org/x/crypto/argon2 包注释所引用的「内存受限时的推荐组合」，
// 配 128 位盐与 256 位派生密钥。本机实测单次约 27ms，落在交互式登录的延迟预算内。
//
// threads 必须是固定常量，不能取 runtime.NumCPU()：并行度参与哈希计算，
// 实测同一(明文,盐)下 threads=4 与 threads=2 的派生密钥不同——一旦随机器核数浮动，
// 开发机写入的哈希在核数不同的服务器上将永远校验不通过。
var paramTable = map[string]param{
	Version: {time: 3, memory: 64 * 1024, threads: 4, saltLen: 16, keyLen: 32},
}

// Hash 用 Argon2id 为明文口令生成哈希，返回「哈希列值」与「盐列值」两个值。
//
// 盐独立成列返回，不采用 PHC 自包含哈希串格式：终版 §四 的 User.盐 是单列属性
// （doc/decisions/记账系统功能与模型.md §四 1. User），调用方需把两个返回值分别
// 写入 User.密码哈希 与 User.盐。
//
// 哈希列形态为 "argon2id_v1$<base64 派生密钥>"，盐列形态为 "<base64 盐>"，
// 两者均用 base64 原始编码（不带 '=' 填充）。
//
// 本包不做 A-8 的长度与字符类别校验（那是业务策略，属 rule/passwd），
// 但拒绝空明文——避免把空口令写成一条合法凭据。
func Hash(ctx context.Context, password string) (string, string, error) {
	if password == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("密码哈希，明文为空")
		return "", "", errors.Errorf("密码哈希，明文为空")
	}

	par, ok := paramTable[Version]
	if !ok {
		//参数表与 Version 常量在同一文件内维护，走到这里说明代码被改坏了
		logrus.WithContext(ctx).WithFields(logrus.Fields{"version": Version}).Error("密码哈希，版本参数缺失")
		return "", "", errors.Errorf("密码哈希，版本参数缺失: %s", Version)
	}

	salt, err := RandBytes(ctx, int(par.saltLen))
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("密码哈希，生成盐异常")
		return "", "", errors.Errorf("密码哈希，生成盐异常: %+v", err)
	}

	key := argon2.IDKey([]byte(password), salt, par.time, par.memory, par.threads, par.keyLen)
	hash := Version + versionSeparator + base64.RawStdEncoding.EncodeToString(key)
	return hash, base64.RawStdEncoding.EncodeToString(salt), nil
}

// Verify 校验明文口令与库内存放的「哈希列 + 盐列」是否匹配。
//
// 返回值语义分三类，调用方（service/auth 的 A-4、service/account 的 A-8）必须区别处理：
//   - (true, nil)：匹配；
//   - (false, nil)：不匹配。属正常业务分支，走 A-8 的连续失败计数；
//   - (false, err)：哈希列或盐列本身不可用（空值、无版本前缀、未知版本、base64 非法、
//     长度与该版本参数不符），即存量数据损坏或跨版本不兼容。必须与「密码错误」分开，
//     否则数据损坏会被静默显示成「密码错误」，运维无从发现。
//
// 明文为空时返回 (false, nil) 而不报错：明文来自用户输入，空口令是正常业务分支；
// 且 Hash 拒绝空明文，空明文不可能匹配任何存量哈希，判不匹配是安全的。
func Verify(ctx context.Context, password, hash, salt string) (bool, error) {
	if hash == "" || salt == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"hashEmpty": hash == "", "saltEmpty": salt == ""}).Error("密码校验，哈希或盐为空")
		return false, errors.Errorf("密码校验，哈希或盐为空")
	}

	//先解析哈希列，再判空明文：解析失败属数据损坏，无论明文是否为空都应报错，
	//否则空明文会把「库里那行坏了」这件事掩盖成「密码不对」
	version, keyText, ok := strings.Cut(hash, versionSeparator)
	if !ok {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("密码校验，哈希缺少版本前缀")
		return false, errors.Errorf("密码校验，哈希缺少版本前缀")
	}
	par, ok := paramTable[version]
	if !ok {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"version": version}).Error("密码校验，未知哈希版本")
		return false, errors.Errorf("密码校验，未知哈希版本: %s", version)
	}

	saltData, err := base64.RawStdEncoding.Strict().DecodeString(salt)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("密码校验，盐解码异常")
		return false, errors.Errorf("密码校验，盐解码异常: %+v", err)
	}
	keyData, err := base64.RawStdEncoding.Strict().DecodeString(keyText)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("密码校验，哈希解码异常")
		return false, errors.Errorf("密码校验，哈希解码异常: %+v", err)
	}

	//长度必须与该版本参数一致：长度不符时 argon2.IDKey 仍会算出一个不同长度的结果，
	//恒定时间比较必然返回不匹配，会把「数据损坏」表现成「密码错误」
	if len(saltData) != int(par.saltLen) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"version": version, "len": len(saltData), "expect": par.saltLen}).Error("密码校验，盐长度不符")
		return false, errors.Errorf("密码校验，盐长度不符: %d，期望 %d", len(saltData), par.saltLen)
	}
	if len(keyData) != int(par.keyLen) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"version": version, "len": len(keyData), "expect": par.keyLen}).Error("密码校验，哈希长度不符")
		return false, errors.Errorf("密码校验，哈希长度不符: %d，期望 %d", len(keyData), par.keyLen)
	}

	if password == "" {
		//空明文判不匹配，不做哈希计算也不报错（见函数注释）
		return false, nil
	}

	//按该哈希自带版本的参数重算，而非一律用当前版本参数，参数升级后存量哈希才仍可校验
	otherKey := argon2.IDKey([]byte(password), saltData, par.time, par.memory, par.threads, par.keyLen)
	//必须用恒定时间比较，普通 bytes.Equal 会在首个不同字节处提前返回，泄露前缀匹配长度
	return subtle.ConstantTimeCompare(keyData, otherKey) == 1, nil
}
