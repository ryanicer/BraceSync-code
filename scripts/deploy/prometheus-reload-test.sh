#!/usr/bin/env bash
# T471 · prometheus reload 段行为测试（把 deploy-staging.sh ⑤ 段那一截抽出来真跑）
# 用法：bash scripts/deploy/prometheus-reload-test.sh [被测 deploy-staging.sh]
#
# 为什么要有这一支（与 e2e-gate-test.sh / deploy-chain-wiring-test.sh 同族）：派发单的判据是
#   「修正发出 cwd」「两种结局都要落可判读日志」「stderr 不再静默」三条，全是行为判据。
#   字面 grep 只能证明那行代码还在，证明不了它跑起来真按说的做 —— T364 那格假绿的教训就是这个
#   （守卫的放行分支从没被执行过，断言却一路绿）。这里用假 docker 把这一段在沙箱里跑起来，
#   并把它「实际从哪个目录被调用」记下来对平 $STAGING_DIR —— 这正是历轮部署日志没人看见的那件事
#   （改前那行自 aac1fff 2026-09-18「T238 restart prometheus」起就在脚本里；docs main 的历轮部署留档里
#    逐轮都能读到这句 —— 命中文件清单与取数命令见交付证据包，不是按轮次推算出来的）。
#
# 被测对象：deploy-staging.sh 里 T471-RELOAD-BEGIN / T471-RELOAD-END 这对标记之间的整段
#   （含外层「仓里有没有 prometheus.yml」判定）。标记被删或配对坏了 ⇒ 本测试响亮判红，不许「没断言所以全绿」。
#
# 正向五腿（夹具由本脚本现生成，假 docker 在 PATH 最前，不发任何真实命令）：
#   L1 正常腿      docker 回 rc=0 且往 stderr 打一行 → 「已执行」行 + rc=0 + stderr 原文回显 + 调用目录==$STAGING_DIR
#   L2 失败腿      docker 回 rc=1 + stderr 原文      → 「未执行」行 + rc=1 + stderr 原文回显，且不许再打那句假理由
#   L3 无 compose  $STAGING_DIR 没有 compose 文件    → 「跳过（未发起）」带理由，且假 docker 一次都没被调
#   L4 无 yml      仓侧 prometheus.yml 不存在        → 整段不进：不 cp、不调 docker、日志里不出现 reload
#   L5 静默成功腿  docker 回 rc=0 且 stderr 为空     → 「reload stderr | (无输出)」那一条兜底分支真的在跑
#   三腿共用判据之外还逐腿核：本段不改部署成败（退出码仍 0）、配置文件照样同步到位。
#
# 反证三例（在临时副本上退回改前形态，要求对应那几格必判红；真本一个字不碰）：
#   M1 去掉子 shell 里的 cd          → L1/L2 的「调用目录」两格必红（日志里的 cwd 是声明，量出来才算）
#   M2 把 stderr 收回 /dev/null      → L1/L2 的「stderr 原文」格必红
#   M3 整段换成改前那六行            → L1 的「已执行」与 stderr、L2 的 rc/理由句、L3 的「没发起」全必红
#   （M3 用的改前文本是从 origin/main 的 deploy-staging.sh 第 244-249 行逐字抄来的，见 old-block）
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="${1:-$HERE/deploy-staging.sh}"
[ -r "$SRC" ] || { echo "[FAIL] 找不到被测部署脚本：$SRC"; exit 1; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/t471-reload.XXXXXX") || { echo "[FAIL] mktemp 失败"; exit 1; }
chmod 700 "$WORK"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAILED=0
ok()  { echo "  OK   $*"; PASS=$((PASS + 1)); }
bad() { echo "  FAIL $*"; FAILED=$((FAILED + 1)); }

REPO="$WORK/repo"
STAGING="$WORK/staging"
BIN="$WORK/bin"
CALLS="$WORK/calls.txt"
MODE="$WORK/mode.txt"
OUT="$WORK/out.log"
SEG="$WORK/segment.sh"
mkdir -p "$REPO/scripts/deploy" "$STAGING" "$BIN"

# ---- 抽段：标记之间逐字节取，不改一个字符（改了测的就不是现网在役的那份） ----
awk '/T471-RELOAD-BEGIN/{f=1; next} /T471-RELOAD-END/{f=0} f' "$SRC" > "$SEG"
if [ ! -s "$SEG" ]; then
  bad "抽段为空：$SRC 里 T471-RELOAD-BEGIN / T471-RELOAD-END 标记没配对或整段被删 —— 本测试无从判定，不许当绿"
  echo ""
  echo "[t471-reload-test] 断言：通过 $PASS / 不通过 $FAILED"
  exit 1
fi
if ! grep -qF 'docker compose restart prometheus' "$SEG"; then
  bad "抽出的段落里没有 compose restart 那一行：标记框错了范围（测了个空壳）"
fi

# ---- 假 sudo / 假 docker：只记账，不碰现网 ----
cat > "$BIN/sudo" <<'FAKE_SUDO'
#!/usr/bin/env bash
exec "$@"
FAKE_SUDO
chmod 700 "$BIN/sudo"

cat > "$BIN/docker" <<'FAKE_DOCKER'
#!/usr/bin/env bash
# 假 docker：记下「在哪个目录、用什么参数被调」，行为由 mode 文件切。
echo "cwd=$(pwd)|args=$*" >> "${RELOAD_TEST_CALLS:?}"
mode="$(head -1 "${RELOAD_TEST_MODE:?}" 2>/dev/null || echo NA)"
case "$mode" in
  ok)      echo 'Container bracesync-staging-prometheus-1  Restarting' >&2; exit 0 ;;
  okquiet) exit 0 ;;
  fail)    echo 'no such service: prometheus' >&2; exit 1 ;;
  *)       echo "假 docker 不认识的模式：$mode" >&2; exit 99 ;;
