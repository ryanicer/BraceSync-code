#!/usr/bin/env bash
# T383 第一处行为测试：cron 备份脚本外置签发（publish-cron-scripts.sh）
# 用法：bash scripts/deploy/publish-cron-scripts-test.sh
# 前五组（T383）：签发侧（字节一致 + 落点在工作树外 + mode 500 + 台账）、幂等侧（重跑不换字节）、
#   判红侧（源不齐 / 源目录没了 ⇒ 非零退出且【不动】已有副本，绝不签出一半）。
# 后三组（T394）：把 Joe 报告里的 M4/M5 从「改了没人喊」变成「有守卫」——
#   [6/8] N2 正证：install 边界注入字节漂移 ⇒ 本轮中止、坏字节不落 cron 引用的路径、台账不写凭证；
#   [7/8] N2 反证：两次比对各摘一次 —— 摘掉 mv 前那行 ⇒ 坏字节真的落到落点（证明它有牙）；
#         摘掉 mv 后那行（Joe 的 M5 原样）⇒ 另一行拦得住，M5 从「摘了全绿」变成「摘了就红」；
#   [8/8] N3：本轮只签出一半时，「只问存在」的旧后验全绿（M4 现场），换成
#         「本轮签发集 含 REQUIRED」的新后验即判红，且「没签」与「签了但陈旧」两句文案可分辨。
# 反证一律在【临时副本】上做变异（mutate），仓内文件字节不动。
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

