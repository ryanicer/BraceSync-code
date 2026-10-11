// T609 守时自记（乙案 2026-10-07 立，甲案 2026-10-11 Boss 裁决后迁移归因面）：
// 把「本 run 相对名义档点时刻迟到了多少分钟」打进 e2e-real-staging 的 job summary。
// 边界：只读 Actions API 里本 run 自己那一格、只往 stdout 打 summary 行；
// 不改调度、不打 staging、不碰用例。
//
// 甲案前：真实档由 GitHub schedule 触发，名义时刻只能由 github.event.schedule 的「分 时」两格推出
//   —— GitHub 不给定时事件的名义触发时刻；两格中任一不是单一整数，或日/月/周任一格不是 * ⇒ 明写不判。
// 甲案后（2026-10-11）：真实档改由值班席 automation 外部定时腿在 UTC 12:00（北京 20:00）打
//   workflow_dispatch（inputs.source=external-cron）。dispatch 事件没有 event.schedule，
//   外部腿的名义档点固定为 0 12 * * *；手动 dispatch（source=manual/缺省）不参与守时达标。

const DAY_MS = 86400000;

/** 甲案：外部定时腿的固定名义档（UTC），与已退役的 schedule "0 12 * * *" 同一时刻 */
export const EXTERNAL_CRON_NOMINAL = "0 12 * * *";

export function parseDailyCron(cron) {
  const f = String(cron || "").trim().split(/\s+/);
  if (f.length !== 5) return { ok: false, why: "CRON_FIELD_COUNT=" + f.length };
  if (!/^\d{1,2}$/.test(f[0]) || !/^\d{1,2}$/.test(f[1])) return { ok: false, why: "CRON_MIN_OR_HOUR_NOT_SINGLE_INT" };
  const minute = Number(f[0]);
  const hour = Number(f[1]);
  if (minute > 59) return { ok: false, why: "CRON_MIN_OUT_OF_RANGE" };
  if (hour > 23) return { ok: false, why: "CRON_HOUR_OUT_OF_RANGE" };
  if (f[2] !== "*" || f[3] !== "*" || f[4] !== "*") return { ok: false, why: "CRON_NOT_DAILY" };
  return { ok: true, minute, hour };
}

export function nominalFromCron(cron, createdMs) {
  const p = parseDailyCron(cron);
  if (!p.ok) return { judged: false, why: p.why };
  const d = new Date(createdMs);
  let n = Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate(), p.hour, p.minute, 0);
  // created_at 早于当日名义时刻 ⇒ 本轮补的是前一天那一档（延迟跨 UTC 零点那一形）
  if (n > createdMs) n -= DAY_MS;
  return { judged: true, nominalMs: n, hour: p.hour, minute: p.minute };
}

export function judge(opts) {
  const eventName = opts.eventName;
  const lineHour = opts.lineHour;
  const maxMinutes = opts.maxMinutes;
  const source = opts.source || "";
  // 甲案：外部定时腿的 dispatch 与旧 schedule 12 档共用同一判定路径（固定名义档 UTC 12:00）
  if (eventName === "workflow_dispatch" && source === "external-cron") {
    const n = nominalFromCron(EXTERNAL_CRON_NOMINAL, opts.createdMs);
    if (!n.judged) return { verdict: "CRON_SHAPE_UNSUPPORTED", lateMinutes: null, nominalMs: null, detail: "外部档常量形不支持：" + n.why };
    const lateMinutes = Math.floor((opts.createdMs - n.nominalMs) / 60000);
    return {
      verdict: lateMinutes <= maxMinutes ? "ON_TIME" : "OVER_LIMIT",
      lateMinutes,
      nominalMs: n.nominalMs,
      detail: "外部定时腿（external-cron）名义档 UTC " + n.hour + ":" + String(n.minute).padStart(2, "0")
        + (lateMinutes <= maxMinutes ? "，达标线内（≤ " + maxMinutes + " 分）" : "，越界（> " + maxMinutes + " 分）")
    };
  }
  if (eventName !== "schedule") {
    return { verdict: "NOT_SCHEDULE_EVENT", lateMinutes: null, nominalMs: null, detail: "触发器是 " + eventName + (eventName === "workflow_dispatch" ? "（source=" + (source || "manual") + "，手动 dispatch 不计守时）" : "") + "，本步不判迟到（只证明取数通路在 run 内跑通过）" };
  }
  const n = nominalFromCron(opts.cron, opts.createdMs);
  if (!n.judged) return { verdict: "CRON_SHAPE_UNSUPPORTED", lateMinutes: null, nominalMs: null, detail: "cron 形不支持单点推导：" + n.why + "，原值 [" + opts.cron + "]" };
  const lateMinutes = Math.floor((opts.createdMs - n.nominalMs) / 60000);
  if (n.hour !== lineHour) {
    return { verdict: "NO_LINE", lateMinutes, nominalMs: n.nominalMs, detail: "名义档不是本卡达标线管的那一档（名义时 " + n.hour + "，线设在 " + lineHour + "），只报数不判线" };
  }
  return {
    verdict: lateMinutes <= maxMinutes ? "ON_TIME" : "OVER_LIMIT",
    lateMinutes,
    nominalMs: n.nominalMs,
    detail: lateMinutes <= maxMinutes ? "达标线内（≤ " + maxMinutes + " 分）" : "越界（> " + maxMinutes + " 分）"
  };
}

