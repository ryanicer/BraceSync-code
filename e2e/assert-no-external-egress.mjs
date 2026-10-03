#!/usr/bin/env node
/**
 * T555 格二：CI 出网兜底断言（患者端 mock 套件）
 *
 * 判据：读 e2e/helpers.ts 落的网络层 JSONL（page.on('request') 逐条），
 * 只要出现【非本机 origin】的 http(s) 请求就判失败 —— mock 套件的写腿一条都不许出本机。
 *
 * 口径底线（T555 验收尺 2）：这里数的每一条都来自真实请求事件，不是源码字符串出现次数。
 *
 * 用法：node e2e/assert-no-external-egress.mjs --log <jsonl 路径>
 * （每次运行都先跑一遍尺子自检：合成 6 条「全本机 origin」（含 hostname 形、只有带端口 host 的旧形、
 *   IPv6 与 127.0.0.0/8 段）判 0 条；在其后注入 2 条假外网判 2 条；坏行判 parse_bad——
 *   三条不齐就 FAIL，判据自己不成立时不给绿牌。）
 * 退出码：0 = 零出网；1 = 有出网 / 日志缺失或为空 / 行解析失败 / 自检不通过。
 */
import { readFileSync, existsSync } from 'node:fs'

const LOOPBACK = new Set(['localhost', '127.0.0.1', '::1', '[::1]'])

function isLoopbackHost(host) {
  const h = String(host || '').toLowerCase()
  if (LOOPBACK.has(h)) return true
  if (h.startsWith('127.')) return true // 127.0.0.0/8 整段都是本机回环
  return false
}

/** URL.host 带端口（localhost:5173），URL.hostname 不带 —— 拿带端口那颗比回环集合会恒假。
 *  旧日志只有 host 字段，所以这里保留剥端口的退路。 */
function stripPort(h) {
  if (h.startsWith('[')) {
    const closing = h.indexOf(']')
    return closing >= 0 ? h.slice(0, closing + 1) : h
  }
  const colon = h.lastIndexOf(':')
  return colon > 0 && /^\d+$/.test(h.slice(colon + 1)) ? h.slice(0, colon) : h
}

function hostOf(rec) {
  const hostname = String(rec.hostname ?? '').trim()
  if (hostname) return hostname
  return stripPort(String(rec.host ?? '').trim())
}

/** 纯函数：一行读数 → 分类。判据只在这一处，便于自检直接喂合成行。 */
function classifyLine(line) {
  const rec = JSON.parse(line)
  const host = hostOf(rec)
  return {
    origin: String(rec.origin || ''),
    host,
    method: String(rec.method || ''),
    external: !isLoopbackHost(host),
  }
}

function evaluate(lines) {
  const offenders = []
  let observed = 0
  let bad = 0
  for (const line of lines) {
    if (!line.trim()) continue
    observed++
    try {
      const c = classifyLine(line)
      if (c.external) offenders.push(`${c.method} ${c.origin}`)
    } catch {
      bad++
    }
  }
  return { observed, offenders, bad }
}

function selftest() {
  const clean = [
    // hostname 优先
    JSON.stringify({ origin: 'http://localhost:5173', host: 'localhost:5173', hostname: 'localhost', method: 'GET' }),
    JSON.stringify({ origin: 'http://127.0.0.1:5174', host: '127.0.0.1:5174', hostname: '127.0.0.1', method: 'POST' }),
    // 旧日志形状：只有带端口的 host
    JSON.stringify({ origin: 'http://localhost:5173', host: 'localhost:5173', method: 'GET' }),
    JSON.stringify({ origin: 'http://127.0.0.1:1', host: '127.0.0.1:1', method: 'PUT' }),
    JSON.stringify({ origin: 'http://[::1]:5175', host: '[::1]:5175', method: 'GET' }),
    JSON.stringify({ origin: 'http://127.8.9.10:5173', host: '127.8.9.10:5173', method: 'GET' }),
  ]
  const dirty = clean.concat([
    JSON.stringify({ origin: 'http://api.t555-selftest.invalid', host: 'api.t555-selftest.invalid:80', method: 'POST' }),
    JSON.stringify({ origin: 'https://api.t555-selftest.invalid', host: 'api.t555-selftest.invalid', hostname: 'api.t555-selftest.invalid', method: 'PUT' }),
  ])
  const a = evaluate(clean)
  const b = evaluate(dirty)
  const cleanOk = a.observed === 6 && a.offenders.length === 0 && a.bad === 0
  const dirtyOk =
    b.observed === 8 &&
    b.offenders.length === 2 &&
    b.offenders[0] === 'POST http://api.t555-selftest.invalid' &&
    b.offenders[1] === 'PUT https://api.t555-selftest.invalid'
  const badFixture = evaluate(['{not json'])
  const badOk = badFixture.observed === 1 && badFixture.bad === 1
  const ok = cleanOk && dirtyOk && badOk
  console.log(
    `SELFTEST clean_observed=${a.observed} clean_offenders=${a.offenders.length} clean_bad=${a.bad} ` +
      `dirty_observed=${b.observed} dirty_offenders=${b.offenders.length} badline_observed=1 badline_bad=1 ok=${ok}`,
  )
  return ok
}

function argOf(name) {
  const i = process.argv.indexOf(name)
  return i > 0 && i + 1 < process.argv.length ? process.argv[i + 1] : null
}

const logPath = argOf('--log')
if (!logPath) {
  console.log('RESULT verdict=FAIL reason=missing --log（断言不许在没有日志的情况下绿）')
  process.exit(1)
}
const selftestOk = selftest()
let verdict = 'FAIL'
let observed = 0
let offenders = []
let bad = 0
if (existsSync(logPath)) {
  const raw = readFileSync(logPath, 'utf8')
  const lines = raw.split(/\r?\n/)
  const r = evaluate(lines)
  observed = r.observed
  offenders = r.offenders
  bad = r.bad
  console.log(`LOG path_present=true bytes=${Buffer.byteLength(raw, 'utf8')} observed_requests=${observed} parse_bad_lines=${bad}`)
} else {
  console.log('LOG path_present=false —— 观测腿没落盘，断言按失败处理（不拿「没有日志」当「没有出网」）')
}

const uniq = [...new Set(offenders)]
console.log(`RESULT observed_requests=${observed} external_origin_hits=${offenders.length} unique_offenders=${uniq.length} parse_bad_lines=${bad} selftest_ok=${selftestOk}`)
if (uniq.length) console.log(`OFFENDERS ${uniq.join(' | ')}`)
const pass = selftestOk && observed > 0 && bad === 0 && offenders.length === 0
if (pass) verdict = 'PASS（零出网）'
else if (!selftestOk) verdict = 'FAIL（尺子自检不通过，判据本身不可信）'
else if (observed === 0) verdict = 'FAIL（观测面为空：一条请求都没记到，不能证明零出网）'
else if (offenders.length) verdict = `FAIL（出网 ${offenders.length} 条）`
else if (bad) verdict = `FAIL（${bad} 行解析不了，覆盖面不完整）`
console.log(`RESULT verdict=${verdict}`)
process.exit(pass ? 0 : 1)
