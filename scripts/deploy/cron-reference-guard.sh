#!/usr/bin/env bash
# T393 · N6：cron 引用零漂移守卫（全程只读，绝不写 crontab）
# 用法：bash scripts/deploy/cron-reference-guard.sh
#   环境变量（只为测试可注入）：
#     CRON_TABLE_FILE     用这份文件代替 crontab -l（CI 里没有真实 crontab，靠它喂夹具）
#     CRON_PUBLISH_DIR    期望的外置落点（cron 应当引用这里的副本）
#     CRON_DEPLOYED_ROOT  禁止被 cron 直接引用的那棵被部署工作树
#   退出码：0 期望与实际一致 / 1 检出漂移（逐条点名） / 3 cron 表取不到
#
# 为什么要这一格（Joe T383 工程验收 N6）：仓里原先没有任何一处描述「两条 cron 应当指向哪里」，
#   也没有任何脚本读 crontab —— 有人把它改回指工作树，没有任何东西会喊，
#   而 T383 判据 1（被调度的字节不住在 ① 步会被换掉的树里）的长期成立正挂在这条上。
#   本脚本把「期望」写成仓内单一权威，并与实际 crontab 做只读比对：期望集合须被实际引用集合包含。
set -uo pipefail

# 期望：cron 必须引用这两条外置副本。deploy-chain-wiring-test.sh 会静态核对
# 「本表 ⊇ publish-cron-scripts.sh 的 REQUIRED」，防止这里被漏改（漏一条就是无声的覆盖缺口）。
EXPECTED_REF_SCRIPTS="pg-backup-cos.sh staging-restore-drill.sh"
PUBLISH_DIR="${CRON_PUBLISH_DIR:-/home/ubuntu/bracesync-ops/bin}"
DEPLOYED_ROOT="${CRON_DEPLOYED_ROOT:-/home/ubuntu/bracesync}"

say() { echo "[cron-ref] $*"; }
die() { echo "[cron-ref] ERROR $*" >&2; exit 3; }

if [ -n "${CRON_TABLE_FILE:-}" ]; then
  [ -r "$CRON_TABLE_FILE" ] || die "cron 表夹具不可读：$CRON_TABLE_FILE（读不到就不判绿）"
  table=$(cat "$CRON_TABLE_FILE" 2>/dev/null) || die "cron 表夹具读取失败：$CRON_TABLE_FILE"
else
  table=$(crontab -l 2>/dev/null) || die "crontab -l 失败：无法核对 cron 引用面 ⇒ 判红不判绿"
fi

# 只看真正的任务行：注释与空行不参与比对（crontab 里注释常残留历史路径，那是文档不是引用）
active=$(printf '%s\n' "$table" | grep -vE '^[[:space:]]*#' | grep -vE '^[[:space:]]*$') || active=""
if [ -z "$active" ]; then
  die "cron 表里没有任务行：期望引用 $EXPECTED_REF_SCRIPTS 却一条都没有（空表不等于一致）"
fi

fails=0
say "外置落点 $PUBLISH_DIR / 禁引用工作树 $DEPLOYED_ROOT / 期望脚本：$EXPECTED_REF_SCRIPTS"
say "实际任务行 $(printf '%s\n' "$active" | wc -l | tr -d '[:space:]') 条"

# 一、期望集合 ⊇ 检查：每条期望脚本都要被某条任务行按外置路径引用
for name in $EXPECTED_REF_SCRIPTS; do
  if printf '%s\n' "$active" | grep -qF -- "$PUBLISH_DIR/$name"; then
    say "OK   cron 引用了外置副本：$PUBLISH_DIR/$name"
  else
    say "FAIL cron 没有按外置路径引用 $name（期望串：$PUBLISH_DIR/$name）"
    printf '%s\n' "$active" | grep -F -- "$name" | sed 's/^/       现行 /' >&2
    fails=$((fails + 1))
  fi
  # 二、引用了不等于能跑：落点那份副本必须存在且可读（被人 rm 掉就是 cron 静默失效）
  if [ ! -r "$PUBLISH_DIR/$name" ]; then
    say "FAIL 外置落点缺可读副本：$PUBLISH_DIR/$name（引用在、文件不在 ⇒ 下一轮定时任务必失败）"
    fails=$((fails + 1))
  fi
done

# 三、反向：任何任务行直接引用被部署工作树 = 漂移回 T383 改前的形态
if printf '%s\n' "$active" | grep -qF -- "$DEPLOYED_ROOT/"; then
  say "FAIL 有 cron 任务行直接引用被部署工作树 $DEPLOYED_ROOT —— T383 外置收口已失效"
  printf '%s\n' "$active" | grep -F -- "$DEPLOYED_ROOT/" | sed 's/^/       漂移 /' >&2
  fails=$((fails + 1))
fi

if [ "$fails" -gt 0 ]; then
  say "结论：检出 $fails 项漂移（本脚本只读，未改动 crontab）"
  exit 1
fi
say "结论：实际引用 ⊇ 期望外置路径，且无任务行指回工作树（只读核对，未改动 crontab）"
