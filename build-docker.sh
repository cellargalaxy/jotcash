#!/usr/bin/env bash

# 编译环境必须跟Dockerfile最终运行的alpine镜像严格一致（Go版本、CGO_ENABLED、GOOS、libc、GOPROXY），
# 所以不另起一套等价但可能跑偏的编译参数，直接复用Dockerfile自己的builder阶段（docker build --target builder），
# 挂载路径、WORKDIR都是Dockerfile里COPY . .那一份，编译产物再从这个builder镜像里拷出来
repo_root="$(cd "$(dirname "$0")" && pwd)"
output="$repo_root"
tag="jotcash-builder"

# 目标架构默认跟本机一致，跨架构编译（比如本机amd64、服务器是arm64）必须显式传 --arch 覆盖，
# 且需要docker buildx与QEMU支持跨架构模拟
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
    -h|--help)
      echo "Usage: $0 [OPTIONS]"
      echo
      echo "Options:"
      echo "  -o, --output <dir>    Output directory for the compiled binary (default: this script's folder)"
      echo "  -a, --arch <goarch>   Target GOARCH, must match the deploy server, not this machine (default: $goarch)"
      echo "  -h, --help            Show this help message"
      exit 0
      ;;
    *)
      shift
      ;;
  esac
done

mkdir -p "$output"
output="$(cd "$output" && pwd)"

echo
echo "goarch: $goarch"
echo "output: $output/jotcash"
echo "input any key go on, or control+c over"
if [ -t 0 ]; then
  read
fi

# Dockerfile的builder阶段有个"build context根目录已有非空jotcash就直接复用、不重新编译"的分支，
# 这个脚本的目的恰恰是产出一份新的编译结果，构建前得把上一次的产物先挪开，不然一直在用旧的
tmp_binary=""
if [ -s "$repo_root/jotcash" ]; then
  tmp_binary="$repo_root/.jotcash.bak.$$"
  mv "$repo_root/jotcash" "$tmp_binary"
fi
# 构建成功、且产物落地路径就是仓库根目录时，新二进制已经就位，旧备份该丢掉而不是覆盖回去；
# 其余情况（构建失败，或产物落到了别的目录）都要把挪走的旧二进制原样放回来
build_ok=""
cleanup() {
  if [ -z "$tmp_binary" ]; then
    return
  fi
  if [ -n "$build_ok" ] && [ "$output/jotcash" = "$repo_root/jotcash" ]; then
    rm -f "$tmp_binary"
  else
    mv "$tmp_binary" "$repo_root/jotcash" 2>/dev/null
  fi
}
trap cleanup EXIT

echo 'docker build (builder stage)'
docker build --target builder --platform "linux/$goarch" -t "$tag" "$repo_root" || exit 1

echo 'docker cp binary out'
container="jotcash-builder-tmp-$$"
docker create --name "$container" "$tag" >/dev/null || exit 1
docker cp "$container:/jotcash" "$output/jotcash" || exit 1
docker rm "$container" >/dev/null

build_ok=1
chmod +x "$output/jotcash"
echo "all finish: $output/jotcash"
