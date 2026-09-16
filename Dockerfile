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
ARG TZ="Asia/Shanghai"
ENV TZ=${TZ}
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apk/repositories
RUN apk update
RUN apk --no-cache add ca-certificates
RUN apk --no-cache add tzdata && cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone
VOLUME /log
VOLUME /resource
WORKDIR /
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget --spider -q http://127.0.0.1:7678/api/ping || exit 1
CMD ["/jotcash"]
