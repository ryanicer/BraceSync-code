# BraceSync alert-service — 多阶段构建
# builder: 编译静态二进制; runtime: distroless 最小运行镜像

FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /bin/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
