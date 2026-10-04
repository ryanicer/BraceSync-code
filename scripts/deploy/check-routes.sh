#!/usr/bin/env bash
# check-routes.sh — gateway 路由 ↔ user-service 路由 ↔ device-simulator 常量比对检查
# 用法: bash scripts/deploy/check-routes.sh
# 退出码: 0=一致, 1=存在差异, 2=解析错误（含某一侧一条都没提取到 —— 那是尺没扫到面，不是通过）
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GATEWAY_DIR="$REPO_ROOT/services/gateway/cmd/server"
USER_SVC_DIR="$REPO_ROOT/services/user-service/internal/handler"
SIM_CMD_DIR="$REPO_ROOT/scripts/dev/device-simulator/cmd"

TMPDIR="${TMPDIR:-/tmp}"
GW_ROUTES="$TMPDIR/check-routes-gw-$$.txt"
US_ROUTES="$TMPDIR/check-routes-us-$$.txt"
GW_DEV_ROUTES="$TMPDIR/check-routes-gwdev-$$.txt"
SIM_ROUTES="$TMPDIR/check-routes-sim-$$.txt"
trap 'rm -f "$GW_ROUTES" "$US_ROUTES" "$GW_DEV_ROUTES" "$SIM_ROUTES"' EXIT

# 0. 自证有牙（T562）：一条比对腿如果没有注入过缺陷并真的判红过，它和恒真表达式在读数上不可区分。
#    所以本脚本默认先在自己的副本上跑三形：合规(期望 rc=0) / 路径漂移(期望 1) / 恒 0 空面(期望 2)，
#    三形任一不符就退 2 —— 宁可对业务面判不出结论，也不放一条自己已经失效的腿过去。
#    关掉只有一条路：CHECK_ROUTES_NO_SELFTEST=1（CI 与本仓约定是默认必跑；内部递归就是这么断的）。
SELFTEST_SCRATCH=""
run_selftest() {
  local sim_rel gs rel rc_drift rc_clean rc_empty hits scratch
  scratch="$(mktemp -d "${TMPDIR%/}/check-routes-selftest-XXXXXX")" || {
    echo "  ❌ 自证腿建不出临时树（读不到就是读不到，不当通过）"; exit 2; }
  # 必须是全局名：trap 在脚本退出时才展开执行，那时函数内的 local 早就不在了
  # （写成本地名的实测后果：比对已经跑完并打出结论，退出时 trap 报 unbound variable 污染出口码）。
  SELFTEST_SCRATCH="$scratch"
  trap 'rm -rf "$SELFTEST_SCRATCH"; rm -f "$GW_ROUTES" "$US_ROUTES" "$GW_DEV_ROUTES" "$SIM_ROUTES"' EXIT
  for rel in scripts/deploy/check-routes.sh \
             services/gateway/cmd/server/proxy_admin.go \
             services/gateway/cmd/server/proxy_services.go \
             services/user-service/internal/handler/handler.go \
             scripts/dev/device-simulator/cmd/simulator.go; do
    if [ ! -f "$REPO_ROOT/$rel" ]; then
      echo "  ❌ 自证腿缺输入：$rel（0 命中是「没扫到面」，不是「一致」）"; exit 2;
    fi
    mkdir -p "$scratch/$(dirname "$rel")"
    cp "$REPO_ROOT/$rel" "$scratch/$rel"
  done
  sim_rel="scripts/dev/device-simulator/cmd/simulator.go"
  gs="$scratch/$sim_rel"
  cp "$gs" "$gs.orig"

  # 形一：原样（合规面）—— 期望 0
  rc_clean=0
  ( cd "$scratch" && CHECK_ROUTES_NO_SELFTEST=1 bash scripts/deploy/check-routes.sh >/dev/null 2>&1 ) || rc_clean=$?

  # 形二：把两条常量改回历史上的 404 路径 —— 期望 1（有差异）
  sed -i 's#"/api/v1/device/records"#"/api/v1/device/report"#; s#"/api/v1/device/records/batch"#"/api/v1/device/report/batch"#' "$gs"
  # 注入必须落在常量行上，否则这一形测的是空气
  hits=$(grep -cE '^[[:space:]]*Path(Single|Batch)[[:space:]]*=[[:space:]]*"/api/v1/device/report' "$gs" || true)
  if [ "$hits" -ne 2 ]; then
    echo "  ❌ 自证腿的注入没落到常量行上（命中 $hits 颗，期望 2 颗）—— 常量行形变了，请同步改本自证"; exit 2;
  fi
  rc_drift=0
  ( cd "$scratch" && CHECK_ROUTES_NO_SELFTEST=1 bash scripts/deploy/check-routes.sh >/dev/null 2>&1 ) || rc_drift=$?

  # 形三：常量整块删掉 —— 期望 2（恒 0 的空面不能当通过）
  cp "$gs.orig" "$gs"
  sed -i '/Path[A-Za-z]*[ \t]*=[ \t]*"\/api/d' "$gs"
  rc_empty=0
  ( cd "$scratch" && CHECK_ROUTES_NO_SELFTEST=1 bash scripts/deploy/check-routes.sh >/dev/null 2>&1 ) || rc_empty=$?

  if [ "$rc_clean" -ne 0 ] || [ "$rc_drift" -ne 1 ] || [ "$rc_empty" -ne 2 ]; then
    echo "  ❌ 自证腿判红：三形实读 rc 为 合规=$rc_clean(期望0) 漂移=$rc_drift(期望1) 空面=$rc_empty(期望2)"
    echo "     这说明比对腿本身已失效（或它的输入面形状变了），此时的「通过」不可信"; exit 2;
  fi
  echo "  自证有牙：合规=$rc_clean 漂移=$rc_drift 空面=$rc_empty（三形与期望一致，注入颗数=$hits）"
}

