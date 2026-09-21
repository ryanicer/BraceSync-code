#!/bin/bash
set -euo pipefail

#============================================================
# BraceSync 恢复演练脚本（T292：恢复到独立演练库）
# 目标：从 COS 拉取生产备份 → 恢复到 staging 实例内的独立演练库 → 验证数据
# 约束：只在 staging 环境执行，不影响生产；
#       staging 现库（bracesync）全程只读，仅在演练前后各做一次只读行数对照
# RTO 目标：≤ 4 小时
# 历史：T283 及之前，恢复目标就是 staging 现库（先删库建库），每周六 03:00 会把
#       e2e-real 依赖的 seed 演示数据整库覆盖掉 —— Boss 2026-09-21 裁定改为独立库。
#============================================================

#---------- 路径与常量 ----------
ENV_FILE="/opt/bracesync/.env"
STAGING_ENV_FILE="/opt/bracesync-staging/.env"
BACKUP_DIR="/opt/bracesync/backup"
LOG_DIR="${BACKUP_DIR}/logs"
STAGING_CONTAINER="bracesync-staging-postgres-1"
# staging 现库：演练只读对照，任何 DROP / CREATE / pg_restore 都不许指向它
STAGING_DB_NAME="bracesync"
# 演练专用库：本次恢复目标，演练结束即删。
# 可用环境变量覆盖，只为让下面的防呆能被真机验证（正常调度不要设）。
DRILL_DB_NAME="${DRILL_DB_NAME:-bracesync_drill}"
COS_BASE_PATH="bracesync-prod/pg-backup"

# 红线防呆：可被 DROP / CREATE / pg_restore 的库名必须含 drill。
# 此处刻意不写日志（LOG_FILE 还没建），只 echo —— cron 的 stdout 落到 cron-restore.log。
assert_drill_db_name() {
  local db="$1"
  case "$db" in
    *drill*)
      return 0 ;;
    *staging*|*bracesync*)
      echo "[ERROR] 拒绝操作数据库 ${db}：演练库名必须含 drill，staging/生产库不可作为恢复目标"
      exit 1 ;;
  esac
  case "$db" in
    postgres|template0|template1)
      echo "[ERROR] 拒绝操作数据库 ${db}：系统库不可作为恢复目标"
      exit 1 ;;
  esac
  if [ "$db" = "$STAGING_DB_NAME" ]; then
    echo "[ERROR] 拒绝操作数据库 ${db}：不得等于 staging 现库名"
    exit 1
  fi
  echo "[ERROR] 拒绝操作数据库 ${db}：库名不含 drill"
  exit 1
}
assert_drill_db_name "$DRILL_DB_NAME"

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

LOCAL_BACKUP=""
DRILL_DB_CREATED=0

# 退出时清理（成功、失败、中止都走这里）：
#   1) 本地下载物是生产库副本，不能留在盘上
#   2) 演练库用完即删，失败也不留（日志里已有全部核对结论）
on_exit() {
  if [ -n "$LOCAL_BACKUP" ]; then
    rm -f "$LOCAL_BACKUP"
  fi
  if [ "$DRILL_DB_CREATED" = "1" ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] 删除演练库 ${DRILL_DB_NAME}" | tee -a "$LOG_FILE"
    docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c \
      "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${DRILL_DB_NAME}' AND pid <> pg_backend_pid();" \
      >/dev/null 2>&1 || true
    docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c \
      "DROP DATABASE IF EXISTS ${DRILL_DB_NAME};" >/dev/null 2>&1 || true
  fi
}
trap on_exit EXIT

echo "[$(date '+%Y-%m-%d %H:%M:%S')] 恢复演练开始" | tee "$LOG_FILE"
echo "  恢复目标库: ${DRILL_DB_NAME}（独立演练库，staging 现库 ${STAGING_DB_NAME} 只读对照）" | tee -a "$LOG_FILE"
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
chmod 700 "$DRILL_DL_DIR"
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
chmod 600 "$LOCAL_BACKUP"
echo "  下载完成，大小: $DOWNLOAD_SIZE" | tee -a "$LOG_FILE"

