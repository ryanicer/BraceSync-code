// T054 小程序「真实模式」E2E 共享 helper（CommonJS · miniprogram-automator 直驱）。
//
// 与 T053 的 e2e-real/real-helpers.ts 不同：T053 走 Playwright(web)；小程序真实模式必须连
// 运行中的微信开发者工具自动化端口（Windows/Boss 侧），故这里复用 T064 scripts/wechat/ 的连接
// 骨架（waitForPort / retry / withTimeout / mp.evaluate 取页面方法），并额外提供：
//   - target 自证（lib-target.js：产物打 staging → 正常跑；打 prod → 拒绝/告警）
//   - uniqueName(prefix) 造数据唯一命名（T054 惯例，对齐 T053）
//   - apiCall() Node 侧直连 staging 真实后端（http/https），用于「写后复核 / token 有效性」数据断言
//   - runSpec() 统一外壳：连接 → target 自证 → 跑 body → 落结果 JSON → 设退出码
//
// ⚠️ 禁止触碰 apps/ 业务代码；仅在本目录 + 复用 scripts/wechat/lib-target.js。

const path = require('path')
const fs = require('fs')
const http = require('http')
const https = require('https')
/* global getCurrentPages */
const automator = require('miniprogram-automator')
const targetReport = require('../scripts/wechat/lib-target')

// ─────────────────────────────────────────────────────────────
// 基础工具（来自 T064 smoke 骨架）
// ─────────────────────────────────────────────────────────────
function waitForPort(port, timeoutMs) {
  const net = require('net')
  return new Promise((resolve, reject) => {
    const start = Date.now()
    const tryConnect = () => {
      const sock = new net.Socket()
      sock.setTimeout(2000)
      sock.on('connect', () => { sock.destroy(); resolve() })
      sock.on('error', () => {
        if (Date.now() - start > timeoutMs) { reject(new Error(`Port ${port} not ready after ${timeoutMs}ms`)) }
        else { setTimeout(tryConnect, 1000) }
      })
      sock.on('timeout', () => { sock.destroy(); setTimeout(tryConnect, 1000) })
      sock.connect(port, '127.0.0.1')
    }
    tryConnect()
  })
}

function withTimeout(promise, ms, label) {
  return Promise.race([
    promise,
    new Promise((_, reject) => setTimeout(() => reject(new Error(`Timeout: ${label} (${ms}ms)`)), ms)),
  ])
}

async function retry(fn, times, intervalMs, label) {
  let lastErr
  for (let i = 0; i < times; i++) {
    try { return await fn() }
    catch (e) {
      lastErr = e
      console.log(`  [retry ${i + 1}/${times}] ${label}: ${e.message}`)
      await new Promise((r) => setTimeout(r, intervalMs))
    }
  }
  throw lastErr
}

/** 连接自动化端口（CONNECT_ONLY 需端口已就绪），返回 { mp, disconnect } */
async function connectMp(port, opts = {}) {
  const timeoutMs = opts.timeoutMs || 60_000
  await withTimeout(waitForPort(port, timeoutMs), timeoutMs + 5000, 'waitForPort')
  await new Promise((r) => setTimeout(r, 3000))
  const mp = await retry(
    () => withTimeout(automator.connect({ wsEndpoint: `ws://127.0.0.1:${port}` }), 15_000, 'connect'),
    5,
    4000,
    'connect'
  )
  return mp
}

/** 取栈顶页面路由（currentPage 偶发超时，用 evaluate + getCurrentPages） */
async function pageRoute(mp) {
  const s = await withTimeout(mp.evaluate(function () {
    const ps = getCurrentPages()
    if (!ps.length) return 'NONE'
    return ps[ps.length - 1].route
  }), 10_000, 'pageRoute')
  return String(s || 'NONE')
}

/** 截图（快速失败不阻塞，避免 WebSocket 掉线） */
async function shot(mp, dir, file) {
  fs.mkdirSync(dir, { recursive: true })
  try {
    await withTimeout(mp.screenshot({ path: path.join(dir, file) }), 5000, file)
    return true
  } catch {
    console.log(`  [截图] ${file} 失败（不阻塞）`)
    return false
  }
}

/** 造数据唯一命名：T054测试-<6位秒级后缀> */
let _seq = 0
function uniqueName(prefix = 'T054测试') {
  const ts = Date.now().toString().slice(-6)
  const n = String(_seq++).padStart(2, '0')
  return `${prefix}-${ts}-${n}`
}

