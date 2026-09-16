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

FROM golang:1.27-alpine
COPY --from=builder /jotcash /jotcash
RUN chmod 755 /jotcash
ARG TZ="Asia/Shanghai"
ENV TZ=${TZ}
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apk/repositories
RUN apk update
RUN apk --no-cache add ca-certificates
RUN apk --no-cache add tzdata && cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone

ARG UID=1000
ARG GID=1000

RUN if getent group ${GID} >/dev/null 2>&1; then \
        group_name=$(getent group ${GID} | cut -d: -f1); \
    else \
        group_name="jotcash"; \
        addgroup -g ${GID} -S ${group_name}; \
    fi && \
    if getent passwd ${UID} >/dev/null 2>&1; then \
        user_name=$(getent passwd ${UID} | cut -d: -f1); \
    else \
        user_name="jotcash"; \
        adduser -u ${UID} -G "${group_name}" -S -D -H ${user_name}; \
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
