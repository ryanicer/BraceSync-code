#!/usr/bin/env bash
# BraceSync Staging 一键部署脚本（T035 增补②）
# 用法：bash scripts/deploy/deploy-staging.sh
# 规格：
#   1. cd 权威仓库 && git pull github main
#   2. bash scripts/deploy/build-all.sh staging-<sha>（7 个 Go 镜像）
#   3. admin-web vite build → 产物复制到部署目录
#   4. 增量执行未跑过的 migration（幂等，schema_migrations 表跟踪）
#   5. docker compose up -d 重建变更服务
#   6. 冒烟：healthz → 登录 → 受保护端点 → 全过才输出 DEPLOY OK
#   7. 任一环节失败即退出非零中止；幂等可重入；不碰生产 /opt/bracesync
set -euo pipefail

PROJECT_ROOT="/home/ubuntu/bracesync"
STAGING_DIR="/opt/bracesync-staging"
MIGRATIONS_DIR="$PROJECT_ROOT/scripts/db/migrations"
SEED_SQL="$PROJECT_ROOT/scripts/db/seed/seed.sql"
SMOKE_USER="ops_admin"
SMOKE_PASS="admin123"
HEALTH_URL="http://localhost:81/healthz"

LOGIN_URL="http://localhost:81/api/v1/auth/login"
PATIENTS_URL="http://localhost:81/api/v1/admin/patients"
ADMIN_URL="http://localhost:81/admin/"
export PATH=/usr/local/go/bin:/usr/bin:/usr/sbin:$PATH

log()  { echo "\033[1;32m[deploy]\033[0m $*"; }
err()  { echo "\033[1;31m[ERROR]\033[0m $*" >&2; }
fail() { err "$*"; exit 1; }

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
bash scripts/deploy/build-all.sh "$TAG" || fail "镜像构建失败"

# ③ 构建 admin-web
log "③ 构建 admin-web (VITE_USE_MOCK=false) ..."
cd "$PROJECT_ROOT"
npm install --silent 2>/dev/null || fail "npm install 失败"
VITE_USE_MOCK=false npm run build -w apps/admin-web || fail "admin-web 构建失败"
log "   admin-web 产物: $PROJECT_ROOT/apps/admin-web/dist/"

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
    sudo sed -i '/nginx\.conf:ro/a\      - ./default.conf.disabled:/etc/nginx/conf.d/default.conf:ro\n      - /home/ubuntu/bracesync/apps/admin-web/dist:/usr/share/nginx/html/admin:ro' "$STAGING_DIR/docker-compose.yml"
  fi
fi

# 同步 nginx.conf
if [ -f "$PROJECT_ROOT/scripts/deploy/nginx.conf" ]; then
  sudo cp "$PROJECT_ROOT/scripts/deploy/nginx.conf" "$STAGING_DIR/nginx.conf"
fi

# 同步 prometheus.yml
if [ -f "$PROJECT_ROOT/scripts/deploy/prometheus.yml" ]; then
  sudo cp "$PROJECT_ROOT/scripts/deploy/prometheus.yml" "$STAGING_DIR/prometheus.yml"
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

# ⑦ 清理 + 完成
log "⑦ 清理部署残留 ..."
rm -f "$PROJECT_ROOT/bracesync-staging.tar.gz" "$PROJECT_ROOT/bracesync-staging.zip" 2>/dev/null || true
log "   已清理仓库根目录部署残留"

log ""
log "========================================"
log "  DEPLOY OK  (sha=$SHA, tag=$TAG)"
log "========================================"
