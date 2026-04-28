# Dockerfile for LLM ext_proc server
# 使用阿里云 Go 镜像加速 (中国区)
FROM docker.1ms.run/library/golang:1.24.13-alpine3.23 AS builder

# 设置 Go 镜像源 (国内加速)
RUN sed -i 's#https\?://dl-cdn.alpinelinux.org/alpine#https://mirrors.tuna.tsinghua.edu.cn/alpine#g' /etc/apk/repositories && apk update  && \
    go env -w GOPROXY=https://goproxy.cn,direct && \
    go env -w GOSUMDB=off

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o ext-proc ./cmd

# 运行阶段 - 使用阿里云 Alpine 镜像
FROM docker.1ms.run/library/alpine:latest

# 设置时区
RUN apk add --no-cache tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    apk del tzdata

# 安装健康检查工具和调试工具
RUN apk add --no-cache \
    # grpc_health_probe \
    ca-certificates \
    curl 

WORKDIR /app
COPY --from=builder /app/ext-proc .

EXPOSE 8888
# HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
#     CMD grpc_health_probe -addr=:8888 || exit 1

ENTRYPOINT ["./ext-proc"]
