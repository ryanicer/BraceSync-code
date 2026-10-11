// T651 · e2e-real 双通道（staging / tst）目标面预检尺
//
// 判什么（三格，缺一格就可能是假绿）：
//   1) 地址门：三颗注入变量非空、形状是 http + 显式端口、且一枚都不命中生产入口（生产零写红线，
//      口径同 e2e-real/h5-origin.ts 与 tests/22、24 号用例）。
//   2) 同源门：本轮 target 决定三颗应当各指哪里 —— staging 档必须逐字等于在册的两串，
//      tst 档必须逐字等于仓内在册的 apps/{patient,tech}-miniapp/.env.tst 里那串 VITE_API_BASE_URL。
//      这一格钉的是「换档只换了入口变量、H5 两腿静默留在 staging」那一形（T614 就是被它咬的），
//      也钉住「默认值漂移」：workflow 里那三枚三元表达式若被人改了，这里当场对不上在册值。
//   3) 可达门：对各枚去重后的 origin 做 TCP + GET /admin/ 探针。入口那一枚连不上即判红并点名，
//      H5 两枚只登记读数不判红（分工见 main 里那段注释：用例自己会因打不通而红，判据没放宽）。
//      这一格是快速失败用的 —— 入口不可达时不许把 45 分钟 runner 分钟烧在逐用例超时上，
//      读数要一眼能看出「红在通道，不在用例」。GET 的状态码只作读数登记（403/500 属产物投放态，
//      归 T650 那一路），本尺不拿它判红，因为判据一枚没放宽：可达性判可达性，投放态判投放态。
//
// 口令类变量不经这里（仍由用例自己的凭据门管），本尺也一行凭据都不打印。
// 本地复跑：node scripts/ci/e2e-real-target-preflight.mjs [--target staging|tst] [--skip-probe] [--selftest]
//   --selftest 把三格各自的正反两向都注牙跑一遍（判据有没有牙由这一发证明，不靠散文）。

import fs from 'node:fs';
import path from 'node:path';
import net from 'node:net';
import http from 'node:http';

const STAGING_ENTRY = 'http://106.52.39.208:81';
const STAGING_H5_ORIGIN = 'http://hbksd.com.cn:81';
const PROD_FORBIDDEN = /api\.hbksd\.com\.cn/; // 49.235.137.217 自 2026-10-11 起为 TST 环境（Boss 口径），移出生产针；生产面=同机 80/443 与 api.hbksd.com.cn
const TST_ENV_FILES = ['apps/patient-miniapp/.env.tst', 'apps/tech-miniapp/.env.tst'];
const URL_VARS = {
  entry: 'E2E_STAGING_URL',
  tech: 'E2E_TECH_H5_URL',
  patient: 'E2E_PATIENT_H5_URL',
};

function arg(name, fallback) {
  const i = process.argv.indexOf('--' + name);
  return i >= 0 && process.argv[i + 1] && !String(process.argv[i + 1]).startsWith('--')
    ? process.argv[i + 1]
    : fallback;
}
const hasFlag = (name) => process.argv.includes('--' + name);

