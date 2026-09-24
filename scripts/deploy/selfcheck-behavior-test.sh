#!/usr/bin/env bash
# T383 自检脚本行为测试：record / verify / receipt 三条路径的退出码与落盘内容
# 用法：bash scripts/deploy/selfcheck-behavior-test.sh
# 定位：deploy 脚本链的「检测类改动」必须双向取证 —— 既验该留凭据的留下了，
#       也验该判红的没被吞成 0（rc 语义 0/2/3 各自都对）。全部在临时目录里跑，不碰仓库真本。
set -uo pipefail

SELFCHK_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/selfcheck-deploy-script.sh"
[ -r "$SELFCHK_SRC" ] || { echo "[FAIL] 找不到被测脚本 $SELFCHK_SRC"; exit 1; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/t383-selfcheck-test.XXXXXX") || { echo "[FAIL] mktemp 失败"; exit 1; }
chmod 700 "$WORK"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAILED=0
SKIPPED=0

ok()   { echo "  OK   $*"; PASS=$((PASS + 1)); }
bad()  { echo "  FAIL $*"; FAILED=$((FAILED + 1)); }
skip() { echo "  SKIP $*"; SKIPPED=$((SKIPPED + 1)); }

# expect_rc <期望码> <实际码> <用例名>
expect_rc() {
  if [ "$1" = "$2" ]; then ok "$3（rc=$2）"; else bad "$3：期望 rc=$1，实得 rc=$2"; fi
}

# expect_field <文件> <键=值正则> <用例名>（行首锚定，用于凭据/基线这类 key=value 文件）
expect_field() {
  if grep -Eq "^$2" "$1" 2>/dev/null; then ok "$3"; else bad "$3：$1 里没匹配到 /^$2/，实际内容：$(tr '\n' '|' < "$1" 2>/dev/null)"; fi
}

# expect_line <文件> <正则> <用例名>（不锚行首，用于被打上 [selfcheck] 前缀的输出日志）
expect_line() {
  if grep -Eq "$2" "$1" 2>/dev/null; then ok "$3"; else bad "$3：$1 里没匹配到 /$2/"; fi
}

# 造一棵最小「工作树」：被测脚本住在 git 仓里才能验 head= 字段（非仓时该值为 unknown，也算合法）
REPO="$WORK/repo"
mkdir -p "$REPO/scripts/deploy"
TARGET="$REPO/scripts/deploy/deploy-staging.sh"
printf '#!/usr/bin/env bash\necho v1\n' > "$TARGET"
STATE="$WORK/state.env"
RECEIPT_DIR="$WORK/receipts"

echo "[1/7] record 落基线"
out=$(bash "$SELFCHK_SRC" record "$TARGET" "$STATE" 2>&1); rc=$?
expect_rc 0 "$rc" "record 正常路径"
for k in mode=record "path=" "sha256=[0-9a-f]{64}" "bytes=[0-9]+" "head=" "at="; do
  expect_field "$STATE" "$k" "基线含 $k"
done

echo "[2/7] verify：字节未变 ⇒ rc=0（放行侧必须真跑一次，不能只看中止侧）"
bash "$SELFCHK_SRC" verify "$TARGET" "$STATE" >"$WORK/verify0.log" 2>&1; rc=$?
expect_rc 0 "$rc" "未变时 verify"
expect_line "$WORK/verify0.log" '\[selfcheck\] PASS' "verify 放行时打了 PASS"

echo "[3/7] verify：字节被换 ⇒ rc=2（既不能是 0 也不能是 1/3）"
printf '#!/usr/bin/env bash\necho v2-换掉了\n' > "$TARGET"
bash "$SELFCHK_SRC" verify "$TARGET" "$STATE" >"$WORK/verify2.log" 2>&1; rc=$?
expect_rc 2 "$rc" "被换时 verify"
expect_line "$WORK/verify2.log" '\[selfcheck\] ALARM' "verify 判红时打了 ALARM"

echo "[4/7] verify：基线文件不在 ⇒ rc=3（区别于「字节已变」，不能混成 2）"
bash "$SELFCHK_SRC" verify "$TARGET" "$WORK/no-such-state.env" >/dev/null 2>&1; rc=$?
expect_rc 3 "$rc" "缺基线时 verify"

echo "[5/7] receipt：中止轮 + 基线在 + 期间字节已变 ⇒ 落凭据且判读为 changed"
bash "$SELFCHK_SRC" receipt "$TARGET" "$STATE" "$RECEIPT_DIR" 1 >"$WORK/rcpt1.log" 2>&1; rc=$?
expect_rc 0 "$rc" "receipt 正常路径"
ABORT_FILE=$(ls -1 "$RECEIPT_DIR"/abort-*.env 2>/dev/null | head -1)
if [ -n "${ABORT_FILE:-}" ]; then
  ok "中止轮凭据已落盘（$(basename "$ABORT_FILE")）"
  expect_field "$ABORT_FILE" "mode=abort-selfcheck" "凭据标了自身用途"
  expect_field "$ABORT_FILE" "abort_rc=1" "凭据记了中止退出码"
  expect_field "$ABORT_FILE" "baseline_sha256=[0-9a-f]{64}" "凭据记了开头基线 sha"
  expect_field "$ABORT_FILE" "abort_sha256=[0-9a-f]{64}" "凭据记了中止时 sha"
  expect_field "$ABORT_FILE" "verdict=changed" "凭据判读为 changed"
  if [ -s "$ABORT_FILE" ]; then
    ok "凭据非空文件（$(wc -c < "$ABORT_FILE" | tr -d '[:space:]') 字节）"
  else
    bad "凭据是空文件"
  fi
  mode=$(stat -c '%a' "$ABORT_FILE" 2>/dev/null || echo unknown)
  # 权限位只在 Linux 上作断言（按平台判，不按取值猜 —— 把真 600 当「不可信」跳过就是假绿口子）
  case "$(uname -s 2>/dev/null)" in
    MINGW*|MSYS*|CYGWIN*) skip "凭据权限位本平台不可信（实得 $mode）⇒ 现网 Linux 那轮实测" ;;
    *) [ "$mode" = "400" ] && ok "凭据 mode=400（只读，落下去就不该再被改）" || bad "凭据权限位错：实得 $mode，期望 400" ;;
  esac
  base=$(grep '^baseline_sha256=' "$ABORT_FILE" | cut -d= -f2-)
  now=$(grep '^abort_sha256=' "$ABORT_FILE" | cut -d= -f2-)
  [ "$base" != "$now" ] && ok "两个 sha 确实不等（对账有意义）" || bad "两个 sha 相等，判读与数据矛盾"
