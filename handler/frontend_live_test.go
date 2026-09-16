package handler_test

import (
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
)

// 前端的真实模式在它自己的单测里打的是录音机，证明得了请求发成什么样，证明不了后端认不认。
// 这里起一个真后端，让 static/js/api.js 原样去打它，两侧的契约一次对齐。
func TestFrontendLive(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("没有 node，跳过前后端联调")
	}
	_, self, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(self))

	engine, clientToken := newTestEngine(t)
	serverToken := config.GetConfig(util.GenCtx()).ServerToken
	server := httptest.NewServer(engine)
	defer server.Close()

	out, err := exec.Command(node, filepath.Join(root, "static_test", "live_drive.mjs"), server.URL, serverToken, clientToken, root).CombinedOutput()
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("前后端联调异常: %+v", err)
	}
}
