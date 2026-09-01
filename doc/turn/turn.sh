#!/bin/bash

# 1. 环境准备
YEARMONTH=$(date +"%y%m")
TIMESTAMP=$(date +"%y%m%d-%H%M%S")
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TURN_DIR="${SCRIPT_DIR}"
TEMPLATE="${TURN_DIR}/000000-000000-prompt.md"

# 2. 自动计算文件夹逻辑
# 初始 n 为 0
N=0
while true; do
    # 格式化文件夹名称，例如 2603p0, 2603p1...
    FOLDER_NAME="${YEARMONTH}p${N}"
    TARGET_DIR="${TURN_DIR}/${FOLDER_NAME}"

    # 如果文件夹不存在，说明可以直接使用这个 n
    if [ ! -d "${TARGET_DIR}" ]; then
        mkdir -p "${TARGET_DIR}"
        break
    fi

    # 如果文件夹存在，统计其中 .md 文件的数量
    # 使用 find 统计文件避免文件名包含空格导致的错误，且不计入子目录
    FILE_COUNT=$(find "${TARGET_DIR}" -maxdepth 1 -type f -name "*.md" | wc -l)

    # 判断数量是否达到阈值（20个）
    if [ "$FILE_COUNT" -lt 20 ]; then
        # 还没满，就用它了
        break
    else
        # 满了，尝试下一个 n
        N=$((N + 1))
    fi
done

# 3. 设置最终文件路径
NEW_PROMPT="${TARGET_DIR}/${TIMESTAMP}-prompt.md"
REL_ANSWER="${TIMESTAMP}-answer.md"

# 4. 执行生成操作
if [ -f "${TEMPLATE}" ]; then
    # 确保目标目录存在（虽然上面 break 前已创建，这里做双保险）
    mkdir -p "${TARGET_DIR}"

    # 复制并替换
    sed "s|YYMMDD-HHMMSS-answer.md|${REL_ANSWER}|g" "${TEMPLATE}" > "${NEW_PROMPT}"
    echo "已生成: ${NEW_PROMPT} (当前目录文件数: $((FILE_COUNT + 1)))"
else
    echo "错误: 找不到模板文件 ${TEMPLATE}"
    exit 1
fi