#---------- 下载物有效性校验（必须在建库/恢复之前）----------
if ! gunzip -t "$LOCAL_BACKUP" 2>>"$LOG_FILE"; then
  echo "[ERROR] 下载物 gzip 完整性校验失败，中止演练（staging 未改动）" | tee -a "$LOG_FILE"
  exit 1
fi
if [ "$(gunzip -c "$LOCAL_BACKUP" | head -c 4)" != "PGDM" ]; then
  echo "[ERROR] 下载物不是有效的 pg_dump custom 归档（缺 PGDM 魔数），中止演练（staging 未改动）" | tee -a "$LOG_FILE"
  exit 1
fi
# 备份外层是 gzip（pg_dump custom 归档），pg_restore 不能直接读 .gz，须先解一层
TOC_LIST=$(gunzip -c "$LOCAL_BACKUP" | docker exec -i "$STAGING_CONTAINER" pg_restore --list 2>>"$LOG_FILE" || true)
if [ -z "$TOC_LIST" ]; then
  echo "[ERROR] pg_restore --list 读不出归档目录，中止演练（staging 未改动）" | tee -a "$LOG_FILE"
  exit 1
fi
ARCHIVE_CREATED=$(echo "$TOC_LIST" | grep -m1 'Archive created at' || echo 'Archive created at: 未知')
ARCHIVE_TABLES=$(echo "$TOC_LIST" | grep -cE '^[0-9]+; [0-9]+ [0-9]+ TABLE public ' || true)
echo "  归档校验通过（PGDM 魔数 + gzip 完整）" | tee -a "$LOG_FILE"
echo "  $ARCHIVE_CREATED" | tee -a "$LOG_FILE"
echo "  归档内 public 表对象数: $ARCHIVE_TABLES" | tee -a "$LOG_FILE"

#---------- 逐表真实行数工具 ----------
# $1 = 库名（pg_stat_user_tables.n_tup_ins 是累计插入计数，不等于当前行数，故用 count(*)）
db_row_counts() {
  local db="$1" tbls t n
  tbls=$(docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d "$db" -t -A \
    -c "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename;" 2>>"$LOG_FILE" || true)
  for t in $tbls; do
    [ -n "$t" ] || continue
    n=$(docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d "$db" -t -A \
      -c "SELECT count(*) FROM public.\"$t\";" 2>>"$LOG_FILE" || echo ERR)
    printf '%s=%s\n' "$t" "$n"
  done
}

#---------- 演练前快照：staging 现库只读对照基线 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 演练前快照 staging 现库 ${STAGING_DB_NAME}（只读，不建不删）..." | tee -a "$LOG_FILE"
STAGING_PRE=$(db_row_counts "$STAGING_DB_NAME")
STAGING_PRE_TABLES=$(echo "$STAGING_PRE" | grep -c '=' || true)
STAGING_PRE_ROWS=$(echo "$STAGING_PRE" | cut -d= -f2 | awk '{s+=$1} END {print s+0}')
echo "  staging 现库表数量: $STAGING_PRE_TABLES，总行数: $STAGING_PRE_ROWS" | tee -a "$LOG_FILE"

#---------- 执行恢复（目标 = 独立演练库）----------
assert_drill_db_name "$DRILL_DB_NAME"
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 创建独立演练库 ${DRILL_DB_NAME}..." | tee -a "$LOG_FILE"

# 上一次若异常退出可能残留演练库（不会是 staging 库），断开连接后重建
docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c \
  "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${DRILL_DB_NAME}' AND pid <> pg_backend_pid();" \
  2>&1 | tee -a "$LOG_FILE"
docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c \
  "DROP DATABASE IF EXISTS ${DRILL_DB_NAME};" 2>&1 | tee -a "$LOG_FILE"
docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d postgres -c \
  "CREATE DATABASE ${DRILL_DB_NAME};" 2>&1 | tee -a "$LOG_FILE"
DRILL_DB_CREATED=1

