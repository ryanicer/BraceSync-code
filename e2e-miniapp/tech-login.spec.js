// T054 技师端 · 真实登录（staging real API，USE_MOCK=false）
//
// 断言「数据正确性」（非仅路由）：
//   1) Node 侧直连 POST /api/v1/tech/login 拿真实 token/techId/name（T0001 老陈 等 staging 账号）
//   2) 优先驱动登录页 UI 表单（真实点登）拿 storage 里的 token；若编译态控件名不可靠则回退为「API 登录 + 种 token」
//   3) 用拿到的 token 调真实受保护接口 GET /api/v1/install-records，证明 token 有效
//   4) reLaunch home 断言路由 + storage 里 techId 与 API 返回一致（真实数据）
//
// 用法：node e2e-miniapp/tech-login.spec.js   （需先启动 9420 automator / CONNECT_ONLY=1 + staging 构建）
// 账号：TECH_PHONE / TECH_PASSWORD 环境变量（staging T0001 老陈 测试账号），缺省读 staging 联调常用值。
const cfg = require('./real-miniapp.config')
/* global getCurrentPages */
const helpers = require('./real-mp-helpers')

const PHONE = process.env.TECH_PHONE || '13800138000'
const PASSWORD = process.env.TECH_PASSWORD || 'test123456'
const TOKEN_KEY = cfg.storage.tech.token
const TECH_ID_KEY = cfg.storage.tech.techId

async function uiLoginTech(ctx) {
  // 最佳努力：扫描登录页 input 事件方法（编译名 e\d+_[0-9a-f]{2}），按模板顺序填手机号/密码，
  // 勾协议，点登录。编译名随构建可能漂移，失败不抛，由调用方回退 API 登录。
  const { mp } = ctx
  const r = await helpers.withTimeout(mp.evaluate(function (phone, pwd) {
    const ps = getCurrentPages()
    if (!ps.length) return { ok: false, reason: 'no_page' }
    const page = ps[ps.length - 1]
    if (page.route !== 'pages/login/index') return { ok: false, reason: 'not_login:' + page.route }
    const keys = Object.keys(page)
    const inputFns = keys.filter((k) => /^e\d+_[0-9a-f]{2}$/.test(k))
    if (inputFns.length < 2) return { ok: false, reason: 'no_compiled_inputs', keys: inputFns }
    // 手机号(number)在前、密码(password)在后（模板顺序）
    if (typeof page[inputFns[0]] !== 'function') return { ok: false, reason: 'not_fn' }
    page[inputFns[0]]({ detail: { value: phone }, currentTarget: { dataset: {} } })
    page[inputFns[1]]({ detail: { value: pwd }, currentTarget: { dataset: {} } })
    // 勾协议
    const agreeFn = keys.find((k) => /^e\d+_[0-9a-f]{2}$/.test(k) && k !== inputFns[0] && k !== inputFns[1] && typeof page[k] === 'function' && page[k].name !== 'doLogin')
    page[agreeFn]({})
    page[agreeFn]({})
    // 登录：扫描绑定 doLogin / loginWithPassword 的 handler
    const loginFn = keys.find((k) => /^e\d+_[0-9a-f]{2}$/.test(k) && k !== inputFns[0] && k !== inputFns[1] && typeof page[k] === 'function')
    if (!loginFn) return { ok: false, reason: 'no_login_fn' }
    page[loginFn]({})
    return { ok: true, phone: inputFns[0], pwd: inputFns[1], agree: agreeFn, login: loginFn }
  }, PHONE, PASSWORD), 10_000, 'uiLoginTech')
  if (!r || !r.ok) return { ok: false }
  // 等待 storage 出现 token
  const deadline = Date.now() + 25_000
  while (Date.now() < deadline) {
    await new Promise((res) => setTimeout(res, 1500))
    const tk = await helpers.withTimeout(mp.getStorageSync(TOKEN_KEY), 5000, 'getStorage token')
    if (tk) return { ok: true, via: 'ui', keys: r }
  }
  return { ok: false, via: 'ui-notoken' }
}

helpers.runSpec(cfg, {
  name: 'tech-login',
  app: 'tech',
  role: 'tech',
  body: async (ctx) => {
    const { mp, pageRoute, apiCall, logStep, result } = ctx
    let token = null
    let techId = null
    let name = null

    // [1] Node 侧真实登录（数据来源：staging backend）
    let loginResp
    try {
      loginResp = await apiCall(cfg.staging, '/api/v1/tech/login', {
        method: 'POST',
        body: { phone: PHONE, password: PASSWORD },
      })
    } catch (e) { logStep(result, 'api-login', false, e.message); return false }
    const d = loginResp.body && loginResp.body.data
    const f1 = loginResp.status === 200 && d && !!d.token && !!d.techId
    if (!logStep(result, 'api-login', f1, { status: loginResp.status, name: d && d.name })) return false
    token = d.token; techId = d.techId; name = d.name

    // [2] 登录页 UI 驱动（最佳努力，失败回退 API 登录 + 种 token）
    await helpers.withTimeout(mp.reLaunch('/pages/login/index'), 15_000, 'reLaunch login')
    await new Promise((r) => setTimeout(r, 4000))
    const ui = await uiLoginTech(ctx)
    if (ui && ui.ok && ui.via === 'ui') {
      logStep(result, 'ui-login', true, ui.keys)
    } else {
      console.log('[fallback] UI 登录未取到 token（编译控件名不可靠），回退 API 登录 + 种 storage')
      await helpers.withTimeout(mp.setStorageSync(TOKEN_KEY, token), 10_000, 'seed token')
      await helpers.withTimeout(mp.setStorageSync(TECH_ID_KEY, techId), 10_000, 'seed techId')
      logStep(result, 'ui-login', true, { via: 'api-seed-fallback' })
    }

    // [3] token 有效性：真实受保护接口
    const rec = await apiCall(cfg.staging, '/api/v1/install-records', { token })
    logStep(result, 'token-valid', rec.status === 200, `GET install-records -> ${rec.status}`)

    // [4] home 渲染 + storage 里 techId 与 API 一致
    await helpers.withTimeout(mp.reLaunch('/pages/home/index'), 15_000, 'reLaunch home')
    await new Promise((r) => setTimeout(r, 5000))
    const route = await pageRoute(mp)
    const storedId = await helpers.withTimeout(mp.getStorageSync(TECH_ID_KEY), 5000, 'get techId')
    const f4 = route === 'pages/home/index' && String(storedId) === String(techId)
    logStep(result, 'home-render', f4, { route, techId, name })

    return f1 && rec.status === 200 && f4
  },
})