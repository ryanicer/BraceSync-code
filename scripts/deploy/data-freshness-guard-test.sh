#!/usr/bin/env bash
# T550 · 防复发新鲜度守卫的行为测试（喂夹具，绝不碰真库；跑完还要证明没写夹具）
# 用法：bash scripts/deploy/data-freshness-guard-test.sh
# 三向：放行侧（新鲜数据判绿）、报警侧（模拟断流判红，含现网真实断流 3 天那一档）、
#       第三态（夹具不可读 / 缺读数 / 非数字阈值 ⇒ rc=3，既不是 0 也不是 1，防「读不到当新鲜」）。
#       另加两格分辨力：阈值两侧各一（差 1 小时就要翻面），以及「最新行在未来」判红。
#       T585 补第 9 至 16 格：前 8 格全走夹具那一侧，psql 那条腿一次都没被走过（这正是 L41 那枚
#       多出来的右括号能活到今天的机制），所以第 9 至 16 格用 PATH 前面的一颗桩 psql 把那条腿
#       的放行 / 报警 / 空表 / 报错透出 / 括号配平 / epoch 形状 / ISO 兼容 / 空串各钉一格。桩绝不连任何库。
#       T562 C-3 补第 17 至 22 格：默认值那颗 4 与「低于 4 按 4 生效」那条下限此前没有任何一格钉着
#       （旧第 1 格把阈值当入参喂成 6，于是「默认」那一面结构性地没人测），本批六格补这一面，
#       并把 stdout 与 stderr 分成两路各数一次（合并面看不见「只走单流」那一形）。
set -uo pipefail

