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

# coscmd list 每个对象一行，列为「对象键 大小 存储类别 日期 时间」；
# 按列号取值会拿到时间列（如 15:22:02），故直接抽取对象键 token，与列顺序无关。
BACKUP_KEYS=$(coscmd list "${COS_BASE_PATH}/daily/" 2>&1 | grep -oE '[^[:space:]]+\.sql\.gz' | sort -r || true)

LATEST_BACKUP=$(echo "$BACKUP_KEYS" | head -1)

if [ -z "$LATEST_BACKUP" ]; then
  echo "[ERROR] COS 上未找到任何备份文件" | tee -a "$LOG_FILE"
  exit 1
fi

if [ "$(echo "$BACKUP_KEYS" | wc -l)" -gt 1 ]; then
  echo "  COS daily 候选备份 $(echo "$BACKUP_KEYS" | wc -l) 个:" | tee -a "$LOG_FILE"
  echo "$BACKUP_KEYS" | sed 's/^/    /' | tee -a "$LOG_FILE"
fi

BACKUP_FILENAME=$(basename "$LATEST_BACKUP")
# 下载到演练专用子目录并带本次运行时间戳：备份目录里同名文件可能已存在（当天 pg_dump
# 落地的副本），coscmd download 非 -f 时遇同名文件会直接报错退出。
DRILL_DL_DIR="${BACKUP_DIR}/restore-drill"
mkdir -p "$DRILL_DL_DIR"
find "$DRILL_DL_DIR" -name '*.sql.gz' -mtime +3 -delete 2>/dev/null || true
LOCAL_BACKUP="${DRILL_DL_DIR}/${NOW}-${BACKUP_FILENAME}"

echo "  最新备份: $LATEST_BACKUP" | tee -a "$LOG_FILE"

#---------- 下载备份 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 下载备份文件..." | tee -a "$LOG_FILE"
if ! coscmd download -f "$LATEST_BACKUP" "$LOCAL_BACKUP" 2>&1 | tee -a "$LOG_FILE"; then
  echo "[ERROR] 从 COS 下载备份失败，中止演练（staging 未改动）" | tee -a "$LOG_FILE"
  exit 1
fi

DOWNLOAD_SIZE=$(du -h "$LOCAL_BACKUP" | cut -f1)
echo "  下载完成，大小: $DOWNLOAD_SIZE" | tee -a "$LOG_FILE"

#---------- 下载物有效性校验（必须在破坏性 DROP 之前）----------
if ! gunzip -t "$LOCAL_BACKUP" 2>>"$LOG_FILE"; then
  echo "[ERROR] 下载物 gzip 完整性校验失败，中止演练（staging 未改动）" | tee -a "$LOG_FILE"
  exit 1
fi
if [ "$(gunzip -c "$LOCAL_BACKUP" | head -c 4)" != "PGDM" ]; then
  echo "[ERROR] 下载物不是有效的 pg_dump custom 归档（缺 PGDM 魔数），中止演练（staging 未改动）" | tee -a "$LOG_FILE"
  exit 1
fi
TOC_LIST=$(docker exec -i "$STAGING_CONTAINER" pg_restore --list < "$LOCAL_BACKUP" 2>>"$LOG_FILE" || true)
if [ -z "$TOC_LIST" ]; then
  echo "[ERROR] pg_restore --list 读不出归档目录，中止演练（staging 未改动）" | tee -a "$LOG_FILE"
  exit 1
fi
ARCHIVE_CREATED=$(echo "$TOC_LIST" | grep -m1 'Archive created at' || echo 'Archive created at: 未知')
ARCHIVE_TABLES=$(echo "$TOC_LIST" | grep -cE '^[0-9]+; [0-9]+ [0-9]+ TABLE public ' || true)
echo "  归档校验通过（PGDM 魔数 + gzip 完整）" | tee -a "$LOG_FILE"
echo "  $ARCHIVE_CREATED" | tee -a "$LOG_FILE"
echo "  归档内 public 表对象数: $ARCHIVE_TABLES" | tee -a "$LOG_FILE"

