package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 页面按 static/js/expense_parse.js 里的相对路径去取识别器，取不到就是上传必报「识别器加载失败」。
// 这里走真路由把 wasm 与胶水脚本取一遍：内嵌没进来、路由挂错、MIME 不对都会在这里暴露
func TestStaticParserWasm(t *testing.T) {
	engine, _ := newTestEngine(t)
	cases := map[string]string{
		"/static/wasm/parser.wasm":    "application/wasm",
		"/static/wasm/wasm_exec.js":   "text/javascript",
		"/static/js/expense_parse.js": "text/javascript",
	}
	for path, contentType := range cases {
		writer := httptest.NewRecorder()
		engine.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path, nil))
		if writer.Code != http.StatusOK || writer.Body.Len() == 0 {
			t.Errorf("%s 应能取到: code=%d len=%d", path, writer.Code, writer.Body.Len())
			continue
		}
		if got := writer.Header().Get("Content-Type"); !strings.HasPrefix(got, contentType) {
			t.Errorf("%s 的 Content-Type 不符: got=%s want=%s", path, got, contentType)
		}
	}
	//wasm 的魔数是 \0asm，取回来的得是真的 wasm，而不是被兜底成了别的页面
	writer := httptest.NewRecorder()
	engine.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/static/wasm/parser.wasm", nil))
	if !strings.HasPrefix(writer.Body.String(), "\x00asm") {
		t.Errorf("取回的不是 wasm")
	}
}
