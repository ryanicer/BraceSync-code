#!/usr/bin/env bash
# BraceSync 全服务构建脚本（服务器侧执行）
# 用法：bash build-all.sh <TAG>   # TAG 必传（如 staging-<sha> 或 <sha>）；无参报错退出
set -euo pipefail

export PATH=/usr/local/go/bin:/usr/bin:/usr/sbin:$PATH
PROJECT_ROOT="/home/ubuntu/bracesync"
BUILD_DIR="/tmp/bracesync-build"

# TAG 必传，禁止缺省 latest（避免覆盖生产镜像）
TAG="${1:-}"
if [ -z "$TAG" ]; then
  echo "ERROR: TAG 必须显式指定（如 staging-<sha> 或 <sha>），禁止缺省 latest" >&2
  echo "用法：bash build-all.sh <TAG>" >&2
  exit 1
fi

mkdir -p "$BUILD_DIR"

# 服务端口映射（healthcheck 使用各服务实际监听端口）
declare -A SVC_PORT
SVC_PORT[gateway]=8080
SVC_PORT[user-service]=8081
SVC_PORT[device-service]=8082
SVC_PORT[data-service]=8083
SVC_PORT[alert-service]=8080
SVC_PORT[file-service]=8085
SVC_PORT[msg-service]=8086

# 服务列表
SERVICES="gateway user-service device-service data-service alert-service file-service msg-service"

echo "==> [0/8] 拉取 alpine 基础镜像 ..."
sudo docker pull alpine:3.20

echo "==> 开始编译 7 个服务 ..."

for svc in $SERVICES; do
  echo ""
  echo "--- [$svc] ---"
  cd "$PROJECT_ROOT/services/$svc"

  # gateway 特殊处理：testhelper 只在测试中引用，构建前临时移除
  if [ "$svc" = "gateway" ]; then
    cp go.mod go.mod.bak
    cp go.sum go.sum.bak
    # 移除 testhelper 的 require 行和 replace 行
    grep -v 'testhelper' go.mod > go.mod.tmp && mv go.mod.tmp go.mod
    # 清理 require 块中可能残留的空行（go mod tidy 会处理）
    go mod tidy 2>/dev/null || true
  fi

  # 编译静态二进制
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "$BUILD_DIR/$svc/server" ./cmd/server/

  # 恢复 gateway 的原始 go.mod
  if [ "$svc" = "gateway" ]; then
    mv go.mod.bak go.mod
    mv go.sum.bak go.sum
  fi

  # 创建 Dockerfile（使用服务实际监听端口）
  PORT="${SVC_PORT[$svc]}"
  cat > "$BUILD_DIR/$svc/Dockerfile" <<DOCKERFILE
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY server /server
EXPOSE $PORT
HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD wget -q --spider http://localhost:$PORT/healthz || exit 1
ENTRYPOINT ["/server"]
DOCKERFILE

  # 构建镜像
  sudo docker build -t "bracesync/$svc:$TAG" "$BUILD_DIR/$svc/"
  echo "    [OK] bracesync/$svc:$TAG"
done

echo ""
echo "=== 全部镜像构建完成（TAG=$TAG） ==="
sudo docker images | grep bracesync
