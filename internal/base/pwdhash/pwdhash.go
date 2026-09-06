// Package pwdhash 提供 Argon2id 密码哈希与校验，以及密码学安全随机源。
//
// 依据 `doc/decisions/仓库代码包的层级设计与划分.md` §六 L0 `base/pwdhash` 行（T36 定案）：
// 「**Argon2id** 哈希与校验（T36）+ 密码学安全随机源。三条口径：**盐独立列存**
// （终版 §四 `User.盐` 是单列属性，不用自包含哈希串格式）；Argon2 参数（内存/迭代/
// 并行度）**写死在包内常量并带版本前缀**，不进配置；对外另出的「安全随机串」
// **只供 `rule/passwd` 采样初始密码**（A-2）」。
//
// 四条硬约束（决定了本包每一个签名的形状）：
//
//  1. **L0 层内零依赖**（§六 L0 表头 + 约定 5）：本包不 import 任何 base 兄弟包，
//     因此**不返回 `base/errs` 的五档错误**，只返回普通 error；映射到五档是调用方
//     （`service/auth` / `service/account`）的事。反向亦然：`base/idgen` 不依赖本包
//     取随机性（§三 问题 2 的修法），本包的随机源只服务 `rule/passwd`（A-2）。
//  2. **盐独立列存**：不采用 PHC 自包含串格式（`$argon2id$v=19$m=..,t=..,p=..$salt$hash`），
//     否则 `User.盐` 这一列就没有意义了。盐一律作为独立返回值给出、由调用方单独落库。
//  3. **参数不可配**（约定 8：业务阈值一律写死在对应包，含 Argon2 参数）：
//     参数既不出现在 `base/config`，也不出现在任何导出函数的入参——
//     **没有任何入口能把安全参数调低**。
//  4. **密码值红线**（§九 红线行 + 终版 8.4 边界②）：本包属「内存流转白名单四包」之一
//     （`rule/passwd` / `service/account` / `service/auth` / 本包），但**不属三类出口**
//     中的任何一类，故本包的日志**只打长度与版本号，绝不打明文 / 哈希 / 盐**。
//     A-3 的 admin 初始密码日志打印是唯一落盘出口，那在 `base/log`，不在这里。
//
// 调用方（§九 红线行「内存流转白名单」）：
//
//	service/account → Hash / Verify   A-2/A-3/A-7 写密码、A-8 自助改密校验旧密码
//	service/auth    → Verify          A-4 登录
//	rule/passwd     → RandInt/RandText A-8 策略下采样初始密码（A-2）
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

// 哈希串的版本前缀。版本号落在**哈希串上**（而非仅是 Go 常量命名），
// 是「参数带版本前缀」这条口径唯一可运行的读法：
// 若版本号只存在于源码里，运行时就无从得知库里某条哈希是用哪组参数算出来的，
// 将来一旦调高 Argon2 参数（安全参数随硬件升级本就该调高），旧哈希无法被识别、
// 重算必然对不上，**全部存量用户登录失败**；叠加终版 A-3「admin 遗忘密码不设计
// 恢复路径」，后果是整个系统永久无法登录。
//
// 带前缀后新旧哈希可共存：加一组参数 = paramsByVersion 加一项 + 换 currentVersion 一行，
// Verify 按串上的前缀查表取参数，存量哈希继续用它自己那组参数校验。
//
// 这与「不用自包含哈希串格式」不冲突：那条反对的是把**盐**编码进哈希串
// （理由是会让 `User.盐` 这一列失去意义），而版本前缀不含盐、也不含参数值本身，
// `User.盐` 仍是唯一的盐来源，独立列存不受影响。
const (
	// VersionArgon2idV1 第 1 版参数的版本标识，取值见 paramsByVersion。
	VersionArgon2idV1 = "argon2id-v1"

	// currentVersion 当前用于**新生成**哈希的版本；校验一律按串上的前缀走，不受此值影响。
	currentVersion = VersionArgon2idV1

	// versionSeparator 版本前缀与载荷的分隔符。取 '$' 是因为 base64 标准字母表
	// （A-Za-z0-9+/=）不含它，切分无歧义。
	versionSeparator = "$"
)

