#!/usr/bin/env bash
# T393 · N1：部署链接线守卫（deploy-staging.sh 里那几处「接上去才算收口」的接线，有没有东西守着）
# 用法：bash scripts/deploy/deploy-chain-wiring-test.sh
#
# 起因（Joe T383 工程验收 N1 / M6 M7 M8）：把 ⑦-b 的签发调用删掉、把 EXIT trap 退回旧的纯清理、
#   把 ⓪ 段必需项检查删掉 —— 两个既有行为测试全绿、bash -n 九份全绿。
#   也就是「这两处收口真的接在部署链上」这件事 CI 一点都不看；将来谁重构 deploy-staging.sh
#   把它挪出成功路径或删掉，门禁不会红，而它正是 T383 判据 1 的持续成立条件。
#
# 本测试的姿势与既有两个行为测试同口径：正向 + 反证。
#   正向：现文逐条要求接线在位。
#   反证：在临时副本上把这行按 Joe 的 M6/M7/M8 三例逐一拆掉（外加两例：改调工作树字节、
#         把守卫挪到 git pull 之后；T463 起再加三例：删起跑门调用、删其必需项检查、
#         把门挪到 ⓪ 段基线采集之后；T471 起再加两例：prometheus 重载退回在仓库根发出、
#         重载那行的 stderr 被收回 /dev/null ⇒ 共十例），要求「对应那一格必判红」——
#         不这么打一遍，就不知道断言是活的还是摆设（T364 那格假绿的教训）。
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_STAGING="${1:-$HERE/deploy-staging.sh}"
PUBLISH_SH="$HERE/publish-cron-scripts.sh"
GUARD_SH="$HERE/cron-reference-guard.sh"
GATE_SH="$HERE/e2e-gate-check.sh"
[ -r "$DEPLOY_STAGING" ] || { echo "[FAIL] 找不到被测部署脚本：$DEPLOY_STAGING"; exit 1; }
[ -r "$PUBLISH_SH" ] || { echo "[FAIL] 找不到 publish-cron-scripts.sh"; exit 1; }
[ -r "$GUARD_SH" ] || { echo "[FAIL] 找不到 cron-reference-guard.sh（N6 的期望件）"; exit 1; }
[ -r "$GATE_SH" ] || { echo "[FAIL] 找不到 e2e-gate-check.sh（T463 起跑门本体，接线对象缺失即本守卫无从判定）"; exit 1; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/t393-wiring.XXXXXX") || { echo "[FAIL] mktemp 失败"; exit 1; }
chmod 700 "$WORK"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAILED=0
ok()  { echo "  OK   $*"; PASS=$((PASS + 1)); }
bad() { echo "  FAIL $*"; FAILED=$((FAILED + 1)); }

# ---- 接线扫描：对一份 deploy-staging.sh 逐条判定，打印 [FAIL][编号] 行，回传不通过条数 ----
scan_file() {
  local f="$1"
  local n_fail=0
  local pull_ln guard_call_ln
  req() { # $1=编号 $2=固定串 $3=说明
    if grep -qF -- "$2" "$f"; then
      echo "  [PASS][$1] $3"
    else
      echo "  [FAIL][$1] $3 —— 现文里找不到这行：$2"
      n_fail=$((n_fail + 1))
    fi
  }
  req W1a '[ -r "$WORKTREE_SCRIPT" ] || fail'            '⓪ 段验部署脚本本体可读'
  req W1b '[ -r "$SELFCHK_SRC" ] || fail'                '⓪ 段验自检脚本可读'
  req W1c '[ -r "$PUBLISH_SRC" ] || fail'                '⓪ 段验外置签发脚本可读（T383 起 ⑦-b 必需）'
  req W1d '[ -r "$CRON_GUARD_SRC" ] || fail'             '⓪ 段验 cron 引用守卫脚本可读（T393 N6 起 ⓪-b 必需）'
  req W2a 'install -m 400 "$PUBLISH_SRC"'                '签发脚本以只读快照副本执行，不解释工作树字节'
  req W2b 'install -m 400 "$CRON_GUARD_SRC"'             'cron 守卫同样进快照副本'
  req W3a "trap 't383_finish \"\$?\"' EXIT"               'EXIT trap 挂到中止回执函数（不是旧的纯清理）'
  req W3b '"$SELFCHK" receipt'                            '回执函数真的调 receipt 落凭据'
  req W4a 'bash "$SNAP_ROOT/publish-cron-scripts.sh"'    '⑦-b 调快照副本里的签发脚本'
  req W4b 'bash "$SNAP_ROOT/cron-reference-guard.sh" || fail' '⓪-b 调 cron 引用守卫且判红即中止本轮'
  req W5  'bash "$SELFCHK" verify'                       '⑧ 段自检 verify 在位'
  req W6  'fail "快照环境缺失'                             '绕开 ⓪ 直接执行快照外入口时拒绝跑'
  # T463：起跑门（e2e-real 打 staging 的 run 在跑就不起部署）—— 与 ⑦-b 同族：
  #   删掉这一行，bash -n 与 e2e-gate-test.sh 全绿（那边测的是门自己，不是「部署链接着门」）。
  req W8a '[ -r "$GATE_SRC" ] || fail'                   '⓪-pre 验起跑门脚本可读（T463 起必需项）'
  req W8b 'bash "$GATE_SRC" || fail'                     '⓪-pre 调起跑门且判红即中止本轮'
  # T471：prometheus 重载的发出目录。为什么这一格归本守卫而不是只靠新的行为测试：
  #   prometheus-reload-test.sh 抽的是 T471-RELOAD-BEGIN/END 之间那一段，段内它测得准；
  #   但「这一整块还在不在部署链里、有没有被搬出 ⑤ 段」属于接线面，与 W4a/W8b 同族（Joe T383 N1 的原始教训）。
  req W10 '( cd "$STAGING_DIR" && sudo docker compose restart prometheus )' \
                                                    '⑤ 段 prometheus 重载在 staging 的 compose 目录里发出（T471）'
  # W13 是「不许出现」判定（T471 的另一半：stderr 不再静默）。判定只看命令行，不看注释行 ——
  #   本脚本的说明段里就写着「2>/dev/null」三个字（讲的就是改前那个坑），按整文件搜会把自己判红。
  #   形状取「同一行上 restart prometheus 之后跟着 2>/dev/null」，改前那一行正是这个形状。
  if grep -nE '^[^#]*restart prometheus[^#]*2>/dev/null' "$f" >/dev/null 2>&1; then
    echo "  [FAIL][W13] prometheus 重载又把 stderr 收回 /dev/null：T471 修的静默面回来了"
    n_fail=$((n_fail + 1))
  else
    echo "  [PASS][W13] prometheus 重载的 stderr 没被吞（行内没有 2>/dev/null）"
  fi

  # W7 是位置判定，不是字面判定：守卫必须在 ① 步 git pull 之前 —— 排在 pull 之后就已经晚了
  #    （① 步含 git checkout -- . ，那正是可能把被引用文件换掉的动作本身）。
  #    锚点必须避开注释：脚本开头「规格」段里也写着 git pull github main（第 5 行），
  #    按整串取行号会取到那行，于是正向必判红（本机实测踩过，见本卡交件说明「过程瑕疵」）。
  # 锚点必须带 bash 前缀：⓪ 段那行 install -m 400 "$CRON_GUARD_SRC" "$SNAP_ROOT/cron-reference-guard.sh" || fail
  #   也含同一个文件名，少写 bash 就会取到 install 行的行号 —— 那是「装副本」不是「调守卫」，
  #   拿它判位置等于把真调用点挪到 git pull 之后也照样判绿（本机实测：改前 W7 取到第 70 行）。
  pull_ln=$(grep -nE '^[[:space:]]*git pull github main' "$f" | head -1 | cut -d: -f1)
  guard_call_ln=$(grep -nF 'bash "$SNAP_ROOT/cron-reference-guard.sh" || fail' "$f" | head -1 | cut -d: -f1)
  if [ -z "$pull_ln" ] || [ -z "$guard_call_ln" ]; then
    echo "  [FAIL][W7] 位置判定取不到行号（pull=$pull_ln guard=$guard_call_ln）"
    n_fail=$((n_fail + 1))
  elif [ "$guard_call_ln" -lt "$pull_ln" ]; then
    echo "  [PASS][W7] cron 守卫跑在 ① 步 git pull 之前（第 $guard_call_ln 行 < 第 $pull_ln 行）：拒绝路径零变更"
  else
    echo "  [FAIL][W7] cron 守卫排在 git pull 之后（第 $guard_call_ln 行 >= 第 $pull_ln 行）：那时工作树已被换过，判红也留不下干净基线"
    n_fail=$((n_fail + 1))
  fi

  # W9 同样是位置判定（T463 判据 1「基线采集之前即退出」）：起跑门必须排在 ⓪ 段那条
  #   selfcheck record 之前 —— 过了 record，本轮就已经采集了工作树基线（并随后 exec 快照），
  #   「拒绝路径零副作用」这条判据的字面要求就不再成立。
  #   锚点取整串 `bash "$SNAP_ROOT/selfcheck-deploy-script.sh" record`：注释里也出现 "record"
  #   这个词（⓪ 段说明段），只按单词取行号会取到注释。
  local gate_ln record_ln
  gate_ln=$(grep -nF 'bash "$GATE_SRC" || fail' "$f" | head -1 | cut -d: -f1)
  record_ln=$(grep -nF 'bash "$SNAP_ROOT/selfcheck-deploy-script.sh" record' "$f" | head -1 | cut -d: -f1)
  if [ -z "$gate_ln" ] || [ -z "$record_ln" ]; then
    echo "  [FAIL][W9] 位置判定取不到行号（gate=$gate_ln record=$record_ln）"
    n_fail=$((n_fail + 1))
  elif [ "$gate_ln" -lt "$record_ln" ]; then
    echo "  [PASS][W9] 起跑门跑在 ⓪ 段基线采集之前（第 $gate_ln 行 < 第 $record_ln 行）：判红即中止时未采基线、未装快照"
  else
    echo "  [FAIL][W9] 起跑门排在基线采集之后（第 $gate_ln 行 >= 第 $record_ln 行）：判据 1 的「不产生任何部署副作用」已不成立"
    n_fail=$((n_fail + 1))
  fi
  return "$n_fail"
}

echo "[1/4] 正向：现网在役的 deploy-staging.sh 逐格接线在位"
if scan_file "$DEPLOY_STAGING" > "$WORK/pos.out" 2>&1; then
  grep '\[PASS\]' "$WORK/pos.out"
  ok "deploy-staging.sh 接线逐格全通过（PASS 行见上，格数随 req 项数走，不在此钉死）"
else
  SCAN_RC=$?
  cat "$WORK/pos.out"
  bad "deploy-staging.sh 接线判红 $SCAN_RC 格（上面逐条点名）"
fi

echo "[2/4] 反证：按 Joe M6/M7/M8 三例逐格拆接线，要求对应那一格必判红"
# 每例：标签 | 期望被判红的编号 | sed 程序（对临时副本动手，绝不碰真本）
counter() { # $1=标签 $2=期望编号 $3=sed 脚本
  local label="$1" want="$2" prog="$3" tmp
  tmp="$WORK/mut-$(echo "$label" | tr -c 'A-Za-z0-9' '_').sh"
  sed "$prog" "$DEPLOY_STAGING" > "$tmp" || { bad "$label：造反证副本失败"; return; }
  if cmp -s "$tmp" "$DEPLOY_STAGING"; then
    bad "$label：改动没落到被测文本上（反证无牙，等于没测）"
    return
  fi
  scan_file "$tmp" > "$WORK/mut.out" 2>&1; local rc=$?
  if [ "$rc" = "0" ]; then
    bad "$label：拆掉接线后扫描仍全绿 ⇒ 该格没有守卫"
  elif ! grep -q "\[FAIL\]\[$want\]" "$WORK/mut.out"; then
    bad "$label：判红了但不是那一格（期望 $want，实得 $(grep -o '\[FAIL\]\[[A-Za-z0-9_]*\]' "$WORK/mut.out" | tr '\n' ' '))"
  else
    ok "$label：$want 如期判红（红格清单 $(grep -o '\[FAIL\]\[[A-Za-z0-9_]*\]' "$WORK/mut.out" | sed 's/\[FAIL\]//' | tr -d '\n')，共 $rc 格）"
  fi
}
# M6 例：删掉 ⑦-b 的签发调用整行
counter "删掉 ⑦-b 签发调用（M6）" W4a '/bash "\$SNAP_ROOT\/publish-cron-scripts.sh"/d'
# M7 例：trap 退回旧的纯清理（T383 改前那一行）
counter "trap 退回旧纯清理（M7）" W3a "s|trap 't383_finish \"\$?\"' EXIT|trap 'rm -rf \"\${SNAP_ROOT:-}\" \"\${STATE_FILE:-}\"' EXIT|"
# M8 例：删掉 ⓪ 段 PUBLISH_SRC 必需项检查
counter "删掉 ⓪ 段签发脚本必需项检查（M8）" W1c '/\[ -r "\$PUBLISH_SRC" \] || fail/d'
# 追加例一：⑦-b 改成解释工作树字节（T364 口径要求的是快照副本）
counter "⑦-b 改调工作树字节而非快照副本" W4a 's|bash "\$SNAP_ROOT/publish-cron-scripts.sh"|bash "$PROJECT_ROOT/scripts/deploy/publish-cron-scripts.sh"|'
# 追加例二：cron 守卫挪到 git pull 之后（字面在位、位置已失效，只有 W7 抓得住）。
# 这一例不能简单 sed 替换：要「把整行搬走」而不是改字节，且落点必须锚在真正的 git pull 命令行，
# 而不是规格段里那句注释 —— 注释那行也算「在 pull 之前」，搬过去等于没搬。
counter_awk() { # $1=标签 $2=期望编号 $3=awk 程序 $4=此格必须仍然通过的编号（证明只搬了调用点）
  local label="$1" want="$2" prog="$3" keep="$4" tmp
  tmp="$WORK/mut-awk.sh"
  awk "$prog" < "$DEPLOY_STAGING" > "$tmp" || { bad "$label：造反证副本失败"; return; }
  if cmp -s "$tmp" "$DEPLOY_STAGING"; then
    bad "$label：改动没落到被测文本上（反证无牙，等于没测）"
    return
  fi
  scan_file "$tmp" > "$WORK/mut.out" 2>&1; local rc=$?
  if [ "$rc" = "0" ]; then
    bad "$label：拆掉接线后扫描仍全绿 ⇒ 该格没有守卫"
  elif ! grep -q "\[FAIL\]\[$want\]" "$WORK/mut.out"; then
    bad "$label：判红了但不是那一格（期望 $want，实得 $(grep -o '\[FAIL\]\[[A-Za-z0-9_]*\]' "$WORK/mut.out" | tr '\n' ' '))"
  elif ! grep -q "\[PASS\]\[$keep\]" "$WORK/mut.out"; then
    bad "$label：$want 是判红了，但 $keep 也跟着红 ⇒ 这一例动的不止调用点，判红归因不纯"
  else
    ok "$label：$want 如期单独判红（$keep 仍在位，共 $rc 格红）"
  fi
}
counter_awk "cron 守卫挪到 ① 步 git pull 之后" W7 '
  index($0, "bash \"$SNAP_ROOT/cron-reference-guard.sh\" || fail") { saved = saved $0 "\n"; next }
  { print }
  /^[[:space:]]*git pull github main/ { printf "%s", saved; saved = "" }
' W4b
# T463 例一：删掉 ⓪-pre 的起跑门调用整行。为什么要单列这一格：e2e-gate-test.sh 整张测的是
#   「门自己判得对不对」，把调用点删了它照样绿 —— 与 ⑦-b 同族（Joe T383 N1 的原始教训）。
#   注：这一例删掉后 W9 会因取不到调用点行号跟着红（共 2 格），属同一条接线的两面而非归因不纯 ——
#   判红集合含 W8b 即为预期；W9 单独被拆的那一格是下面的例三（字面在位、位置失效）。
counter "删掉 ⓪-pre 起跑门调用（T463 接线反面）" W8b '/^bash "\$GATE_SRC" || fail/d'
# T463 例二：删掉 ⓪-pre 的必需项可读性检查（只拆这一格，W8b 必须仍绿，否则判红归因不纯）
counter "删掉 ⓪-pre 起跑门必需项检查" W8a '/\[ -r "\$GATE_SRC" \] || fail/d'
# T463 例三：把调用点搬到 ⓪ 段基线采集之后（字面仍在位、位置已失效，只有 W9 抓得住）。
#   这一格正是派发单判据 1 的括号那句「基线采集之前即退出」：只按字面判的话，
#   把两行调换顺序照样全绿，而部署副作用已经发生了一次。
counter_awk "起跑门挪到 ⓪ 段基线采集之后" W9 '
  index($0, "bash \"$GATE_SRC\" || fail") { saved = saved $0 "\n"; next }
  { print }
  index($0, "bash \"$SNAP_ROOT/selfcheck-deploy-script.sh\" record") { printf "%s", saved; saved = "" }
' W8b
# T471 例一：把 prometheus 重载的发出目录退回不切目录（改前形态）。W10 抓的就是这个：字面还在，
#   只是不再从 staging 的 compose 目录发出 —— 第 2 至 20 轮每轮都栽在这半件事上。
counter "prometheus 重载退回在仓库根发出（T471）" W10 \
  's@( cd "\$STAGING_DIR" \&\& sudo docker compose restart prometheus )@sudo docker compose restart prometheus@'
# T471 例二：把重载那行的 stderr 收回 /dev/null（静默面回来）。W13 是唯一该红的格 ——
#   W10 的字面（发出目录那段）不该被这一例碰到，红了就是判读不纯。
counter "prometheus 重载的 stderr 被吞回 /dev/null（T471）" W13 's@2>"\$PROM_ERR"@2>/dev/null@'

echo "[3/4] N6 期望件与签发件的清单不得各说各话"
guard_expect=$(grep '^EXPECTED_REF_SCRIPTS=' "$GUARD_SH" | cut -d'"' -f2)
publish_req=$(grep '^REQUIRED=' "$PUBLISH_SH" | cut -d'"' -f2)
[ -n "$guard_expect" ] && [ -n "$publish_req" ] \
  && ok "两份清单都取得到（守卫：$guard_expect / 签发：$publish_req）" \
  || bad "取不到清单：guard=[$guard_expect] publish=[$publish_req]"
missing=0
for name in $publish_req; do
  case " $guard_expect " in
    *" $name "*) : ;;
    *) missing=$((missing + 1)); echo "    签发件要求外置、守卫却没期望它：$name" ;;
  esac
done
[ "$missing" = "0" ] && ok "守卫期望集合 ⊇ 签发必需集合（漏一条就是无声覆盖缺口）" \
  || bad "守卫期望集合少 $missing 条 —— 往 REQUIRED 加脚本时必须同步加这里"

echo "[4/4] 语法：四份脚本 bash -n"
for f in "$DEPLOY_STAGING" "$GUARD_SH" "$PUBLISH_SH" "$GATE_SH"; do
  if bash -n "$f" 2>"$WORK/syn.err"; then
    ok "bash -n $(basename "$f")"
  else
    bad "bash -n $(basename "$f")：$(tr '\n' '|' < "$WORK/syn.err")"
  fi
done

echo ""
echo "[t393-wiring-test] 断言：通过 $PASS / 不通过 $FAILED"
[ "$FAILED" = "0" ] || exit 1
exit 0
