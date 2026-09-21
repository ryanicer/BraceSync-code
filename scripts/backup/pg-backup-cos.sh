#!/bin/bash
set -euo pipefail

#============================================================
# BraceSync 生产库 pg_dump → COS 每日备份管道
# 保留策略：7 日 / 4 周 / 3 月
# 安全约束：生产只做只读 dump，不执行任何写操作
#============================================================

#---------- 路径与常量 ----------
ENV_FILE="/opt/bracesync/.env"
BACKUP_DIR="/opt/bracesync/backup"
LOG_DIR="${BACKUP_DIR}/logs"
CONTAINER="bracesync-postgres-1"
DB_NAME="bracesync"
COS_BASE_PATH="bracesync-prod/pg-backup"
RETENTION_DAILY=7
RETENTION_WEEKLY=4
RETENTION_MONTHLY=3

#---------- 加载环境变量 ----------
if [ ! -f "$ENV_FILE" ]; then
  echo "[ERROR] .env 文件不存在: $ENV_FILE" | tee -a "$LOG_DIR/backup-error.log"
  exit 1
fi

source <(grep -E '^(POSTGRES_USER|POSTGRES_PASSWORD|COS_SECRET_ID|COS_SECRET_KEY|COS_BUCKET|COS_REGION)=' "$ENV_FILE")

DB_USER="${POSTGRES_USER:-bracesync}"
DB_PASS="$POSTGRES_PASSWORD"
COS_BUCKET_VAL="${COS_BUCKET:-bracesync-prod}"
COS_REGION_VAL="${COS_REGION:-ap-guangzhou}"

#---------- 校验 COS 凭据 ----------
if [ "$COS_SECRET_ID" = "prod-placeholder" ] || [ "$COS_SECRET_KEY" = "prod-placeholder" ]; then
  echo "[ERROR] COS 凭据为占位符，请在 .env 中配置真实的 COS_SECRET_ID 和 COS_SECRET_KEY" | tee -a "$LOG_DIR/backup-error.log"
  exit 1
fi

#---------- 初始化 ----------
mkdir -p "$BACKUP_DIR" "$LOG_DIR"
export PATH="$HOME/.local/bin:$PATH"

NOW=$(date '+%Y-%m-%d_%H%M%S')
TODAY=$(date '+%Y-%m-%d')
WEEK_NUM=$(date '+%W')
MONTH=$(date '+%Y-%m')
LOG_FILE="$LOG_DIR/backup-${NOW}.log"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] 备份开始" | tee "$LOG_FILE"

#---------- 配置 coscmd ----------
coscmd config -a "$COS_SECRET_ID" -s "$COS_SECRET_KEY" -b "$COS_BUCKET_VAL" -r "$COS_REGION_VAL" 2>&1 | tee -a "$LOG_FILE"

#---------- 执行 pg_dump（只读）----------
DUMP_FILE="bracesync-${TODAY}.sql.gz"
DUMP_PATH="${BACKUP_DIR}/${DUMP_FILE}"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] 执行 pg_dump（只读模式）..." | tee -a "$LOG_FILE"

docker exec "$CONTAINER" pg_dump \
  -U "$DB_USER" \
  -d "$DB_NAME" \
  --format=custom \
  --no-owner \
  --no-privileges \
  --verbose 2>>"$LOG_FILE" \
  | gzip > "$DUMP_PATH"

DUMP_SIZE=$(du -h "$DUMP_PATH" | cut -f1)
echo "[$(date '+%Y-%m-%d %H:%M:%S')] pg_dump 完成，文件大小: $DUMP_SIZE" | tee -a "$LOG_FILE"

#---------- 上传到 COS（三级保留目录）----------
# 1. 每日备份
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 上传到 COS daily 目录..." | tee -a "$LOG_FILE"
coscmd upload "$DUMP_PATH" "${COS_BASE_PATH}/daily/${DUMP_FILE}" 2>&1 | tee -a "$LOG_FILE"