# 恢复数据（pg_restore 退出码单独接住，保证报告完整落盘后再判定）
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 开始恢复到独立演练库 ${DRILL_DB_NAME}..." | tee -a "$LOG_FILE"
RESTORE_ERR_FILE="${LOG_DIR}/restore-drill-${NOW}.pgrestore.log"
RESTORE_RC=0
gunzip -c "$LOCAL_BACKUP" | docker exec -i "$STAGING_CONTAINER" pg_restore \
  -U "$STAGING_DB_USER" \
  -d "$DRILL_DB_NAME" \
  --no-owner \
  --no-privileges \
  --verbose > "$RESTORE_ERR_FILE" 2>&1 || RESTORE_RC=$?
cat "$RESTORE_ERR_FILE" >> "$LOG_FILE"
RESTORE_ERRORS=$(grep -cE '^pg_restore(:| ) (error|warning)' "$RESTORE_ERR_FILE" || true)
rm -f "$RESTORE_ERR_FILE"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] 恢复完成（pg_restore 退出码: ${RESTORE_RC}，error/warning 行数: ${RESTORE_ERRORS}）" | tee -a "$LOG_FILE"

#---------- 数据完整性验证（针对演练库）----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 数据完整性验证（演练库 ${DRILL_DB_NAME}）..." | tee -a "$LOG_FILE"

DRILL_COUNTS=$(db_row_counts "$DRILL_DB_NAME")
DRILL_TABLES=$(echo "$DRILL_COUNTS" | grep -c '=' || true)
DRILL_ROWS=$(echo "$DRILL_COUNTS" | cut -d= -f2 | awk '{s+=$1} END {print s+0}')
echo "  演练库表数量: $DRILL_TABLES，总行数: $DRILL_ROWS" | tee -a "$LOG_FILE"
echo "  归档声明 public 表数: $ARCHIVE_TABLES" | tee -a "$LOG_FILE"

echo "  演练库各表行数:" | tee -a "$LOG_FILE"
echo "$DRILL_COUNTS" | sed 's/^/    /' | tee -a "$LOG_FILE"

# 关键表（users / device_data / messages 在本库 schema 中不存在，换成实际承载业务的表）
KEY_TABLES=("patients" "devices" "alerts" "teams" "admins" "audit_logs")
KEY_TABLES_MISS=0
echo "  关键表核对（演练库）:" | tee -a "$LOG_FILE"
for tbl in "${KEY_TABLES[@]}"; do
  DRILL_N=$(echo "$DRILL_COUNTS" | grep -m1 "^${tbl}=" | cut -d= -f2 || true)
  if [ -z "$DRILL_N" ]; then
    echo "    [WARN] 表 ${tbl} 恢复后不存在（该表可能已改名，见演练库各表行数）" | tee -a "$LOG_FILE"
    KEY_TABLES_MISS=$((KEY_TABLES_MISS + 1))
  else
    echo "    ${tbl}: ${DRILL_N} 行" | tee -a "$LOG_FILE"
  fi
done

DB_SIZE=$(docker exec "$STAGING_CONTAINER" psql -U "$STAGING_DB_USER" -d "$DRILL_DB_NAME" -t -c \
  "SELECT pg_size_pretty(pg_database_size('${DRILL_DB_NAME}'));" 2>&1 | tr -d '[:space:]')
echo "  演练库大小: $DB_SIZE" | tee -a "$LOG_FILE"

#---------- RTO 计算（删库清理不计入 RTO）----------
RTO_END=$(date +%s)
RTO_SECONDS=$((RTO_END - RTO_START))
RTO_MINUTES=$((RTO_SECONDS / 60))
RTO_HOURS=$((RTO_SECONDS / 3600))
RTO_REMAINDER_MIN=$(( (RTO_SECONDS % 3600) / 60 ))

