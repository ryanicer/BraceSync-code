#!/usr/bin/env bash
# T383 第一处行为测试：cron 备份脚本外置签发（publish-cron-scripts.sh）
# 用法：bash scripts/deploy/publish-cron-scripts-test.sh
# 三向：签发侧（字节一致 + 落点在工作树外 + mode 500 + 台账）、幂等侧（重跑不换字节）、
#       判红侧（源不齐 / 源目录没了 ⇒ 非零退出且【不动】已有副本，绝不签出一半）。
set -uo pipefail

PUB_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/publish-cron-scripts.sh"
[ -r "$PUB_SRC" ] || { echo "[FAIL] 找不到被测脚本 $PUB_SRC"; exit 1; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/t383-publish-test.XXXXXX") || { echo "[FAIL] mktemp 失败"; exit 1; }
chmod 700 "$WORK"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAILED=0
SKIPPED=0
ok()  { echo "  OK   $*"; PASS=$((PASS + 1)); }
bad() { echo "  FAIL $*"; FAILED=$((FAILED + 1)); }
skip() { echo "  SKIP $*"; SKIPPED=$((SKIPPED + 1)); }
expect_rc() { if [ "$1" = "$2" ]; then ok "$3（rc=$2）"; else bad "$3：期望 rc=$1，实得 rc=$2"; fi; }

# 权限位断言按【平台】而不是按取值决定信不信：只看值会把 Linux 上真正的 444（签发错了）
# 当成「本平台不可信」跳过 —— 那是给自己留假绿口子。只有 MSYS/MINGW/CYGWIN 才跳过。
case "$(uname -s 2>/dev/null)" in
  MINGW*|MSYS*|CYGWIN*) MODE_TRUSTED=0 ;;
  *) MODE_TRUSTED=1 ;;
esac
assert_mode() {
  local path="$1" want="$2" label="$3" got
  got=$(stat -c '%a' "$path" 2>/dev/null || echo unknown)
  if [ "$MODE_TRUSTED" = "0" ]; then
    skip "$label 本平台权限位不可信（实得 $got，期望 $want）⇒ 现网 Linux 那轮实测"
  elif [ "$got" = "$want" ]; then
    ok "$label mode=$got"
  else
    bad "$label 权限位错：实得 $got，期望 $want"
  fi
}

sha_of() { sha256sum -- "$1" | awk '{print $1}'; }

# 造一棵假「权威仓」：工作树内的源目录 + 工作树外的落点
TREE="$WORK/repo/scripts/backup"
OPS="$WORK/ops/bin"
mkdir -p "$TREE"
printf '#!/usr/bin/env bash\necho backup-v1\n' > "$TREE/pg-backup-cos.sh"
printf '#!/usr/bin/env bash\necho drill-v1\n'  > "$TREE/staging-restore-drill.sh"
run_pub() {
  CRON_SOURCE_DIR="$TREE" CRON_PUBLISH_DIR="$OPS" CRON_PUBLISH_LOG="$WORK/published.log" \
    bash "$PUB_SRC" testhead >"$WORK/pub.out" 2>&1
}

echo "[1/5] 首次签发：两个脚本都落到工作树之外，字节与源一致"
run_pub; rc=$?
expect_rc 0 "$rc" "首次签发"
for f in pg-backup-cos.sh staging-restore-drill.sh; do
  if [ -r "$OPS/$f" ]; then
    ok "外置副本存在：$f（落点 $OPS 不在源树 $TREE 内）"
    [ "$(sha_of "$TREE/$f")" = "$(sha_of "$OPS/$f")" ] && ok "$f 副本 sha 与源相等" || bad "$f 副本 sha 与源不等"
    assert_mode "$OPS/$f" 500 "$f 副本"
  else
    bad "$f 没被签发出去"
  fi
