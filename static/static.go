package static

import "embed"

// 识别器编译成 wasm 放进 wasm/ 一起内嵌，这样预编译好的二进制直接上传到服务器也带着它，服务器上不用再编译。
// wasm/ 是构建产物不进仓库：本地开发、跑测试、Dockerfile 的 builder 阶段都先执行 go generate ./static
//go:generate sh -c "mkdir -p wasm && GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w' -o wasm/parser.wasm ../wasm && cp \"$(go env GOROOT)/lib/wasm/wasm_exec.js\" wasm/wasm_exec.js"

//go:embed *
var StaticFile embed.FS