# T394 反证用：在【副本】上按锚点换行，原件字节全程不动（Joe 的 M4/M5 是直接改仓内文件再复原，
# 这里改成一次性的临时副本，避免「跑挂了留下变异体」这一格风险）。
# 每个变异体都必须与原件字节不同 —— 锚点没命中就等于反证无牙，宁可判红也不能让它假绿。
# 第 5 参 keep_anchor=1 用于「替换文本里必须保留锚点原文」那种变异（反证 B 把新后验的
# case 分行留在原地，只在这行前面加一句「落点有副本就跳过」），此时锚点存在性检查不适用，
# 改由「替换文本在 + 与原件字节不同」两条兜住。
mutate() {
  local fin="$1" fout="$2" anchor="$3" repl="$4" keep_anchor="${5:-0}"
  awk -v a="$anchor" -v r="$repl" 'index($0,a)>0 && !hit{print r; hit=1; next} {print}' "$fin" > "$fout"
  if [ "$keep_anchor" = "0" ]; then
    grep -qF -- "$anchor" "$fout" && { bad "变异体里仍留有锚点（替换没生效）：$anchor"; return 1; }
  fi
  grep -qF -- "$repl" "$fout" || { bad "变异体里找不到替换文本（awk 没命中）：$repl"; return 1; }
  cmp -s "$fin" "$fout" && { bad "变异体与原件字节相同 ⇒ 反证无牙：$anchor"; return 1; }
  if bash -n "$fout" 2>/dev/null; then
    return 0
  fi
  bad "变异体语法不可解析（反证若判红会误成脚本坏）：$fout"
  return 1
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

# N2 的注入点选在【工具边界】而不是改脚本内部：PATH 上摆一个假 install，
# 它照抄真 install 的净效果（落 mode 500）但往目标文件多写一行漂移标记 ⇒
# 「临时副本与源不同字节」这个前提是真的发生了，而不是靠注释里假设出来的。
SHIM_DIR="$WORK/shim"
SHIM_LOG="$WORK/shim-hits.log"
REAL_INSTALL=$(command -v install 2>/dev/null || true)

write_shim() {
  mkdir -p "$SHIM_DIR" || return 1
  cat > "$SHIM_DIR/install" <<EOF || return 1
#!/usr/bin/env bash
dst="\${@: -1}"
src="\${@: -2:1}"
"$REAL_INSTALL" -m 600 "\$src" "\$dst" || exit 1
printf '# t394-drift-marker\\n' >> "\$dst" || exit 1
chmod 500 "\$dst" 2>/dev/null || true
echo hit >> "$SHIM_LOG"
exit 0
EOF
  chmod 700 "$SHIM_DIR/install" || return 1
  [ -x "$SHIM_DIR/install" ] || return 1
  return 0
}

run_with_shim() {   # $1=被测脚本 $2=stdout 落点
  PATH="$SHIM_DIR:$PATH" CRON_SOURCE_DIR="$TREE" CRON_PUBLISH_DIR="$OPS" \
    CRON_PUBLISH_LOG="$WORK/published.log" bash "$1" testhead >"$2" 2>&1
}

shim_hits() { grep -c . "$SHIM_LOG" 2>/dev/null | tr -d '[:space:]'; }

echo "[1/8] 首次签发：两个脚本都落到工作树之外，字节与源一致"
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

echo "[2/8] 落点里没有临时文件残留（install+mv 的中间态不该留）"
left=$(ls -1A "$OPS" | grep -c '^\.' || true)
[ "$left" = "0" ] && ok "无 .tmp 残留" || bad "落点残留 $left 个隐藏临时文件：$(ls -1A "$OPS" | tr '\n' ' ')"

echo "[3/8] 幂等：源不变重跑一次，副本字节与台账口径都应稳定"
before=$(sha_of "$OPS/pg-backup-cos.sh")
run_pub; rc=$?
expect_rc 0 "$rc" "重跑签发"
[ "$before" = "$(sha_of "$OPS/pg-backup-cos.sh")" ] && ok "重跑后副本字节未变" || bad "重跑改变了副本字节"
[ "$(wc -l < "$WORK/published.log" | tr -d '[:space:]')" = "4" ] && ok "台账按轮追加（4 行）" || bad "台账没按轮追加：$(wc -l < "$WORK/published.log")"

echo "[4/8] 源更新后重签：副本要跟上新字节（外置不等于冻结）"
printf '#!/usr/bin/env bash\necho backup-v2\n' > "$TREE/pg-backup-cos.sh"
run_pub; rc=$?
expect_rc 0 "$rc" "源变更后重签"
[ "$(sha_of "$TREE/pg-backup-cos.sh")" = "$(sha_of "$OPS/pg-backup-cos.sh")" ] \
  && ok "副本跟上了新源字节" || bad "副本仍是旧字节（部署了却没签发出去）"

echo "[5/8] 判红两例：源缺一半 / 源目录整个不见 ⇒ 非零退出且已有副本一个字节都不动"
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

echo "[6/8] N2 正证：install 边界注入字节漂移 ⇒ 本轮中止，且坏字节不落进 cron 引用的路径"
mv "$WORK/hide.sh" "$TREE/pg-backup-cos.sh"   # 第 5 组把源挪走过，这里放回，让签发循环能跑到注入点
run_pub; rc=$?
expect_rc 0 "$rc" "注入前先用正式脚本对齐落点"
keep_sha=$(sha_of "$OPS/pg-backup-cos.sh"); keep_drill=$(sha_of "$OPS/staging-restore-drill.sh")
ledger_before=$(wc -l < "$WORK/published.log" | tr -d '[:space:]')
if [ -z "$REAL_INSTALL" ]; then
  bad "平台找不到 install ⇒ 无法注入字节漂移，第 6/7 组不可信（宁可判红，不留假绿）"
elif ! write_shim; then
  bad "假 install 生成失败 ⇒ 第 6/7 组没跑成，判红"
else
  : > "$SHIM_LOG"
  run_with_shim "$PUB_SRC" "$WORK/pub-drift.out"; rc=$?
  [ "$rc" != "0" ] && ok "漂移轮判红（rc=$rc）" || bad "漂移轮返回 0 ⇒ 字节校验形同虚设"
  grep -qF -- '临时副本与源字节不一致' "$WORK/pub-drift.out" \
    && ok "文案走的是 mv 之前那次比对（挡在落点之外）" \
    || bad "文案没走 mv 前分支：$(tr '\n' '|' < "$WORK/pub-drift.out")"
  [ "$(shim_hits)" != "0" ] && ok "注入确实发生（shim 命中 $(shim_hits) 次，非空转）" || bad "shim 一次都没被走到 ⇒ 这一组是空转"
  [ "$keep_sha" = "$(sha_of "$OPS/pg-backup-cos.sh")" ] && [ "$keep_drill" = "$(sha_of "$OPS/staging-restore-drill.sh")" ] \
    && ok "中止时已有副本一个字节都没动（cron 继续用上一轮，宁可旧不可半截）" \
    || bad "漂移轮改写了落点副本"
  grep -qF -- 't394-drift-marker' "$OPS/pg-backup-cos.sh" \
    && bad "坏字节已经落到 cron 引用的路径上（只喊了出事，没挡住出事）" \
    || ok "落点副本里查不到漂移标记"
  ledger_after=$(wc -l < "$WORK/published.log" | tr -d '[:space:]')
  [ "$ledger_after" = "$ledger_before" ] && ok "失败轮没往台账写成功凭证（台账行数 $ledger_after 未涨）" \
    || bad "失败轮往台账多写了 $((ledger_after - ledger_before)) 行凭证"
  left=$(ls -1A "$OPS" | grep -c '^\.' || true)
  [ "$left" = "0" ] && ok "中止后临时文件已清掉（不留隐藏半成品）" \
    || bad "落点残留 $left 个隐藏临时文件：$(ls -1A "$OPS" | tr '\n' ' ')"
fi

echo "[7/8] N2 反证：两次比对各摘一次 —— 摘掉 mv 前那行 ⇒ 坏字节真落落点；摘掉 mv 后那行（Joe 的 M5）⇒ 另一行拦得住"
MUT_C="$WORK/publish-mutC.sh"
MUT_D="$WORK/publish-mutD.sh"
if [ -z "$REAL_INSTALL" ]; then
  skip "install 缺失（第 6 组已就此判红），本组依赖同一注入"
elif mutate "$PUB_SRC" "$MUT_C" '[ "$src_sha" = "$(sha_of "$tmp")" ] || abort_tmp' \
          '  : # T394 反证：摘掉 T394 新加的 mv 之前那次字节比对'; then
  : > "$SHIM_LOG"
  run_with_shim "$MUT_C" "$WORK/pub-drift-mut.out"; rc=$?
  [ "$rc" != "0" ] && ok "摘掉新行后仍判红（rc=$rc）⇒ 不是漏检" || bad "变异体放行了漂移字节"
  grep -qF -- '签发后字节不一致' "$WORK/pub-drift-mut.out" \
    && ok "只在 mv 之后才发现（走的正是 T383 旧那一句文案）" \
    || bad "文案不符：$(tr '\n' '|' < "$WORK/pub-drift-mut.out")"
  grep -qF -- 't394-drift-marker' "$OPS/pg-backup-cos.sh" \
    && ok "但落点副本已被坏字节改写 ⇒ 新那行管的是「挡住」，不是「喊了出事」（这行被摘掉从此可观测）" \
    || bad "落点没被改写 ⇒ 反证与正证没有区分度，这一格不算拦住过什么"
  left=$(ls -1A "$OPS" | grep -c '^\.' || true)
  [ "$left" = "0" ] && ok "变异体轮结束后落点无隐藏临时文件" || bad "变异体轮残留 $left 个隐藏临时文件"
  # 恢复现场：换回正式脚本重签，落点重新与源一致，后面的组不受这格污染
  run_pub; rc=$?
  expect_rc 0 "$rc" "清掉变异体后用正式脚本重签（恢复落点）"
  [ "$(sha_of "$TREE/pg-backup-cos.sh")" = "$(sha_of "$OPS/pg-backup-cos.sh")" ] \
    && ok "落点恢复到与源字节一致" || bad "落点没恢复（后面各组将跑在脏夹具上）"
fi
# Joe 的 M5 原样：摘掉 mv 之后那次比对。T383 时代这一摘整轮全绿（17/0/2 rc=0）；
# T394 之后 mv 之前那行还在，同一注入它先就把坏字节挡在落点之外 ⇒ 这一格从「不可观测」变「有守卫」。
if [ -n "$REAL_INSTALL" ]; then
  if mutate "$PUB_SRC" "$MUT_D" '[ "$src_sha" = "$dst_sha" ] || die "签发后字节不一致' \
          '  : # T394 反证 M5：摘掉 mv 之后那次字节比对'; then
    : > "$SHIM_LOG"
    run_with_shim "$MUT_D" "$WORK/pub-drift-m5.out"; rc=$?
    [ "$rc" != "0" ] && ok "M5 变异体判红（rc=$rc）⇒ Joe 报的「摘掉这行测试全绿」从此不成立" \
      || bad "M5 变异体仍返回 0 ⇒ N2 那格没真补上"
    grep -qF -- '临时副本与源字节不一致' "$WORK/pub-drift-m5.out" \
      && ok "红的原因是 mv 之前那行拦下（新行接替了守卫）" \
      || bad "M5 轮文案不符：$(tr '\n' '|' < "$WORK/pub-drift-m5.out")"
    grep -qF -- 't394-drift-marker' "$OPS/pg-backup-cos.sh" \
      && bad "M5 变异体把坏字节签进了 cron 引用的路径" || ok "M5 变异体同样没让坏字节落落点"
  fi
fi

echo "[8/8] N3：本轮只签出一半时 —— 旧后验（只问存在）全绿（M4 现场），新后验（本轮签发集）判红"
TREE2="$WORK/repo2/scripts/backup"
OPS2="$WORK/ops2/bin"
LOG2="$WORK/published2.log"
mkdir -p "$TREE2"
printf '#!/usr/bin/env bash\necho backup-B\n' > "$TREE2/pg-backup-cos.sh"
printf '#!/usr/bin/env bash\necho drill-B\n'  > "$TREE2/staging-restore-drill.sh"
run2() {   # $1=被测脚本 $2=stdout 落点；夹具与 1~7 组完全隔离
  CRON_SOURCE_DIR="$TREE2" CRON_PUBLISH_DIR="$OPS2" CRON_PUBLISH_LOG="$LOG2" \
    bash "$1" testhead >"$2" 2>&1
}
run2 "$PUB_SRC" "$WORK/pub2-base.out"; rc=$?
expect_rc 0 "$rc" "第 8 组夹具首轮全签"
[ "$(wc -l < "$LOG2" | tr -d '[:space:]')" = "2" ] && ok "夹具台账 2 行（两条都签出去了）" \
  || bad "夹具台账行数不是 2：$(tr '\n' '|' < "$LOG2")"
# M4 的前提：本轮少一条源，但前置齐验不再拦（改外部夹具状态，不改被测脚本内部逻辑）
mv "$TREE2/pg-backup-cos.sh" "$WORK/hide2.sh"
half_sha=$(sha_of "$OPS2/pg-backup-cos.sh")
MUT_A="$WORK/publish-mutA.sh"
MUT_B="$WORK/publish-mutB.sh"
if mutate "$PUB_SRC" "$MUT_A" '[ -r "$SOURCE_DIR/$req" ] || die "源目录缺 cron' \
        '  [ -r "$SOURCE_DIR/$req" ] || continue   # T394 反证 M4：前置齐验被摘成 continue' \
   && mutate "$MUT_A" "$MUT_B" 'case " $signed_names " in' \
        '  if [ -r "$PUBLISH_DIR/$req" ]; then continue; fi   # T394 反证：后验退回只问存在
  case " $signed_names " in' 1; then
  run2 "$MUT_B" "$WORK/pub2-m4.out"; rc=$?
  expect_rc 0 "$rc" "M4 现场（前置 continue + 只问存在的旧后验）"
  grep -qF -- '完成：1 个脚本已外置' "$WORK/pub2-m4.out" \
    && ok "旧后验下本轮只签出 1 条却全绿 ⇒ 这就是 Joe 报的「少签和全签在回执里长得一样」" \
    || bad "M4 现场文案不符：$(tr '\n' '|' < "$WORK/pub2-m4.out")"
  [ "$(grep -c 'script=pg-backup-cos.sh' "$LOG2" | tr -d '[:space:]')" = "1" ] \
    && ok "台账里那条缺项仍只有首轮那一行（旧后验不会补写也不会报）" \
    || bad "台账缺项行数异常：$(grep -c 'script=pg-backup-cos.sh' "$LOG2")"
  # 同一夹具状态、同一前置摘法，只把后验换回「本轮签发集 含 REQUIRED」
  run2 "$MUT_A" "$WORK/pub2-new.out"; rc=$?
  [ "$rc" != "0" ] && ok "换上新后验后同一现场判红（rc=$rc）⇒ N3 这格有牙" || bad "新后验没拦住本轮少签"
  grep -qF -- '本轮没签出去' "$WORK/pub2-new.out" && ok "文案点名「本轮没签出去」" \
    || bad "文案没说是少签：$(tr '\n' '|' < "$WORK/pub2-new.out")"
  grep -qF -- '陈旧副本' "$WORK/pub2-new.out" && ok "文案分辨出「落点那份是上一轮留下的」" \
    || bad "文案没分辨陈旧副本：$(tr '\n' '|' < "$WORK/pub2-new.out")"
  [ "$half_sha" = "$(sha_of "$OPS2/pg-backup-cos.sh")" ] && ok "判红期间陈旧副本未被改写" \
    || bad "判红路径动了落点副本"
  # 第三种形态：落点里连陈旧副本都没有 ⇒ 该走另一句文案（两种少签必须分得开）
  mv "$OPS2/pg-backup-cos.sh" "$WORK/ops2-stale.bak"
  run2 "$MUT_A" "$WORK/pub2-none.out"; rc=$?
  [ "$rc" != "0" ] && ok "落点无副本时同样判红（rc=$rc）" || bad "落点无副本却返回 0"
  grep -qF -- 'cron 下一轮直接无可执行文件' "$WORK/pub2-none.out" && ok "无副本分支走的是另一句文案（可分辨）" \
    || bad "无副本分支文案不符：$(tr '\n' '|' < "$WORK/pub2-none.out")"
  grep -qF -- '陈旧副本' "$WORK/pub2-none.out" && bad "无副本轮误报成「陈旧副本」" || ok "无副本轮没误报成陈旧副本"
fi

echo ""
echo "[t383-publish-test] 断言：通过 $PASS / 不通过 $FAILED / 跳过 $SKIPPED（跳过项均为本平台权限位不可信，现网 Linux 实测兜底）"
[ "$FAILED" = "0" ] || exit 1
exit 0
