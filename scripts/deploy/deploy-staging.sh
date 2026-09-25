#!/usr/bin/env bash
# BraceSync Staging 一键部署脚本（T035 增补②）
# 用法：bash scripts/deploy/deploy-staging.sh
# 规格：
#   1. cd 权威仓库 && git pull github main
#   2. bash scripts/deploy/build-all.sh staging-<sha>（7 个 Go 镜像）
#   3. admin-web vite build → 产物复制到部署目录
#   4. 增量执行未跑过的 migration（幂等，schema_migrations 表跟踪）
#   5. docker compose up -d 重建变更服务
#   6. 冒烟：healthz → 登录 → 受保护端点 → 挂载前缀/深链（T336）→ 全过才输出 DEPLOY OK
#   7. 任一环节失败即退出非零中止；幂等可重入；不碰生产 /opt/bracesync
#   8. 自改隐患（T364）：先从只读快照副本执行，开头记基线、结尾比对工作树真本
#   9. cron 入口外置（T383 第一处）：每轮把 cron 用的备份脚本原子签发到工作树之外的 CRON_PUBLISH_DIR，
#      crontab 只引用外置副本 —— 被调度脚本的字节不再随 ① 步 checkout/pull 变动
#  10. 中止轮留凭据（T383 第二处）：① 至 ⑦ 任一步非零退出时，先落一份「开头基线 vs 中止时」
#      自比对凭据到 SELFCHK_RECEIPT_DIR 再清理（⑧ 段只在成功路径跑，中止轮原先什么都不留）
#  11. cron 引用零漂移核对（T393 N6）：⓪-b 段用仓内期望清单只读比对实际 crontab，
#      发现「引用指回工作树 / 期望副本没被引用 / 落点副本不见」即在任何变更前中止本轮
set -euo pipefail

PROJECT_ROOT="/home/ubuntu/bracesync"
STAGING_DIR="/opt/bracesync-staging"
MIGRATIONS_DIR="$PROJECT_ROOT/scripts/db/migrations"
SEED_SQL="$PROJECT_ROOT/scripts/db/seed/seed.sql"
SMOKE_USER="ops_admin"
SMOKE_PASS="admin123"
HEALTH_URL="http://localhost:81/healthz"

# T383：被部署工作树【之外】的运行副本区。
#   判据「凡正在被调度 / 正在被解释的字节，都不许住在 ① 步会被 checkout、pull 换掉的树里」，
#   所以 cron 实际引用的备份脚本副本、以及中止轮的自比对凭据，都落在这里（树内只留源文件）。
#   四个变量与 publish-cron-scripts.sh / selfcheck receipt 的同名入参一一对应，覆盖它们只为测试。
OPS_ROOT="${BRACESYNC_OPS_ROOT:-/home/ubuntu/bracesync-ops}"
CRON_SOURCE_DIR="${CRON_SOURCE_DIR:-$PROJECT_ROOT/scripts/backup}"
CRON_PUBLISH_DIR="${CRON_PUBLISH_DIR:-$OPS_ROOT/bin}"
CRON_PUBLISH_LOG="${CRON_PUBLISH_LOG:-$OPS_ROOT/cron-scripts.published.log}"
SELFCHK_RECEIPT_DIR="${SELFCHK_RECEIPT_DIR:-$OPS_ROOT/deploy-selfcheck}"

LOGIN_URL="http://localhost:81/api/v1/auth/login"
PATIENTS_URL="http://localhost:81/api/v1/admin/patients"
ADMIN_URL="http://localhost:81/admin/"
export PATH=/usr/local/go/bin:/usr/bin:/usr/sbin:$PATH

log()  { echo "\033[1;32m[deploy]\033[0m $*"; }
err()  { echo "\033[1;31m[ERROR]\033[0m $*" >&2; }
fail() { err "$*"; exit 1; }

# ⓪ T364 自改隐患隔离（必须在 ① 之前）
#   本脚本就住在 ① 步会被 checkout/pull 替换的那个工作树里，而 bash 是边读边解释：
#   文件一被换掉，解释器就按旧字节偏移续读新文件，后半段能整段跳过却照样打 DEPLOY OK
#   （T339 18:36 轮实测漏跑挂载点守卫段）。做法：把真本装成只读快照后 exec 快照，
#   快照不会被任何部署动作改动；同时记录工作树真本基线，交给 ⑧ 比对。
WORKTREE_SCRIPT="$PROJECT_ROOT/scripts/deploy/deploy-staging.sh"
SELFCHK_SRC="$PROJECT_ROOT/scripts/deploy/selfcheck-deploy-script.sh"
PUBLISH_SRC="$PROJECT_ROOT/scripts/deploy/publish-cron-scripts.sh"
CRON_GUARD_SRC="$PROJECT_ROOT/scripts/deploy/cron-reference-guard.sh"
STATE_FILE="${TMPDIR:-/tmp}/t364-deploy-selfcheck.$$.env"