esac
FAKE_DOCKER
chmod 700 "$BIN/docker"

# ---- runner：与真本同口径的前导（set -euo pipefail + 同名 log/err/fail + 从 PROJECT_ROOT 起跑） ----
gen_runner() { # $1=段文件
  {
    echo 'set -euo pipefail'
    echo "export PATH=\"$BIN:\$PATH\""
    echo "export RELOAD_TEST_CALLS=\"$CALLS\""
    echo "export RELOAD_TEST_MODE=\"$MODE\""
    echo "PROJECT_ROOT=\"$REPO\""
    echo "STAGING_DIR=\"$STAGING\""
    echo 'log()  { echo "\033[1;32m[deploy]\033[0m $*"; }'
    echo 'err()  { echo "\033[1;31m[ERROR]\033[0m $*" >&2; }'
    echo 'fail() { err "$*"; exit 1; }'
    echo "cd \"$REPO\""
    cat "$1"
  } > "$WORK/run.sh"
}

prepare_env() { # $1=mode $2=有 compose 文件 yes|no $3=仓侧 yml 存在 yes|no
  : > "$CALLS"
  printf '%s\n' "$1" > "$MODE"
  if [ "$2" = yes ]; then
    printf 'services: {}\n' > "$STAGING/docker-compose.yml"
  else
    rm -f "$STAGING/docker-compose.yml"
  fi
  if [ "$3" = yes ]; then
    printf 'global:\n  scrape_interval: 15s\n' > "$REPO/scripts/deploy/prometheus.yml"
  else
    rm -f "$REPO/scripts/deploy/prometheus.yml"
  fi
  rm -f "$STAGING/prometheus.yml"
}

leg_spec() { # 回显「mode 有compose 有yml」
  case "$1" in
    L1) echo "ok yes yes" ;;
    L2) echo "fail yes yes" ;;
    L3) echo "ok no yes" ;;
    L4) echo "ok yes no" ;;
    L5) echo "okquiet yes yes" ;;
    *)  echo "?? ?? ??" ;;
  esac
}