GUARD="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/data-freshness-guard.sh"
[ -r "$GUARD" ] || { echo "[FAIL] 找不到被测脚本 $GUARD"; exit 1; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/t550-fresh.XXXXXX") || { echo "[FAIL] mktemp 失败"; exit 1; }
chmod 700 "$WORK"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAILED=0
ok()  { echo "  OK   $*"; PASS=$((PASS + 1)); }
bad() { echo "  FAIL $*"; FAILED=$((FAILED + 1)); }
expect_rc() { # $1=期望 rc $2=实得 rc $3=说明
  if [ "$1" = "$2" ]; then ok "$3（rc=$2）"; else bad "$3：期望 rc=$1，实得 rc=$2"; fi
}

NOW=1759480000                 # 钉死「现在」，让全部格次可复算
set_fixture() { printf '%s\n' "$1" > "$WORK/latest.txt"; }
OUT="$WORK/g.out"              # 合并面（stdout 之后并入 stderr），既有格次沿用它
ERRF="$WORK/g.err"             # 单独一面：报警与 WARN 是否只走单流，要靠这一面分开数

run_guard() { # $1=夹具路径 $2=阈值（不给＝让被测脚本用自己的默认值；旧形把 6 写死在这里，默认那一面就没人钉）
  [ -n "$1" ] || { bad "run_guard 没拿到夹具路径，这一发根本没跑成，读数不作数"; return 9; }
  : > "$ERRF"
  if [ -n "${2:-}" ]; then
    FRESHNESS_LATEST_FILE="$1" FRESHNESS_NOW_EPOCH="$NOW" FRESHNESS_MAX_SILENT_HOURS="$2" bash "$GUARD" >"$OUT" 2>"$ERRF"
  else
    FRESHNESS_LATEST_FILE="$1" FRESHNESS_NOW_EPOCH="$NOW" bash "$GUARD" >"$OUT" 2>"$ERRF"
  fi
  rc=$?
  cat "$ERRF" >> "$OUT"
  return $rc
}

echo "[1/22] 放行侧：3 小时前有行、不给阈值入参（走脚本自己的默认 4h）必须判绿"
set_fixture "$((NOW - 3 * 3600))"
run_guard "$WORK/latest.txt"; rc=$?
expect_rc 0 "$rc" "静默 3 小时 / 默认阈值"
grep -q '数据新鲜' "$WORK/g.out" && ok "放行时把判读依据打出来（回执要能转给 PM）" \
  || bad "放行却什么都没交代：$(tr '\n' '|' < "$WORK/g.out")"
grep -q '阈值 4 小时' "$WORK/g.out" && ok "默认那颗数当场钉死为 4（C-3 裁的值；回退成 6 这一格就红）" \
  || bad "默认阈值没打出「4 小时」这一形：$(tr '\n' '|' < "$WORK/g.out")"

echo "[2/22] 分辨力一：同一形状改到 5 小时（越界 1 小时）必须翻红 —— 旧默认 6 恰恰放过这一档"
set_fixture "$((NOW - 5 * 3600))"
run_guard "$WORK/latest.txt"; rc=$?
expect_rc 1 "$rc" "静默 5 小时 / 默认阈值"
grep -q '断流报警' "$WORK/g.out" && ok "报警文案点名「断流」" || bad "判红却没说是断流：$(tr '\n' '|' < "$WORK/g.out")"

echo "[3/22] 现网真实档：静默 3 天（T550 派发单实测形态）必须报警"
set_fixture "$((NOW - 72 * 3600))"
run_guard "$WORK/latest.txt"; rc=$?
expect_rc 1 "$rc" "静默 72 小时（现网那一档）"
grep -qE '约 72 小时' "$WORK/g.out" && ok "报警里带得出小时数（72），值班席据此能定级" \
  || bad "报警没带小时数：$(tr '\n' '|' < "$WORK/g.out")"

echo "[4/22] 空表：库里一条都没有 —— 判红不判绿"
set_fixture "EMPTY|0"
run_guard "$WORK/latest.txt"; rc=$?
expect_rc 1 "$rc" "空表"
grep -q '一条记录都没有' "$WORK/g.out" && ok "空表文案独立可辨（不是「阈值超时」那种红）" || bad "空表却混在通用红里：$(tr '\n' '|' < "$WORK/g.out")"

echo "[5/22] 第三态一：夹具不可读 ⇒ rc=3（读不到不等于新鲜）"
run_guard "$WORK/nope.txt"; rc=$?
expect_rc 3 "$rc" "夹具不存在"

echo "[6/22] 第三态二：既无夹具又无连接串 ⇒ rc=3"
: > "$ERRF"
( FRESHNESS_LATEST_FILE= FRESHNESS_NOW_EPOCH="$NOW" bash "$GUARD" >"$OUT" 2>"$ERRF" ); rc=$?
cat "$ERRF" >> "$OUT"
expect_rc 3 "$rc" "两个读数来源都没给"

echo "[7/22] 分辨力二：配置仍可放宽 —— 同一份 7 小时夹具，阈值显式给 8h 应判绿（下限只管「低于 4」那一侧）"
set_fixture "$((NOW - 7 * 3600))"
run_guard "$WORK/latest.txt" 8; rc=$?
expect_rc 0 "$rc" "阈值 8 小时"
grep -q '阈值 8 小时' "$WORK/g.out" && ok "生效值打的是配置那颗 8，不是被顶成 4" \
  || bad "配置 8 小时没如实打出生效值：$(tr '\n' '|' < "$WORK/g.out")"

echo "[8/22] 未来时间戳判红（时钟造假或写入超前不许靠「越新越好」蒙过）"
set_fixture "$((NOW + 3600))"
run_guard "$WORK/latest.txt"; rc=$?
expect_rc 1 "$rc" "最新行在未来 1 小时"
grep -q '未来' "$WORK/g.out" && ok "红得说明原因" || bad "判红却没交代：$(tr '\n' '|' < "$WORK/g.out")"

# ── 第 9 至 14 格：psql 那条腿（T585 补）。桩只顶在本测试的 PATH 前面，绝不连任何库。 ──
mkdir -p "$WORK/bin"
cat > "$WORK/bin/psql" <<'STUB'
#!/usr/bin/env bash
# 桩 psql：把被测脚本递给真 psql 的那条 SQL 原文落盘，再按环境变量回一份读数或一句报错
sql=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-c" ]; then sql="$a"; fi
  prev="$a"
done
printf '%s' "$sql" >"${STUB_SQL_OUT:-/dev/null}"
case "${STUB_MODE:-reading}" in
  syntax-error)
    printf 'psql: ERROR:  syntax error at or near ")"\n' >&2
    exit 3
    ;;
  empty-string)
    printf '\n'
    exit 0
    ;;