if [ -z "${T364_SNAP_ROOT:-}" ]; then
  SNAP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/t364-deploy-snap.XXXXXX") || fail "mktemp 失败，中止（不退回原地解释）"
  chmod 700 "$SNAP_ROOT"
  trap 'rm -rf "${SNAP_ROOT:-}" "${STATE_FILE:-}"' EXIT
  [ -r "$WORKTREE_SCRIPT" ] || fail "工作树内部署脚本不可读：$WORKTREE_SCRIPT"
  [ -r "$SELFCHK_SRC" ] || fail "自检脚本缺失：$SELFCHK_SRC（本轮部署包不完整）"
  [ -r "$PUBLISH_SRC" ] || fail "外置签发脚本缺失：$PUBLISH_SRC（T383 后 ⑦-b 段必需，本轮部署包不完整）"
  [ -r "$CRON_GUARD_SRC" ] || fail "cron 引用守卫脚本缺失：$CRON_GUARD_SRC（T393 后 ⓪-b 段必需，本轮部署包不完整）"
  install -m 400 "$WORKTREE_SCRIPT" "$SNAP_ROOT/deploy-staging.sh" || fail "部署脚本快照安装失败"
  install -m 400 "$SELFCHK_SRC" "$SNAP_ROOT/selfcheck-deploy-script.sh" || fail "自检脚本快照安装失败"
  install -m 400 "$PUBLISH_SRC" "$SNAP_ROOT/publish-cron-scripts.sh" || fail "外置签发脚本快照安装失败"
  install -m 400 "$CRON_GUARD_SRC" "$SNAP_ROOT/cron-reference-guard.sh" || fail "cron 引用守卫脚本快照安装失败"
  bash "$SNAP_ROOT/selfcheck-deploy-script.sh" record "$WORKTREE_SCRIPT" "$STATE_FILE" || fail "自改基线记录失败"
  export T364_SNAP_ROOT="$SNAP_ROOT" T364_STATE_FILE="$STATE_FILE"
  exec bash "$SNAP_ROOT/deploy-staging.sh" "$@"
fi

if [ -z "${T364_SNAP_ROOT:-}" ] || [ ! -d "$T364_SNAP_ROOT" ] \
   || [ -z "${T364_STATE_FILE:-}" ] || [ ! -r "$T364_STATE_FILE" ]; then
  fail "快照环境缺失（T364_SNAP_ROOT / T364_STATE_FILE），请从 $WORKTREE_SCRIPT 正常入口调用"
fi
SNAP_ROOT="$T364_SNAP_ROOT"
STATE_FILE="$T364_STATE_FILE"
SELFCHK="$SNAP_ROOT/selfcheck-deploy-script.sh"
# T383 第二处：⑧ 段（自比对）只在成功路径上跑，① 至 ⑦ 任一步 fail 时 trap 会把基线连快照
#   一起删掉 ⇒ 中止轮拿不出「开头 vs 中止时」的自比对凭据。改为：非零退出先落一份只读凭据，
#   再清理 —— 每轮无论成败都可对账，成功轮行为一字不动。
t383_finish() {
  rc_at_exit="$1"
  if [ "$rc_at_exit" != "0" ]; then
    bash "$SELFCHK" receipt "$WORKTREE_SCRIPT" "$STATE_FILE" "$SELFCHK_RECEIPT_DIR" "$rc_at_exit" \
      || err "中止轮自比对凭据落盘失败（不改变本轮中止原因本身）"
  fi
  rm -rf "${SNAP_ROOT:-}" "${STATE_FILE:-}"
}
trap 't383_finish "$?"' EXIT
log "⓪ 自改隔离：本次执行只读快照 $0"

# ⓪-b T393 N6：cron 引用零漂移核对（只读，排在 ① 之前 ⇒ 判红时本轮对现网与工作树零变更）
#   仓里第一次写明「两条 cron 应当指向外置副本」的期望，并由 cron-reference-guard.sh 拿它
#   与实际 crontab 做只读比对。放在 git pull 之前是有意的：① 步含 git checkout -- . ，
#   正是可能把被引用文件换掉的那个动作本身，排在它后面就已经丢了零变更这条底线。
log "⓪-b cron 引用零漂移核对（期望落点 $CRON_PUBLISH_DIR，只读）..."
bash "$SNAP_ROOT/cron-reference-guard.sh" || fail "cron 引用与期望外置落点不一致（详见上方 [cron-ref] 行）；本轮未做任何变更即中止"