// ─────────────────────────────────────────────────────────────
// Node 侧直连 staging 真实后端（数据断言用，不依赖小程序 UI）
// ─────────────────────────────────────────────────────────────
function apiCall(baseUrl, pathname, { method = 'GET', token, body } = {}) {
  const u = new URL(pathname, baseUrl)
  const httpMod = u.protocol === 'https:' ? https : http
  return new Promise((resolve, reject) => {
    const payload = body ? JSON.stringify(body) : null
    const req = httpMod.request(
      {
        hostname: u.hostname,
        port: u.port || (u.protocol === 'https:' ? 443 : 80),
        path: u.pathname + u.search,
        method,
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
          ...(payload ? { 'Content-Length': Buffer.byteLength(payload) } : {}),
        },
      },
      (res) => {
        let data = ''
        res.on('data', (c) => { data += c })
        res.on('end', () => {
          let json
          try { json = JSON.parse(data) }
          catch { json = data }
          resolve({ status: res.statusCode, body: json, raw: data })
        })
      }
    )
    req.on('error', reject)
    if (payload) req.write(payload)
    req.end()
  })
}

// ─────────────────────────────────────────────────────────────
// 步骤与结果收集（对齐 T053 自报口径：数据正确性 + 路由）
// ─────────────────────────────────────────────────────────────
function createResult(name) {
  return { name, startedAt: new Date().toISOString(), steps: [], errors: [], screenshots: [] }
}

function attachConsole(mp, result) {
  mp.on('console', (msg) => {
    const t = String(msg.text == null ? '' : msg.text)
    console.log(`  [Console.${msg.type}] ${t.substring(0, 200)}`)
    if (msg.type === 'error' && t && t !== 'undefined') result.errors.push(`[Console] ${t}`)
  })
  mp.on('exception', (err) => {
    console.log(`  [Exception] ${JSON.stringify(err)}`)
    result.errors.push(`[Exception] ${JSON.stringify(err)}`)
  })
}

function logStep(result, step, pass, actual) {
  result.steps.push({ step, result: pass ? 'PASS' : 'FAIL', actual: actual == null ? '' : String(actual) })
  console.log(`  [${step}] ${pass ? 'PASS' : 'FAIL'}${actual == null ? '' : ' · ' + JSON.stringify(actual)}`)
  return pass
}

/**
 * 统一外壳：target 自证 → 连接 → 跑 body → 落结果 → 设退出码。
 * body(mp, ctx) 返回 { pass }；ctx = { result, apiCall, uniqueName, logStep, shot, pageRoute, config, appDir }
 */
async function runSpec(cfg, opts) {
  const result = createResult(opts.name)
  fs.mkdirSync(cfg.resultsDir, { recursive: true })
  const appDir = cfg.appDir(opts.app)
  const distDir = cfg.distDir(opts.app)

  // 1. target 自证（staging 跑 / prod 拒 / unknown 告警）
  const built = targetReport.printTargetReport(appDir, distDir)
  let mp = null
  try {
    if (built.target === 'prod') {
      console.warn('\n[WARN] ⚠ 产物为生产目标（直连生产后端）！默认拒绝运行，需显式 ALLOW_PRODUCTION=1。\n')
      if (process.env.ALLOW_PRODUCTION !== '1') {
        result.errors.push('[target] 产物为 prod，拒绝运行')
        finish(cfg, opts, result, false)
        return
      }
      console.warn('[WARN] 已通过 ALLOW_PRODUCTION=1 显式放行。\n')
    } else if (built.target === 'unknown') {
      console.warn('\n[WARN] 产物未命中已知后端地址，可能未构建 staging。结果请谨慎判定。\n')
    }

    // 2. 连接自动化端口
    const port = cfg.ports[opts.role || opts.app] || opts.port
    console.log(`[连接] 连接自动化端口 ${port} ...`)
    mp = opts.connectPort
      ? await connectMp(opts.connectPort)
      : await connectMp(port)

    attachConsole(mp, result)

    // 3. 等待 IDE 就绪 + 跑 body
    console.log(`[就绪] 当前页面: ${await pageRoute(mp)}`)
    const ctx = {
      result, mp, apiCall, uniqueName, logStep, shot, pageRoute,
      config: cfg, appDir,
      baseUrl: cfg.staging,
    }
    const outcome = await opts.body(ctx)
    await new Promise((r) => setTimeout(r, 1000))
    finish(cfg, opts, result, !!outcome)
  } catch (e) {
    result.errors.push(e.message || String(e))
    if (e.stack) result.errors.push(e.stack)
    finish(cfg, opts, result, false)
  } finally {
    if (mp) { try { await mp.disconnect() } catch (e) { void e } }
  }
}

function finish(cfg, opts, result, pass) {
  result.finishedAt = new Date().toISOString()
  result.pass = pass && result.errors.length === 0
  // 不把 Console 里的异步 error 当失败（很多是业务告警/竞态），仅统计异常
  const filePath = path.join(cfg.resultsDir, `${opts.name}.json`)
  fs.writeFileSync(filePath, JSON.stringify(result, null, 2), 'utf8')
  console.log('\n===== [%s] 结果 =====', opts.name)
  console.log('步骤:', JSON.stringify(result.steps, null, 2))
  console.log('错误:', result.errors.length === 0 ? '无' : result.errors.join('\n'))
  console.log('总结:', result.pass ? 'PASS' : 'FAIL')
  process.exitCode = result.pass ? 0 : 1
}