esac
printf '%s|0\n' "${STUB_READING:-EMPTY}"
exit "${STUB_RC:-0}"
STUB
chmod 700 "$WORK/bin/psql"
[ -x "$WORK/bin/psql" ] || { echo "[FAIL] 桩 psql 造不出来（第 9 至 14 格无从谈起）"; exit 1; }

psql_leg() { # $1=桩要回的那颗读数；不给夹具、给假连接串，逼被测脚本走 psql 那一支。$2=阈值（不给＝默认）
  : > "$ERRF"
  if [ -n "${2:-}" ]; then
    FRESHNESS_LATEST_FILE= FRESHNESS_DB="postgres://stub.invalid/t585-selftest" \
      FRESHNESS_NOW_EPOCH="$NOW" FRESHNESS_MAX_SILENT_HOURS="$2" \
      STUB_READING="$1" STUB_SQL_OUT="$WORK/psql.sql" PATH="$WORK/bin:$PATH" \
      bash "$GUARD" >"$OUT" 2>"$ERRF"
  else
    FRESHNESS_LATEST_FILE= FRESHNESS_DB="postgres://stub.invalid/t585-selftest" \
      FRESHNESS_NOW_EPOCH="$NOW" \
      STUB_READING="$1" STUB_SQL_OUT="$WORK/psql.sql" PATH="$WORK/bin:$PATH" \
      bash "$GUARD" >"$OUT" 2>"$ERRF"
  fi
  rc=$?
  cat "$ERRF" >> "$OUT"
  return $rc
}

echo "[9/22] psql 腿放行侧：桩回一颗 3 小时前的 epoch（第 1 格的同款，换这条腿走）必须判绿"
psql_leg "$((NOW - 3 * 3600))"; rc=$?
expect_rc 0 "$rc" "psql 腿静默 3 小时 / 默认阈值"
grep -q '数据新鲜' "$WORK/g.out" && ok "这条腿的读数真进了判读（不是走夹具那一侧蒙过去的）" \
  || bad "psql 腿放行却没交代：$(tr '\n' '|' < "$WORK/g.out")"

echo "[10/22] psql 腿报警侧：同一颗读数改到 5 小时前，越过默认阈值必须翻红"
psql_leg "$((NOW - 5 * 3600))"; rc=$?
expect_rc 1 "$rc" "psql 腿静默 5 小时 / 默认阈值"
grep -q '断流报警' "$WORK/g.out" && ok "这条腿也能报断流" || bad "psql 腿判红却没说是断流：$(tr '\n' '|' < "$WORK/g.out")"

echo "[11/22] psql 腿空表侧：桩回 EMPTY（真库实测 EMPTY 那颗从 coalesce 的文本臂出来）必须判红"
psql_leg "EMPTY"; rc=$?
expect_rc 1 "$rc" "psql 腿空表"
grep -q '一条记录都没有' "$WORK/g.out" && ok "空表文案在这条腿上也独立可辨" \
  || bad "psql 腿空表却混在通用红里：$(tr '\n' '|' < "$WORK/g.out")"

echo "[12/22] psql 腿报错侧：桩以 syntax-error 形退出 ⇒ 守卫判红，且报错原文不许再被丢进 null"
STUB_MODE=syntax-error psql_leg "EMPTY"; rc=$?
expect_rc 3 "$rc" "psql 报错走第三态（fail-closed 方向没倒置）"
grep -q 'syntax error' "$WORK/g.out" && ok "psql 那句报错透到本脚本的输出面了（T585 格三）" \
  || bad "报错又被静默吞了：$(tr '\n' '|' < "$WORK/g.out")"
grep -q '左右括号' "$WORK/g.out" && ok "die 文案里带得出「先数括号」那句指引" \
  || bad "判红却没留下定位指引：$(tr '\n' '|' < "$WORK/g.out")"