# ① git pull
log "① 拉取最新代码 ..."
cd "$PROJECT_ROOT"
git checkout -- . 2>/dev/null || true
git pull github main || fail "git pull 失败"
SHA=$(git rev-parse --short HEAD)
TAG="staging-$SHA"
log "   当前版本: $SHA (TAG=$TAG)"

# ② 构建 Go 镜像
log "② 构建 7 个 Go 服务镜像 (TAG=$TAG) ..."
if [ ! -f scripts/deploy/build-all.sh ]; then
  fail "build-all.sh 不存在，请确认仓库完整性"
fi
log "   调用 build-all.sh TAG=$TAG ..."
bash scripts/deploy/build-all.sh "$TAG" || {
  err "镜像构建失败 (TAG=$TAG)"
  err "请检查上方 build-all.sh 输出中的错误信息"
  err "常见原因：Go 编译错误、Docker daemon 未运行、磁盘空间不足"
  exit 1
}

# ③ 构建 admin-web
log "③ 构建 admin-web (VITE_USE_MOCK=false) ..."
cd "$PROJECT_ROOT"
npm install --silent 2>/dev/null || fail "npm install 失败"
VITE_USE_MOCK=false npm run build -w apps/admin-web || fail "admin-web 构建失败"
log "   admin-web 产物: $PROJECT_ROOT/apps/admin-web/dist/"

# rsync admin-web 产物到 staging 目录
log "   rsync admin-web 产物到 $STAGING_DIR/apps/admin-web/dist/ ..."
sudo mkdir -p "$STAGING_DIR/apps/admin-web/dist"
sudo rsync -a --delete "$PROJECT_ROOT/apps/admin-web/dist/" "$STAGING_DIR/apps/admin-web/dist/"

# ④ 增量数据库迁移
log "④ 增量数据库迁移 ..."
PG_CONTAINER="bracesync-staging-postgres-1"

docker exec "$PG_CONTAINER" psql -U bracesync -d bracesync -c "
  CREATE TABLE IF NOT EXISTS schema_migrations (
    version    VARCHAR(32) PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
  );" || fail "创建 schema_migrations 表失败"

# 迁移关键对象探测表（version → 探测 SQL）
# 面对旧库（已有 schema 但 schema_migrations 空表）时，探测关键对象存在即视为已执行
declare -A MIGRATION_PROBE
MIGRATION_PROBE[000001]="SELECT to_regclass('public.teams')"
MIGRATION_PROBE[000002]="SELECT to_regclass('public.device_bindings')"
MIGRATION_PROBE[000003]="SELECT to_regclass('public.notification_records')"
MIGRATION_PROBE[000004]="SELECT to_regclass('public.archive_status')"
MIGRATION_PROBE[000005]="SELECT column_name FROM information_schema.columns WHERE table_name='patients' AND column_name='password_hash'"

