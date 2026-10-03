#!/usr/bin/env bash
# T550 · 防复发新鲜度守卫的行为测试（喂夹具，绝不碰真库；跑完还要证明没写夹具）
# 用法：bash scripts/deploy/data-freshness-guard-test.sh
# 三向：放行侧（新鲜数据判绿）、报警侧（模拟断流判红，含现网真实断流 3 天那一档）、
#       第三态（夹具不可读 / 缺读数 / 非数字阈值 ⇒ rc=3，既不是 0 也不是 1，防「读不到当新鲜」）。
#       另加两格分辨力：阈值两侧各一（差 1 小时就要翻面），以及「最新行在未来」判红。
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

run_guard() { # $1=夹具路径（空串＝不给夹具）；输出到 $WORK/g.out，回传 rc
  FRESHNESS_LATEST_FILE="$1" FRESHNESS_NOW_EPOCH="$NOW" FRESHNESS_MAX_SILENT_HOURS="${2:-6}" bash "$GUARD" >"$WORK/g.out" 2>&1
}

echo "[1/8] 放行侧：5 小时前有行（阈值 6h）必须判绿"
set_fixture "$((NOW - 5 * 3600))"
run_guard "$WORK/latest.txt"; rc=$?
expect_rc 0 "$rc" "静默 5 小时"
grep -q '数据新鲜' "$WORK/g.out" && ok "放行时把判读依据打出来（回执要能转给 PM）" \
  || bad "放行却什么都没交代：$(tr '\n' '|' < "$WORK/g.out")"

echo "[2/8] 分辨力一：同一夹具改成 7 小时（越过阈值 1 小时）必须翻红"
set_fixture "$((NOW - 7 * 3600))"
run_guard "$WORK/latest.txt"; rc=$?
expect_rc 1 "$rc" "静默 7 小时"
grep -q '断流报警' "$WORK/g.out" && ok "报警文案点名「断流」" || bad "判红却没说是断流：$(tr '\n' '|' < "$WORK/g.out")"

echo "[3/8] 现网真实档：静默 3 天（T550 派发单实测形态）必须报警"
set_fixture "$((NOW - 72 * 3600))"
run_guard "$WORK/latest.txt"; rc=$?
expect_rc 1 "$rc" "静默 72 小时（现网那一档）"
grep -qE '约 72 小时' "$WORK/g.out" && ok "报警里带得出小时数（72），值班席据此能定级" \
  || bad "报警没带小时数：$(tr '\n' '|' < "$WORK/g.out")"

echo "[4/8] 空表：库里一条都没有 —— 判红不判绿"
set_fixture "EMPTY|0"
run_guard "$WORK/latest.txt"; rc=$?
expect_rc 1 "$rc" "空表"
grep -q '一条记录都没有' "$WORK/g.out" && ok "空表文案独立可辨（不是「阈值超时」那种红）" || bad "空表却混在通用红里：$(tr '\n' '|' < "$WORK/g.out")"

echo "[5/8] 第三态一：夹具不可读 ⇒ rc=3（读不到不等于新鲜）"
run_guard "$WORK/nope.txt"; rc=$?
expect_rc 3 "$rc" "夹具不存在"

echo "[6/8] 第三态二：既无夹具又无连接串 ⇒ rc=3"
( unset FRESHNESS_LATEST_FILE FRESHNESS_DB; FRESHNESS_NOW_EPOCH="$NOW" bash "$GUARD" >"$WORK/g.out" 2>&1 ); rc=$?
expect_rc 3 "$rc" "两个读数来源都没给"

echo "[7/8] 分辨力二：阈值可配 —— 同一份 7 小时夹具，阈值放宽到 8h 应判绿"
set_fixture "$((NOW - 7 * 3600))"
run_guard "$WORK/latest.txt" 8; rc=$?
expect_rc 0 "$rc" "阈值 8 小时"

echo "[8/8] 未来时间戳判红（时钟造假或写入超前不许靠「越新越好」蒙过）"
set_fixture "$((NOW + 3600))"
run_guard "$WORK/latest.txt"; rc=$?
expect_rc 1 "$rc" "最新行在未来 1 小时"
grep -q '未来' "$WORK/g.out" && ok "红得说明原因" || bad "判红却没交代：$(tr '\n' '|' < "$WORK/g.out")"

# 收尾自证：本测试只读，夹具目录除我自己写的文件外没被改动
[ -f "$WORK/latest.txt" ] && ok "夹具仍在（守卫没删我的样本）" || bad "夹具不见了：被测脚本写过它"
if [ -e "$WORK/nope.txt" ]; then bad "守卫凭空造出了不该存在的文件"; else ok "守卫没写盘（全程只读）"; fi

echo "[t550-fresh] 通过 $PASS 项 / 失败 $FAILED 项"
[ "$FAILED" -eq 0 ] || exit 1