#---------- staging 现库未被动过的对照 ----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 演练后复核 staging 现库 ${STAGING_DB_NAME}（只读）..." | tee -a "$LOG_FILE"
STAGING_POST=$(db_row_counts "$STAGING_DB_NAME")
STAGING_POST_TABLES=$(echo "$STAGING_POST" | grep -c '=' || true)
STAGING_POST_ROWS=$(echo "$STAGING_POST" | cut -d= -f2 | awk '{s+=$1} END {print s+0}')
echo "  staging 现库表数量: $STAGING_POST_TABLES，总行数: $STAGING_POST_ROWS" | tee -a "$LOG_FILE"
if [ "$STAGING_PRE" = "$STAGING_POST" ]; then
  STAGING_UNCHANGED=1
  echo "  [OK] staging 现库逐表行数与演练前完全一致（本脚本对 staging 只执行了 SELECT）" | tee -a "$LOG_FILE"
else
  STAGING_UNCHANGED=0
  echo "  [WARN] staging 现库逐表行数与演练前不一致，差异（- 演练前 / + 演练后）:" | tee -a "$LOG_FILE"
  diff <(echo "$STAGING_PRE") <(echo "$STAGING_POST") | sed 's/^/    /' | tee -a "$LOG_FILE" || true
  echo "  [WARN] 本脚本对 staging 现库只执行了 SELECT count(*)，不一致来自演练窗口内的并发写入（如 e2e-real / 人工操作），需人工核查" | tee -a "$LOG_FILE"
fi

#---------- 演练判定 ----------
DRILL_STATUS="PASS"
DRILL_NOTES=()
if [ "$RESTORE_RC" -ne 0 ]; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("pg_restore 退出码 ${RESTORE_RC}"); fi
if [ "$RESTORE_ERRORS" -ne 0 ]; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("pg_restore 报出 ${RESTORE_ERRORS} 条 error/warning"); fi
if [ "$DRILL_TABLES" -eq 0 ]; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("演练库 0 张表"); fi
if [ "$DRILL_TABLES" -lt "$ARCHIVE_TABLES" ]; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("演练库表数 ${DRILL_TABLES} 少于归档声明 ${ARCHIVE_TABLES}"); fi
if echo "$DRILL_COUNTS" | grep -q '=ERR$'; then DRILL_STATUS="FAIL"; DRILL_NOTES+=("存在 count(*) 失败的表"); fi
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
echo "  恢复目标库: ${DRILL_DB_NAME}（独立演练库，报告打印后删除）" | tee -a "$LOG_FILE"
echo "  演练库表数: $DRILL_TABLES（总行数 $DRILL_ROWS，归档声明 $ARCHIVE_TABLES）" | tee -a "$LOG_FILE"
echo "  演练库大小: $DB_SIZE" | tee -a "$LOG_FILE"
echo "  staging 现库: ${STAGING_DB_NAME} 演练前后 ${STAGING_PRE_TABLES}表/${STAGING_PRE_ROWS}行 -> ${STAGING_POST_TABLES}表/${STAGING_POST_ROWS}行（未变动: $([ "$STAGING_UNCHANGED" = "1" ] && echo 是 || echo 否)）" | tee -a "$LOG_FILE"
echo "  RTO: ${RTO_STR} (${RTO_STATUS}, 目标 ≤4h)" | tee -a "$LOG_FILE"
echo "  演练结果: ${DRILL_STATUS}" | tee -a "$LOG_FILE"
if [ "${#DRILL_NOTES[@]}" -gt 0 ]; then
  printf '    - %s\n' "${DRILL_NOTES[@]}" | tee -a "$LOG_FILE"
fi
echo "  演练时间: $(date '+%Y-%m-%d %H:%M:%S')" | tee -a "$LOG_FILE"
echo "  日志: $LOG_FILE" | tee -a "$LOG_FILE"
echo "===================================" | tee -a "$LOG_FILE"

#---------- 清理本地下载文件（演练库由 EXIT trap 删除）----------
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 清理临时文件完成" | tee -a "$LOG_FILE"

if [ "$DRILL_STATUS" != "PASS" ]; then
  echo "[ERROR] 恢复演练未通过，详见 $LOG_FILE" | tee -a "$LOG_FILE"
  exit 1
fi