for script in "$MIGRATIONS_DIR"/*.up.sql; do
  ver=$(basename "$script" | grep -oE '^[0-9]+')
  applied=$(docker exec "$PG_CONTAINER" psql -U bracesync -d bracesync -t -c "SELECT 1 FROM schema_migrations WHERE version='$ver'" 2>/dev/null | xargs)
  if [ "$applied" = "1" ]; then
    log "   跳过: $(basename "$script") (已登记)"
    continue
  fi
  # 探测关键对象是否已存在（旧库兼容）
  probe_sql="${MIGRATION_PROBE[$ver]:-}"
  if [ -n "$probe_sql" ]; then
    probe_result=$(docker exec "$PG_CONTAINER" psql -U bracesync -d bracesync -t -c "$probe_sql" 2>/dev/null | xargs)
    if [ -n "$probe_result" ] && [ "$probe_result" != "" ]; then
      log "   跳过: $(basename "$script") (关键对象已存在，登记为已执行)"
      docker exec "$PG_CONTAINER" psql -U bracesync -d bracesync -c "INSERT INTO schema_migrations (version) VALUES ('$ver') ON CONFLICT DO NOTHING;" || true
      continue
    fi
  fi
  log "   执行: $(basename "$script")"
  docker exec -i "$PG_CONTAINER" psql -U bracesync -d bracesync -v ON_ERROR_STOP=1 < "$script" || fail "迁移失败: $(basename "$script")"
  docker exec "$PG_CONTAINER" psql -U bracesync -d bracesync -c "INSERT INTO schema_migrations (version) VALUES ('$ver') ON CONFLICT DO NOTHING;" || true
done

log "   执行 seed 数据 ..."
docker exec -i "$PG_CONTAINER" psql -U bracesync -d bracesync < "$SEED_SQL" >/dev/null 2>&1 || log "   seed 执行完成（部分冲突已跳过）"

# ⑤ 同步部署资产 + docker compose up
log "⑤ 同步部署资产到 $STAGING_DIR ..."
sudo mkdir -p "$STAGING_DIR"

# 同步 docker-compose.yml（从仓库 scripts/deploy/ 复制）并做 staging 端口错位改写
if [ -f "$PROJECT_ROOT/scripts/deploy/docker-compose.yml" ]; then
  sudo cp "$PROJECT_ROOT/scripts/deploy/docker-compose.yml" "$STAGING_DIR/docker-compose.yml"
  # staging 端口错位：80→81:80，443→8443:443（避免与生产 80/443 冲突）
  sudo sed -i 's/"80:80"/"81:80"/g' "$STAGING_DIR/docker-compose.yml"
  sudo sed -i 's/"443:443"/"8443:443"/g' "$STAGING_DIR/docker-compose.yml"
  # 确保 nginx volumes 包含 default.conf.disabled 和 admin-web dist 挂载
  if ! grep -q 'default.conf.disabled' "$STAGING_DIR/docker-compose.yml"; then
    sudo sed -i '/nginx\.conf:ro/a\      - ./default.conf.disabled:/etc/nginx/conf.d/default.conf:ro' "$STAGING_DIR/docker-compose.yml"
  fi
fi

# 同步 nginx.conf
if [ -f "$PROJECT_ROOT/scripts/deploy/nginx.conf" ]; then
  sudo cp "$PROJECT_ROOT/scripts/deploy/nginx.conf" "$STAGING_DIR/nginx.conf"
fi

# 同步 prometheus.yml
if [ -f "$PROJECT_ROOT/scripts/deploy/prometheus.yml" ]; then
  sudo cp "$PROJECT_ROOT/scripts/deploy/prometheus.yml" "$STAGING_DIR/prometheus.yml"
  # restart prometheus 容器使新配置生效
  sudo docker compose restart prometheus 2>/dev/null || log "   prometheus 容器未运行或不存在，跳过 restart"
fi

# 确保 .env 存在
if [ ! -f "$STAGING_DIR/.env" ]; then
  fail "$STAGING_DIR/.env 不存在，请先手动配置"
fi

# 更新 TAG
sudo sed -i "s/^TAG=.*/TAG=$TAG/" "$STAGING_DIR/.env" 2>/dev/null || true

# 确保 default.conf.disabled 存在（nginx 502 修复）
echo '# default.conf disabled — using nginx.conf server block' | sudo tee "$STAGING_DIR/default.conf.disabled" >/dev/null

# 确保 certs 目录存在
sudo mkdir -p "$STAGING_DIR/certs"

log "   docker compose up -d (TAG=$TAG) ..."
cd "$STAGING_DIR"
sudo docker compose up -d || fail "docker compose up 失败"

log "   等待服务启动 ..."
sleep 10

# 重启 nginx 以清除旧 IP 缓存（根治冒烟 502 误报）
sudo docker compose restart nginx
log "   nginx 已重启（清除旧 IP 缓存）"
sleep 2    # 等待 nginx 恢复，避免 healthz HTTP 000 误报

# ⑥ 冒烟验证
log "⑥ 冒烟验证 ..."

HEALTH_CODE=$(curl -sS -o /dev/null -w '%{http_code}' "$HEALTH_URL" 2>/dev/null || echo "000")
if [ "$HEALTH_CODE" != "200" ]; then
  fail "healthz 检查失败 (HTTP $HEALTH_CODE)"
fi
log "   ✅ healthz → 200"

LOGIN_RESP=$(curl -sS -X POST "$LOGIN_URL" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$SMOKE_USER\",\"password\":\"$SMOKE_PASS\"}" 2>/dev/null || echo "")
TOKEN=$(echo "$LOGIN_RESP" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['token'])" 2>/dev/null || echo "")
if [ -z "$TOKEN" ]; then
  fail "登录失败: $LOGIN_RESP"
fi
log "   ✅ 登录 → JWT 获取成功"

PATIENTS_CODE=$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $TOKEN" \
  "$PATIENTS_URL" 2>/dev/null || echo "000")