# scan_leg <腿名> <段文件>：跑这一腿并逐条判定，打印 [PASS][编号]/[FAIL][编号]，回传不通过条数
scan_leg() {
  local leg="$1" seg="$2"
  local n_fail=0 spec mode hc hp
  spec=$(leg_spec "$leg")
  read -r mode hc hp <<EOF
$spec
EOF
  prepare_env "$mode" "$hc" "$hp"
  gen_runner "$seg"
  bash "$WORK/run.sh" > "$OUT" 2> "$WORK/err.log"
  local run_rc=$?
  local calls_n call_cwd call_args
  calls_n=$(grep -c . "$CALLS" || true)
  call_cwd=$(sed -n '1s/^cwd=\([^|]*\)|.*/\1/p' "$CALLS")
  call_args=$(sed -n '1s/^cwd=[^|]*|args=//p' "$CALLS")
  [ -n "$calls_n" ] || calls_n=0

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
  hasfile() { # $1=编号 $2=路径 $3=说明
    if [ -f "$2" ]; then echo "  [PASS][$1] $3"; else echo "  [FAIL][$1] $3 —— 文件不在：$2"; n_fail=$((n_fail + 1)); fi
  }
  nofile() { # $1=编号 $2=路径 $3=说明
    if [ -f "$2" ]; then echo "  [FAIL][$1] $3 —— 文件却在：$2"; n_fail=$((n_fail + 1)); else echo "  [PASS][$1] $3"; fi
  }

  case "$leg" in
    L1)
      req  L1.1 'prometheus reload 已执行'          '成功腿要有「已执行」判定行'
      req  L1.2 "已执行（cwd=$STAGING rc=0）"        '判定行要带上发出的 compose 目录与 rc'
      req  L1.3 'Container bracesync-staging-prometheus-1  Restarting' 'docker 的 stderr 原文要回显进日志（T471：不再 2>/dev/null 吞掉）'
      eq   L1.4 "$call_args" 'compose restart prometheus' '假 docker 记到的调用形状（走的是 compose restart，不是裸 restart；sudo 桩 exec 后 argv[0] 就是 docker，不含在 $* 里）'
      eq   L1.5 "$call_cwd" "$STAGING"              '量出来的发出目录必须落在 staging 的 compose 目录（T471 本体）'
      eq   L1.6 "$calls_n" 1                        '本腿恰好发起一次'
      hasfile L1.7 "$STAGING/prometheus.yml"        '配置同步这半没被动'
      eq   L1.8 "$run_rc" 0                         'reload 成功不改部署退出码'
      ;;
    L2)
      req  L2.1 'prometheus reload 未执行'          '失败腿要明说「未执行」'
      req  L2.2 'rc=1'                              '判定行要带真实 rc'
      req  L2.3 'no such service: prometheus'       '失败原因取 stderr 原文进日志'
      req  L2.4 '原因见下方 stderr 原文'            '跳过不再是无声的默认分支'
      eq   L2.5 "$call_cwd" "$STAGING"              '失败也要落在 staging 的 compose 目录发出（否则分不清「没这个服务」与「没 compose 文件」）'
      ban  L2.6 '容器未运行或不存在，跳过 restart'    '改前那句假理由不许再出现（现网 prometheus 一直在跑）'
      hasfile L2.7 "$STAGING/prometheus.yml"        '失败腿里配置照样已同步'
      eq   L2.8 "$run_rc" 0                         'prometheus 重载失败不阻断本轮部署（本卡只让日志可读，不改部署成败口径）'
      ;;
    L3)
      eq   L3.1 "$calls_n" 0                        'compose 文件不在时根本不发起（不拿假失败当真跳过）'
      req  L3.2 'reload 跳过（未发起）'              '显式跳过要有这一行'
      req  L3.3 'docker-compose.yml 不存在'          '跳过的理由要点名是哪个文件缺'
      hasfile L3.4 "$STAGING/prometheus.yml"        '配置文件仍要同步过去，下一轮 compose up 自然带上'
      eq   L3.5 "$run_rc" 0                         '跳过不改部署退出码'
      ;;
    L4)
      eq   L4.1 "$calls_n" 0                        '仓侧没有 prometheus.yml 时整段不进'
      nofile L4.2 "$STAGING/prometheus.yml"         '没有源文件就不该往 staging 落配置'
      ban  L4.3 'reload'                            '整段没执行，日志里连 reload 字样都不该出现'
      eq   L4.4 "$run_rc" 0                         '退出码 0'
      ;;
    L5)
      req  L5.1 'prometheus reload 已执行'          '静默成功腿也认「已执行」'
      req  L5.2 'reload stderr | (无输出)'          'stderr 为空时要有兜底行（这条分支不真跑一遍就是死码）'
      eq   L5.3 "$calls_n" 1                        '本腿恰好发起一次'
      ;;
  esac
  return "$n_fail"
}