function readEnvFile(file) {
  const out = {};
  if (!fs.existsSync(file)) return out;
  for (const line of fs.readFileSync(file, 'utf8').replace(/\r\n/g, '\n').split('\n')) {
    const m = line.match(/^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/);
    if (m) out[m[1]] = m[2].trim().replace(/^["']|["']$/g, '');
  }
  return out;
}

/** 仓内在册的 TST 基址：两颗 .env.tst 的 VITE_API_BASE_URL，必须都在场且同值。 */
function registeredTstOrigin(repoRoot) {
  const per = TST_ENV_FILES.map((f) => {
    const file = path.join(repoRoot, f);
    return { f, file, url: (readEnvFile(file).VITE_API_BASE_URL ?? '').trim() };
  });
  const findings = [];
  for (const p of per) if (!p.url) findings.push(`在册 TST 基址缺席：${p.f} 没有非空 VITE_API_BASE_URL`);
  const uniq = [...new Set(per.map((p) => p.url).filter(Boolean))];
  if (uniq.length > 1) findings.push(`在册 TST 基址两串不等值：${uniq.join(' / ')}`);
  return { origin: uniq[0] ?? '', per, findings };
}

function shapeOk(origin) {
  return /^http:\/\/[^/]+:\d+$/.test(origin);
}

/**
 * 三格里的地址门 + 同源门（纯函数，--selftest 直接喂合成 env 打牙）。
 * @param {{target?: string, entry?: string, tech?: string, patient?: string, tstRegistered?: string}} env
 * @returns {string[]} 判语数组，空即绿
 */
function validateAddresses(env) {
  const findings = [];
  const target = (env.target ?? 'staging').trim().toLowerCase();
  if (target !== 'staging' && target !== 'tst') {
    findings.push(`未知 target=[${target}]，取值域只有 staging / tst（不静默兜底成 staging）`);
    return findings;
  }
  const got = { entry: env.entry ?? '', tech: env.tech ?? '', patient: env.patient ?? '' };
  for (const [role, name] of Object.entries(URL_VARS)) {
    const v = got[role];
    if (!v) {
      findings.push(`缺注入变量 ${name}（CI 不许回落默认值，判红而不是静默）`);
      continue;
    }
    if (!shapeOk(v)) findings.push(`${name}=${v} 形状不合（应是 http + 显式端口）`);
    if (PROD_FORBIDDEN.test(v)) findings.push(`${name}=${v} 命中生产入口，红线拒绝`);
  }
  if (findings.length > 0) return findings;

  const expect =
    target === 'staging'
      ? { entry: STAGING_ENTRY, tech: STAGING_H5_ORIGIN, patient: STAGING_H5_ORIGIN }
      : (() => {
          const tst = (env.tstRegistered ?? '').trim();
          if (!tst) {
            findings.push(`target=tst 但仓内在册 TST 基址取不到（见 ${TST_ENV_FILES.join(' / ')}）`);
            return null;
          }
          if (!shapeOk(tst)) {
            findings.push(`仓内在册 TST 基址 ${tst} 形状不合（应是 http + 显式端口）`);
            return null;
          }
          if (PROD_FORBIDDEN.test(tst)) {
            findings.push(`仓内在册 TST 基址 ${tst} 命中生产入口，红线拒绝`);
            return null;
          }
          return { entry: tst, tech: tst, patient: tst };
        })();
  if (!expect) return findings;

  for (const [role, name] of Object.entries(URL_VARS)) {
    if (got[role] !== expect[role]) {
      findings.push(
        `同源门不合：target=${target} 时 ${name} 应在册为 ${expect[role]}，实得 ${got[role]}` +
          (role === 'entry' ? '（入口腿）' : `（${role === 'tech' ? '链 B 技师端' : '链 C 患者端'} H5 腿）`),
      );
    }
  }
  return findings;
}

function tcp(host, port, ms) {
  return new Promise((res) => {
    const s = net.connect({ host, port, timeout: ms });
    s.on('connect', () => { s.destroy(); res('OPEN'); });
    s.on('timeout', () => { s.destroy(); res('TIMEOUT'); });
    s.on('error', (e) => res('ERR/' + e.code));
  });
}

function get(origin, p, ms) {
  return new Promise((res) => {
    const u = new URL(origin);
    const req = http.get({ host: u.hostname, port: u.port, path: p, timeout: ms }, (r) => {
      let n = 0;
      r.on('data', (c) => { n += c.length; });
      r.on('end', () => res('HTTP ' + r.statusCode + '/' + n + 'B'));
    });
    req.on('timeout', () => { req.destroy(); res('REQ_TIMEOUT'); });
    req.on('error', (e) => res('REQ_ERR/' + e.code));
  });
}

async function probeOrigins(origins, ms) {
  const rows = [];
  for (const origin of origins) {
    const u = new URL(origin);
    const t = await tcp(u.hostname, Number(u.port), ms);
    const h = t === 'OPEN' ? await get(origin, '/admin/', ms) : '-';
    rows.push({ origin, tcp: t, http: h });
  }
  return rows;
}

function summaryLines(o) {
  const L = [];
  L.push('## T651 e2e-real 目标面预检（只读，零写）');
  L.push('');
  L.push(`- target：${o.target}`);
  L.push(`- 入口腿 ${URL_VARS.entry}：${o.got.entry}`);
  L.push(`- 链 B H5 ${URL_VARS.tech}：${o.got.tech}`);
  L.push(`- 链 C H5 ${URL_VARS.patient}：${o.got.patient}`);
  L.push(`- 仓内在册 TST 基址（${TST_ENV_FILES.join(' + ')} 的 VITE_API_BASE_URL）：${o.tstRegistered || '<取不到>'}`);
  for (const r of o.probe) L.push(`- 探针 ${r.origin} /admin/：tcp=${r.tcp} probe=${r.http}`);
  L.push(`- 结论：${o.verdict}`);
  return L;
}

function writeSummary(lines) {
  const file = process.env.GITHUB_STEP_SUMMARY;
  if (!file) return 'GITHUB_STEP_SUMMARY 未设（本地复跑，summary 只打 stdout）';
  try {
    fs.appendFileSync(file, lines.join('\n') + '\n', 'utf8');
    return 'summary 已写 ' + file;
  } catch (e) {
    return 'summary 写失败 ' + e.message;
  }
}

/** --selftest：三格各正反两向注牙，全部走纯函数，不触网、不读盘上的 .env.tst */
function selftest() {
  const base = { target: 'staging', entry: STAGING_ENTRY, tech: STAGING_H5_ORIGIN, patient: STAGING_H5_ORIGIN };
  const cases = [
    { what: 'staging 档在册三串齐 ⇒ 绿', env: base, expect: 0 },
    { what: '未知 target ⇒ 红', env: { ...base, target: 'prod' }, expect: 1 },
    { what: '入口变量缺席 ⇒ 红', env: { ...base, entry: '' }, expect: 1 },
    { what: '入口指生产域名 ⇒ 红（生产红线）', env: { ...base, entry: 'http://api.hbksd.com.cn:81' }, expect: 1 },
    { what: 'H5 腿指生产域 ⇒ 红（生产红线）', env: { ...base, patient: 'http://api.hbksd.com.cn:443' }, expect: 1 },
    { what: '缺显式端口 ⇒ 红（形状门）', env: { ...base, entry: 'http://106.52.39.208' }, expect: 1 },
    {
      what: '换档只换入口、链 B 的 H5 静默留 staging ⇒ 红（T614 那一形）',
      env: { target: 'tst', entry: 'http://49.235.137.217:81', tech: STAGING_H5_ORIGIN, patient: 'http://49.235.137.217:81', tstRegistered: 'http://49.235.137.217:81' },
      expect: 1,
    },
    { what: 'tst 档三串齐 ⇒ 绿', env: { target: 'tst', entry: 'http://49.235.137.217:81', tech: 'http://49.235.137.217:81', patient: 'http://49.235.137.217:81', tstRegistered: 'http://49.235.137.217:81' }, expect: 0 },
    { what: 'tst 档但在册基址取不到 ⇒ 红', env: { target: 'tst', entry: 'http://49.235.137.217:81', tech: 'http://49.235.137.217:81', patient: 'http://49.235.137.217:81', tstRegistered: '' }, expect: 1 },
    { what: 'tst 档入口漂到另一枚地址（与在册不同源）⇒ 红', env: { target: 'tst', entry: 'http://49.235.137.217:8081', tech: 'http://49.235.137.217:81', patient: 'http://49.235.137.217:81', tstRegistered: 'http://49.235.137.217:81' }, expect: 1 },
  ];
  let bad = 0;
  for (const c of cases) {
    const f = validateAddresses(c.env);
    const hit = f.length > 0 ? 1 : 0;
    const ok = c.expect === 0 ? f.length === 0 : f.length >= c.expect;
    if (!ok) bad++;
    console.log(`[selftest] ${ok ? 'ok  ' : 'FAIL'} 判语=${f.length} 期望>=${c.expect} | ${c.what}` + (f.length ? ` | 首条=${f[0]}` : ''));
  }
  const reg = registeredTstOrigin(process.cwd());
  console.log(`[selftest] 在册 TST 基址面读取：origin=${reg.origin || '<空>'} 判语=${reg.findings.length}（两枚 .env.tst 各一行在场即应等值）`);
  console.log('[selftest] 面名=validateAddresses 合成用例=' + cases.length + ' 不合期望=' + bad);
  console.log(bad === 0 ? 'RESULT GATE-GREEN 牙全咬住' : 'RESULT GATE-RED 有期望没被满足 bad=' + bad);
  return bad === 0 ? 0 : 1;
}

async function main() {
  const repoRoot = process.cwd();
  if (hasFlag('selftest')) process.exit(selftest());

  const target = (arg('target', process.env.E2E_TARGET ?? 'staging') || 'staging').trim().toLowerCase();
  const got = {
    entry: (process.env[URL_VARS.entry] ?? '').trim(),
    tech: (process.env[URL_VARS.tech] ?? '').trim(),
    patient: (process.env[URL_VARS.patient] ?? '').trim(),
  };
  const reg = registeredTstOrigin(repoRoot);
  const findings = validateAddresses({ target, ...got, tstRegistered: reg.origin });
  for (const f of reg.findings) findings.push('在册面：' + f);

  const origins = [...new Set([got.entry, got.tech, got.patient].filter(shapeOk))];
  const skipProbe = hasFlag('skip-probe');
  const probe = skipProbe ? [] : await probeOrigins(origins, Number(arg('timeout-ms', '6000')));
  // 硬门只押入口那一枚：真实模式每条用例都要在它上面导航，今天它不通就已经是红的
  //   ⇒ 这一枚判红不新增失败模式，只是把「45 分钟烧在逐用例超时」换成「秒级点名通道」。
  // H5 两枚 origin 只登记读数不判红：链 B / 链 C 的用例自己会因打不通而判红（判据一枚没放宽），
  //   而它们的源是域名形、多绕一枚 DNS ⇒ 升级成硬门等于给 staging 档默认 lane 新增本卡不需要的
  //   失败模式（本卡要求「现有 staging lane 行为零变更」）。
  if (!skipProbe) {
    for (const r of probe) {
      if (r.tcp === 'OPEN') continue;
      if (r.origin === got.entry) {
        findings.push(`可达门（硬）：入口 ${r.origin} tcp=${r.tcp} —— 当前执行面打不到这一枚，先判通道再判用例`);
      } else {
        console.log('NOTE H5 源探针（只登记，不判红）：' + r.origin + ' tcp=' + r.tcp + ' probe=' + r.http);
      }
    }
  }

  const verdict = findings.length === 0 ? 'PASS（三格全过，本尺不碰任何写路由）' : 'FAIL（判语逐条见下）';
  const lines = summaryLines({ target, got, tstRegistered: reg.origin, probe, verdict });
  for (const l of lines) console.log(l);
  for (const f of findings) console.log('FINDING ' + f);
  console.log('PREFLIGHT target=' + target + ' origins=' + origins.length + ' findings=' + findings.length + ' probe=' + (skipProbe ? 'skipped' : probe.length) + ' face=env:three-vars + repo:.env.tst + tcp');
  console.log(writeSummary(lines.concat(findings.map((f) => '- 判语：' + f))));
  console.log(findings.length === 0 ? 'RESULT GATE-GREEN' : 'RESULT GATE-RED findings=' + findings.length);
  process.exit(findings.length === 0 ? 0 : 1);
}

main().catch((e) => {
  console.log('RESULT ABORT ' + (e && e.message ? e.message : String(e)));
  process.exit(1);
});
