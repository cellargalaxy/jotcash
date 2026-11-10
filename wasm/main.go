//go:build js && wasm

// 浏览器端识别器入口：编译成 static/wasm/parser.wasm，由页面加载后调用 jotcashParse。
// 构建命令见 static/static.go 的 go:generate
package main

import (
	"context"
	"encoding/json"
	"syscall/js"

	"github.com/cellargalaxy/jotcash/service/expense/parser"
	_ "github.com/cellargalaxy/jotcash/service/expense/parsers"
)

// jotcashParse(识别器名, Uint8Array) → {expense: 明细数组JSON} 或 {error: 报错文案}。
// 报错不走 JS 异常：Go 的 panic 会让整个 wasm 实例退出，后面的上传就再也解析不了
func parse(this js.Value, args []js.Value) any {
	if len(args) != 2 || args[0].Type() != js.TypeString || args[1].Type() != js.TypeObject {
		return map[string]any{"error": "解析明细，参数非法"}
	}
	data := make([]byte, args[1].Get("length").Int())
	js.CopyBytesToGo(data, args[1])

	objects, err := parser.Parse(context.Background(), args[0].String(), data)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	text, err := json.Marshal(objects)
	if err != nil {
		return map[string]any{"error": "解析明细，序列化明细异常: " + err.Error()}
	}
	return map[string]any{"expense": string(text)}
}

func names(this js.Value, args []js.Value) any {
	list := parser.Names()
	values := make([]any, 0, len(list))
	for i := range list {
		values = append(values, list[i])
	}
	return values
}

func main() {
	js.Global().Set("jotcashParse", js.FuncOf(parse))
	js.Global().Set("jotcashParsers", js.FuncOf(names))
	//导出的函数要靠 Go 运行时活着才能被调用，main 不能返回
	select {}
}