/**
 * 技师端真实登录（Node 侧直连 staging），供 tech-* 多支 driver 复用。
 * 返回 { token, techId, name }；登录失败抛错（由调用方断言）。默认账号 T0001 老陈（staging 联调）。
 */
async function loginTech(baseUrl, phone, password) {
  const resp = await apiCall(baseUrl, '/api/v1/tech/login', {
    method: 'POST',
    body: { phone, password },
  })
  const d = resp.body && resp.body.data
  if (resp.status !== 200 || !d || !d.token || !d.techId) {
    throw new Error(`tech/login failed: status=${resp.status} body=${JSON.stringify(resp.body)}`)
  }
  return { token: d.token, techId: d.techId, name: d.name }
}

module.exports = {
  waitForPort,
  withTimeout,
  retry,
  connectMp,
  pageRoute,
  shot,
  uniqueName,
  apiCall,
  loginTech,
  ensurePatientToken,
  createResult,
  attachConsole,
  logStep,
  runSpec,
}

/**
 * 患者端鉴权获取（供 patient-* 多支 driver 复用，方案 C）。
 * 返回 { token, patientId, via: 'ui' | 'env' | 'prebound' }。
 * 顺序：
 *   1) 若 env 提供 PATIENT_TOKEN + PATIENT_ID → 种 storage（方案 C-纯，最稳）
 *   2) 否则驱动登录页「微信一键登录」→ 轮询 storage 拿 token（真实 UI → wx-login；需该 openid 已人工预置绑定）
 *   3) 若 UI 后落在 bind 页（NEED_BIND）或仍未取到 → 抛错说明「需一次性人工预置绑定 getPhoneNumber」（如实，不造假）
 */
async function ensurePatientToken(ctx) {
  const { mp } = ctx
  const service = require('./real-miniapp.config')
  const TOKEN_KEY = service.storage.patient.token
  const PID_KEY = service.storage.patient.patientId

  const envToken = process.env.PATIENT_TOKEN
  const envPid = process.env.PATIENT_ID
  if (envToken && envPid) {
    await mp.setStorageSync(TOKEN_KEY, envToken)
    await mp.setStorageSync(PID_KEY, envPid)
    return { token: envToken, patientId: envPid, via: 'env' }
  }

  // 已种/已登录：直接读 storage
  let token = await withTimeout(mp.getStorageSync(TOKEN_KEY), 5000, 'getStorage patient token')
  if (token) {
    const pid = await withTimeout(mp.getStorageSync(PID_KEY), 5000, 'getStorage patientId')
    return { token, patientId: pid, via: pid ? 'prebound' : 'token-only' }
  }

  // 驱动登录页 UI
  await withTimeout(mp.reLaunch('/pages/login/index'), 15_000, 'reLaunch patient login')
  await new Promise((r) => setTimeout(r, 4000))
  const tapped = await withTimeout(mp.evaluate(function () {
    const ps = getCurrentPages()
    if (!ps.length) return { ok: false }
    const page = ps[ps.length - 1]
    if (page.route !== 'pages/login/index') return { ok: false, route: page.route }
    const keys = Object.keys(page)
    const fn = keys.find(function (k) {
      if (typeof page[k] !== 'function') return false
      const src = String(page[k].toString() || '')
      return src.indexOf('wechatLogin') >= 0 || src.indexOf('wxLogin') >= 0
    })
    if (!fn) return { ok: false, reason: 'no_login_handler' }
    page[fn]({})
    return { ok: true, handler: fn }
  }), 10_000, 'tap wechat login')
  if (!tapped || !tapped.ok) {
    throw new Error(`patient wx-login handler 未找到: ${JSON.stringify(tapped)}`)
  }

  // 轮询 storage token（succ）或落在 bind 页（need_bind）
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 1500))
    token = await withTimeout(mp.getStorageSync(TOKEN_KEY), 5000, 'getStorage patient token')
    if (token) {
      const pid = await withTimeout(mp.getStorageSync(PID_KEY), 5000, 'getStorage patientId')
      return { token, patientId: pid, via: 'ui' }
    }
    const route = await pageRoute(mp)
    if (route === 'pages/login/bind' || route === 'pages/login/bind-success') {
      throw new Error('patient wx-login 返回 NEED_BIND：需一次性人工预置绑定 getPhoneNumber（方案 C 预置），或提供 PATIENT_TOKEN+PATIENT_ID')
    }
  }
  throw new Error('patient wx-login 30s 内未取到 token（需一次性人工预置绑定，或提供 PATIENT_TOKEN+PATIENT_ID）')
}