else
  bad "receipt 说成功但没落任何文件"
fi

echo "[6/7] receipt：基线取不到 / 被检脚本取不到 ⇒ 仍落凭据并如实标 unknown（读不到不等于没发生）"
bash "$SELFCHK_SRC" receipt "$TARGET" "$WORK/no-such-state.env" "$RECEIPT_DIR" 9 >"$WORK/rcpt2.log" 2>&1; rc=$?
expect_rc 0 "$rc" "缺基线时 receipt"
NOBASE=$(ls -1 "$RECEIPT_DIR"/abort-*.env 2>/dev/null | while read -r f; do grep -l 'baseline_sha256=missing' "$f"; done | head -1)
if [ -n "${NOBASE:-}" ]; then
  expect_field "$NOBASE" "verdict=unknown" "缺基线判读为 unknown"
  expect_field "$NOBASE" "abort_rc=9" "退出码 9 原样入档"
else
  bad "缺基线时没落凭据（等于中止轮又回到无痕）"
fi
bash "$SELFCHK_SRC" receipt "$WORK/no-such-script.sh" "$STATE" "$RECEIPT_DIR" 1 >/dev/null 2>&1; rc=$?
expect_rc 0 "$rc" "被检脚本不可读时 receipt"
NOSCRIPT=$(ls -1 "$RECEIPT_DIR"/abort-*.env 2>/dev/null | while read -r f; do grep -l 'abort_sha256=unreadable' "$f"; done | head -1)
[ -n "${NOSCRIPT:-}" ] && ok "被检脚本不可读也留了痕（abort_sha256=unreadable）" || bad "被检脚本不可读时没落凭据"

echo "[7/7] receipt：参数缺目录 ⇒ rc=3；凭据份数被收口（反复失败轮不涨盘）"
bash "$SELFCHK_SRC" receipt "$TARGET" "$STATE" >/dev/null 2>&1; rc=$?
expect_rc 3 "$rc" "缺凭据目录参数"
BULK="$WORK/bulk"
for i in $(seq 1 25); do
  bash "$SELFCHK_SRC" receipt "$TARGET" "$STATE" "$BULK" 1 >/dev/null 2>&1
  sleep 0.05
done
kept=$(ls -1 "$BULK"/abort-*.env 2>/dev/null | wc -l | tr -d '[:space:]')
if [ "$kept" -le 20 ]; then ok "25 次中止只留 $kept 份（上限 20）"; else bad "凭据没收口，留了 $kept 份"; fi

echo ""
echo "[t383-selfcheck-test] 断言：通过 $PASS / 不通过 $FAILED / 跳过 $SKIPPED（跳过项均为本平台权限位不可信，现网 Linux 实测兜底）"
[ "$FAILED" = "0" ] || exit 1
exit 0