echo "[13/22] 括号配平尺：喂桩 psql 实际收到的那颗 SQL 原文（不是手抄），左右必须配平；注坏一次尺必须咬"
face_bytes=$(wc -c < "$WORK/psql.sql" | tr -d '[:space:]')
[ "$face_bytes" -gt 0 ] && ok "被扫那面在场（$face_bytes 字节）" \
  || { bad "被扫那面零字节，下面的配平读数一律不作数"; }
sql_face=$(cat "$WORK/psql.sql")
paren_ruler() { # $1=要量的那颗串；先打三面读数，再判左右是否等颗
  local s=$1 l r
  l=$(printf '%s' "$s" | tr -cd '(' | wc -c | tr -d '[:space:]')
  r=$(printf '%s' "$s" | tr -cd ')' | wc -c | tr -d '[:space:]')
  echo "    读数：左括号 $l 颗 / 右括号 $r 颗 / 差 $((l - r)) 颗"
  [ "$l" -gt 0 ] && [ "$l" = "$r" ]
}
paren_ruler "$sql_face" && ok "psql 腿收到的那颗 SQL 左右配平" \
  || bad "那颗 SQL 的括号没配平（上面一行是当场读数）"
paren_ruler "${sql_face})" && bad "注坏那一发尺没咬住（尺没牙）" || ok "注进去那一颗右括号被尺数出来了（负对照：尺有牙）"
paren_ruler "" && bad "空面被尺当成配平（尺没牙）" || ok "空面那颗 0 被尺拒了（负对照：不报假绿）"

echo "[14/22] 时间那一格的形状尺：收到的 SQL 必须取 epoch，不许退回裸的日期串"
echo "$sql_face" | grep -q 'extract(epoch' && ok "取的是 epoch 秒（真库实测：不带零时区的裸日期串在 UTC 以外的宿主机上会把「距今多久」整档算偏）" \
  || bad "SQL 退回日期串那一形了（这条腿的判读会随时区漂）"
echo "SELECT max(ts) AT TIME ZONE 'UTC' FROM t" | grep -q 'extract(epoch' && bad "形状尺在明知没货那一面上也报了命中（尺没牙）" \
  || ok "同一把尺喂已知不含 epoch 那一形回不命中（正对照）"

echo "[15/22] ISO 串兼容腿：真库上「AT TIME ZONE 'UTC'」旧形给出的就是裸串，解析那一支不能因为我改取 epoch 就断"
iso_now=$(date -u -d "@$((NOW - 3600))" '+%Y-%m-%dT%H:%M:%SZ')
psql_leg "$iso_now|0"; rc=$?
expect_rc 0 "$rc" "psql 腿回 ISO 串（带 Z）仍解析成功"
grep -q '距今 3600 秒' "$WORK/g.out" && ok "ISO 那一支解出的偏移恰为 3600 秒（带 Z 不受宿主机时区影响）" \
  || bad "ISO 串解析出的偏移不是 3600 秒：$(tr '\n' '|' < "$WORK/g.out")"
iso_naive=$(date -u -d "@$((NOW - 3600))" '+%Y-%m-%d %H:%M:%S')
psql_leg "$iso_naive|0"; rc_naive=$?
naive_shown=$(grep -o '距今 [-0-9]* 秒' "$WORK/g.out" | head -1)
echo "    信息面（不判绿不判红）：不带 Z 那一形的实得是「$naive_shown」，宿主机时区不同这里就不同——"
echo "    这正是真库那一腿改取 epoch 秒的理由：守卫不能把「距今多久」交给宿主机时区决定。"
if [ "$rc_naive" = 0 ] || [ "$rc_naive" = 1 ]; then ok "不带 Z 那一形仍走解析支（没崩成第三态）"; else bad "不带 Z 那一形回了第三态 rc=$rc_naive（解析支断了？）"; fi

echo "[16/22] psql 腿空串侧：桩 rc=0 但整份输出为空 ⇒ 守卫必须 rc=3，不许把空串当「库里真没行」放行"
STUB_MODE=empty-string psql_leg "EMPTY"; rc=$?
expect_rc 3 "$rc" "psql 腿空串走第三态（区分不了「查询失败」和「真没行」就不判绿）"
grep -q '读数为空串' "$WORK/g.out" && ok "空串文案独立可辨" || bad "空串却混进别的红里：$(tr '\n' '|' < "$WORK/g.out")"

