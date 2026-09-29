#!/usr/bin/env bash
# T479 · DEPLOY OK 收尾段「执行副本身份」行为测试（把 deploy-staging.sh 收尾那一截抽出来真跑）
# 用法：bash scripts/deploy/deploy-ok-runid-test.sh [被测 deploy-staging.sh]
#
# 为什么要有这一支（与 e2e-gate-test.sh / deploy-chain-wiring-test.sh / prometheus-reload-test.sh 同族）：
#   T479 派发单顺带腿 1 的判据是「DEPLOY OK 行含执行脚本副本 sha256 前缀，消除 pass1/pass2 副本歧义」，
#   这是行为判据 —— 字面 grep 只能证明那行代码还在，证明不了它跑起来真给的是「本轮 exec 的那一份」的哈希。
#   背景（派发单第三节的原话）：第二十一轮同轮两份部署日志形状互反，Ella 靠字节对平才钉住判据。
#   这里用沙箱把收尾段真跑起来：先把快照副本与工作树真本写成【不同字节】，再看日志里落到哪个 ——
#   取错侧（工作树真本）恰是这条判据唯一可能的失效方式，且它在纯字面守卫下完全看不出来。
#
# 被测对象：deploy-staging.sh 里 T479-RUNID-BEGIN / T479-RUNID-END 这对标记之间的整段
#   （含哈希取值、UNAVAILABLE 兜底、两条分隔线与 DEPLOY OK 行）。标记被删或配对坏了 ⇒ 本测试响亮判红，
#   不许「没断言所以全绿」。
#
# 正向五腿（夹具由本脚本现生成，不碰真仓、不发任何真实命令）：
#   L1 正常腿      快照副本可读                → DEPLOY OK 行带 run_script_sha256=<该文件 sha256 前 12 位>（现算对平）+ rc=0 + 块形状没碎
#   L2 副本区分腿  快照 != 工作树真本（两份字节）→ 落的是快照那份、明确不等真本那份；换一次快照字节再跑 ⇒ 两次前缀必不同（这才是「消除 pass1/pass2 歧义」的可执行化）
#   L3 快照缺件腿  $SNAP_ROOT 目录在、文件不在  → UNAVAILABLE 且 DEPLOY OK 照打、rc=0（一行日志没资格掐掉已成功部署）
#   L4 无快照环境腿 整个 SNAP_ROOT 变量未定义    → 同上 UNAVAILABLE + rc=0（收尾段不许依赖调用面）
#   L5 工具缺件腿  PATH 最前放一个 rc=127 的假 sha256sum → 同上 UNAVAILABLE + rc=0
#
# 反证四例（在临时副本上退回旧写法/坏写法，要求对应那几格必判红；真本一个字不碰）：
#   M1 收尾行去掉 run_script_sha256 字段   → L1 的「字段在位」格必红
#   M2 哈希源从快照副本改成工作树真本       → L2 的「取的是快照」与「不等真本」两格必红（判据本体）
#   M3 去掉取哈希那行的 `|| true` 兜底      → L3 的 rc=0 格必红（set -euo pipefail 下 sha256sum 一失败整轮就死 —— 这正是「日志行不许改成败」那半件事）
#   M4 删掉 UNAVAILABLE 兜底行             → L3 的「UNAVAILABLE」格必红（取不到时宁可明说取不到，不许打空字段）
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="${1:-$HERE/deploy-staging.sh}"
[ -r "$SRC" ] || { echo "[FAIL] 找不到被测部署脚本：$SRC"; exit 1; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/t479-runid.XXXXXX") || { echo "[FAIL] mktemp 失败"; exit 1; }
chmod 700 "$WORK"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAILED=0
ok()  { echo "  OK   $*"; PASS=$((PASS + 1)); }
bad() { echo "  FAIL $*"; FAILED=$((FAILED + 1)); }

REPO="$WORK/repo"
SNAP="$WORK/snap"
BIN="$WORK/bin"
OUT="$WORK/out.log"
SEG="$WORK/segment.sh"
mkdir -p "$REPO/scripts/deploy" "$SNAP" "$BIN"

# ---- 抽段：标记之间逐字节取，不改一个字符（改了测的就不是现网在役的那份） ----
awk '/T479-RUNID-BEGIN/{f=1; next} /T479-RUNID-END/{f=0} f' "$SRC" > "$SEG"
if [ ! -s "$SEG" ]; then
  bad "抽段为空：$SRC 里 T479-RUNID-BEGIN / T479-RUNID-END 标记没配对或整段被删 —— 本测试无从判定，不许当绿"
  echo ""
  echo "[t479-runid-test] 断言：通过 $PASS / 不通过 $FAILED"
  exit 1
fi
if ! grep -qF 'run_script_sha256=' "$SEG"; then
  bad "抽出的段落里没有 run_script_sha256 字段：标记框错了范围（测了个空壳）"
fi
# 抽段自检：这一段只有收尾那十几行。若把脚本开头的变量区也框进来，说明标记名在文件别处被当正文提过
#   （本机实测踩过：规格段里写了 BEGIN 的字面串，awk 从第 29 行一路取到收尾 —— 测的就不是收尾段了）。
if grep -qE '^PROJECT_ROOT=' "$SEG"; then
  bad "抽出的段落覆盖了脚本开头的变量区（$(wc -l < "$SEG" | tr -d '[:space:]') 行）：标记名在别处被当正文提过，抽段框已坏"
  echo ""
  echo "[t479-runid-test] 断言：通过 $PASS / 不通过 $FAILED"
  exit 1
fi

# 哈希工具在沙箱外也要用（算期望值），先确认可用；不可用就直接判红而不是「跳过断言」。
if ! command -v sha256sum >/dev/null 2>&1; then
  bad "本机取不到 sha256sum —— 期望值算不出来，本测试无法判定（不许念成绿）"
  echo ""
  echo "[t479-runid-test] 断言：通过 $PASS / 不通过 $FAILED"
  exit 1
fi
digest12() { sha256sum "$1" | cut -c1-12; }   # 与段内同口径：整串摘要的前 12 位（夹具是纯 ASCII，cut -c 按字节等价）

# ---- runner：与真本同口径的前导（set -euo pipefail + 同名 log/err/fail + 同样变量名） ----
# $1=段文件 $2=snap_root 变量给不给(give|unset) $3=假 sha256sum(quiet|none)
gen_runner() {
  {
    echo 'set -euo pipefail'
    if [ "$3" = quiet ]; then
      echo "export PATH=\"$BIN:\$PATH\""
    fi
    echo "SHA='s1'"
    echo "TAG='staging-s1'"
    echo "WORKTREE_SCRIPT=\"$REPO/scripts/deploy/deploy-staging.sh\""
    if [ "$2" = give ]; then
      echo "SNAP_ROOT=\"$SNAP\""
    fi
    echo 'log()  { echo "\033[1;32m[deploy]\033[0m $*"; }'
    echo 'err()  { echo "\033[1;31m[ERROR]\033[0m $*" >&2; }'
    echo 'fail() { err "$*"; exit 1; }'
    cat "$1"
  } > "$WORK/run.sh"
}

leg_spec() { # 回显「给不给SNAP_ROOT 假工具」
  case "$1" in
    L1) echo "give none" ;;
    L2) echo "give none" ;;
    L3) echo "give none" ;;
    L4) echo "unset none" ;;
    L5) echo "give quiet" ;;
    *)  echo "??" ;;
  esac
}

