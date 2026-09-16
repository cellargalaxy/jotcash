#!/usr/bin/env bash

# 解析命令行参数（支持传参指定 server_name / listen_port / resource / timezone）
while [[ $# -gt 0 ]]; do
  case "$1" in
    -s|--server_name|--server-name)
      server_name="$2"
      shift 2
      ;;
    -p|--listen_port|--listen-port|--port)
      listen_port="$2"
      shift 2
      ;;
    -r|--resource)
      resource="$2"
      shift 2
      ;;
    -t|-z|--tz|--timezone)
      timezone="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [OPTIONS]"
      echo
      echo "Options:"
      echo "  -s, --server_name <name>      Server/container name (default: jotcash)"
      echo "  -p, --listen_port <host:port> Listen port (default: 127.0.0.1:7678)"
      echo "  -r, --resource <path|volume>  Resource mount path or volume name (default: <server_name>_resource)"
      echo "  -t, --timezone <tz>           Timezone for container (default: Asia/Shanghai or host timezone)"
      echo "  -h, --help                    Show this help message"
      exit 0
      ;;
    *)
      shift
      ;;
  esac
done

if [ -z "$server_name" ]; then
  read -p "please enter server_name(default:jotcash):" server_name
fi
if [ -z "$server_name" ]; then
  server_name="jotcash"
fi

if [ -z "$listen_port" ]; then
  read -p "please enter listen port(default:'127.0.0.1:7678'):" listen_port
fi
if [ -z "$listen_port" ]; then
  listen_port="127.0.0.1:7678"
fi

if [ -z "$resource" ]; then
  read -p "please enter resource(default:volume):" resource
fi
if [ -z "$resource" ]; then
  resource=$server_name'_resource'
fi

# 探测宿主机时区作为缺省推荐值
default_timezone="Asia/Shanghai"
if [ -f /etc/timezone ]; then
  host_tz=$(cat /etc/timezone | tr -d ' \n\r')
  [ -n "$host_tz" ] && default_timezone="$host_tz"
elif [ -h /etc/localtime ]; then
  host_tz=$(readlink /etc/localtime | sed 's#.*/zoneinfo/##')
  [ -n "$host_tz" ] && default_timezone="$host_tz"
fi

if [ -z "$timezone" ]; then
  read -p "please enter timezone(default:'$default_timezone'):" timezone
fi
if [ -z "$timezone" ]; then
  timezone="$default_timezone"
fi

# 若 resource 为相对路径，转换为绝对路径以便 Docker 正确识别为 bind mount
if [[ "$resource" == ./* || "$resource" == ../* ]]; then
  resource=$(cd "$resource" 2>/dev/null && pwd || echo "$PWD/$resource")
fi

echo
echo "server_name: $server_name"
echo "listen_port: $listen_port"
echo "resource:    $resource"
echo "timezone:    $timezone"
echo "input any key go on, or control+c over"
if [ -t 0 ]; then
  read
fi

echo 'create volume'
docker volume create log

# 若 resource 不是路径（无斜杠），作为 Docker volume 创建
if [[ "$resource" != /* && "$resource" != .* ]]; then
  docker volume create "$resource"
else
  # 若 resource 为宿主机目录，提前创建目录
  mkdir -p "$resource"
fi

echo 'stop container'
docker stop $server_name
echo 'remove container'
docker rm $server_name
echo 'remove image'
docker rmi $server_name
echo 'docker build'
docker build --build-arg TZ="$timezone" -t $server_name .
echo 'docker run'
docker run -d \
  --restart=always \
  --name $server_name \
  -v log:/log \
  -v "$resource":/resource \
  -p $listen_port:7678 \
  -e server_name=$server_name \
  -e TZ="$timezone" \
  $server_name

echo 'all finish'