echo "[1/4] 正向：现网在役的 ⑤ 段逐腿真跑判定"
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

echo "[2/4] 反证：把修法逐格退回改前形态，要求对应那几格必判红（真本一个字不碰）"
mutate_seg() { # $1=sed 程序 → 产出 $WORK/mut.sh
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
# M1：去掉子 shell 里的 cd —— 这正是改前那行的真实形态（命令在仓库根发出）。
if mutate_seg 's@( cd "\$STAGING_DIR" \&\& sudo docker compose restart prometheus )@sudo docker compose restart prometheus@'; then
  expect_red "M1 发出目录退回不切目录" L1 L1.5
  expect_red "M1 发出目录退回不切目录" L2 L2.5
else
  bad "M1 的改动没落到段落文本上（反证无牙，等于没测）"
fi
# M2：把 stderr 收回 /dev/null —— 测的就是「静默」这半件事。
if mutate_seg 's@2>"\$PROM_ERR"@2>/dev/null@'; then
  expect_red "M2 stderr 退回被吞" L1 L1.3
  expect_red "M2 stderr 退回被吞" L2 L2.3
else
  bad "M2 的改动没落到段落文本上（反证无牙，等于没测）"
fi
# M3：整段换成改前那六行（逐字取自 code 仓基线 deploy-staging.sh 第 244-249 行）。
#   这份夹具不是我凭记忆编的：下面 [3/4] 里拿基线 commit 的 blob 与本夹具逐字节对平，
#   取不到 blob 时明写「本格未测」（CI 是浅克隆，没有这个对象），不许念成绿。
#   基线号钉在 293bcb1（本分支的起点＝改前那一份），钉历史号而不是 origin/main 是为了将来不假红：
#   T471 并进 main 之后，main 上那段已经是新写法，按 ref 对平就会永久判红。
cat > "$WORK/old-block.txt" <<'OLD_BLOCK'
# 同步 prometheus.yml
if [ -f "$PROJECT_ROOT/scripts/deploy/prometheus.yml" ]; then
  sudo cp "$PROJECT_ROOT/scripts/deploy/prometheus.yml" "$STAGING_DIR/prometheus.yml"
  # restart prometheus 容器使新配置生效
  sudo docker compose restart prometheus 2>/dev/null || log "   prometheus 容器未运行或不存在，跳过 restart"
fi
OLD_BLOCK
awk -v oldf="$WORK/old-block.txt" '
  /T471-RELOAD-BEGIN/ { while ((getline line < oldf) > 0) print line; skip = 1; next }
  /T471-RELOAD-END/   { skip = 0; next }
  !skip { print }
' "$SRC" > "$WORK/mut-src.sh"
# 结构自证：这对标记框住的正好是「同步 prometheus.yml」那一整块 —— 拿改前文本换掉它之后，
# 整份 deploy-staging.sh 仍要能解析，且两个标记都不再出现（少一个就说明框歪了，多一个就说明没换干净）。
if grep -q 'T471-RELOAD-\(BEGIN\|END\)' "$WORK/mut-src.sh"; then
  bad "M3 换段后 mut-src.sh 里还留着标记（BEGIN/END 没配对，替换框歪了）"
elif ! bash -n "$WORK/mut-src.sh" 2>"$WORK/syn3.err"; then
  bad "M3 换段后的整份脚本解析不过：$(tr '\n' '|' < "$WORK/syn3.err") ⇒ 标记框出的不是完整一块"
else
  cp "$WORK/old-block.txt" "$WORK/mut.sh"
  if cmp -s "$WORK/mut.sh" "$SEG"; then
    bad "M3 整段退回改前后抽出的段落却没变（替换没落地，反证无牙）"
  else
    expect_red "M3 整段退回改前六行" L1 L1.1
    expect_red "M3 整段退回改前六行" L2 L2.3
    expect_red "M3 整段退回改前六行" L3 L3.1
  fi
fi

echo "[3/4] 标记、文法与夹具来源：抽出的段落要能独立解析，M3 夹具要与基线那六行逐字节相同"
if bash -n "$SEG" 2>"$WORK/syn.err"; then
  ok "bash -n 抽出的 T471 段（标记配对正确、语句完整）"
else
  bad "bash -n 抽出的 T471 段：$(tr '\n' '|' < "$WORK/syn.err") —— 多半是标记框坏了"
fi
for f in "$SRC" "$HERE/deploy-chain-wiring-test.sh" "$HERE/e2e-gate-test.sh"; do
  if bash -n "$f" 2>"$WORK/syn2.err"; then
    ok "bash -n $(basename "$f")"
  else
    bad "bash -n $(basename "$f")：$(tr '\n' '|' < "$WORK/syn2.err")"
  fi
done
# 夹具对平：只钉历史 commit（默认 293bcb1 = 本分支起点 = 改前那一份），可用 T471_BASE_SHA 覆盖。
T471_BASE_SHA="${T471_BASE_SHA:-293bcb1}"
# 取 blob 时要 MSYS_NO_PATHCONV=1（防 MSYS 把 "rev:path" 里的冒号参数改成路径），
# 但同一个前缀会让原生 git 收不到 -C 的 POSIX 目录 —— 两者不能同时挂在一个命令上。
# 所以先拿仓根，再在子 shell 里 cd 过去（cd 是 bash 内建，不受路径转换影响），Windows 与 CI 两侧都成立。
repo_top=$(git -C "$HERE" rev-parse --show-toplevel 2>/dev/null || true)
base_sha=""
[ -n "$repo_top" ] && base_sha=$(git -C "$HERE" rev-parse --quiet --verify "$T471_BASE_SHA^{commit}" 2>/dev/null || true)
if [ -z "$base_sha" ]; then
  echo "  SKIP 夹具对平未测：本地取不到基线对象 $T471_BASE_SHA（CI 浅克隆属正常）—— 权威那一次对平连原始输出在交付证据包里"
else
  ( cd "$repo_top" && MSYS_NO_PATHCONV=1 git cat-file blob "$base_sha:scripts/deploy/deploy-staging.sh" ) > "$WORK/base-blob.txt" 2>"$WORK/base.err"
  if [ ! -s "$WORK/base-blob.txt" ]; then
    bad "取基线 blob 失败：$(tr '\n' '|' < "$WORK/base.err")"
  else
    sed -n '244,249p' "$WORK/base-blob.txt" > "$WORK/base-block.txt"
    if cmp -s "$WORK/base-block.txt" "$WORK/old-block.txt"; then
      ok "M3 夹具与 $T471_BASE_SHA 的第 244-249 行逐字节相同（$(wc -c < "$WORK/base-block.txt" | tr -d '[:space:]') 字节，改前形态不是我编的）"
    else
      bad "M3 夹具与 $T471_BASE_SHA 的第 244-249 行不一致 —— 基线挪过或夹具抄错，两边都要重取"
      diff -u "$WORK/base-block.txt" "$WORK/old-block.txt" | sed 's/^/         /'
    fi
  fi
fi

echo "[4/4] 判定基数自证：五腿正向格数与反证红格都来自本次实测"
pos_cells=$(grep -ho '\[PASS\]\[[A-Za-z0-9_.]*\]' "$WORK"/L*.pos 2>/dev/null | wc -l | tr -d '[:space:]')
echo "  本次正向通过格数 = $pos_cells（L1 八格 / L2 八格 / L3 五格 / L4 四格 / L5 三格，逐腿清单见上，不在本脚本里钉死）"
[ "$pos_cells" -ge 28 ] && ok "五腿正向格全部落地（$pos_cells 格 ≥ 28）" \
  || bad "正向格数只有 $pos_cells（不足 28 ⇒ 有腿没跑起来）"

echo ""
echo "[t471-reload-test] 断言：通过 $PASS / 不通过 $FAILED"
[ "$FAILED" = "0" ] || exit 1
exit 0