const FIXTURES = [
  { name: "卡面那一档的 9 时 35 分", eventName: "schedule", cron: "0 12 * * *", createdMs: Date.UTC(2026, 9, 4, 21, 35), expect: { verdict: "OVER_LIMIT", lateMinutes: 575 } },
  { name: "准点起（2 分）", eventName: "schedule", cron: "0 12 * * *", createdMs: Date.UTC(2026, 9, 4, 12, 2), expect: { verdict: "ON_TIME", lateMinutes: 2 } },
  { name: "零分整", eventName: "schedule", cron: "0 12 * * *", createdMs: Date.UTC(2026, 9, 4, 12, 0), expect: { verdict: "ON_TIME", lateMinutes: 0 } },
  { name: "迟到跨 UTC 零点要回退一天", eventName: "schedule", cron: "0 12 * * *", createdMs: Date.UTC(2026, 9, 5, 0, 30), expect: { verdict: "OVER_LIMIT", lateMinutes: 750 } },
  { name: "非定时触发", eventName: "pull_request", cron: "", createdMs: Date.UTC(2026, 9, 4, 12, 2), expect: { verdict: "NOT_SCHEDULE_EVENT", lateMinutes: null } },
  { name: "分钟格是步长形 ⇒ 不判", eventName: "schedule", cron: "*/15 0 * * *", createdMs: Date.UTC(2026, 9, 4, 12, 2), expect: { verdict: "CRON_SHAPE_UNSUPPORTED", lateMinutes: null } },
  { name: "带日期限定 ⇒ 非每日形，不判", eventName: "schedule", cron: "0 12 1 * *", createdMs: Date.UTC(2026, 9, 4, 12, 2), expect: { verdict: "CRON_SHAPE_UNSUPPORTED", lateMinutes: null } },
  { name: "分钟格越界 ⇒ 不判", eventName: "schedule", cron: "61 12 * * *", createdMs: Date.UTC(2026, 9, 4, 12, 2), expect: { verdict: "CRON_SHAPE_UNSUPPORTED", lateMinutes: null } },
  { name: "巡检档（名义 2 时）只报数不判线", eventName: "schedule", cron: "0 2 * * *", createdMs: Date.UTC(2026, 9, 4, 3, 0), expect: { verdict: "NO_LINE", lateMinutes: 60 } },
  { name: "甲案外部定时腿准点（12:02，2 分）", eventName: "workflow_dispatch", source: "external-cron", cron: "", createdMs: Date.UTC(2026, 9, 11, 12, 2), expect: { verdict: "ON_TIME", lateMinutes: 2 } },
  { name: "甲案外部定时腿迟到 90 分判越界", eventName: "workflow_dispatch", source: "external-cron", cron: "", createdMs: Date.UTC(2026, 9, 11, 13, 30), expect: { verdict: "OVER_LIMIT", lateMinutes: 90 } },
  { name: "甲案外部定时腿跨 UTC 零点回退当天档", eventName: "workflow_dispatch", source: "external-cron", cron: "", createdMs: Date.UTC(2026, 9, 12, 0, 30), expect: { verdict: "OVER_LIMIT", lateMinutes: 750 } },
  { name: "手动 dispatch（manual）不判守时", eventName: "workflow_dispatch", source: "manual", cron: "", createdMs: Date.UTC(2026, 9, 11, 12, 2), expect: { verdict: "NOT_SCHEDULE_EVENT", lateMinutes: null } },
  { name: "缺省 source 的 dispatch 按 manual 不判守时", eventName: "workflow_dispatch", source: "", cron: "", createdMs: Date.UTC(2026, 9, 11, 12, 2), expect: { verdict: "NOT_SCHEDULE_EVENT", lateMinutes: null } }
];

