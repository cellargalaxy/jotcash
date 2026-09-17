#!/usr/bin/env bash

# 用docker里的golang环境编译，编译产物落到宿主机目录，而不是让宿主机自己装Go工具链去编译。
# 这样编译环境（Go版本、CGO_ENABLED、GOOS、libc）与Dockerfile里最终运行的alpine镜像天然一致，
# 不会再出现"宿主机随手go build出一个动态链接glibc的二进制，扔进alpine跑不起来"的问题
image="golang:1.27-alpine"
goproxy="https://goproxy.cn,direct"
output="$(cd "$(dirname "$0")" && pwd)"

# 目标架构默认跟本机一致，跨架构编译（比如本机amd64、服务器是arm64）必须显式传 --arch 覆盖
case "$(uname -m)" in
  x86_64|amd64) goarch="amd64" ;;
  aarch64|arm64) goarch="arm64" ;;
  *) goarch="amd64" ;;
esac

while [[ $# -gt 0 ]]; do
  case "$1" in
    -o|--output)
      output="$2"
      shift 2
      ;;
    -a|--arch|--goarch)
      goarch="$2"
      shift 2
      ;;
    -i|--image)
      image="$2"
      shift 2
      ;;
    --goproxy)
      goproxy="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [OPTIONS]"
      echo
      echo "Options:"
      echo "  -o, --output <dir>     Output directory for the compiled binary (default: this script's folder)"
      echo "  -a, --arch <goarch>    Target GOARCH, must match the deploy server, not this machine (default: $goarch)"
      echo "  -i, --image <image>    Builder image (default: $image, same as Dockerfile builder stage)"
      echo "      --goproxy <url>    GOPROXY (default: $goproxy, same as Dockerfile)"
      echo "  -h, --help             Show this help message"
      exit 0
      ;;
    *)
      shift
      ;;
  esac
done

repo_root="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$output"
output="$(cd "$output" && pwd)"

echo
echo "image:   $image"
echo "goarch:  $goarch"
echo "output:  $output/jotcash"
echo "input any key go on, or control+c over"
if [ -t 0 ]; then
  read
fi

echo 'docker run build'
# 显式指定GOCACHE/GOMODCACHE到独立挂载卷：容器里用宿主机当前用户身份跑（避免编译产物落地成root属主，
# 后面宿主机自己再删/再编译都要sudo），但docker命名卷首次创建时属主是root，非root身份写不进去，
# 所以先用root把这两个卷的属主对齐成宿主机当前用户，对齐后是持久化的，之后每次构建都不用再做
docker run --rm --user root \
  -v jotcash_gomodcache:/cache/gomod \
  -v jotcash_gocache:/cache/gobuild \
  "$image" \
  chown -R "$(id -u):$(id -g)" /cache/gomod /cache/gobuild

docker run --rm \
  --user "$(id -u):$(id -g)" \
  -v "$repo_root":/src \
  -v "$output":/out \
  -v jotcash_gomodcache:/cache/gomod \
  -v jotcash_gocache:/cache/gobuild \
  -w /src \
  -e GOPROXY="$goproxy" \
  -e GO111MODULE=on \
  -e CGO_ENABLED=0 \
  -e GOOS=linux \
  -e GOARCH="$goarch" \
  -e GOMODCACHE=/cache/gomod \
  -e GOCACHE=/cache/gobuild \
  "$image" \
  sh -c "go mod download && go build -o /out/jotcash"

chmod +x "$output/jotcash"
echo "all finish: $output/jotcash"
