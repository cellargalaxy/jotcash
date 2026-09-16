#!/bin/sh
#前端单测入口。跑的是 static/js 下的真代码，不是它的复述。
#用法：sh static_test/run.sh          跑全部
#      sh static_test/run.sh 用例名    只跑名字里带这几个字的用例
#路径写成通配而不是目录：node --test 拿到目录会当模块去加载，拿到通配才按测试文件收集
cd "$(dirname "$0")/.." || exit 1
if [ -n "$1" ]; then
  exec node --test --test-name-pattern="$1" "static_test/*_test.js"
fi
exec node --test "static_test/*_test.js"
