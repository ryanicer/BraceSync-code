#!/usr/bin/env node
// BraceSync CI 卫生门禁（T381 交付④：占位/跳过 job 必须自曝）
//
// 起因：main 上出现过两类「绿色但什么都没验」的 job ——
//   1 「小程序冒烟 (miniprogram-automator)」全文只有两句 echo（TODO + 需配置 devtools），恒绿；
//   2 job 的 if 条件与本 workflow 的触发器集合无交集 ⇒ 该 job 在这条线上永远不会跑，
//     而看板上它显示为「已跳过」，没人把它当缺覆盖。
// 本门禁把这两条变成机器判据，防第 4 次复发（同族：T358 / T364 / T374）。
//
// 判据（任一命中即 exit 1）：
//   A1 占位 job：某个 job 的全部 run 步骤里没有一条真断言命令，且没按 A2 自曝。
//   A2 自曝四件齐（缺一即判红）：job 块内有 `placeholder-ok:` 标记注释、
//      job 名含「占位」或「placeholder」、run 步骤里有 `::warning::`、且写 `$GITHUB_STEP_SUMMARY`。
//   A3 触发器死条件：job 的 `if:` 里出现的 github.event_name 字面量没有一个在本文件 `on:` 里注册。
//
// 只读文件、不装 node_modules、无第三方依赖（对齐 scripts/contract/go-json-tag-audit.mjs 的作业方式）。
// 用法：
//   node scripts/ci/workflow-placeholder-audit.mjs            # 扫本仓 .github/workflows
//   node scripts/ci/workflow-placeholder-audit.mjs --selftest # 注入式自证：判红/判绿两个方向都要对

import fs from "node:fs";
import os from "node:os";
import path from "node:path";

// 一条 run 里出现下面任一样式，就算这个 job「真在做验证」（不是只看绿色）。
// npm ci / docker login 这类准备工作不算；`playwright test --list` 也不算（T304 教训：
// 只 --list 等于一条用例都没跑过却报绿），所以在 hasAssertion() 里先剔掉带 --list 的行。
const ASSERTION_RE =
  /(playwright\s+test|vitest|\bjest\b|npm\b[^\n]*\brun\s+(test|lint|build|check|verify|typecheck)|npm\s+test|go\s+(test|vet|build)|golangci-lint|staticcheck|gofmt|pytest|cargo\s+test|mvn\s+test|node\s+--check|node\s+\S+\.mjs|node\s+\S+\.cjs|python3?\s+\S+\.py|tsc\b|eslint\b|bash\s+\S+\.sh|sh\s+\S+\.sh|bash\s+-n|shellcheck|shfmt)/;

// 只有「验证类」job 受 A1 约束（部署/发布类 job 的产出不是断言，硬要求会误伤）。
const TESTY_NAME_RE =
  /(e2e|playwright|vitest|jest|\btest|spec|smoke|冒烟|回归|lint|vet|golangci|check|verify|inspect|gate|门禁|对拍|audit|contract|drift|syntax|覆盖)/i;

