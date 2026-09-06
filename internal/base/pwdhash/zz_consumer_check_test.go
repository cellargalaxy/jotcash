package pwdhash_test

// 外部消费者验证：以**黑盒**方式（只用导出 API，包名为 pwdhash_test）模拟
// service/auth 的 A-4 登录与 A-8 失败计数、service/account 的 A-8 改密、
// rule/passwd 的 A-2 初始密码采样，确认包对外契约真的可用。
//
// 另有一条独立校验路径：**绕过 Verify**，直接用 argon2 原语按文档声明的参数回算，
// 与 Hash 的产物逐字节比对——同一实现自己校验自己必然自洽，
// 只有独立回算才能证明产物确实是「用声明参数算出的 Argon2id」。

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/cellargalaxy/jotcash/internal/base/pwdhash"
	"golang.org/x/crypto/argon2"
)

// TestHashIsGenuineArgon2id 不调用 Verify：自行按 RFC 9106 §4 第二组参数回算，
// 与 Hash 产物的载荷逐字节比对。若实现悄悄换了参数或算法，本用例即失败。
func TestHashIsGenuineArgon2id(t *testing.T) {
	ctx := context.Background()
	const plaintext = "Jotcash#2026"
	hash, salt, err := pwdhash.Hash(ctx, plaintext)
	if err != nil {
		t.Fatalf("生成哈希异常: %+v", err)
	}

	prefix := "$" + pwdhash.VersionArgon2idV1 + "$"
	if !strings.HasPrefix(hash, prefix) {
		t.Fatalf("前缀不符: %s", hash)
	}
	gotKey, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(hash, prefix))
	if err != nil {
		t.Fatalf("载荷解码异常: %+v", err)
	}
	saltRaw, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		t.Fatalf("盐解码异常: %+v", err)
	}

	// 独立回算：参数硬编码为文档声明值，不从被测包读取
	wantKey := argon2.IDKey([]byte(plaintext), saltRaw, 3, 64*1024, 4, 32)
	if string(gotKey) != string(wantKey) {
		t.Fatalf("产物与独立回算不一致——参数或算法与声明不符\n实际 %x\n期望 %x", gotKey, wantKey)
	}
	t.Logf("独立回算一致：Argon2id(t=3, m=64MiB, p=4, salt=%d字节, key=%d字节)", len(saltRaw), len(gotKey))
}

// TestAuthFlowA4A8 模拟 service/auth 的 A-4 登录 + A-8 失败计数分流：
// 密码错误必须走 (false, nil) 并计数；坏数据必须走 error 且**不计数**。
func TestAuthFlowA4A8(t *testing.T) {
	ctx := context.Background()
	const correct = "Jotcash#2026"

	// A-2/A-7：建账户时生成哈希与盐，分别落 User.密码哈希 / User.盐 两列
	dbHash, dbSalt, err := pwdhash.Hash(ctx, correct)
	if err != nil {
		t.Fatalf("建账户生成哈希异常: %+v", err)
	}

	failCount := 0 // A-8 连续失败次数
	login := func(input, hash, salt string) (bool, error) {
		ok, err := pwdhash.Verify(ctx, input, hash, salt)
		if err != nil {
			return false, err // 系统错误，不计入失败次数
		}
		if !ok {
			failCount++
			return false, nil
		}
		failCount = 0 // 成功即清零
		return true, nil
	}

	// 连续 4 次错密码 → 计数到 4，未达 5 次锁定阈值
	for i := 0; i < 4; i++ {
		if ok, err := login("WrongPass#1", dbHash, dbSalt); ok || err != nil {
			t.Fatalf("第 %d 次错密码登录: ok=%v err=%+v", i+1, ok, err)
		}
	}
	if failCount != 4 {
		t.Fatalf("失败次数 = %d，期望 4", failCount)
	}

	// 一条坏数据（版本未知）：必须报 error 且不推高计数
	if ok, err := login(correct, "$argon2id-v9$AAAA", dbSalt); ok || err == nil {
		t.Fatalf("坏哈希应报错: ok=%v err=%v", ok, err)
	}
	if failCount != 4 {
		t.Fatalf("坏数据竟推高了失败次数: %d（会让用户被无故锁定）", failCount)
	}

	// 正确密码 → 通过并清零
	if ok, err := login(correct, dbHash, dbSalt); !ok || err != nil {
		t.Fatalf("正确密码登录失败: ok=%v err=%+v", ok, err)
	}
	if failCount != 0 {
		t.Fatalf("登录成功后失败次数未清零: %d", failCount)
	}

	// A-8 改密：新密码生成新哈希，旧密码立即失效
	const newPass = "Jotcash#2027!"
	newHash, newSalt, err := pwdhash.Hash(ctx, newPass)
	if err != nil {
		t.Fatalf("改密生成哈希异常: %+v", err)
	}
	if ok, _ := login(correct, newHash, newSalt); ok {
		t.Fatalf("改密后旧密码竟仍可登录")
	}
	if ok, err := login(newPass, newHash, newSalt); !ok || err != nil {
		t.Fatalf("改密后新密码登录失败: ok=%v err=%+v", ok, err)
	}
}

// TestRandForPasswdPolicy 模拟 rule/passwd 用本包随机源采样 A-2 初始密码：
// 长度 ≥ 10、字母/数字/符号至少两类（A-8），且两次生成不同。
func TestRandForPasswdPolicy(t *testing.T) {
	ctx := context.Background()
	// 字符集由调用方持有（业务语义不进 base/）
	const charset = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%^&*"
	gen := func() string {
		var sb strings.Builder
		for i := 0; i < 16; i++ {
			idx, err := pwdhash.RandInt(ctx, len(charset))
			if err != nil {
				t.Fatalf("采样异常: %+v", err)
			}
			sb.WriteByte(charset[idx])
		}
		return sb.String()
	}
	first, second := gen(), gen()
	if len(first) < 10 {
		t.Fatalf("初始密码长度不足: %d", len(first))
	}
	if first == second {
		t.Fatalf("两次生成的初始密码相同，随机源可疑")
	}
	// 生成的初始密码必须能正常哈希与校验
	hash, salt, err := pwdhash.Hash(ctx, first)
	if err != nil {
		t.Fatalf("初始密码哈希异常: %+v", err)
	}
	if ok, err := pwdhash.Verify(ctx, first, hash, salt); !ok || err != nil {
		t.Fatalf("初始密码校验失败: ok=%v err=%+v", ok, err)
	}
	// RandText 亦可用
	if text := pwdhash.RandText(); len(text) != 26 {
		t.Fatalf("RandText 长度 = %d", len(text))
	}
}