#---------- 恢复前检查 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 恢复前检查 staging 数据库..." | tee -a "$LOG_FILE"

# 逐表真实行数（pg_stat_user_tables.n_tup_ins 是累计插入计数，不等于当前行数）
db_row_counts() {
  local tbls row t n
  tbls=$(docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d "$DB_NAME" -t -A \
    -c "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename;" 2>>"$LOG_FILE" || true)
  for t in $tbls; do
    [ -n "$t" ] || continue
    n=$(docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d "$DB_NAME" -t -A \
      -c "SELECT count(*) FROM public.\"$t\";" 2>>"$LOG_FILE" || echo ERR)
    printf '%s=%s\n' "$t" "$n"
  done
}

PRE_COUNTS=$(db_row_counts)
PRE_TABLES=$(echo "$PRE_COUNTS" | grep -c '=' || true)
PRE_ROWS=$(echo "$PRE_COUNTS" | cut -d= -f2 | awk '{s+=$1} END {print s+0}')
echo "  恢复前表数量: $PRE_TABLES，总行数: $PRE_ROWS" | tee -a "$LOG_FILE"

#---------- 执行恢复 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 开始恢复到 staging 数据库..." | tee -a "$LOG_FILE"

# 先断开现有连接
docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${DB_NAME}' AND pid <> pg_backend_pid();" 2>&1 | tee -a "$LOG_FILE"

# 删除并重建数据库
docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c "DROP DATABASE IF EXISTS ${DB_NAME};" 2>&1 | tee -a "$LOG_FILE"
docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c "CREATE DATABASE ${DB_NAME};" 2>&1 | tee -a "$LOG_FILE"

# 恢复数据（pg_restore 退出码单独接住，保证报告完整落盘后再判定）
RESTORE_ERR_FILE="${LOG_DIR}/restore-drill-${NOW}.pgrestore.log"
RESTORE_RC=0
gunzip -c "$LOCAL_BACKUP" | docker exec -i "$STAGING_CONTAINER" pg_restore \
  -U "$STAGING_DB_USER" \
  -d "$DB_NAME" \
  --no-owner \
  --no-privileges \
  --verbose > "$RESTORE_ERR_FILE" 2>&1 || RESTORE_RC=$?
cat "$RESTORE_ERR_FILE" >> "$LOG_FILE"
RESTORE_ERRORS=$(grep -cE '^pg_restore(:| ) (error|warning)' "$RESTORE_ERR_FILE" || true)
rm -f "$RESTORE_ERR_FILE"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] 恢复完成（pg_restore 退出码: ${RESTORE_RC}，error/warning 行数: ${RESTORE_ERRORS}）" | tee -a "$LOG_FILE"

#---------- 数据完整性验证 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 数据完整性验证..." | tee -a "$LOG_FILE"

# 1. 表数量 + 真实行数
POST_COUNTS=$(db_row_counts)
POST_TABLES=$(echo "$POST_COUNTS" | grep -c '=' || true)
POST_ROWS=$(echo "$POST_COUNTS" | cut -d= -f2 | awk '{s+=$1} END {print s+0}')
echo "  恢复后表数量: $POST_TABLES，总行数: $POST_ROWS" | tee -a "$LOG_FILE"
echo "  归档声明 public 表数: $ARCHIVE_TABLES" | tee -a "$LOG_FILE"

# 2. 恢复后逐表行数（真实 count(*)，非统计计数器）
echo "  恢复后各表行数:" | tee -a "$LOG_FILE"
echo "$POST_COUNTS" | sed 's/^/    /' | tee -a "$LOG_FILE"

