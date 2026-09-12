#!/bin/bash
set -euo pipefail

#============================================================
# BraceSync Staging 恢复演练脚本
# 目标：从 COS 拉取生产备份 → 恢复到 staging → 验证数据
# 约束：只在 staging 环境执行，不影响生产
# RTO 目标：≤ 4 小时
#============================================================

#---------- 路径与常量 ----------
ENV_FILE="/opt/bracesync/.env"
STAGING_ENV_FILE="/opt/bracesync-staging/.env"
BACKUP_DIR="/opt/bracesync/backup"
LOG_DIR="${BACKUP_DIR}/logs"
STAGING_CONTAINER="bracesync-staging-postgres-1"
DB_NAME="bracesync"
COS_BASE_PATH="bracesync-prod/pg-backup"

#---------- 加载生产环境 COS 配置 ----------
if [ ! -f "$ENV_FILE" ]; then
  echo "[ERROR] 生产 .env 文件不存在: $ENV_FILE"
  exit 1
fi

source <(grep -E '^(COS_SECRET_ID|COS_SECRET_KEY|COS_BUCKET|COS_REGION)=' "$ENV_FILE")

COS_BUCKET_VAL="${COS_BUCKET:-bracesync-prod}"
COS_REGION_VAL="${COS_REGION:-ap-guangzhou}"

#---------- 加载 staging 数据库配置 ----------
if [ ! -f "$STAGING_ENV_FILE" ]; then
  echo "[ERROR] staging .env 文件不存在: $STAGING_ENV_FILE"
  exit 1
fi

source <(grep -E '^(POSTGRES_USER|POSTGRES_PASSWORD)=' "$STAGING_ENV_FILE")
STAGING_DB_USER="${POSTGRES_USER:-bracesync}"
STAGING_DB_PASS="$POSTGRES_PASSWORD"

#---------- 校验 COS 凭据 ----------
if [ "$COS_SECRET_ID" = "prod-placeholder" ] || [ "$COS_SECRET_KEY" = "prod-placeholder" ]; then
  echo "[ERROR] COS 凭据为占位符，请在生产 .env 中配置真实的 COS_SECRET_ID 和 COS_SECRET_KEY"
  exit 1
fi

#---------- 初始化 ----------
mkdir -p "$BACKUP_DIR" "$LOG_DIR"
export PATH="$HOME/.local/bin:$PATH"

NOW=$(date '+%Y-%m-%d_%H%M%S')
LOG_FILE="$LOG_DIR/restore-drill-${NOW}.log"
RTO_START=$(date +%s)

echo "[$(date '+%Y-%m-%d %H:%M:%S')] 恢复演练开始" | tee "$LOG_FILE"
echo "  RTO 计时起点: $(date '+%Y-%m-%d %H:%M:%S')" | tee -a "$LOG_FILE"

#---------- 配置 coscmd ----------
coscmd config -a "$COS_SECRET_ID" -s "$COS_SECRET_KEY" -b "$COS_BUCKET_VAL" -r "$COS_REGION_VAL" 2>&1 | tee -a "$LOG_FILE"

#---------- 获取最新每日备份 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 查找最新备份..." | tee -a "$LOG_FILE"

LATEST_BACKUP=$(coscmd list "${COS_BASE_PATH}/daily/" 2>&1 | grep '.sql.gz' | awk '{print $NF}' | sort -r | head -1)

if [ -z "$LATEST_BACKUP" ]; then
  echo "[ERROR] COS 上未找到任何备份文件" | tee -a "$LOG_FILE"
  exit 1
fi

BACKUP_FILENAME=$(basename "$LATEST_BACKUP")
LOCAL_BACKUP="${BACKUP_DIR}/${BACKUP_FILENAME}"

echo "  最新备份: $LATEST_BACKUP" | tee -a "$LOG_FILE"

#---------- 下载备份 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 下载备份文件..." | tee -a "$LOG_FILE"
coscmd download "$LATEST_BACKUP" "$LOCAL_BACKUP" 2>&1 | tee -a "$LOG_FILE"

DOWNLOAD_SIZE=$(du -h "$LOCAL_BACKUP" | cut -f1)
echo "  下载完成，大小: $DOWNLOAD_SIZE" | tee -a "$LOG_FILE"

#---------- 恢复前检查 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 恢复前检查 staging 数据库..." | tee -a "$LOG_FILE"

