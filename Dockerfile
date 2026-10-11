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
RUN (sed -i 's/deb.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || \
     sed -i 's/deb.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list 2>/dev/null || true) && \
    apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata wget libgl1 libglib2.0-0 && \
    ln -snf /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone && \
    (pip install --no-cache-dir rapidocr-onnxruntime -i https://pypi.tuna.tsinghua.edu.cn/simple || \
     pip install --no-cache-dir rapidocr-onnxruntime) && \
    python3 -c "from rapidocr_onnxruntime import RapidOCR; RapidOCR()" && \
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
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget --spider -q http://127.0.0.1:7678/api/ping || exit 1
CMD ["/jotcash"]
