#!/usr/bin/env bash
# BraceSync 本地 DB 一键初始化
# 用法：bash scripts/dev/init-db.sh
# 前置：Docker + docker compose + golang-migrate (migrate) + psql
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.dev.yml"
DB_URL="postgres://bracesync:bracesync@localhost:5432/bracesync?sslmode=disable"

echo "==> [1/4] 启动 PG + Redis ..."
docker compose -f "$COMPOSE_FILE" up -d

echo "==> [2/4] 等待 PostgreSQL 就绪 ..."
for i in $(seq 1 30); do
  if docker compose -f "$COMPOSE_FILE" exec -T postgres pg_isready -U bracesync >/dev/null 2>&1; then
    echo "    PostgreSQL ready."
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "ERROR: PostgreSQL 启动超时" >&2
    exit 1
  fi
  sleep 1
done

echo "==> [3/4] 执行数据库迁移 ..."
migrate -path "$PROJECT_ROOT/scripts/db/migrations" -database "$DB_URL" up

echo "==> [4/4] 载入种子数据 ..."
psql "$DB_URL" -f "$PROJECT_ROOT/scripts/db/seed/seed.sql"

echo ""
echo "✓ 本地 DB 初始化完成！"
echo "  PG:    postgres://bracesync:bracesync@localhost:5432/bracesync"
echo "  Redis: redis://localhost:6379"
echo ""
echo "验证命令："
echo "  psql \"$DB_URL\" -c '\\dt'"
echo "  psql \"$DB_URL\" -c 'SELECT count(*) FROM consents;'"