// argon2Params 一组 Argon2id 代价参数。字段全部小写不导出，
// 保证参数只能由本包的版本表给出，外部无法构造、也无法传入（约束 3）。
type argon2Params struct {
	time    uint32 // 迭代次数（对内存的遍历遍数）
	memory  uint32 // 内存代价，单位 KiB
	threads uint8  // 并行度
	saltLen int    // 盐长度（字节）
	keyLen  uint32 // 输出哈希长度（字节）
}

// paramsByVersion 版本 → 参数的对照表，是本包**唯一**的参数来源。
//
// v1 取 RFC 9106 §4 的**第二组**推荐值（time=3、memory=64 MiB、threads=4），
// 即 `golang.org/x/crypto/argon2` 包注释所述「If much less memory is available,
// the second recommended option is time=3, memory=64*1024 KiB (64 MiB), and threads=4」。
// 不取第一组（time=1、memory=2 GiB）的理由：终版定位为个人自建单二进制系统
// （SQLite 单文件、`File.文件内容` 与业务表同库），不具备为一次登录申请 2 GiB 的条件。
// 盐 16 字节、哈希 32 字节为 OWASP 密码存储建议的下限。
//
// ⚠️ threads 必须是**编译期常量**，绝不能取 runtime.NumCPU()：
// threads 参与哈希计算——x/crypto/argon2 的 deriveKey 把它传进 initHash 生成 h0，
// 并用它调整实际内存（memory / (syncPoints * threads) * (syncPoints * threads)）。
// 同一密码 + 同一盐在 threads=4 与 threads=8 下得到的哈希**不同**，
// 因此「并行度自适应 CPU」这种常见写法会导致：换一台核数不同的机器部署，
// 全部用户的密码校验立刻全部失败，且 A-3 已明确 admin 无恢复路径。
var paramsByVersion = map[string]argon2Params{
	VersionArgon2idV1: {
		time:    3,
		memory:  64 * 1024,
		threads: 4,
		saltLen: 16,
		keyLen:  32,
	},
}

// Hash 为明文口令生成 Argon2id 哈希与随机盐，供 A-2/A-3/A-7/A-8 写密码时落库。
//
// 返回的 hash 带版本前缀（形如 `$argon2id-v1$<base64>`），salt 是**独立**的 base64 文本
// ——二者分别落 `User.密码哈希` 与 `User.盐` 两列（约束 2）。同一明文每次调用都会
// 生成新盐，因此两次返回的 hash 与 salt 均不同，库里不会出现「相同密码相同哈希」。
//
// 明文为空时返回错误：A-8 的密码策略（长度 ≥ 10、字母/数字/符号至少两类）由
// `rule/passwd` 校验，本包不重复实现策略；但此处兜一道底，防止调用方漏校验时
// 把空口令哈希进库——那等于给账户开了一道空密码后门。
func Hash(ctx context.Context, plaintext string) (string, string, error) {
	if plaintext == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{}).Error("密码哈希，明文为空")
		return "", "", errors.Errorf("密码哈希，明文为空")
	}

	params, ok := paramsByVersion[currentVersion]
	if !ok {
		// 不可达：currentVersion 恒为版本表中的一项。留作后人改错版本号时的即时暴露点。
		logrus.WithContext(ctx).WithFields(logrus.Fields{"version": currentVersion}).Error("密码哈希，未知版本参数")
		return "", "", errors.Errorf("密码哈希，未知版本参数: %s", currentVersion)
	}

	saltRaw, err := RandBytes(ctx, params.saltLen)
	if err != nil {
		// RandBytes 已打日志，此处只补业务语境，不重复打
		return "", "", errors.Errorf("密码哈希，生成盐异常: %+v", err)
	}

	key := argon2.IDKey([]byte(plaintext), saltRaw, params.time, params.memory, params.threads, params.keyLen)
	return encodeHash(currentVersion, key), encodeSalt(saltRaw), nil
}