# 记录恢复前的表数量
PRE_TABLES=$(docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d "$DB_NAME" -t -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';" 2>&1 | tr -d '[:space:]')
echo "  恢复前表数量: $PRE_TABLES" | tee -a "$LOG_FILE"

#---------- 执行恢复 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 开始恢复到 staging 数据库..." | tee -a "$LOG_FILE"

# 先断开现有连接
docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${DB_NAME}' AND pid <> pg_backend_pid();" 2>&1 | tee -a "$LOG_FILE"

# 删除并重建数据库
docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c "DROP DATABASE IF EXISTS ${DB_NAME};" 2>&1 | tee -a "$LOG_FILE"
docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c "CREATE DATABASE ${DB_NAME};" 2>&1 | tee -a "$LOG_FILE"

# 恢复数据
gunzip -c "$LOCAL_BACKUP" | docker exec -i "$STAGING_CONTAINER" pg_restore \
  -U "$STAGING_DB_USER" \
  -d "$DB_NAME" \
  --no-owner \
  --no-privileges \
  --verbose 2>>"$LOG_FILE"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] 恢复完成" | tee -a "$LOG_FILE"

#---------- 数据完整性验证 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 数据完整性验证..." | tee -a "$LOG_FILE"

# 1. 表数量
POST_TABLES=$(docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d "$DB_NAME" -t -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';" 2>&1 | tr -d '[:space:]')
echo "  恢复后表数量: $POST_TABLES" | tee -a "$LOG_FILE"

# 2. 各表行数汇总
TABLE_ROWS=$(docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d "$DB_NAME" -t -c "
SELECT schemaname || '.' || relname || ': ' || n_tup_ins
FROM pg_stat_user_tables
WHERE schemaname = 'public'
ORDER BY relname;" 2>&1)
echo "  各表行数:" | tee -a "$LOG_FILE"
echo "$TABLE_ROWS" | tee -a "$LOG_FILE"

# 3. 关键表存在性检查
KEY_TABLES=("users" "patients" "devices" "device_data" "alerts" "teams" "messages")
for tbl in "${KEY_TABLES[@]}"; do
  EXISTS=$(docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d "$DB_NAME" -t -c "SELECT to_regclass('public.${tbl}');" 2>&1 | tr -d '[:space:]')
  if [ -n "$EXISTS" ] && [ "$EXISTS" != "" ]; then
    echo "  [OK] 表 ${tbl} 存在" | tee -a "$LOG_FILE"
  else
    echo "  [WARN] 表 ${tbl} 不存在" | tee -a "$LOG_FILE"
  fi
done

# 4. 数据库大小
DB_SIZE=$(docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d "$DB_NAME" -t -c "SELECT pg_size_pretty(pg_database_size('${DB_NAME}'));" 2>&1 | tr -d '[:space:]')
echo "  数据库大小: $DB_SIZE" | tee -a "$LOG_FILE"

#---------- RTO 计算 ----------
RTO_END=$(date +%s)
RTO_SECONDS=$((RTO_END - RTO_START))
RTO_MINUTES=$((RTO_SECONDS / 60))
RTO_HOURS=$((RTO_SECONDS / 3600))
RTO_REMAINDER_MIN=$(( (RTO_SECONDS % 3600) / 60 ))

RTO_STR="${RTO_HOURS}h${RTO_REMAINDER_MIN}m"
RTO_STATUS="PASS"
if [ "$RTO_SECONDS" -gt 14400 ]; then
  RTO_STATUS="FAIL"
fi

echo "" | tee -a "$LOG_FILE"
echo "========== 恢复演练报告 ==========" | tee -a "$LOG_FILE"
echo "  备份文件: $BACKUP_FILENAME" | tee -a "$LOG_FILE"
echo "  备份大小: $DOWNLOAD_SIZE" | tee -a "$LOG_FILE"
echo "  恢复前表数: $PRE_TABLES" | tee -a "$LOG_FILE"
echo "  恢复后表数: $POST_TABLES" | tee -a "$LOG_FILE"
echo "  数据库大小: $DB_SIZE" | tee -a "$LOG_FILE"
echo "  RTO: ${RTO_STR} (${RTO_STATUS}, 目标 ≤4h)" | tee -a "$LOG_FILE"
echo "  演练时间: $(date '+%Y-%m-%d %H:%M:%S')" | tee -a "$LOG_FILE"
echo "===================================" | tee -a "$LOG_FILE"

#---------- 清理本地下载文件 ----------
rm -f "$LOCAL_BACKUP"
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 清理临时文件完成" | tee -a "$LOG_FILE"
