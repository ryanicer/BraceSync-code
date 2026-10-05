#!/usr/bin/env bash
# T550 · 防复发：设备压力数据新鲜度守卫（全程只读，绝不写库）
# 用法：bash scripts/deploy/data-freshness-guard.sh
#   环境变量（只为测试可注入）：
#     FRESHNESS_LATEST_FILE      用这份夹具代替数据库（CI 里没有 staging 凭据，靠它喂读数）
#     FRESHNESS_MAX_SILENT_HOURS 静默多少小时判红（默认 6）
#     FRESHNESS_NOW_EPOCH        把「现在」钉成给定 epoch（默认取 date +%s）
#     FRESHNESS_DB               真实读数用 psql 连接串（未给且无夹具 ⇒ rc=3 判红不判绿）
#   退出码：0 数据新鲜 / 1 断流（报警）/ 3 取不到读数（读不到不等于新鲜）
#
# 为什么要这一格（T550 派发单 §三.4）：真实设备上报链路已全断 3 天而无人知——
#   设备上报被网关验签时间窗挡死（死锁），库里最后一条压力记录停在 10-01，
#   请求侧 10 万次 401 一条日志都没打到 device-service，所以「服务健康」一切正常，
#   缺的正是「数据有没有在长」这道闸。本脚本把「最后一行距今多久」变成可调度判据。
#   接法：与 T393 那两条 cron 同一侧外置部署，报警走退出码（非 0 即由调度侧发通知）。
set -uo pipefail

MAX_SILENT_HOURS="${FRESHNESS_MAX_SILENT_HOURS:-6}"
NOW="${FRESHNESS_NOW_EPOCH:-$(date +%s)}"
TABLE="pressure_records"

say() { echo "[freshness] $*"; }
die() { echo "[freshness] ERROR $*" >&2; exit 3; }

case "$MAX_SILENT_HOURS" in
  '' | *[!0-9]*) die "FRESHNESS_MAX_SILENT_HOURS 要非负整数，实得「$MAX_SILENT_HOURS」" ;;
esac
case "$NOW" in
  '' | *[!0-9]*) die "FRESHNESS_NOW_EPOCH 要整数秒，实得「$NOW」" ;;
esac

# 读数：夹具优先（测试/CI），其次 psql 只读查询
if [ -n "${FRESHNESS_LATEST_FILE:-}" ]; then
  [ -r "$FRESHNESS_LATEST_FILE" ] || die "夹具不可读：$FRESHNESS_LATEST_FILE（读不到就不判绿）"
  raw=$(cat "$FRESHNESS_LATEST_FILE" 2>/dev/null) || die "夹具读取失败：$FRESHNESS_LATEST_FILE"
  latest=$(printf '%s\n' "$raw" | head -1 | tr -d '[:space:]')
else
  [ -n "${FRESHNESS_DB:-}" ] || die "既没给 FRESHNESS_LATEST_FILE 也没给 FRESHNESS_DB ⇒ 无读数，判红不判绿"
  command -v psql >/dev/null 2>&1 || die "FRESHNESS_DB 给了但 psql 不在 PATH ⇒ 取不到读数"
  # 时间那一格取 epoch 秒（不是 date 认得的串）：真库实测「AT TIME ZONE 'UTC'」出来的是不带零时区的
  #   裸串，宿主机时区一非 UTC 就会把「距今多久」整档算偏（实测偏 8 小时）。
  SQL="SELECT coalesce(floor(extract(epoch from max(ts)))::text, 'EMPTY'), count(*) FILTER (WHERE ingest_source = 'mock') FROM $TABLE"
  latest=$(psql "$FRESHNESS_DB" -At -F'|' -c "$SQL") \
    || die "psql 查询失败（连接/权限/表不存在/SQL 语法错都算取不到读数）；上一条 psql 报错原文就在本行上面（不再丢进 null）；若那句写着 syntax error，先数 $SQL 引号内 SQL 自身的左右括号是否配平"
fi

[ -n "$latest" ] || die "读数为空串：区分不了「查询失败」和「库里真没行」，不判绿"

# 解析：latest | mock_count（夹具可以只给第一格）
ts_part=${latest%%|*}
mock_cnt=${latest##*|}
[ "$mock_cnt" = "$latest" ] && mock_cnt="(夹具未给)"

if [ "$ts_part" = "EMPTY" ]; then
  say "结论：$TABLE 里一条记录都没有 —— 判红（空表不等于新鲜）"
  exit 1
fi

# 接受两种形状：纯 epoch 秒，或 ISO8601（可带 Z / 偏移）
if case "$ts_part" in (*[!0-9]*) false ;; (*) true ;; esac; then
  last_epoch=$ts_part
else
  last_epoch=$(date -d "$ts_part" +%s 2>/dev/null) || die "最新行时间戳解析失败：「$ts_part」既不是 epoch 秒也不是 date 认得的 ISO 串"
fi

age=$((NOW - last_epoch))
if [ "$age" -lt 0 ]; then
  say "FAIL 最新行时间戳在未来 $(( -age )) 秒（时钟异常或写入造假）—— 判红：新鲜度尺不能靠「越新越好」蒙过去"
  exit 1
fi
threshold=$((MAX_SILENT_HOURS * 3600))
age_h=$((age / 3600))
say "表 $TABLE / 最新行 $ts_part / 距今 ${age} 秒（约 ${age_h} 小时）/ 阈值 ${MAX_SILENT_HOURS} 小时 / mock 行数 $mock_cnt"

if [ "$age" -gt "$threshold" ]; then
  say "结论：断流报警 —— 静默 ${age_h} 小时 > 阈值 ${MAX_SILENT_HOURS} 小时（设备上报链路或写入侧已停，须查网关验签侧）"
  exit 1
fi
say "结论：数据新鲜（静默 ${age_h} 小时 ≤ 阈值 ${MAX_SILENT_HOURS} 小时）"
