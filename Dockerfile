FROM golang:1.27-alpine AS builder
ENV GOPROXY="https://goproxy.cn,direct"
ENV GO111MODULE=on
WORKDIR /
COPY . .
RUN if [ -s jotcash ]; then \
        echo "Binary already exists, skipping build"; \
        chmod +x jotcash; \
    else \
        echo "Binary not found, building from source"; \
        go mod download && \
        CGO_ENABLED=0 GOOS=linux go build -o /jotcash; \
    fi

FROM python:3.12-slim-bookworm
COPY --from=builder /jotcash /jotcash
RUN chmod 755 /jotcash
ARG TZ="Asia/Shanghai"
ENV TZ=${TZ}
COPY service/expense/unionpay_screenshot/requirements.txt service/expense/unionpay_screenshot/ocr.py service/expense/unionpay_screenshot/testdata/synthetic.png /tmp/ocr/
RUN (sed -i 's/deb.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || \
     sed -i 's/deb.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list 2>/dev/null || true) && \
    apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata wget libgl1 libglib2.0-0 && \
    ln -snf /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone && \
    (pip install --no-cache-dir -r /tmp/ocr/requirements.txt -i https://pypi.tuna.tsinghua.edu.cn/simple || \
     pip install --no-cache-dir -r /tmp/ocr/requirements.txt) && \
    ocr_version=$(sed -n 's/^rapidocr-onnxruntime==//p' /tmp/ocr/requirements.txt) && \
    card_lines=$(python3 /tmp/ocr/ocr.py "$ocr_version" < /tmp/ocr/synthetic.png | grep -oE '\[[0-9]{4}\](］)?[[:space:]]*·[[:space:]]*[0-9]{1,2}:[0-9]{2}' | wc -l) && \
    if [ "$card_lines" -ne 16 ]; then echo "OCR自检失败：合成截图应识别出16个卡号行，实际${card_lines}个"; exit 1; fi && \
    rm -rf /tmp/ocr && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

ARG UID=1000
ARG GID=1000

RUN if getent group ${GID} >/dev/null 2>&1; then \
        group_name=$(getent group ${GID} | cut -d: -f1); \
    else \
        group_name="jotcash"; \
        groupadd -g ${GID} ${group_name}; \
    fi && \
    if getent passwd ${UID} >/dev/null 2>&1; then \
        user_name=$(getent passwd ${UID} | cut -d: -f1); \
    else \
        user_name="jotcash"; \
        useradd -u ${UID} -g ${group_name} -M -s /bin/sh ${user_name}; \
    fi && \
    mkdir -p /log /resource && \
    chown -R ${UID}:${GID} /log /resource && \
    chmod 777 /log /resource

VOLUME /log
VOLUME /resource
WORKDIR /
USER ${UID}:${GID}
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:7678/api/ping || exit 1
CMD ["/jotcash"]