# 假 sha256sum：只放在 $BIN（PATH 最前），模拟 coreutils 缺件 —— 段里那条 2>/dev/null 之外还要 rc≠0。
cat > "$BIN/sha256sum" <<'FAKE_SHA'
#!/usr/bin/env bash
echo '假 sha256sum：本机没有这个工具' >&2
exit 127
FAKE_SHA
chmod 700 "$BIN/sha256sum"

# scan_leg <腿名> <段文件>：跑这一腿并逐条判定，打印 [PASS][编号]/[FAIL][编号]，回传不通过条数
scan_leg() {
  local leg="$1" seg="$2"
  local n_fail=0 spec give fake snap_a snap_b run_rc exp_snap exp_wt exp_after
  spec=$(leg_spec "$leg")
  read -r give fake <<EOF
$spec
EOF

  # 夹具复位：快照副本与工作树真本默认「同一份字节」，L2 才在它们之间做区分。
  snap_a="$WORK/snap-A.txt"
  snap_b="$WORK/snap-B.txt"
  printf 'pass1 bytes\nsha line 1\n' > "$snap_a"
  printf 'pass1 bytes\nsha line 1\n' > "$snap_b"
  install -m 400 "$snap_a" "$SNAP/deploy-staging.sh"
  printf 'worktree bytes (the copy step 1 will overwrite)\n' > "$REPO/scripts/deploy/deploy-staging.sh"

  req() { # $1=编号 $2=日志里必须有的固定串 $3=说明
    if grep -qF -- "$2" "$OUT"; then
      echo "  [PASS][$1] $3"
    else
      echo "  [FAIL][$1] $3 —— 日志里没有：$2"
      n_fail=$((n_fail + 1))
    fi
  }
  ban() { # $1=编号 $2=日志里不许出现的固定串 $3=说明
    if grep -qF -- "$2" "$OUT"; then
      echo "  [FAIL][$1] $3 —— 日志里出现了不该出现的：$2"
      n_fail=$((n_fail + 1))
    else
      echo "  [PASS][$1] $3"
    fi
  }
  eq() { # $1=编号 $2=实测值 $3=期望值 $4=说明
    if [ "$2" = "$3" ]; then
      echo "  [PASS][$1] $4（实测 $2）"
    else
      echo "  [FAIL][$1] $4 —— 实得 [$2]，期望 [$3]"
      n_fail=$((n_fail + 1))
    fi
  }
  neq() { # $1=编号 $2=实测值 $3=不许等于的值 $4=说明
    if [ -n "$2" ] && [ "$2" != "$3" ]; then
      echo "  [PASS][$1] $4（实测 $2，确不等 $3）"
    else
      echo "  [FAIL][$1] $4 —— 实得 [$2]，而它不许等于 [$3]"
      n_fail=$((n_fail + 1))
    fi
  }

  logged() { sed -n "s/.*run_script_sha256=\\([^ )]*\\).*/\\1/p" "$OUT" | head -1; }

  case "$leg" in
    L1)
      gen_runner "$seg" "$give" "$fake"
      bash "$WORK/run.sh" > "$OUT" 2> "$WORK/err.log"
      run_rc=$?
      exp_snap=$(digest12 "$SNAP/deploy-staging.sh")
      req  L1.1 'DEPLOY OK'                    '收尾块还在打 DEPLOY OK（改这半不能把 OK 行改没）'
      req  L1.2 'run_script_sha256='           'OK 行要带上执行副本身份字段'
      req  L1.3 "run_script_sha256=$exp_snap"  "字段值等于快照副本 sha256 前 12 位（本次实测摘要：$exp_snap）"
      local val
      val=$(logged)
      if printf '%s' "$val" | grep -qE '^[0-9a-f]{12}$'; then
        echo "  [PASS][L1.4] 值是 12 位十六进制前缀（实测 $val）"
      else
        echo "  [FAIL][L1.4] 值不是 12 位十六进制前缀 —— 实得 [$val]"
        n_fail=$((n_fail + 1))
      fi
      req  L1.5 'DEPLOY OK  (sha=s1, tag=staging-s1, run_script_sha256=' \
                                             '原有两个字段（sha/tag）没被挪位，新字段追加在后'
      local bars
      bars=$(grep -c '================' "$OUT" || true)
      eq   L1.6 "$bars" 2                     '两条 ==== 分隔线仍在（整块形状没碎，回执 reader 按这两条定位收尾）'
      eq   L1.7 "$run_rc" 0                   '正常腿退出码 0'
      ;;
    L2)
      # 把快照副本与工作树真本写成【不同字节】—— 这正是 T479 要消歧的那个现场。
      printf 'pass1 snapshot bytes\n' > "$snap_b"
      install -m 400 "$snap_b" "$SNAP/deploy-staging.sh"
      printf 'worktree bytes before step 1 pull\n' > "$REPO/scripts/deploy/deploy-staging.sh"
      gen_runner "$seg" "$give" "$fake"
      bash "$WORK/run.sh" > "$OUT" 2> "$WORK/err.log"
      run_rc=$?
      exp_snap=$(digest12 "$SNAP/deploy-staging.sh")
      exp_wt=$(digest12 "$REPO/scripts/deploy/deploy-staging.sh")
      local v1
      v1=$(logged)
      eq   L2.1 "$v1" "$exp_snap"             '两份字节不同时，日志取的是【快照副本】那份（exec 的真执行体）'
      neq  L2.2 "$v1" "$exp_wt"               '并且明确不等工作树真本那份（取错侧就是这条判据的失效方式）'
      # 换一次快照字节再跑：同轮 pass1/pass2 必然给出两个不同前缀 —— 这就是「消除副本歧义」本身。
      printf 'pass2 snapshot bytes (the re-run in the same window)\n' > "$snap_b"
      install -m 400 "$snap_b" "$SNAP/deploy-staging.sh"
      gen_runner "$seg" "$give" "$fake"
      bash "$WORK/run.sh" > "$OUT" 2> "$WORK/err.log"
      local v2
      v2=$(logged)
      exp_after=$(digest12 "$SNAP/deploy-staging.sh")
      eq   L2.3 "$v2" "$exp_after"            '第二次跑取的是新快照副本自己的摘要（不是沿用上一次的值）'
      neq  L2.4 "$v2" "$v1"                   '两次前缀必不同 ⇒ 同轮两份日志从此可判读（派发单那句歧义的可执行化）'
      eq   L2.5 "$run_rc" 0                   '区分腿退出码 0'
      ;;
    L3)
      rm -f "$SNAP/deploy-staging.sh"
      gen_runner "$seg" "$give" "$fake"
      bash "$WORK/run.sh" > "$OUT" 2> "$WORK/err.log"
      run_rc=$?
      req  L3.1 'run_script_sha256=UNAVAILABLE' '取不到摘要时明说取不到（不许打空字段骗过 reader）'
      req  L3.2 'DEPLOY OK'                     '取不到摘要不抹掉 DEPLOY OK：这一格只是让日志可读，不改部署成败'
      eq   L3.3 "$run_rc" 0                     'M3 的靶子：去掉 || true 后这格必红（set -e 下日志行能把成功轮掐成失败）'
      ban  L3.4 'run_script_sha256=)'           '不许出现「字段名后直接空值」的形态（M4 的靶子）'
      ;;
    L4)
      gen_runner "$seg" unset "$fake"
      bash "$WORK/run.sh" > "$OUT" 2> "$WORK/err.log"
      run_rc=$?
      req  L4.1 'run_script_sha256=UNAVAILABLE' 'SNAP_ROOT 整个未定义也走兜底（收尾段不许依赖调用面）'
      eq   L4.2 "$run_rc" 0                     '未定义变量不许把 set -u 的轮次掐掉'
      ;;
    L5)
      gen_runner "$seg" "$give" quiet
      bash "$WORK/run.sh" > "$OUT" 2> "$WORK/err.log"
      run_rc=$?
      req  L5.1 'run_script_sha256=UNAVAILABLE' 'sha256sum 缺件（假工具 rc=127，PATH 最前）也走兜底'
      req  L5.2 'DEPLOY OK'                     '工具缺件不抹掉 DEPLOY OK'
      eq   L5.3 "$run_rc" 0                     '工具缺件不改部署成败'
      ;;
  esac
  return "$n_fail"
}