echo "[17/22] 下限顶起那一发：配置 2 + 静默 3 小时 ⇒ 判绿，但生效值必须是 4，且 WARN 双流各自在场"
set_fixture "$((NOW - 3 * 3600))"
: > "$ERRF"
FRESHNESS_LATEST_FILE="$WORK/latest.txt" FRESHNESS_NOW_EPOCH="$NOW" FRESHNESS_MAX_SILENT_HOURS=2 \
  bash "$GUARD" >"$OUT" 2>"$ERRF"; rc=$?
expect_rc 0 "$rc" "配置 2 被顶到 4 之后，3 小时仍在阈内"
stdout_hits=$(grep -c 'FRESHNESS_MAX_SILENT_HOURS=2' "$OUT" || true)
stderr_hits=$(grep -c 'FRESHNESS_MAX_SILENT_HOURS=2' "$ERRF" || true)
echo "    读数：stdout 面 $stdout_hits 颗 / stderr 面 $stderr_hits 颗（两路分开数，合并面看不见单流那一形）"
if [ "$stdout_hits" -ge 1 ] && [ "$stderr_hits" -ge 1 ]; then
  ok "那句 WARN 在 stdout 与 stderr 上各在场一次（调度侧只收一路也看得见）"
else
  bad "WARN 只走单流：stdout=$stdout_hits stderr=$stderr_hits"
fi
grep -q '阈值 4 小时' "$OUT" && ok "生效值打的是 4（不是配置那颗 2）" \
  || bad "生效值没顶到 4：$(tr '\n' '|' < "$OUT")"

echo "[18/22] 下限不是放行那一形：配置 2 + 静默 5 小时 ⇒ 顶到 4 之后仍越界，必须判红"
set_fixture "$((NOW - 5 * 3600))"
run_guard "$WORK/latest.txt" 2; rc=$?
expect_rc 1 "$rc" "顶起不等于放过（5 小时仍越界）"
grep -q '断流报警' "$WORK/g.out" && ok "红的文案仍是断流（不是被 WARN 替换掉）" \
  || bad "判红文案丢了：$(tr '\n' '|' < "$WORK/g.out")"

echo "[19/22] 配置 0 不许把闸拆掉：0 低于下限，按 4 生效，72 小时那档照旧判红"
set_fixture "$((NOW - 72 * 3600))"
run_guard "$WORK/latest.txt" 0; rc=$?
expect_rc 1 "$rc" "配置 0 经下限顶起后仍判红"
grep -q '阈值 4 小时' "$WORK/g.out" && ok "0 没有当成「阈值 0 秒」用（生效值仍是 4）" \
  || bad "配置 0 的生效值不是 4：$(tr '\n' '|' < "$WORK/g.out")"

echo "[20/22] 非数字配置仍走第三态：实得那颗要原样打回报错面，且必须在取数动作之前就停住"
set_fixture "$((NOW - 3 * 3600))"
rm -f "$WORK/psql.sql"
if [ -e "$WORK/psql.sql" ]; then bad "前置不成立：取数落盘面没清掉，下面那条「没取数」读数会失真"; else ok "前置：桩的 SQL 落盘面已清空"; fi
psql_leg "$((NOW - 3 * 3600))" abc; rc=$?
expect_rc 3 "$rc" "非数字入参走第三态"
if [ -e "$WORK/psql.sql" ]; then bad "非法入参仍先跑了一次取数（入参门排在取数腿之后）"; else ok "非法入参在取数动作之前停住（桩没被调用）"; fi
grep -qF '实得「abc」' "$WORK/g.out" && ok "报错原文带回实得那颗 abc（不兜默认、不静默放行）" \
  || bad "报错没点名实得值：$(tr '\n' '|' < "$WORK/g.out")"

echo "[21/22] 注坏负对照（内存副本，不落仓）：把默认那颗改回旧的 6 ⇒ 第 2 格那发必须翻成判绿 —— 证第 2 格的红是这颗 4 撑着的"
before_sha=$(sha256sum "$GUARD" | cut -d' ' -f1)
sed 's/FRESHNESS_MAX_SILENT_HOURS:-4/FRESHNESS_MAX_SILENT_HOURS:-6/' "$GUARD" > "$WORK/guard-old6.sh"
if cmp -s "$GUARD" "$WORK/guard-old6.sh"; then
  bad "注入没改到任何字节（这份副本与被测脚本同枚，下面两读一律不作数）"