/** 把 workflow 文本切成 job 块（行首两空格缩进的 key，位于 `jobs:` 之后）。 */
function parseJobs(text) {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const jobsIdx = lines.findIndex((l) => /^jobs:\s*$/.test(l));
  if (jobsIdx < 0) return [];
  const jobs = [];
  let cur = null;
  let inRun = false;
  let runIndent = 0;
  for (let i = jobsIdx + 1; i < lines.length; i++) {
    const line = lines[i];
    const topKey = i > jobsIdx && /^[A-Za-z_]/.test(line);
    if (topKey) break;
    const jobKey = line.match(/^ {2}([A-Za-z0-9_.-]+):\s*(#.*)?$/);
    if (jobKey) {
      // placeholder-ok 约定写在 job 上方的注释块里（注释贴着 key 才算是给这个 job 的）
      const lead = [];
      for (let k = i - 1; k > jobsIdx; k--) {
        const p = lines[k];
        if (/^ {2,4}#/.test(p)) lead.unshift(p);
        else if (p.trim() === "") continue;
        else break;
      }
      cur = { key: jobKey[1], lineNo: i + 1, name: "", ifExpr: "", runText: [], uses: [], raw: lead };
      jobs.push(cur);
      inRun = false;
      continue;
    }
    if (!cur) continue;
    cur.raw.push(line);
    const indent = line.match(/^\s*/)[0].length;
    if (inRun) {
      if (line.trim() === "" || indent > runIndent) {
        cur.runText.push(line);
        continue;
      }
      inRun = false;
    }
    const name = line.match(/^ {4}name:\s*(.+?)\s*$/);
    if (name && !cur.name) cur.name = name[1].replace(/^["']|["']$/g, "");
    const cond = line.match(/^ {4}if:\s*(.+?)\s*$/);
    if (cond && !cur.ifExpr) cur.ifExpr = cond[1];
    const useStep = line.match(/^\s*-?\s*uses:\s*(.+?)\s*$/);
    if (useStep) cur.uses.push(useStep[1]);
    const run = line.match(/^(\s*)-?\s*run:\s*(\|[>-]?)?\s*(.*)$/);
    if (run) {
      const block = run[2] && run[2].startsWith("|") === false ? false : Boolean(run[2]);
      if (run[3]) cur.runText.push(run[3]);
      if (block) {
        inRun = true;
        runIndent = run[1].length;
      }
    }
  }
  return jobs;
}

/** 本文件的触发器集合（`on:` 块里行首两空格的 key）。 */
function parseTriggers(text) {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const onIdx = lines.findIndex((l) => /^on:\s*$/.test(l));
  if (onIdx < 0) return new Set();
  const out = new Set();
  for (let i = onIdx + 1; i < lines.length; i++) {
    const line = lines[i];
    if (/^[A-Za-z_]/.test(line)) break;
    const key = line.match(/^ {2}([A-Za-z_]+):\s*/);
    if (key) out.add(key[1]);
  }
  return out;
}

function auditJob(fileName, job, triggers) {
  const findings = [];
  const runAll = job.runText.join("\n");
  const rawAll = job.raw.join("\n");

  const cond = job.ifExpr.match(/github\.event_name\s*==\s*'([A-Za-z_]+)'/g) || [];
  const wanted = cond.map((s) => s.match(/'([A-Za-z_]+)'/)[1]);
  const dead = wanted.length > 0 && wanted.every((w) => !triggers.has(w));
  if (dead) {
    findings.push(
      `${fileName}#L${job.lineNo} 「${job.key}」的 if 只允许事件 ${wanted.join("/")}，` +
        `而本文件 on: 注册的触发器是 ${[...triggers].join("/") || "无"} ⇒ 这个 job 在这条线上永不执行（A3 死条件）`,
    );
  }

  const hasAssertion = job.runText.some(
    (l) => !/--list\b/.test(l) && ASSERTION_RE.test(l),
  );
  const isTesty = TESTY_NAME_RE.test(`${job.name} ${job.key}`);
  if (!hasAssertion && isTesty) {
    const markers = {
      tag: /placeholder-ok:/.test(rawAll),
      name: /占位|placeholder/i.test(job.name || job.key),
      warn: /::warning::/.test(runAll),
      summary: /\$GITHUB_STEP_SUMMARY/.test(runAll),
    };
    const declared = Object.entries(markers).filter(([, v]) => v).map(([k]) => k);
    const missing = Object.entries(markers).filter(([, v]) => !v).map(([k]) => k);
    if (missing.length === 0) {
      // 四件齐 ⇒ 已自曝，不算缺陷（绿色但看板与日志都会写明「没验」）
    } else if (declared.length === 0) {
      findings.push(
        `${fileName}#L${job.lineNo} 「${job.key}」${job.name ? `「${job.name}」` : ""} 的 run 步骤里没有任何断言命令，` +
          `也没有自曝（缺 ${missing.join("/")}）⇒ 它会以绿色出现在看板上（A1 占位 job 未自曝）`,
      );
    } else {
      findings.push(
        `${fileName}#L${job.lineNo} 「${job.key}」自曝不完整：已有 ${declared.join("/")}，仍缺 ${missing.join("/")}` +
          `（A2 要求 placeholder-ok + 名字含「占位」+ ::warning:: + 写 job summary 四件齐）`,
      );
    }
  }
  return findings;
}

export function auditFiles(dir) {
  const findings = [];
  const scanned = [];
  for (const f of fs.readdirSync(dir).filter((x) => /\.ya?ml$/.test(x)).sort()) {
    const text = fs.readFileSync(path.join(dir, f), "utf8");
    const triggers = parseTriggers(text);
    const jobs = parseJobs(text);
    scanned.push({ file: f, jobs: jobs.length, triggers: [...triggers] });
    for (const job of jobs) findings.push(...auditJob(f, job, triggers));
  }
  return { findings, scanned };
}

/* ---------------------------- 注入式自证 ---------------------------- */

const CLEAN = `name: Clean
on:
  pull_request:
    branches: [main]
jobs:
  spec:
    name: Unit tests
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Run tests
        run: |
          npm test
`;

const SILENT_PLACEHOLDER = CLEAN.replace(
  `      - name: Run tests
        run: |
          npm test
`,
  `      - name: Run smoke
        run: |
          echo "TODO: 冒烟测试"
`,
);

const DECLARED_PLACEHOLDER = SILENT_PLACEHOLDER.replace(
  `  spec:
    name: Unit tests`,
  `  spec:
    # placeholder-ok: 需微信开发者工具，CI runner 无该环境，验收权在 Boss 侧实跑
    name: Unit tests（占位，未实跑）`,
).replace(
  `          echo "TODO: 冒烟测试"`,
  `          echo "::warning::本 job 是占位，未执行任何断言，绿色不代表已验"
          echo "占位 job：见 placeholder-ok 理由" >> "$GITHUB_STEP_SUMMARY"`,
);

const DEAD_TRIGGER = `name: Dead
on:
  schedule:
    - cron: "0 2 * * *"
jobs:
  pr-only:
    name: PR 门禁
    runs-on: ubuntu-latest
    if: github.event_name == 'pull_request'
    steps:
      - name: Run tests
        run: |
          npm test
`;

const LIST_ONLY = `name: ListOnly
on:
  pull_request:
    branches: [main]
jobs:
  admin-e2e:
    name: Admin E2E
    runs-on: ubuntu-latest
    steps:
      - name: List cases
        run: npx playwright test --list
`;

function selftest() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "t381-audit-"));
  const cases = [
    { file: "clean.yml", text: CLEAN, expect: 0, what: "有断言的普通 job ⇒ 判绿" },
    { file: "silent.yml", text: SILENT_PLACEHOLDER, expect: 1, what: "恒绿占位 job 未自曝 ⇒ 判红" },
    { file: "declared.yml", text: DECLARED_PLACEHOLDER, expect: 0, what: "四件齐自曝 ⇒ 判绿" },
    { file: "dead.yml", text: DEAD_TRIGGER, expect: 1, what: "if 与触发器无交集 ⇒ 判红" },
    { file: "listonly.yml", text: LIST_ONLY, expect: 1, what: "只 playwright --list 不实跑 ⇒ 判红" },
  ];
  let bad = 0;
  for (const c of cases) {
    fs.rmSync(dir, { recursive: true, force: true });
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(path.join(dir, c.file), c.text, "utf8");
    const { findings } = auditFiles(dir);
    const ok = findings.length === c.expect;
    if (!ok) bad++;
    console.log(`${ok ? "PASS" : "FAIL"} ${c.what}（期望 ${c.expect}，实际 ${findings.length}）`);
    for (const f of findings) console.log(`      ${f}`);
  }
  fs.rmSync(dir, { recursive: true, force: true });
  console.log(`自证结论：${bad === 0 ? "判红/判绿两个方向都对" : `${bad} 个方向不对`}`);
  return bad === 0 ? 0 : 1;
}

/* ------------------------------- main ------------------------------- */

if (process.argv[2] === "--selftest") {
  process.exit(selftest());
}

const wfDir = path.resolve(process.cwd(), ".github/workflows");
const { findings, scanned } = auditFiles(wfDir);
console.log(`扫描 ${scanned.length} 个 workflow，共 ${scanned.reduce((n, s) => n + s.jobs, 0)} 个 job`);
for (const s of scanned) console.log(`  ${s.file}: jobs=${s.jobs} triggers=${s.triggers.join("/") || "无"}`);
if (findings.length) {
  console.log(`判红 ${findings.length} 条：`);
  for (const f of findings) console.log(`  - ${f}`);
  console.log("修法：补成真断言；确实在 CI 跑不了的，按 A2 四件齐自曝（placeholder-ok 注释 + 名字含「占位」+ ::warning:: + 写 job summary）。");
  process.exit(1);
}
console.log("判绿：无「恒绿占位」与「触发器死条件」job");