if [ "$PATIENTS_CODE" != "200" ]; then
  fail "受保护端点检查失败 (HTTP $PATIENTS_CODE)"
fi
log "   ✅ 受保护端点 /api/v1/admin/patients → 200"

ADMIN_CODE=$(curl -sS -o /dev/null -w '%{http_code}' "$ADMIN_URL" 2>/dev/null || echo "000")
if [ "$ADMIN_CODE" != "200" ]; then
  fail "admin-web 检查失败 (HTTP $ADMIN_CODE)"
fi
log "   ✅ admin-web /admin/ → 200"

# T336：挂载点部署期实检 —— 构建 base 与 nginx location 必须同值，否则深链/刷新会被弹回首页。
# 静态契约在 apps/admin-web/test/base-mount-contract.spec.ts，这里管「这次构建真的带上前缀」。
if ! curl -sS "$ADMIN_URL" 2>/dev/null | grep -q 'src="/admin/assets/'; then
  fail "admin-web 入口 HTML 未引用 /admin/assets/ 资源（vite base 与 nginx 挂载点不一致 ⇒ 子路由深链必坏）"
fi
DEEP_CODE=$(curl -sS -o /dev/null -w '%{http_code}' "${ADMIN_URL}patients" 2>/dev/null || echo "000")
if [ "$DEEP_CODE" != "200" ]; then
  fail "admin-web 深链 /admin/patients 检查失败 (HTTP $DEEP_CODE)"
fi
log "   ✅ 入口资源带 /admin/ 前缀 + 深链 /admin/patients → 200"

# ⑦ 清理 + 完成
log "⑦ 清理部署残留 ..."
rm -f "$PROJECT_ROOT/bracesync-staging.tar.gz" "$PROJECT_ROOT/bracesync-staging.zip" 2>/dev/null || true
log "   已清理仓库根目录部署残留"

# ⑦-b T383 第一处：把 cron 引用的备份脚本外置签发到被部署工作树之外
#   现状（改前）：ubuntu crontab 两条定时任务直接 /bin/bash $PROJECT_ROOT/scripts/backup/*.sh，
#   而 $PROJECT_ROOT 正是 ① 步 `git checkout -- .` + `git pull` 要换掉的那棵树 —— 与被部署脚本同域，
#   「跑批中途被覆盖」与「随回滚漂移」两条隐患和 T364 同源。
#   签发逻辑抽到 publish-cron-scripts.sh（可在 CI 里单独测），这里跑的是 ⓪ 步的快照副本。
#   备份脚本内部全是绝对路径、与自身位置无关（实测），换目录不改行为。
log "⑦-b cron 备份脚本外置签发（$CRON_SOURCE_DIR → $CRON_PUBLISH_DIR）..."
mkdir -p "$OPS_ROOT" "$CRON_PUBLISH_DIR" || fail "外置区建不出来：$OPS_ROOT"
chmod 700 "$OPS_ROOT" 2>/dev/null || true
CRON_SOURCE_DIR="$CRON_SOURCE_DIR" CRON_PUBLISH_DIR="$CRON_PUBLISH_DIR" CRON_PUBLISH_LOG="$CRON_PUBLISH_LOG" \
  bash "$SNAP_ROOT/publish-cron-scripts.sh" "$SHA" || fail "cron 备份脚本外置签发失败（详见上方 [publish-cron] 行）"

# ⑧ T364 自改隐患自检：① 步有没有换掉「正在被解释的那份脚本」
log "⑧ 自改隐患自检（比对工作树真本 vs 开头基线）..."
if bash "$SELFCHK" verify "$WORKTREE_SCRIPT" "$STATE_FILE"; then
  log "   ✅ 跑批期间工作树内部署脚本字节未变，本轮无自改风险"
else
  SELFCHK_RC=$?
  if [ "$SELFCHK_RC" = "2" ]; then
    err "SELF-CHECK ALARM 本次部署区间内工作树内的 deploy-staging.sh 被 checkout 换掉了（基线 != 结束时）。"
    err "   本次运行未受影响：执行的是 ⓪ 步装好的只读快照 $SNAP_ROOT/deploy-staging.sh"
    err "   该行必须随部署回执原样转 PM 复核（说明这轮合并改过部署脚本本身）"
  else
    err "自检以 rc=$SELFCHK_RC 退出（既非未变也非「字节已变」），本次部署结论不可信，请人工核对"
    exit 1
  fi
fi

log ""
log "========================================"
log "  DEPLOY OK  (sha=$SHA, tag=$TAG)"
log "========================================"