done
[ "$(wc -l < "$WORK/published.log" 2>/dev/null | tr -d '[:space:]')" = "2" ] \
  && ok "台账落了 2 行" || bad "台账行数不是 2：$(cat "$WORK/published.log" 2>/dev/null | tr '\n' '|')"
grep -q 'head=testhead' "$WORK/published.log" && ok "台账记了本轮 head" || bad "台账缺 head 字段"

echo "[2/5] 落点里没有临时文件残留（install+mv 的中间态不该留）"
left=$(ls -1A "$OPS" | grep -c '^\.' || true)
[ "$left" = "0" ] && ok "无 .tmp 残留" || bad "落点残留 $left 个隐藏临时文件：$(ls -1A "$OPS" | tr '\n' ' ')"

echo "[3/5] 幂等：源不变重跑一次，副本字节与台账口径都应稳定"
before=$(sha_of "$OPS/pg-backup-cos.sh")
run_pub; rc=$?
expect_rc 0 "$rc" "重跑签发"
[ "$before" = "$(sha_of "$OPS/pg-backup-cos.sh")" ] && ok "重跑后副本字节未变" || bad "重跑改变了副本字节"
[ "$(wc -l < "$WORK/published.log" | tr -d '[:space:]')" = "4" ] && ok "台账按轮追加（4 行）" || bad "台账没按轮追加：$(wc -l < "$WORK/published.log")"

echo "[4/5] 源更新后重签：副本要跟上新字节（外置不等于冻结）"
printf '#!/usr/bin/env bash\necho backup-v2\n' > "$TREE/pg-backup-cos.sh"
run_pub; rc=$?
expect_rc 0 "$rc" "源变更后重签"
[ "$(sha_of "$TREE/pg-backup-cos.sh")" = "$(sha_of "$OPS/pg-backup-cos.sh")" ] \
  && ok "副本跟上了新源字节" || bad "副本仍是旧字节（部署了却没签发出去）"

echo "[5/5] 判红两例：源缺一半 / 源目录整个不见 ⇒ 非零退出且已有副本一个字节都不动"
keep_sha=$(sha_of "$OPS/pg-backup-cos.sh"); keep_drill=$(sha_of "$OPS/staging-restore-drill.sh")
mv "$TREE/pg-backup-cos.sh" "$WORK/hide.sh"
CRON_SOURCE_DIR="$TREE" CRON_PUBLISH_DIR="$OPS" CRON_PUBLISH_LOG="$WORK/published.log" \
  bash "$PUB_SRC" testhead >"$WORK/pub-miss.out" 2>&1; rc=$?
[ "$rc" != "0" ] && ok "源缺 cron 引用项时判红（rc=$rc）" || bad "源缺项却返回 0（判红分支是死的）"
grep -qi 'pg-backup-cos.sh' "$WORK/pub-miss.out" && ok "判红文案点名了缺的那一项" || bad "判红文案没说是哪个脚本缺：$(tr '\n' '|' < "$WORK/pub-miss.out")"
CRON_SOURCE_DIR="$WORK/no-such-dir" CRON_PUBLISH_DIR="$OPS" CRON_PUBLISH_LOG="$WORK/published.log" \
  bash "$PUB_SRC" testhead >/dev/null 2>&1; rc=$?
[ "$rc" != "0" ] && ok "源目录不存在时判红（rc=$rc）" || bad "源目录不存在却返回 0"
[ "$keep_sha" = "$(sha_of "$OPS/pg-backup-cos.sh")" ] && [ "$keep_drill" = "$(sha_of "$OPS/staging-restore-drill.sh")" ] \
  && ok "两次判红期间已有副本未被改写（宁可让 cron 跑上一轮，也不签一半）" \
  || bad "判红路径动了已有副本"

echo ""
echo "[t383-publish-test] 断言：通过 $PASS / 不通过 $FAILED / 跳过 $SKIPPED（跳过项均为本平台权限位不可信，现网 Linux 实测兜底）"
[ "$FAILED" = "0" ] || exit 1
exit 0