echo "[1/4] 正向：现网在役的收尾段逐腿真跑判定"
for leg in L1 L2 L3 L4 L5; do
  if scan_leg "$leg" "$SEG" > "$WORK/$leg.pos" 2>&1; then
    grep '\[PASS\]' "$WORK/$leg.pos"
    ok "$leg 逐格全通过（$(grep -c '\[PASS\]' "$WORK/$leg.pos") 格）"
  else
    SCAN_RC=$?
    cat "$WORK/$leg.pos"
    bad "$leg 判红 $SCAN_RC 格（上面逐条点名）"
  fi
done

echo "[2/4] 反证：把修法逐格退回旧写法/坏写法，要求对应那几格必判红（真本一个字不碰）"
mutate_seg() { # $1=sed 程序 → 产出 $WORK/mut.sh；改动没落地就算无牙
  sed "$1" "$SEG" > "$WORK/mut.sh"
  if cmp -s "$WORK/mut.sh" "$SEG"; then
    echo ""
    return 1
  fi
  return 0
}
expect_red() { # $1=标签 $2=腿 $3=期望必红的编号
  local label="$1" leg="$2" want="$3" rc
  scan_leg "$leg" "$WORK/mut.sh" > "$WORK/mut.out" 2>&1
  rc=$?
  if [ "$rc" = 0 ]; then
    bad "$label：退回旧写法后 $leg 仍全绿 ⇒ 正向那格是摆设（无牙）"
  elif ! grep -q "\[FAIL\]\[$want\]" "$WORK/mut.out"; then
    bad "$label：$leg 判红了但不是那一格（期望 $want，实得 $(grep -o '\[FAIL\]\[[A-Za-z0-9_.]*\]' "$WORK/mut.out" | tr '\n' ' ')）"
  else
    ok "$label：$leg 的 $want 如期判红（红格清单 $(grep -o '\[FAIL\]\[[A-Za-z0-9_.]*\]' "$WORK/mut.out" | sed 's/\[FAIL\]//' | tr -d '\n')，共 $rc 格）"
  fi
}
# M1：收尾行去掉新字段（退回改前那行）。
if mutate_seg 's@, run_script_sha256=\$RUN_SCRIPT_SHA256@@'; then
  expect_red "M1 收尾行退回无字段" L1 L1.2
