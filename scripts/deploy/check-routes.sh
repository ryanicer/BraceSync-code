#!/usr/bin/env bash
# check-routes.sh — gateway 路由 ↔ user-service 路由自动比对检查
# 用法: bash scripts/deploy/check-routes.sh
# 退出码: 0=一致, 1=存在差异, 2=解析错误
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GATEWAY_DIR="$REPO_ROOT/services/gateway/cmd/server"
USER_SVC_DIR="$REPO_ROOT/services/user-service/internal/handler"

TMPDIR="${TMPDIR:-/tmp}"
GW_ROUTES="$TMPDIR/check-routes-gw-$$.txt"
US_ROUTES="$TMPDIR/check-routes-us-$$.txt"
trap 'rm -f "$GW_ROUTES" "$US_ROUTES"' EXIT

echo "=== BraceSync 路由比对检查 ==="
echo ""

# 1. 从 gateway 提取 user-service 路由（proxy_admin.go 中的 userServiceRoutes）
echo "[1/3] 提取 gateway user-service 路由..."
awk '
  /^var userServiceRoutes = \[\]proxyRoute\{/ { in_block=1; next }
  in_block && /^\}/ { in_block=0 }
  in_block && /http\.Method/ && !/^[ \t]*\/\// {
    # 提取 method 和 path，如 {http.MethodPost, "/admin/patients"},
    gsub(/[\t ,{}]+/, " ", $0)
    for(i=1;i<=NF;i++) {
      if($i ~ /^http\.Method/) { method=$i; gsub(/.*\./,"",method); gsub(/^Method/,"",method); method=toupper(method) }
      if($i ~ /^"\//) { path=$i; gsub(/[",]/,"",path) }
    }
    if(method && path) print method " " path
    method=""; path=""
  }
' "$GATEWAY_DIR/proxy_admin.go" | sort > "$GW_ROUTES"

GW_COUNT=$(wc -l < "$GW_ROUTES")
echo "  提取到 $GW_COUNT 条 gateway user-service 路由"

# 2. 从 user-service handler.go 提取路由注册
echo "[2/3] 提取 user-service 路由注册..."
awk '
  /v1\.(GET|POST|PUT|DELETE|PATCH)\(/ {
    line=$0
    gsub(/^[ \t]+/, "", line)
    # 提取 method
    if(match(line, /v1\.(GET|POST|PUT|DELETE|PATCH)\(/, m_arr)) {
      method=m_arr[1]
    }
    # 提取 path（第一个字符串参数）
    if(match(line, /"([^"]+)"/, p_arr)) {
      path=p_arr[1]
    }
    if(method && path) print method " " path
    method=""; path=""
  }
' "$USER_SVC_DIR/handler.go" | sort > "$US_ROUTES"

US_COUNT=$(wc -l < "$US_ROUTES")
echo "  提取到 $US_COUNT 条 user-service 路由"

# 3. 比对差异
echo ""
echo "[3/3] 比对差异..."

# 找出 user-service 有但 gateway 没有的路由（即 gateway 缺失的）
MISSING=$(comm -23 "$US_ROUTES" "$GW_ROUTES" || true)

# 找出 gateway 有但 user-service 没有的路由（即 gateway 多余的）
EXTRA=$(comm -13 "$US_ROUTES" "$GW_ROUTES" || true)

if [ -z "$MISSING" ] && [ -z "$EXTRA" ]; then
  echo ""
  echo "✅ 路由比对通过：gateway 与 user-service 路由完全一致"
  exit 0
fi

if [ -n "$MISSING" ]; then
  echo ""
  echo "❌ 发现 user-service 已注册但 gateway 缺失的路由（将导致 404）："
  echo "$MISSING" | while read -r line; do
    echo "  - $line"
  done
fi

if [ -n "$EXTRA" ]; then
  echo ""
  echo "⚠️  发现 gateway 已注册但 user-service 未定义的路由（可能是废弃路由）："
  echo "$EXTRA" | while read -r line; do
    echo "  - $line"
  done
fi

echo ""
echo "请检查以上差异，补齐 gateway 路由注册后重新运行本脚本验证。"
exit 1