# 2. 每周备份（每周一上传到 weekly 目录）
DAY_OF_WEEK=$(date '+%u')
if [ "$DAY_OF_WEEK" = "1" ]; then
  WEEK_FILE="bracesync-week-${WEEK_NUM}.sql.gz"
  cp "$DUMP_PATH" "${BACKUP_DIR}/${WEEK_FILE}"
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] 上传到 COS weekly 目录..." | tee -a "$LOG_FILE"
  coscmd upload "${BACKUP_DIR}/${WEEK_FILE}" "${COS_BASE_PATH}/weekly/${WEEK_FILE}" 2>&1 | tee -a "$LOG_FILE"
  rm -f "${BACKUP_DIR}/${WEEK_FILE}"
fi

# 3. 每月备份（每月 1 日上传到 monthly 目录）
DAY_OF_MONTH=$(date '+%d')
if [ "$DAY_OF_MONTH" = "01" ]; then
  MONTH_FILE="bracesync-month-${MONTH}.sql.gz"
  cp "$DUMP_PATH" "${BACKUP_DIR}/${MONTH_FILE}"
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] 上传到 COS monthly 目录..." | tee -a "$LOG_FILE"
  coscmd upload "${BACKUP_DIR}/${MONTH_FILE}" "${COS_BASE_PATH}/monthly/${MONTH_FILE}" 2>&1 | tee -a "$LOG_FILE"
  rm -f "${BACKUP_DIR}/${MONTH_FILE}"
fi

#---------- 清理过期备份 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 清理过期备份..." | tee -a "$LOG_FILE"

# 清理本地临时文件（只保留最近 3 天）
find "$BACKUP_DIR" -name 'bracesync-*.sql.gz' -mtime +3 -delete 2>&1 | tee -a "$LOG_FILE"

# 清理 COS 过期备份
# coscmd list 每个对象一行，列为「对象键 大小 存储类别 日期 时间」；原先取 awk '{print $NF}'
# 拿到的是时间列（如 02:00:02），既删不掉对象又会让后面的日期匹配失败。
# 统一抽对象键 token，与列顺序无关。
# DRY_RUN=1 只报告不删除。
DRY_RUN="${DRY_RUN:-0}"
PRUNED=0
PRUNE_FAILED=0

prune_report() {
  if [ "$DRY_RUN" = "1" ]; then
    echo "  [DRY_RUN] 本应删除 $1: $2 ($3)" | tee -a "$LOG_FILE"
  else
    echo "  删除过期 $1: $2 ($3)" | tee -a "$LOG_FILE"
  fi
}

# 清理 COS daily（保留 7 天）
COS_DAILY_KEYS=$(coscmd list "${COS_BASE_PATH}/daily/" 2>&1 | grep -oE '[^[:space:]]+\.sql\.gz' | sort || true)
for f in $COS_DAILY_KEYS; do
  FILE_DATE=$(echo "$f" | grep -oE '[0-9]{4}-[0-9]{2}-[0-9]{2}' | tail -1 || true)
  if [ -n "$FILE_DATE" ]; then
    AGE_DAYS=$(( ($(date +%s) - $(date -d "$FILE_DATE" +%s)) / 86400 ))
    if [ "$AGE_DAYS" -gt "$RETENTION_DAILY" ]; then
      prune_report "daily" "$f" "${AGE_DAYS}天"
      if [ "$DRY_RUN" != "1" ]; then
        if coscmd delete "$f" >>"$LOG_FILE" 2>&1; then
          PRUNED=$((PRUNED + 1))
        else
          PRUNE_FAILED=$((PRUNE_FAILED + 1))
          echo "  [ERROR] 删除失败: $f" | tee -a "$LOG_FILE"
        fi
      fi
    fi
  fi
done