# 3. 关键表行数 恢复前 -> 恢复后
# （原清单里的 users / device_data / messages 三表在本库 schema 中并不存在，恒为 WARN，
#   换成实际存在且承载业务的表）
KEY_TABLES=("patients" "devices" "alerts" "teams" "admins" "audit_logs")
KEY_TABLES_MISS=0
echo "  关键表对照:" | tee -a "$LOG_FILE"
for tbl in "${KEY_TABLES[@]}"; do
  PRE_N=$(echo "$PRE_COUNTS" | grep -m1 "^${tbl}=" | cut -d= -f2 || true)
  POST_N=$(echo "$POST_COUNTS" | grep -m1 "^${tbl}=" | cut -d= -f2 || true)
  if [ -z "$POST_N" ]; then
    echo "    [WARN] 表 ${tbl} 恢复后不存在（该表可能已改名，见恢复后各表行数）" | tee -a "$LOG_FILE"
    KEY_TABLES_MISS=$((KEY_TABLES_MISS + 1))
  else
    echo "    ${tbl}: ${PRE_N:-无} -> ${POST_N}" | tee -a "$LOG_FILE"
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

#---------- 演练判定 ----------
DRILL_STATUS="PASS"
DRILL_NOTES=()
if [ "$RESTORE_RC" -ne 0 ]; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("pg_restore 退出码 ${RESTORE_RC}"); fi
if [ "$RESTORE_ERRORS" -ne 0 ]; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("pg_restore 报出 ${RESTORE_ERRORS} 条 error/warning"); fi
if [ "$POST_TABLES" -eq 0 ]; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("恢复后 0 张表"); fi
if [ "$POST_TABLES" -lt "$ARCHIVE_TABLES" ]; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("恢复后表数 ${POST_TABLES} 少于归档声明 ${ARCHIVE_TABLES}"); fi
if echo "$POST_COUNTS" | grep -q '=ERR$'; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("存在 count(*) 失败的表"); fi
if [ "$KEY_TABLES_MISS" -ne 0 ]; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("${KEY_TABLES_MISS} 张关键表缺失"); fi

RTO_STR="${RTO_HOURS}h${RTO_REMAINDER_MIN}m"
RTO_STATUS="PASS"
if [ "$RTO_SECONDS" -gt 14400 ]; then
  RTO_STATUS="FAIL"
fi

echo "" | tee -a "$LOG_FILE"
echo "========== 恢复演练报告 ==========" | tee -a "$LOG_FILE"
echo "  备份对象: $LATEST_BACKUP" | tee -a "$LOG_FILE"
echo "  备份文件: $BACKUP_FILENAME" | tee -a "$LOG_FILE"
echo "  备份大小: $DOWNLOAD_SIZE" | tee -a "$LOG_FILE"
echo "  恢复前表数: $PRE_TABLES（总行数 $PRE_ROWS）" | tee -a "$LOG_FILE"
echo "  恢复后表数: $POST_TABLES（总行数 $POST_ROWS，归档声明 $ARCHIVE_TABLES）" | tee -a "$LOG_FILE"
echo "  数据库大小: $DB_SIZE" | tee -a "$LOG_FILE"
echo "  RTO: ${RTO_STR} (${RTO_STATUS}, 目标 ≤4h)" | tee -a "$LOG_FILE"
echo "  演练结果: ${DRILL_STATUS}" | tee -a "$LOG_FILE"
if [ "${#DRILL_NOTES[@]}" -gt 0 ]; then
  printf '    - %s\n' "${DRILL_NOTES[@]}" | tee -a "$LOG_FILE"
fi
echo "  演练时间: $(date '+%Y-%m-%d %H:%M:%S')" | tee -a "$LOG_FILE"
echo "  日志: $LOG_FILE" | tee -a "$LOG_FILE"
echo "===================================" | tee -a "$LOG_FILE"

#---------- 清理本地下载文件 ----------
rm -f "$LOCAL_BACKUP"
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 清理临时文件完成" | tee -a "$LOG_FILE"

if [ "$DRILL_STATUS" != "PASS" ]; then
  echo "[ERROR] 恢复演练未通过，详见 $LOG_FILE" | tee -a "$LOG_FILE"
  exit 1
fi
