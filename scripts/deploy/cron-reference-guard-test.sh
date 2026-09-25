#!/usr/bin/env bash
# T393 · N6：cron 引用零漂移守卫的行为测试（喂夹具表，不碰真 crontab；跑完还要证明没写夹具）
# 用法：bash scripts/deploy/cron-reference-guard-test.sh
# 三向：放行侧（现网真实形态判绿）、判红侧（指回工作树 / 期望没被引用 / 落点副本不见）、
#       第三态（夹具不可读、空表 ⇒ rc=3，既不是 0 也不是 1，防「读不到被当成一致」）。
set -uo pipefail

GUARD="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/cron-reference-guard.sh"
[ -r "$GUARD" ] || { echo "[FAIL] 找不到被测脚本 $GUARD"; exit 1; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/t393-guard.XXXXXX") || { echo "[FAIL] mktemp 失败"; exit 1; }
chmod 700 "$WORK"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAILED=0
ok()  { echo "  OK   $*"; PASS=$((PASS + 1)); }
bad() { echo "  FAIL $*"; FAILED=$((FAILED + 1)); }
expect_rc() { # $1=期望 rc $2=实得 rc $3=说明
  if [ "$1" = "$2" ]; then ok "$3（rc=$2）"; else bad "$3：期望 rc=$1，实得 rc=$2"; fi
}

ROOT="$WORK/repo"                    # 冒充被部署工作树
PUB="$WORK/ops/bin"                  # 冒充外置落点
mkdir -p "$ROOT/scripts/backup" "$PUB"
for f in pg-backup-cos.sh staging-restore-drill.sh; do
  printf '#!/usr/bin/env bash\necho copy\n' > "$PUB/$f"
done

# 现网真实形态：任务行引用外置副本，日志重定向落在 /opt/bracesync/backup（生产目录，不是被部署工作树）
cat > "$WORK/table-good.txt" <<EOF
0 2 * * * /bin/bash $PUB/pg-backup-cos.sh >> /opt/bracesync/backup/logs/cron-backup.log 2>&1
0 3 * * 6 /bin/bash $PUB/staging-restore-drill.sh >> /opt/bracesync/backup/logs/cron-restore.log 2>&1
EOF

run_guard() { # $1=表文件 $2=落点 $3=工作树根 ；输出到 $WORK/g.out，回传 rc
  CRON_TABLE_FILE="$1" CRON_PUBLISH_DIR="$2" CRON_DEPLOYED_ROOT="$3" bash "$GUARD" >"$WORK/g.out" 2>&1
}

echo "[1/6] 放行侧：现网真实两行的形态必须判绿（守卫误伤 = 每轮部署白中止）"
run_guard "$WORK/table-good.txt" "$PUB" "$ROOT"; rc=$?
expect_rc 0 "$rc" "现网形态"
grep -q '实际引用' "$WORK/g.out" && ok "放行时也把判读依据打出来（回执要能转给 PM 看）" \
  || bad "放行却什么都没交代：$(tr '\n' '|' < "$WORK/g.out")"

echo "[2/6] 判红一：任务行指回被部署工作树（T383 改前形态）"
sed "s|$PUB/|$ROOT/scripts/backup/|g" "$WORK/table-good.txt" > "$WORK/table-drift.txt"
run_guard "$WORK/table-drift.txt" "$PUB" "$ROOT"; rc=$?
[ "$rc" = "1" ] && ok "指回工作树判红（rc=1）" || bad "指回工作树却 rc=$rc（该红不红 = 守卫是摆设）"
grep -q 'T383' "$WORK/g.out" && ok "判红文案点名是哪条收口失效" || bad "判红却没说是哪条规矩：$(tr '\n' '|' < "$WORK/g.out")"

echo "[3/6] 判红二：期望清单里的脚本没被引用（少一条 = cron 静默失去该任务的守卫）"
grep 'pg-backup-cos.sh' "$WORK/table-good.txt" > "$WORK/table-half.txt"
run_guard "$WORK/table-half.txt" "$PUB" "$ROOT"; rc=$?
[ "$rc" = "1" ] && ok "只剩一条时判红（rc=1）" || bad "期望两条只剩一条却 rc=$rc"
grep -q 'staging-restore-drill.sh' "$WORK/g.out" && ok "文案点名缺的那条" || bad "判红没点名缺哪条：$(tr '\n' '|' < "$WORK/g.out")"

echo "[4/6] 判红三：引用路径写对了，但落点那份副本被删 / 不可读（下一轮 cron 必失败）"
mkdir -p "$WORK/ops-missing/bin"
run_guard "$WORK/table-good.txt" "$WORK/ops-missing/bin" "$ROOT"; rc=$?
[ "$rc" = "1" ] && ok "落点无副本时判红（rc=1）" || bad "副本不见却 rc=$rc"
grep -qi '缺可读副本' "$WORK/g.out" && ok "文案交代是「引用在、文件不在」" || bad "文案没区分「没引用」与「文件不见」：$(tr '\n' '|' < "$WORK/g.out")"

echo "[5/6] 第三态：读不到与空表都不许当「一致」（rc 既非 0 也非 1 的那一条）"
run_guard "$WORK/no-such-table.txt" "$PUB" "$ROOT"; rc=$?
expect_rc 3 "$rc" "夹具文件不存在"
: > "$WORK/table-empty.txt"
run_guard "$WORK/table-empty.txt" "$PUB" "$ROOT"; rc=$?
expect_rc 3 "$rc" "空表（没有任何任务行）"
printf '# 历史上这里放过工作树路径 /bin/bash %s/scripts/backup/pg-backup-cos.sh\n' "$ROOT" > "$WORK/table-comments.txt"
run_guard "$WORK/table-comments.txt" "$PUB" "$ROOT"; rc=$?
expect_rc 3 "$rc" "整表只剩注释（注释里的残留路径不算引用）"

echo "[6/6] 两条边界：只读性 + 近邻前缀不误伤"
# 只读性：跑四遍之后夹具字节必须一字不变（守卫写 crontab 就等于替产品改现网）
before=$(sha256sum < "$WORK/table-good.txt" | awk '{print $1}')
run_guard "$WORK/table-good.txt" "$PUB" "$ROOT" >/dev/null 2>&1
run_guard "$WORK/table-drift.txt" "$PUB" "$ROOT" >/dev/null 2>&1
after=$(sha256sum < "$WORK/table-good.txt" | awk '{print $1}')
[ "$before" = "$after" ] && ok "判红与放行路径都不改夹具（本脚本只读）" || bad "守卫改写了输入：$before 变 $after"
# 现网真实形状里外置落点是 /home/ubuntu/bracesync-ops，与被部署工作树 /home/ubuntu/bracesync 只差后缀；
# 期望串若少了结尾的斜杠就会把合规行误判成漂移，这里把这条钉住。
PUB2="$ROOT-ops/bin"; mkdir -p "$PUB2"
for f in pg-backup-cos.sh staging-restore-drill.sh; do cp "$PUB/$f" "$PUB2/$f"; done
cat > "$WORK/table-nearmiss.txt" <<EOF
0 2 * * * /bin/bash $PUB2/pg-backup-cos.sh >> /opt/bracesync/backup/logs/cron-backup.log 2>&1
0 3 * * 6 /bin/bash $PUB2/staging-restore-drill.sh >> /opt/bracesync/backup/logs/cron-restore.log 2>&1
EOF
run_guard "$WORK/table-nearmiss.txt" "$PUB2" "$ROOT"; rc=$?
expect_rc 0 "$rc" "落点带工作树名前缀（bracesync-ops 之于 bracesync）不误伤"

echo ""
echo "[t393-guard-test] 断言：通过 $PASS / 不通过 $FAILED"
[ "$FAILED" = "0" ] || exit 1
exit 0
