// T054 技师端 · 真实安装记录/历史（staging real API，USE_MOCK=false）
//
// 断言数据正确性（非仅路由跳转）：
//   1) Node 侧 tech/login 拿 token → 直连 GET /install-records，断言返回真实非空列表（结构正确）
//   2) 小程序 UI：驱动安装记录页渲染，断言其 page.data 里确实落有后端返回的真实 installId（非空）
//
// 独立可跑（不依赖 tech-bind 先行写数；但 staging 需已存在安装记录）。对「空列表」——
// 若 staging 确实无任何安装记录，则如实 FAIL（不造假数据、不自欺空态当通过）。
//
// 用法：node e2e-miniapp/tech-records.spec.js
const cfg = require('./real-miniapp.config')
/* global getCurrentPages */
const helpers = require('./real-mp-helpers')

const PHONE = process.env.TECH_PHONE || '13800138000'
const PASSWORD = process.env.TECH_PASSWORD || 'test123456'

helpers.runSpec(cfg, {
  name: 'tech-records',
  app: 'tech',
  role: 'tech',
  body: async (ctx) => {
    const { mp, pageRoute, apiCall, logStep, result } = ctx

    // [1] 登录 + 直连列表
    let token
    try { token = (await helpers.loginTech(cfg.staging, PHONE, PASSWORD)).token }
    catch (e) { logStep(result, 'api-login', false, e.message); return false }
    let recList, total
    try {
      const r = await apiCall(cfg.staging, '/api/v1/install-records', { token })
      recList = (r.body && r.body.data && r.body.data.list) || []
      total = (r.body && r.body.data && r.body.data.total) != null ? r.body.data.total : recList.length
    } catch (e) { logStep(result, 'api-list', false, e.message); return false }
    const f1 = Array.isArray(recList) && recList.length > 0
    logStep(result, 'api-list', f1, { total, count: recList.length })
    if (!f1) return false

    // 取一条真实 installId 作为 UI 断言锚点
    const anchor = recList[0].installId

    // [2] UI 渲染真实数据
    await helpers.withTimeout(mp.reLaunch('/pages/home/index'), 15_000, 'reLaunch home')
    await new Promise((r) => setTimeout(r, 3000))
    await helpers.withTimeout(mp.reLaunch('/pages/records/index'), 15_000, 'reLaunch records')
    await new Promise((r) => setTimeout(r, 5000))
    const route = await pageRoute(mp)
    const uiAnchor = await helpers.withTimeout(mp.evaluate(function (instId) {
      const ps = getCurrentPages()
      const page = ps[ps.length - 1]
      const raw = JSON.stringify(page.data || {})
      return raw.indexOf(instId) >= 0
    }, anchor), 10_000, 'records render anchor')
    const f2 = route === 'pages/records/index' && !!uiAnchor
    logStep(result, 'ui-records-render', f2, { route, anchor, uiAnchor: !!uiAnchor })

    return f1 && f2
  },
})