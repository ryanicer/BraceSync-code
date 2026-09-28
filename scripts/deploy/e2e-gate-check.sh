#!/usr/bin/env bash
# T463 · 部署起跑门：有 e2e-real 打 staging 的 run 在跑或排队 ⇒ 本轮不许起部署（全程只读，只发 GET）
# 用法：bash scripts/deploy/e2e-gate-check.sh
#   环境变量（只为测试可注入，生产不设）：
#     E2E_GATE_FIXTURE   用这份 JSON 代替 GitHub API 响应（CI 夹具，也是判据 3 的「最近一次 run 记录回放」腿）
#     E2E_GATE_REPO      仓库全名（默认 ryanicer/BraceSync-code）
#     E2E_GATE_API       API 根（默认 https://api.github.com）
#     E2E_GATE_DISABLE=1 显式跳过本门 —— 会打一行醒目声明进部署日志，不静默放行
#   退出码：0 无在跑/排队的打 staging run / 1 检出碰撞窗口（逐条点名） / 3 判据面读不到（读不到绝不判绿）
#
# 为什么要这一格（T460 交件自曝 → PM 裁定立卡机器化）：起跑门「现查无 in_progress 的 E2E 在打 staging」
#   原先是人读的纪律。T460 第十八轮实测踩中：最后一次读门 15:04:21，Iris 那条 PR 的 run 创建于
#   15:04:32（她读数后 11 秒），我 15:07:32 起跑前没再取第三次 ⇒ 部署窗口撞进她的 04-monitor 4.3 用例
#   窗口，attempt 1 判红、重跑一次才绿。纪律靠人复读就会有这一格漏，改成机器查一次就拒。
#
# 判据面口径（三件都在 e2e.yml 里，取的是该文件在役的 job 条件，不是我的推测）：
#   一、workflow 名 = E2E（`.github/workflows/e2e.yml` 的 name:），本仓只有它的 job 会打 staging。
#   二、事件 ∈ pull_request / schedule / workflow_dispatch。push 显式排除：e2e.yml 第 253 行给
#       e2e-real-staging 那个 job 加的 if 不含 push（T381 就是为了排除它加的），push run 根本不碰 staging，
#       把它算进来会让「每次合并后」都无端堵死部署窗口。
#   三、状态 ∈ queued / in_progress / waiting / requested。已完成的 run 不算 —— 它打完了。
#   派发单原话是「in_progress / queued 的 STRICT 轮」，这里比它宽：STRICT（E2E_POST_DEPLOY_STRICT=1）
#   只在非 pull_request 事件置位，而 T460 撞的正是 pull_request 那一轮（非 STRICT）⇒ 只拦 STRICT 轮
#   等于把本次事故的那一类留在门外。口径订正写进本卡交件。
set -uo pipefail

REPO="${E2E_GATE_REPO:-ryanicer/BraceSync-code}"
API_ROOT="${E2E_GATE_API:-https://api.github.com}"
FIXTURE="${E2E_GATE_FIXTURE:-}"
WORKFLOW_NAME="E2E"
STAGING_EVENTS="pull_request schedule workflow_dispatch"
BLOCKING_STATUS="queued in_progress waiting requested"

say() { echo "[e2e-gate] $*"; }
die() { say "ERROR $*" >&2; exit 3; }

if [ "${E2E_GATE_DISABLE:-0}" = "1" ]; then
  say "SKIP 本门被 E2E_GATE_DISABLE=1 显式关闭 —— 本轮起跑没有经过 staging 碰撞检查，谁关的请在部署日志同轮说明"
  exit 0
fi

command -v curl >/dev/null 2>&1 || die "curl 不存在：无法读起跑门判据面 ⇒ 判红不判绿"
command -v python3 >/dev/null 2>&1 || die "python3 不存在：起跑门响应体无人解析 ⇒ 判红不判绿"

WORK=$(mktemp -d "${TMPDIR:-/tmp}/t463-gate.XXXXXX") || die "mktemp 失败"
chmod 700 "$WORK"
trap 'rm -rf "$WORK"' EXIT

sources=()
if [ -n "$FIXTURE" ]; then
  [ -r "$FIXTURE" ] || die "夹具不可读：$FIXTURE（读不到就不判绿）"
  sources+=("$FIXTURE")
  say "判据面来源 = 夹具 $FIXTURE（不发网络请求）"