else
  ok "副本字节确实变了（被测脚本本体没动，改动只落在这份内存面副本上）"
fi
hit6=$(grep -c 'FRESHNESS_MAX_SILENT_HOURS:-6' "$WORK/guard-old6.sh" || true)
echo "    读数：副本里旧默认那一形命中 $hit6 颗（押 1）"
[ "$hit6" = "1" ] && ok "副本按预期被改回 6" || bad "副本改坏或改多了（命中 $hit6 颗）"
set_fixture "$((NOW - 5 * 3600))"
: > "$ERRF"
FRESHNESS_LATEST_FILE="$WORK/latest.txt" FRESHNESS_NOW_EPOCH="$NOW" bash "$WORK/guard-old6.sh" >"$OUT" 2>"$ERRF"; rc_old6=$?
echo "    读数：旧默认 6 那一发 rc=$rc_old6（期望 0，即 5 小时被放过）"
expect_rc 0 "$rc_old6" "注坏那一发翻成判绿 —— 反证第 2 格有牙：默认从 6 变 4 才是它判红的原因"
after_sha=$(sha256sum "$GUARD" | cut -d' ' -f1)
[ "$before_sha" = "$after_sha" ] && ok "被测脚本本体哈希前后同枚（注坏只落副本）" \
  || bad "被测脚本本体被改动了（哈希前后不同枚）"

echo "[22/22] 格次普查尺（本测试自己的面）：声明颗数、标签行颗数、末颗编号、缩进面颗数四面并打"
declared=22
labels_col0=$(grep -c '^echo "\[[0-9]' "$0" || true)
labels_indented=$(grep -c '^  echo "\[[0-9]' "$0" || true)
last_num=$(grep '^echo "\[[0-9]' "$0" | tail -1 | sed 's/^echo "\[\([0-9]*\)\/[0-9]*\].*/\1/')
echo "    读数：声明 $declared 颗 / 标签行（列 0 且首字符是数字）$labels_col0 颗 / 缩进面标签行 $labels_indented 颗 / 末颗编号 $last_num"
echo "    口径：收尾那句 echo \"[t550-fresh...\" 也顶在列 0，所以针要带数字首字符；不带数字的那把尺上一发数出 23 颗（当场红过，已改）"
[ "$labels_col0" = "$declared" ] && ok "标签行颗数 = 声明颗数（没有漏编号的格，也没有多出来的编号）" \
  || bad "标签行 $labels_col0 与声明 $declared 不同枚（有格没编号或编号重复）"
[ "$labels_indented" = "0" ] && ok "缩进面 0 颗（22 条标签全在列 0；将来谁把标签缩进，上一条读数就会少一颗，这一腿就是那道闸）" \
  || bad "缩进面出现 $labels_indented 颗标签，列 0 面那把尺会结构性少报"
[ "$last_num" = "$declared" ] && ok "最后一颗标签编号 = 声明颗数 $declared" \
  || bad "末颗编号=$last_num 与声明 $declared 不同枚"
if [ -s "$WORK/guard-old6.sh" ]; then rm -f "$WORK/guard-old6.sh"; fi
[ -e "$WORK/guard-old6.sh" ] && bad "注坏副本没清掉（会留进夹具目录）" || ok "注坏副本已清理（本测试只读，产物不落仓）"

# 收尾自证：本测试只读，夹具目录除我自己写的文件外没被改动
[ -f "$WORK/latest.txt" ] && ok "夹具仍在（守卫没删我的样本）" || bad "夹具不见了：被测脚本写过它"
if [ -e "$WORK/nope.txt" ]; then bad "守卫凭空造出了不该存在的文件"; else ok "守卫没写盘（全程只读）"; fi

echo "[t550-fresh+t562] 通过 $PASS 项 / 失败 $FAILED 项"
[ "$FAILED" -eq 0 ] || exit 1
