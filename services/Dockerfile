# syntax=docker/dockerfile:1
# BraceSync 服务镜像构建模板（7 服务统一入口，T013 新增）
# 用法（仓库根目录执行）：
#   docker build --build-arg SERVICE=<svc> -f services/Dockerfile .
# 对齐：docs/ §1/§3.2 · scripts/deploy/build-all.sh
# 运行时镜像与服务器本地构建产出一致：alpine:3.20 + ca-certificates + tzdata + 静态二进制
ARG GO_VERSION=1.22

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
# build context = 仓库根：COPY go.work + services/，go.work 接管 module 解析
# （gateway 依赖 services/testhelper 的 replace 指令在 go.work 下正常解析）
COPY go.work ./
COPY services/ ./services/
ARG SERVICE
RUN test -n "$SERVICE" && \
    cd services/${SERVICE} && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/server ./cmd/server/

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /out/server /server
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD wget -q --spider http://localhost:8080/healthz || exit 1
ENTRYPOINT ["/server"]