else
  say "判据面来源 = $API_ROOT（匿名只读 GET，仓库为 public，不落任何凭据）"
  for st in in_progress queued; do
    url="$API_ROOT/repos/$REPO/actions/runs?status=$st&per_page=100"
    out="$WORK/runs-$st.json"
    # -f：4xx/5xx 直接非零；-m 30：卡住的读腿按「读不到」处理，不许当「没有在跑」
    if ! http_code=$(curl -fsS -m 30 -o "$out" -w '%{http_code}' "$url" 2>"$WORK/curl-$st.err"); then
      say "ERROR 查询 $st 失败（http=${http_code:-NA}）：$(tr '\n' '|' < "$WORK/curl-$st.err")" >&2
      die "起跑门判据面读不到 ⇒ 本轮不放行（宁可中止，也不撞进别人的真跑窗口）"
    fi
    [ -s "$out" ] || die "查询 $st 返回空响应体：无判据 ⇒ 不判绿"
    say "查询 $st 完成（http=$http_code bytes=$(wc -c < "$out" | tr -d '[:space:]')）"
    sources+=("$out")
  done
fi

# 解析 + 过滤 + 去重交给 python（本服务器无 jq：实测 command -v jq 为空，见本卡证据包）
#   三个判据集合只在上面的 bash 常量里定义一次，逐参数传给 python —— 两侧各写一份就会分叉。
rows=$(python3 - "$WORK/rows.out" "$WORK/excluded.out" \
            "$WORKFLOW_NAME" "$STAGING_EVENTS" "$BLOCKING_STATUS" "${sources[@]}" <<'PY'
import json, sys

out_path, excl_path, workflow_name, events, statuses = sys.argv[1:6]
staging_events = set(events.split())
blocking_status = set(statuses.split())
sources = sys.argv[6:]

runs, seen = [], set()
for path in sources:
    with open(path, encoding="utf-8") as fh:
        doc = json.load(fh)
    # 形状守卫：没有 workflow_runs 列表就什么都判不了（200 + 空壳不等于「没有在跑」）
    if not isinstance(doc, dict) or not isinstance(doc.get("workflow_runs"), list):
        print("SHAPE_BAD|%s" % path, file=sys.stderr)
        sys.exit(3)
    for r in doc["workflow_runs"]:
        rid = r.get("id")
        if rid is None or rid in seen:
            continue
        seen.add(rid)
        runs.append(r)

# 命中集 = workflow 名 + 状态两关都过；事件只在「打不打 staging」这一维度上分流
hits = [r for r in runs if r.get("name") == workflow_name and r.get("status") in blocking_status]

def fmt(r):
    return "|".join([
        str(r.get("id")), str(r.get("name")), str(r.get("event")),
        str(r.get("head_branch")), str(r.get("status")),
        str(r.get("run_started_at") or r.get("created_at")),
        str(r.get("html_url")),
    ]) + "\n"

with open(out_path, "w", encoding="utf-8") as out:
    for r in hits:
        if r.get("event") in staging_events:
            out.write(fmt(r))
# 被事件维度排除的那一类要能看见，否则「push 不算」出问题时只会表现为「一律放行」
with open(excl_path, "w", encoding="utf-8") as out:
    for r in hits:
        if r.get("event") not in staging_events:
            out.write(fmt(r))
print("SCANNED|%d" % len(runs))
PY
)
parse_rc=$?
if [ "$parse_rc" != "0" ]; then
  say "ERROR 响应体形状不合预期（缺 workflow_runs 列表）：无判据 ⇒ 不判绿" >&2
  exit 3
fi

summary=$(printf '%s\n' "$rows" | grep '^SCANNED|' || true)
scanned=$(printf '%s\n' "$summary" | cut -d'|' -f2)
[ -n "$scanned" ] || die "汇总行取不到候选数（SCANNED 行缺失）：无判据 ⇒ 不判绿"
say "判据面：去重后候选 run $scanned 条"
while IFS='|' read -r rid rname revent rbranch rstatus rstarted rurl; do
  [ -n "$rid" ] || continue
  say "排除（workflow=$rname 状态=$rstatus 但事件 $revent 不打 staging，见 e2e.yml 的 job if）run=$rid started=$rstarted"
done < "$WORK/excluded.out"

blocked=0
while IFS='|' read -r rid rname revent rbranch rstatus rstarted rurl; do
  [ -n "$rid" ] || continue
  blocked=$((blocked + 1))
  say "BLOCK run=$rid workflow=$rname event=$revent branch=$rbranch status=$rstatus run_started_at=$rstarted"
  say "      url=$rurl"
done < "$WORK/rows.out"

if [ "$blocked" -gt 0 ]; then
  say "结论：检出 $blocked 条正在跑 / 排队的打 staging E2E run ⇒ 起跑门判红，本轮不该部署"
  say "      等它跑完再起跑；确需强行起跑要显式置 E2E_GATE_DISABLE=1 并在部署日志同轮写明谁批的"
  exit 1
fi
say "结论：无在跑 / 排队的打 staging E2E run ⇒ 起跑门放行（只读检查，未改动任何东西）"
exit 0