function selftest() {
  let bad = 0;
  for (const fx of FIXTURES) {
    const got = judge({ eventName: fx.eventName, cron: fx.cron, source: fx.source, createdMs: fx.createdMs, lineHour: 12, maxMinutes: 60 });
    const ok = got.verdict === fx.expect.verdict && got.lateMinutes === fx.expect.lateMinutes;
    if (!ok) bad++;
    console.log((ok ? "PASS " : "FAIL ") + fx.name + " => verdict=" + got.verdict + " late=" + got.lateMinutes + " expect=" + fx.expect.verdict + "/" + fx.expect.lateMinutes);
  }
  console.log("FIXTURES=" + FIXTURES.length + " BAD=" + bad);
  console.log(bad === 0 ? "RESULT SELFTEST_OK" : "RESULT SELFTEST_FAIL");
  return bad === 0 ? 0 : 1;
}

async function live() {
  const repo = process.env.GITHUB_REPOSITORY || "";
  const runId = process.env.GITHUB_RUN_ID || "";
  const token = process.env.GITHUB_TOKEN || "";
  const eventName = process.env.GITHUB_EVENT_NAME || "";
  const cron = process.env.SCHEDULE_CRON || "";
  const source = process.env.TRIGGER_SOURCE || "";
  const maxMinutes = Number(process.env.E2E_ON_TIME_MAX_MINUTES || 60);
  const lineHour = Number(process.env.E2E_ON_TIME_LINE_HOUR_UTC || 12);

  console.log("### e2e-real 守时自记（T609）");
  console.log("- 触发器（github.event_name）：" + eventName);
  console.log("- cron（github.event.schedule）：" + (cron === "" ? "（非定时触发，无此格）" : cron));
  console.log("- 来源（inputs.source，甲案守时档认 external-cron）：" + (source === "" ? "（未提供，按 manual）" : source));

  let createdMs = null;
  let createdAt = null;
  let runStartedAt = null;
  try {
    const res = await fetch("https://api.github.com/repos/" + repo + "/actions/runs/" + runId, {
      headers: { Authorization: "Bearer " + token, "User-Agent": "e2e-real-lateness-summary" }
    });
    if (!res.ok) throw new Error("HTTP " + res.status);
    const j = await res.json();
    createdAt = j.created_at;
    runStartedAt = j.run_started_at;
    createdMs = Date.parse(createdAt);
  } catch (e) {
    console.log("::warning:: 守时自记取数失败（" + e.message + "），本步只自曝不影响用例判定");
    console.log("LATE_MINUTES=NA VERDICT=API_READ_FAILED");
    return 0;
  }
  console.log("- 本 run created_at（Actions API 现读，run " + runId + "）：" + createdAt + "（run_started_at " + runStartedAt + "）");
  const r = judge({ eventName, cron, source, createdMs, lineHour, maxMinutes });
  if (r.nominalMs != null) console.log("- 名义触发时刻（" + (eventName === "workflow_dispatch" ? "external-cron 固定档 UTC 12:00" : "cron 分/时两格推") + "，早于 created_at 才用当日，否则回退一天）：" + new Date(r.nominalMs).toISOString());
  console.log("- 实测迟到：" + (r.lateMinutes == null ? "不判" : r.lateMinutes + " 分") + "；判语：" + r.detail);
  console.log("LATE_MINUTES=" + (r.lateMinutes == null ? "NA" : r.lateMinutes) + " VERDICT=" + r.verdict);
  return 0;
}

if (process.argv.includes("--selftest")) {
  process.exitCode = selftest();
} else {
  process.exitCode = await live();
}