// Verify 校验明文口令是否与库中的哈希 + 盐匹配，供 A-4 登录与 A-8 改密校验旧密码。
//
// 两个返回值的分工是刻意的，调用方无需解析错误内容即可分流：
//
//	(false, nil) —— 密码不对。属「用户输入错误」，A-8 据此累计连续失败次数
//	               （5 次锁定 10 分钟），调用方应映射为 base/errs 的用户输入错误档。
//	(false, err) —— 哈希串或盐本身有问题（格式非法、版本未知、长度不符）。
//	               属「系统错误」，**不得计入 A-8 失败次数**——否则用户会因为
//	               一条坏数据被反复锁定，且从提示里永远查不出原因。
//
// 明文为空时直接返回 (false, nil)：Hash 拒绝空明文，故库中不存在与空明文对应的哈希，
// 结论必然为不匹配；提前返回还省掉一次 64 MiB 的 Argon2 开销。
//
// 比较用 crypto/subtle.ConstantTimeCompare，不用 bytes.Equal——后者会在首个不同字节处
// 提前返回，逐字节试探即可把比较耗时变成信息通道。
func Verify(ctx context.Context, plaintext, hash, salt string) (bool, error) {
	if plaintext == "" {
		return false, nil
	}

	version, params, expected, err := decodeHash(ctx, hash)
	if err != nil {
		return false, err
	}
	saltRaw, err := decodeSalt(ctx, salt, params)
	if err != nil {
		return false, err
	}

	actual := argon2.IDKey([]byte(plaintext), saltRaw, params.time, params.memory, params.threads, params.keyLen)
	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"version": version}).Info("密码校验，不匹配")
		return false, nil
	}
	return true, nil
}

// encodeHash 把派生出的哈希编码为带版本前缀的存储形态：`$<版本>$<base64(哈希)>`。
func encodeHash(version string, key []byte) string {
	return versionSeparator + version + versionSeparator + base64.StdEncoding.EncodeToString(key)
}

// encodeSalt 把盐编码为存储形态。盐**不带**版本前缀：它只是一串随机字节，
// 参数版本已由哈希串携带，两处都带反而会出现「两个版本号不一致」的无解状态。
func encodeSalt(saltRaw []byte) string {
	return base64.StdEncoding.EncodeToString(saltRaw)
}

// decodeHash 解析存储形态的哈希串，返回版本、该版本的参数与哈希原文。
//
// 一切解析失败（含版本未知）一律返回 error，而**不是** (false, nil)：
// 若把「本版本代码不认识的前缀」当成密码错误，`service/auth` 会计入 A-8 失败次数、
// 5 次后锁定 10 分钟，用户被锁死却查不出原因。回退部署与数据被改写都会触发这一幕。
func decodeHash(ctx context.Context, hash string) (string, argon2Params, []byte, error) {
	var empty argon2Params
	// 形如 "$argon2id-v1$<base64>"，按 '$' 切分后为 ["", "argon2id-v1", "<base64>"]
	segments := strings.Split(hash, versionSeparator)
	if len(segments) != 3 || segments[0] != "" || segments[1] == "" || segments[2] == "" {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"hashLen": len(hash)}).Error("密码校验，哈希串格式非法")
		return "", empty, nil, errors.Errorf("密码校验，哈希串格式非法")
	}

	version := segments[1]
	params, ok := paramsByVersion[version]
	if !ok {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"version": version}).Error("密码校验，哈希串版本未知")
		return "", empty, nil, errors.Errorf("密码校验，哈希串版本未知: %s", version)
	}

	key, err := base64.StdEncoding.DecodeString(segments[2])
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"version": version, "err": err}).Error("密码校验，哈希串Base64解码异常")
		return "", empty, nil, errors.Errorf("密码校验，哈希串Base64解码异常: %+v", err)
	}
	if len(key) != int(params.keyLen) {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"version": version, "keyLen": len(key), "expectLen": params.keyLen}).Error("密码校验，哈希长度不符")
		return "", empty, nil, errors.Errorf("密码校验，哈希长度不符: %d，期望 %d", len(key), params.keyLen)
	}
	return version, params, key, nil
}

// decodeSalt 解析存储形态的盐，并校验其长度与该版本参数一致。
// 长度校验不可省：盐被截断后 Argon2 仍能算出结果，校验会静默恒不匹配，
// 表现为「密码明明没错却登不进去」，且会被 A-8 计成失败次数。
func decodeSalt(ctx context.Context, salt string, params argon2Params) ([]byte, error) {
	saltRaw, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"err": err}).Error("密码校验，盐Base64解码异常")
		return nil, errors.Errorf("密码校验，盐Base64解码异常: %+v", err)
	}
	if len(saltRaw) != params.saltLen {
		logrus.WithContext(ctx).WithFields(logrus.Fields{"saltLen": len(saltRaw), "expectLen": params.saltLen}).Error("密码校验，盐长度不符")
		return nil, errors.Errorf("密码校验，盐长度不符: %d，期望 %d", len(saltRaw), params.saltLen)
	}
	return saltRaw, nil
}
