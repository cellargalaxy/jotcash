package tool_test

import (
	"strings"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/tool"
)

// 生成的口令要满足A-6强度，且每次都不一样
func TestGenToken(t *testing.T) {
	ctx := util.GenCtx()

	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := tool.GenToken(ctx, tool.TokenLen)
		if err != nil {
			t.Fatalf("生成口令异常: %+v", err)
		}
		if len([]rune(token)) != tool.TokenLen {
			t.Fatalf("口令长度不符: got=%d want=%d", len([]rune(token)), tool.TokenLen)
		}
		if err = tool.CheckToken(ctx, token); err != nil {
			t.Fatalf("生成的口令不满足强度: token=%s err=%+v", token, err)
		}
		if tokens[token] {
			t.Fatalf("生成的口令重复: %s", token)
		}
		tokens[token] = true
	}

	//长度低于下限时按下限生成，不能生成出一个弱口令
	token, err := tool.GenToken(ctx, 1)
	if err != nil {
		t.Fatalf("生成口令异常: %+v", err)
	}
	if len([]rune(token)) != tool.TokenMinLen {
		t.Errorf("低于下限时应按下限生成: got=%d want=%d", len([]rune(token)), tool.TokenMinLen)
	}
}

// A-6：长度不少于12，且不能是纯数字或纯字母
func TestCheckToken(t *testing.T) {
	ctx := util.GenCtx()

	if err := tool.CheckToken(ctx, "abc123"); err == nil {
		t.Errorf("长度不足应报错")
	}
	if err := tool.CheckToken(ctx, strings.Repeat("a", 20)); err == nil {
		t.Errorf("纯字母应报错")
	}
	if err := tool.CheckToken(ctx, strings.Repeat("1", 20)); err == nil {
		t.Errorf("纯数字应报错")
	}
	if err := tool.CheckToken(ctx, ""); err == nil {
		t.Errorf("空口令应报错")
	}
	if err := tool.CheckToken(ctx, "abcdefghijk1"); err != nil {
		t.Errorf("12位字母加数字应通过: %+v", err)
	}
	//中文/符号算“非数字”，与字母同等看待
	if err := tool.CheckToken(ctx, "口令口令口令口令口令口1"); err != nil {
		t.Errorf("非纯数字应通过: %+v", err)
	}
}