if [ "${CHECK_ROUTES_NO_SELFTEST:-0}" = "1" ]; then
  echo "=== BraceSync 路由比对检查（自证腿已由 CHECK_ROUTES_NO_SELFTEST=1 关闭）==="
else
  echo "=== BraceSync 路由比对检查 ==="
  echo ""
  echo "[0/4] 自证有牙（注入三形）..."
  run_selftest
fi
echo ""

# 1. 从 gateway 提取 user-service 路由（proxy_admin.go 中的 userServiceRoutes）
echo "[1/4] 提取 gateway user-service 路由..."
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
echo "[2/4] 提取 user-service 路由注册..."
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

# 3. 设备上报路径 ↔ device-simulator 常量（T562 M1）
#    成因：模拟器曾把上报路径写成 /api/v1/device/report 与 /report/batch，而网关注册的是
#    deviceReportRoutes 那两条 ⇒ 现网实测前者 404 page not found，「用模拟器验过链路」
#    得到的是假阴性（T551 S-1）。scripts/dev 既不在 go.work 也不在任何 Go/CI job，
#    这一条是本仓唯一会为此响的门，所以它必须在这里，而不是在模拟器的注释里。
echo ""
echo "[3/4] 设备上报路径 ↔ 模拟器常量..."

# 3a. gateway 侧：registerDeviceReportRoutes 的组前缀 + deviceReportRoutes 的相对路径
GW_PREFIX=$(awk '
  /^func registerDeviceReportRoutes/ { in_fn=1 }
  in_fn && /r\.Group\(/ {
    if (match($0, /"[^"]*"/)) { print substr($0, RSTART+1, RLENGTH-2); exit }
  }
' "$GATEWAY_DIR/proxy_services.go")

awk -v prefix="$GW_PREFIX" '
  /^var deviceReportRoutes = \[\]proxyRoute\{/ { in_block=1; next }
  in_block && /^\}/ { in_block=0 }
  in_block && /http\.Method/ && !/^[ \t]*\/\// {
    # 形如 {http.MethodPost, "/device/records"}, —— 前缀在代码里拼，这里同样拼
    line=$0
    if (match(line, /http\.Method[A-Za-z]+/)) {
      method=substr(line, RSTART, RLENGTH); gsub(/.*\./, "", method); gsub(/^Method/, "", method); method=toupper(method)
    }
    if (match(line, /"\/[^"]*"/)) {
      path=substr(line, RSTART+1, RLENGTH-2)
      if (method != "") print method " " prefix path
    }
    method=""
  }
' "$GATEWAY_DIR/proxy_services.go" | sort > "$GW_DEV_ROUTES"

GW_DEV_COUNT=$(wc -l < "$GW_DEV_ROUTES")
if [ -z "$GW_PREFIX" ] || [ "$GW_DEV_COUNT" -eq 0 ]; then
  echo "  ❌ 解析错误：gateway 侧没提取到设备上报路由（prefix=[$GW_PREFIX] 条数=$GW_DEV_COUNT）"
  echo "     这条腿自己没扫到面时，0 命中不是「一致」，是判据失效 ⇒ 按退出码 2 处理"
  exit 2
fi
echo "  提取到 $GW_DEV_COUNT 条 gateway 设备上报路由（前缀 $GW_PREFIX）"

# 3b. 模拟器侧：PathSingle / PathBatch 两个常量
awk '
  /^[ \t]*Path[ \t]*=/ || /^[ \t]*(PathSingle|PathBatch)[ \t]+=/ {
    if (match($0, /"\/[^"]*"/)) print "POST " substr($0, RSTART+1, RLENGTH-2)
  }
' "$SIM_CMD_DIR/simulator.go" | sort > "$SIM_ROUTES"

SIM_COUNT=$(wc -l < "$SIM_ROUTES")
if [ "$SIM_COUNT" -eq 0 ]; then
  echo "  ❌ 解析错误：$SIM_CMD_DIR/simulator.go 里没提取到上报路径常量"
  echo "     常量被删或改名会让这一腿恒 0 —— 恒 0 的腿不能当通过，按退出码 2 处理"
  exit 2
fi
echo "  提取到 $SIM_COUNT 条 device-simulator 上报路径常量"

SIM_DIFF=$(comm -3 "$GW_DEV_ROUTES" "$SIM_ROUTES" || true)
if [ -n "$SIM_DIFF" ]; then
  echo "  ❌ 设备上报路径不一致（模拟器会 404 空跑）："
  echo "$SIM_DIFF" | while read -r line; do echo "     $line"; done
fi

# 4. 比对差异
echo ""
echo "[4/4] 比对差异..."

# 找出 user-service 有但 gateway 没有的路由（即 gateway 缺失的）
MISSING=$(comm -23 "$US_ROUTES" "$GW_ROUTES" || true)

# 找出 gateway 有但 user-service 没有的路由（即 gateway 多余的）
EXTRA=$(comm -13 "$US_ROUTES" "$GW_ROUTES" || true)

if [ -z "$MISSING" ] && [ -z "$EXTRA" ] && [ -z "$SIM_DIFF" ]; then
  echo ""
  echo "✅ 路由比对通过：gateway ↔ user-service ↔ device-simulator 上报路径完全一致"
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

if [ -n "$SIM_DIFF" ]; then
  echo ""
  echo "❌ 设备上报路径与模拟器常量不一致（详见 [3/4] 的输出）"
fi

echo ""
echo "请检查以上差异，补齐路由注册后重新运行本脚本验证。"
exit 1
