#!/usr/bin/env bash
# T463 · 起跑门行为测试（e2e-gate-check.sh 的放行 / 判红 / 读不到三态，外加五格变异反证）
# 用法：bash scripts/deploy/e2e-gate-test.sh
#
# 为什么不只跑夹具就交差（本仓既有教训）：
#   T364 那格假绿 —— 守卫的放行分支从没被执行过；T393 N1 的接线反证 —— 断言写在那儿不等于它在跑。
#   检测类改动要跑满三向：该放行的放行、该判红的判红、还要证明「命中即真跑、不走守卫」
#   （把过滤链逐格拆掉，对应那格必须改变结论 —— 否则断言是摆设）。
#
# 覆盖的格（夹具数据全部由本脚本现生成，形状照 api.github.com 的 runs 响应，字段名一个没改）：
#   A clean        只有 completed 的 run（含一条 E2E completed）        → 期望 rc=0
#   B pr-running   E2E / pull_request / in_progress                    → 期望 rc=1 并打印 run 号与 run_started_at
#   C sched-queued E2E / schedule / queued                            → 期望 rc=1（定时 STRICT 轮）
#   D push-only    E2E / push / in_progress                           → 期望 rc=0，且必须有排除行（口径是活的）
#   E wf-dispatch  E2E / workflow_dispatch / waiting                  → 期望 rc=1（手工触发/等待中也算在跑）
#   F bad-shape    响应体里没有 workflow_runs                          → 期望 rc=3（读不到不判绿）
#   G unreadable   夹具路径指向不存在的文件                            → 期望 rc=3
#   H disabled     E2E_GATE_DISABLE=1 打在 B 上                        → 期望 rc=0 且有 SKIP 声明行
#   I empty-set    夹具里 workflow_runs 是空列表（取到了，确实没有在跑）  → 期望 rc=0（与 F/G 的 rc=3 是两回事）
#   网络腿：本地 127.0.0.1 假 API 服务走真 curl 腿 → hit=1 / clean=0 / 200+空集=0 / 拒接=3
#   方向区分断言（T463 评论 2358 的第二件事）：同一道门，「判据面取不到」判 rc=3 而「取到但为空集」放行 rc=0，
#     两个 rc 必须不同、两条用例必须分开写 —— 防止将来有人把「什么都没查到」和「没东西可查」写成同一个结论。
#   变异 M1..M5：逐格拆过滤链，要求对应夹具的结论按预期翻转（M5 拆的正是 I 那一半：把空集当形状坏）
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATE="${1:-$HERE/e2e-gate-check.sh}"
[ -r "$GATE" ] || { echo "[FAIL] 找不到被测脚本：$GATE"; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "[FAIL] 本测试需要 python3 生成夹具"; exit 1; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/t463-gatetest.XXXXXX") || { echo "[FAIL] mktemp 失败"; exit 1; }
chmod 700 "$WORK"
SRV_PID=""
cleanup() { [ -n "$SRV_PID" ] && kill "$SRV_PID" >/dev/null 2>&1; rm -rf "$WORK"; }
trap cleanup EXIT

PASS=0
FAILED=0
ok()  { echo "  OK   $*"; PASS=$((PASS + 1)); }
bad() { echo "  FAIL $*"; FAILED=$((FAILED + 1)); }

# 生成一份 runs 响应体：每个 run 一行 JSON（参数：文件名 然后若干 "id|name|event|branch|status|started" 串）
make_fixture() {
  local out="$1"; shift
  python3 - "$out" "$@" <<'PY'
import json, sys
out = sys.argv[1]
runs = []
for spec in sys.argv[2:]:
    rid, name, event, branch, status, started = spec.split("|")
    runs.append({
        "id": int(rid), "name": name, "event": event, "head_branch": branch,
        "status": status, "conclusion": None if status != "completed" else "success",
        "run_started_at": started, "created_at": started, "updated_at": started,
        "html_url": "https://github.com/ryanicer/BraceSync-code/actions/runs/%s" % rid,
        "display_title": "fixture %s" % name,
    })
with open(out, "w", encoding="utf-8") as fh:
    json.dump({"total_count": len(runs), "workflow_runs": runs}, fh, ensure_ascii=False)
PY
}

run_gate() { # $1=夹具（可空） $2.. = 额外 env 赋值 KEY=VAL
  local fx="$1"; shift
  local envs=()
  for kv in "$@"; do envs+=("$kv"); done
  if [ -n "$fx" ]; then envs+=("E2E_GATE_FIXTURE=$fx"); fi
  env "${envs[@]}" bash "$GATE" 2>&1
}

expect_rc() { # $1=格名 $2=期望 rc $3=夹具 $4=必须出现的子串（可空） 之后是 env 赋值
  local label="$1" want="$2" fx="$3" must="$4"; shift 4
  local out rc
  out=$(run_gate "$fx" "$@"); rc=$?
  if [ "$rc" != "$want" ]; then
    bad "$label：期望 rc=$want 实得 rc=$rc"
    printf '%s\n' "$out" | sed 's/^/         /'
    return
  fi
  if [ -n "$must" ] && ! printf '%s\n' "$out" | grep -qF -- "$must"; then
    bad "$label：rc 对但少了判据行（期望含「$must」）"
    printf '%s\n' "$out" | sed 's/^/         /'
    return
  fi
  ok "$label：rc=$want$( [ -n "$must" ] && printf ' 且判据行在位' )"
  LAST_OUT="$out"
  export LAST_OUT
}

# ---- 夹具 ----
F_CLEAN="$WORK/a-clean.json"
F_PR="$WORK/b-pr.json"
F_SCHED="$WORK/c-sched.json"
F_PUSH="$WORK/d-push.json"
F_MIXED="$WORK/g-mixed.json"
F_EMPTYSET="$WORK/i-emptyset.json"
make_fixture "$F_CLEAN"  "9001|E2E|pull_request|t-x|completed|2026-09-28T07:00:00Z" \
                        "9002|CI-FE|pull_request|t-x|completed|2026-09-28T07:00:00Z"
make_fixture "$F_PR"     "36389680597|E2E|pull_request|t456-teams-header-iris|in_progress|2026-09-28T07:20:49Z" \
                        "9003|CI-FE|pull_request|t-y|in_progress|2026-09-28T07:20:49Z"
make_fixture "$F_SCHED"  "36394469647|E2E|schedule|main|queued|2026-09-28T12:00:00Z"
make_fixture "$F_PUSH"   "36393124786|E2E|push|main|in_progress|2026-09-28T07:42:33Z" \
                        "9004|Release|push|main|in_progress|2026-09-28T07:42:33Z"
make_fixture "$F_MIXED"  "9005|E2E|workflow_dispatch|main|waiting|2026-09-28T08:00:00Z"
# 空集（T463 评论 2358 承诺的第一件事）：total_count=0、workflow_runs 是「空列表」而不是缺键，
#   也不是零字节响应体 —— 这三种「什么都没有」在本门里落到三个不同结论，所以下面分开三格写。
make_fixture "$F_EMPTYSET"
printf '%s\n' '{"total_count": 0}' > "$WORK/e-shape.json"

echo "[1/5] 正向四态：该放行的放行、该判红的判红"
expect_rc "A clean（只有 completed）" 0 "$F_CLEAN" "起跑门放行"
expect_rc "B E2E/pull_request/in_progress" 1 "$F_PR" "BLOCK run=36389680597"
if printf '%s\n' "$LAST_OUT" | grep -qF 'run_started_at=2026-09-28T07:20:49Z'; then
  ok "B 追加：判红行同时带 run 号与 run_started_at（判据 1 的打印要求）"
else
  bad "B 追加：判红行没带 run_started_at —— 派发单要求两件都打印"
fi
expect_rc "C E2E/schedule/queued" 1 "$F_SCHED" "BLOCK run=36394469647"
expect_rc "D 只有 push 在跑（e2e.yml 的 if 不含 push）" 0 "$F_PUSH" "但事件 push 不打 staging"
expect_rc "E workflow_dispatch/waiting 也算在跑" 1 "$F_MIXED" "BLOCK run=9005"

echo "[2/5] 读不到与显式跳过：不许把「无判据」当「无在跑」"
expect_rc "F 响应体缺 workflow_runs" 3 "$WORK/e-shape.json" "形状不合预期"
expect_rc "G 夹具不可读" 3 "$WORK/nope.json" "夹具不可读"
expect_rc "H E2E_GATE_DISABLE=1（打在 B 上）" 0 "$F_PR" "SKIP 本门被 E2E_GATE_DISABLE=1 显式关闭" E2E_GATE_DISABLE=1
expect_rc "I 取到了、但集合是空的（workflow_runs 为空列表）" 0 "$F_EMPTYSET" "判据面：去重后候选 run 0 条"

echo "[3/5] 真 curl 腿：本地假 API 服务（E2E_GATE_API 指向 127.0.0.1），不发真实外网请求"
# 服务自带「跑满 N 次请求就自己退出」的预算 —— 本机实测 MSYS 的 kill 对一个 Win32 python
#   进程不生效（第一版靠 kill 停服务，下一格照样拿到 200，判据面全是旧数据）。留监听进程 = 脏，
#   所以这里让服务自终止，末格再用「它已经退出」的那个端口去验「读不到」。
cat > "$WORK/srv.py" <<'PY'
import http.server, os, sys, threading
port, mode_file, base_dir, budget = int(sys.argv[1]), sys.argv[2], sys.argv[3], int(sys.argv[4])
served = 0

class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        global served
        served += 1
        with open(mode_file, encoding="utf-8") as fh:
            fx_name = fh.read().strip()
        # 夹具按「目录 + 文件名」拼：argv 里的目录由 MSYS 自动转成对端看得懂的形式，
        # 而写在 mode 文件里的正文不会被转换 —— 本机踩过 Windows python 读 /tmp 路径 FileNotFoundError
        # 导致整格网络腿假失败（CI 的 Linux runner 两种写法都行，这里取两边都成立的那种）。
        with open(os.path.join(base_dir, fx_name), "rb") as fh:
            body = fh.read()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
        if served >= budget:
            threading.Thread(target=self.server.shutdown, daemon=True).start()

    def log_message(self, *args):
        pass

srv = http.server.HTTPServer(("127.0.0.1", port), H)
srv.socket.settimeout(60)
try:
    while served < budget:
        srv.handle_request()
except OSError:
    pass
PY
PORT=$(( 21000 + (RANDOM % 10000) ))
printf '%s\n' "$(basename "$F_PR")" > "$WORK/mode.txt"
python3 "$WORK/srv.py" "$PORT" "$WORK/mode.txt" "$WORK" 6 > "$WORK/srv.log" 2>&1 &
SRV_PID=$!
sleep 2
if ! kill -0 "$SRV_PID" 2>/dev/null; then
  bad "本地假 API 服务没起来（端口 $PORT）：网络腿未测"
  sed 's/^/         /' "$WORK/srv.log"
else
  BASE_API="http://127.0.0.1:$PORT"
  out=$(E2E_GATE_API="$BASE_API" bash "$GATE" 2>&1); rc=$?
  if [ "$rc" = "1" ] && printf '%s\n' "$out" | grep -qF 'BLOCK run=36389680597' \
     && [ "$(printf '%s\n' "$out" | grep -cF '查询 in_progress 完成')" = "1" ] \
     && [ "$(printf '%s\n' "$out" | grep -cF '查询 queued 完成')" = "1" ]; then
    ok "网络腿-判红：真走 curl 的两次查询（in_progress + queued 各一次、各有 http 与字节数原文），夹具里的在跑 run 被拦下（rc=1）"
  else
    bad "网络腿-判红：rc=$rc 期望 1，且两次查询各要有一行原文"
    printf '%s\n' "$out" | sed 's/^/         /'
  fi
  printf '%s\n' "$(basename "$F_CLEAN")" > "$WORK/mode.txt"
  out=$(E2E_GATE_API="$BASE_API" bash "$GATE" 2>&1); rc=$?
  if [ "$rc" = "0" ] && printf '%s\n' "$out" | grep -qF '起跑门放行'; then
    ok "网络腿-放行：同一服务换成 clean 响应体 ⇒ 真 curl 取得后 rc=0（判据面变了结论就变，不是硬编码放行）"
  else
    bad "网络腿-放行：rc=$rc 期望 0"
    printf '%s\n' "$out" | sed 's/^/         /'
  fi
  # T463 评论 2358 承诺的第一件事（本轮补格）：in_progress 与 queued 两腿都回 HTTP 200 且响应体是「空集」。
  #   这一格与下面「读不到判 rc=3」那格分开写、不合并：两条讲的是两种「什么都没有」，合在一条就看不出谁是谁。
  #   两腿各一次查询，所以两次都要有 http=200 的原文行 —— 否则「200」这个前提没被证据钉住。
  printf '%s\n' "$(basename "$F_EMPTYSET")" > "$WORK/mode.txt"
  out=$(E2E_GATE_API="$BASE_API" bash "$GATE" 2>&1); rc=$?
  RC_EMPTYSET=$rc
  OUT_EMPTYSET="$out"
  es_http200=$(printf '%s\n' "$out" | grep -cF 'http=200')
  if [ "$rc" = "0" ] && printf '%s\n' "$out" | grep -qF '起跑门放行' \
     && printf '%s\n' "$out" | grep -qF '判据面：去重后候选 run 0 条' \
     && [ "$es_http200" = "2" ]; then
    ok "网络腿-200 且空集：两腿各回一次 http=200、体是空列表 ⇒ 候选 0 条、rc=0（「取到了，只是没有」）"
  else
    bad "网络腿-200 且空集：rc=$rc 期望 0，且要「候选 run 0 条」行 + 两行 http=200（实得 http=200 计数 $es_http200）"
    printf '%s\n' "$out" | sed 's/^/         /'
  fi
  # 预算用满 → 服务自退出；此时同一端口再问就应当「读不到」（curl 拒接 ⇒ rc=3，绝不静默放行）
  #   sleep 1：给自退出留出落地时间 —— Linux runner 上若进程还没退，SYN 会被内核接住而无人 accept，
  #   这一格要等两次 -m 30 超时才判红（结论仍是 rc=3，只是白等一分钟）。
  sleep 1
  out=$(E2E_GATE_API="$BASE_API" bash "$GATE" 2>&1); rc=$?
  RC_UNREADABLE=$rc
  if [ "$rc" = "3" ] && printf '%s\n' "$out" | grep -qF '起跑门判据面读不到'; then
    ok "网络腿-读不到：判据面取不到 ⇒ rc=3 并显式声明不放行（服务已退出，端口无人听）"
  else
    bad "网络腿-读不到：rc=$rc 期望 3，且要有「判据面读不到」行"
    printf '%s\n' "$out" | sed 's/^/         /'
  fi
  # T463 评论 2358 承诺的第二件事：方向区分断言 —— 「取到但为空集⇒放行」与「取不到⇒拦截」不许写反。
  #   上面两格各自只盯一个结论，把两条代码互换时两格都能凑上自己的期望（各自自洽），
  #   只有这一格会比「两个 rc 必须不相等、且各自落在自己那一边」把它们钉死。
  if [ "$RC_EMPTYSET" = "0" ] && [ "$RC_UNREADABLE" = "3" ]; then
    ok "方向区分：空集腿 rc=$RC_EMPTYSET（放行）／读不到腿 rc=$RC_UNREADABLE（拦截）—— 两种「什么都没有」结论不同，写反即红"
  else
    bad "方向区分：空集腿 rc=$RC_EMPTYSET 期望 0、读不到腿 rc=$RC_UNREADABLE 期望 3 ⇒ 两者至少有一格走错方向"
    printf '%s\n' "$OUT_EMPTYSET" | sed 's/^/         /'
  fi
  if kill -0 "$SRV_PID" 2>/dev/null; then
    bad "临时假 API 服务没自终止（PID $SRV_PID 仍活）：本测试会留监听进程，请手工收口"
  else
    ok "反证自检：假 API 服务跑满 6 次请求（判红 2 + 放行 2 + 空集 2）后自行退出，测试没留监听进程"
  fi
fi

echo "[4/5] 变异反证：把过滤链逐格拆掉，要求对应夹具的结论按预期翻转（ pristine 基线先自证 ）"
mutate() { # $1=标签 $2=夹具 $3=基线期望rc $4=变异后期望rc $5=sed 程序 $6=标签内说明
  local label="$1" fx="$2" base_want="$3" mut_want="$4" prog="$5"
  local tmp out rc bout brc
  tmp="$WORK/mut-$(printf '%s' "$label" | tr -c 'A-Za-z0-9' '_').sh"
  sed "$prog" "$GATE" > "$tmp" || { bad "$label：造变异副本失败"; return; }
  if cmp -s "$tmp" "$GATE"; then
    bad "$label：改动没落到被测文本上（反证无牙，等于没测）"
    return
  fi
  # pristine 基线：同夹具在真本上必须是期望结论，否则变异后的差异无从归因
  bout=$(E2E_GATE_FIXTURE="$fx" bash "$GATE" 2>&1); brc=$?
  if [ "$brc" != "$base_want" ]; then
    bad "$label：未变异基线 rc=$brc 与期望 $base_want 不符 ⇒ 这一格归因不成立"
    return
  fi
  out=$(E2E_GATE_FIXTURE="$fx" bash "$tmp" 2>&1); rc=$?
  if [ "$rc" = "$mut_want" ]; then
    ok "$label：真本判 rc=$brc、拆掉该格后判 rc=$rc（该格确实在执行）"
  else
    bad "$label：拆掉该格后 rc=$rc，期望翻转为 $mut_want ⇒ 该格本来就没牙"
    printf '%s\n' "$out" | sed 's/^/         /'
  fi
}
# M1/M2 拆的是同一行过滤的两个条件（命中集 = workflow 名 AND 状态在阻塞集）：
#   各去掉一半，对应夹具必须从放行翻成判红 —— 证明两个条件都在真跑，而不是只写着一个。
make_fixture "$WORK/f-noname.json" "9006|CI-FE|pull_request|t-z|in_progress|2026-09-28T07:00:00Z"
mutate "M1 workflow 名条件（从命中集里去掉）" "$WORK/f-noname.json" 0 1 \
  's/if r.get("name") == workflow_name and r.get("status") in blocking_status\]/if r.get("status") in blocking_status]/'
mutate "M2 状态条件（从命中集里去掉）" "$F_CLEAN" 0 1 \
  's/if r.get("name") == workflow_name and r.get("status") in blocking_status\]/if r.get("name") == workflow_name]/'
# M3 拆的是 bash 侧的集合常量本身（判据集合只在 bash 定义一次、逐参数传给 python）：
#   把 push 加进「打 staging 的事件集」，push-only 夹具必须从放行翻成判红 ⇒ 常量真的被消费。
mutate "M3 事件集合（bash 常量里加进 push）" "$F_PUSH" 0 1 \
  's|^STAGING_EVENTS="pull_request schedule workflow_dispatch"|STAGING_EVENTS="pull_request schedule workflow_dispatch push"|'
# M4 打的是「判红退出链」：BLOCK 行照打，但计数被抹 ⇒ 结论必须从判红翻成放行。
#   这一格为什么重要：只看「打印了 BLOCK 行」会让人以为门在拦，实际 rc 仍是 0，部署照跑。
#   形状守卫那一格（夹具 F：响应体里没有 workflow_runs）这里不做变异反证：它由 bash 的显式 die 与 python 的解析崩溃两道
#   同指一个结果，拆掉任一道另一道照样兜住 ⇒ 变异看不到翻转，属双保险而非无牙（本卡交件已披露）。
mutate "M4 判红计数链（blocked 自增抹掉，BLOCK 行照打）" "$F_PR" 1 0 \
  's/blocked=\$((blocked + 1))/blocked=0/'
# M5（本轮为 T463 评论 2358 承诺格补的反证）：把形状守卫写成「空集也算形状坏」，即有人把
#   「取到但为空集」与「取不到」写反时的那副样子 —— 上面 I 那一格必须从放行(0)翻成拦截(3)。
#   为什么只反证这一头：另一头（把「取不到」当空集放过去）在门本体里由三道各自独立的兜底同指一个结果
#   （curl -f 非零即 die、[ -s ] 空响应体兜底、python 解析崩溃后 parse_rc 兜底），拆掉任一道另两道照样
#   判 rc=3 ⇒ 变异看不到翻转，属双保险而非无牙（与上面形状守卫那格不做变异的披露同口径）。
mutate "M5 形状守卫写反（把空集也算作形状坏）" "$F_EMPTYSET" 0 3 \
  's@not isinstance(doc.get("workflow_runs"), list):@not isinstance(doc.get("workflow_runs"), list) or doc.get("workflow_runs") == []:@'

echo "[5/5] 语法：bash -n 两份脚本"
for f in "$GATE" "$HERE/deploy-staging.sh"; do
  if bash -n "$f" 2>"$WORK/syn.err"; then
    ok "bash -n $(basename "$f")"
  else
    bad "bash -n $(basename "$f")：$(tr '\n' '|' < "$WORK/syn.err")"
  fi
done

echo ""
echo "[t463-gate-test] 断言：通过 $PASS / 不通过 $FAILED"
[ "$FAILED" = "0" ] || exit 1
exit 0