else
  bad "M1 的改动没落到段落文本上（反证无牙，等于没测）"
fi
# M2：哈希源从快照副本改成工作树真本 —— 派发单那句「执行脚本副本」的唯一实质失效方式。
if mutate_seg 's@"\${SNAP_ROOT:-}/deploy-staging.sh"@"$WORKTREE_SCRIPT"@'; then
  expect_red "M2 哈希源退回工作树真本" L2 L2.1
  expect_red "M2 哈希源退回工作树真本" L2 L2.2
else
  bad "M2 的改动没落到段落文本上（反证无牙，等于没测）"
fi
# M3：去掉 `|| true` —— 快照缺件时 sha256sum rc=1，pipefail 让整条赋值失败，set -e 直接掐掉本轮。
if mutate_seg 's@| cut -c1-12 || true)@| cut -c1-12)@'; then
  expect_red "M3 取哈希不再兜底" L3 L3.3
else
  bad "M3 的改动没落到段落文本上（反证无牙，等于没测）"
fi
# M4：删掉 UNAVAILABLE 兜底行 —— 取不到时宁可打空字段，也不肯明说「取不到」。
if mutate_seg '/^\[ -n "\$RUN_SCRIPT_SHA256" \] || RUN_SCRIPT_SHA256=UNAVAILABLE$/d'; then
  expect_red "M4 删掉 UNAVAILABLE 兜底" L3 L3.1