# 清理 COS weekly（保留 4 周）
COS_WEEKLY_KEYS=$(coscmd list "${COS_BASE_PATH}/weekly/" 2>&1 | grep -oE '[^[:space:]]+\.sql\.gz' | sort || true)
for f in $COS_WEEKLY_KEYS; do
  WEEK_STR=$(echo "$f" | grep -oE 'week-[0-9]{2}' | tail -1 || true)
  if [ -n "$WEEK_STR" ]; then
    WEEK_NUM_FILE=$(echo "$WEEK_STR" | grep -oE '[0-9]{2}' | tail -1 || true)
    CURRENT_WEEK=$(date '+%W')
    AGE_WEEKS=$(( (10#$CURRENT_WEEK - 10#$WEEK_NUM_FILE + 53) % 53 ))
    if [ "$AGE_WEEKS" -gt "$RETENTION_WEEKLY" ]; then
      prune_report "weekly" "$f" "${AGE_WEEKS}周"
      if [ "$DRY_RUN" != "1" ]; then
        if coscmd delete "$f" >>"$LOG_FILE" 2>&1; then
          PRUNED=$((PRUNED + 1))
        else
          PRUNE_FAILED=$((PRUNE_FAILED + 1))
          echo "  [ERROR] 删除失败: $f" | tee -a "$LOG_FILE"
        fi
      fi
    fi
  fi
done

# 清理 COS monthly（保留 3 月）
COS_MONTHLY_KEYS=$(coscmd list "${COS_BASE_PATH}/monthly/" 2>&1 | grep -oE '[^[:space:]]+\.sql\.gz' | sort || true)
for f in $COS_MONTHLY_KEYS; do
  MONTH_STR=$(echo "$f" | grep -oE 'month-[0-9]{4}-[0-9]{2}' | tail -1 || true)
  if [ -n "$MONTH_STR" ]; then
    MONTH_FILE=$(echo "$MONTH_STR" | grep -oE '[0-9]{4}-[0-9]{2}' | tail -1 || true)
    AGE_MONTHS=$(( (10#$(date '+%Y') * 12 + 10#$(date '+%m')) - (10#$(echo "$MONTH_FILE" | cut -d'-' -f1) * 12 + 10#$(echo "$MONTH_FILE" | cut -d'-' -f2)) ))
    if [ "$AGE_MONTHS" -gt "$RETENTION_MONTHLY" ]; then
      prune_report "monthly" "$f" "${AGE_MONTHS}月"
      if [ "$DRY_RUN" != "1" ]; then
        if coscmd delete "$f" >>"$LOG_FILE" 2>&1; then
          PRUNED=$((PRUNED + 1))
        else
          PRUNE_FAILED=$((PRUNE_FAILED + 1))
          echo "  [ERROR] 删除失败: $f" | tee -a "$LOG_FILE"
        fi
      fi
    fi
  fi
done

echo "  清理小结: 删除 ${PRUNED} 个，失败 ${PRUNE_FAILED} 个（保留策略 ${RETENTION_DAILY}日/${RETENTION_WEEKLY}周/${RETENTION_MONTHLY}月；DRY_RUN=${DRY_RUN}）" | tee -a "$LOG_FILE"
if [ "$PRUNE_FAILED" -ne 0 ]; then
  echo "[ERROR] 存在删除失败的过期备份，详见 $LOG_FILE" | tee -a "$LOG_FILE"
  exit 1
fi

#---------- 清理旧日志（保留 30 天）----------
find "$LOG_DIR" -name 'backup-*.log' -mtime +30 -delete 2>&1 || true

#---------- 完成汇总 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 备份完成" | tee -a "$LOG_FILE"
echo "  文件: $DUMP_FILE" | tee -a "$LOG_FILE"
echo "  大小: $DUMP_SIZE" | tee -a "$LOG_FILE"
echo "  COS:  cos://${COS_BUCKET_VAL}/${COS_BASE_PATH}/daily/${DUMP_FILE}" | tee -a "$LOG_FILE"