else
  bad "M4 的改动没落到段落文本上（反证无牙，等于没测）"
fi

echo "[3/4] 文法与期望值来源：抽出的段落要能独立解析，期望摘要要有第二把尺"
if bash -n "$SEG" 2>"$WORK/syn.err"; then
  ok "bash -n 抽出的 T479 段（标记配对正确、语句完整）"
else
  bad "bash -n 抽出的 T479 段：$(tr '\n' '|' < "$WORK/syn.err") —— 多半是标记框坏了"
fi
for f in "$SRC" "$HERE/deploy-chain-wiring-test.sh" "$HERE/prometheus-reload-test.sh"; do
  if bash -n "$f" 2>"$WORK/syn2.err"; then
    ok "bash -n $(basename "$f")"
  else
    bad "bash -n $(basename "$f")：$(tr '\n' '|' < "$WORK/syn2.err")"
  fi
done
# 期望值的第二把尺：同一份夹具用 openssl 再算一次，两把尺不一致就不许用 sha256sum 单方结论判绿。
probe="$WORK/probe.txt"
printf 'second-ruler bytes\n' > "$probe"
if command -v openssl >/dev/null 2>&1; then
  ossl=$(openssl dgst -sha256 "$probe" 2>/dev/null | sed -n 's/.*= *\([0-9a-f]\{12\}\).*/\1/p')
  sha12=$(digest12 "$probe")
  if [ "$ossl" = "$sha12" ]; then
    ok "期望摘要双尺对平（sha256sum 与 openssl 对同一夹具同前缀 $sha12）"
  else
    bad "期望摘要两把尺不一致：sha256sum=$sha12 openssl=$ossl —— 期望值不可信，正向格的红绿都不作数"
  fi
else
  echo "  SKIP 第二把尺未测：本机没有 openssl —— 期望值只由 sha256sum 单尺给出（本机实测有此工具，CI ubuntu 亦有）"
fi

echo "[4/4] 判定基数自证：五腿正向格数与反证红格都来自本次实测"
pos_cells=$(grep -ho '\[PASS\]\[[A-Za-z0-9_.]*\]' "$WORK"/L*.pos 2>/dev/null | wc -l | tr -d '[:space:]')
echo "  本次正向通过格数 = $pos_cells（L1 七格 / L2 五格 / L3 四格 / L4 两格 / L5 三格，逐腿清单见上，不在本脚本里钉死）"
[ "$pos_cells" -ge 21 ] && ok "五腿正向格全部落地（$pos_cells 格 ≥ 21）" \
  || bad "正向格数只有 $pos_cells（不足 21 ⇒ 有腿没跑起来）"

echo ""
echo "[t479-runid-test] 断言：通过 $PASS / 不通过 $FAILED"
[ "$FAILED" = "0" ] || exit 1
exit 0
