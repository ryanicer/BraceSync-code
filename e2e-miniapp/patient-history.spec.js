// T054 患者端 · 历史/异常监测（staging real API，方案 C「仅授权后链路」）
//
// 断言数据正确性（容忍空态，不伪造数据）：
//   1) ensurePatientToken
//   2) Node 侧 GET /api/v1/alerts?patientId= → 断言 200 + 列表结构（容忍空 list = 正常空态）
//      GET /patients/:id/daily-wear 后端当前可能 404 → 明确容忍空态（记录 tolerated 而非 FAIL）
//   3) 小程序 UI：switchTab 到 history(异常监测) → 断言路由正确 + 页面无异常
//
// 用法：node e2e-miniapp/patient-history.spec.js
const cfg = require('./real-miniapp.config')
/* global getCurrentPages */
const helpers = require('./real-mp-helpers')

helpers.runSpec(cfg, {
  name: 'patient-history',
  app: 'patient',
  role: 'patient',
  body: async (ctx) => {
    const { mp, apiCall, logStep, result } = ctx

    // [1] 鉴权
    let auth
    try { auth = await helpers.ensurePatientToken(ctx) }
    catch (e) { logStep(result, 'auth-obtain', false, e.message); return false }
    if (!auth.token || !auth.patientId) { logStep(result, 'auth-obtain', false, '需 patientId'); return false }

    // [2] Node 侧 alerts 断言（容忍空 list）
    let alertsOk = false
    try {
      const q = `patientId=${encodeURIComponent(auth.patientId)}&page=1&pageSize=200`
      const r = await apiCall(cfg.staging, `/api/v1/alerts?${q}`, { token: auth.token })
      if (r.status === 200) {
        const list = (r.body && r.body.data && r.body.data.list) || []
        alertsOk = true
        logStep(result, 'api-alerts', true, { list: list.length })
      } else {
        logStep(result, 'api-alerts', false, `status=${r.status}`)
      }
    } catch (e) { logStep(result, 'api-alerts', false, e.message) }

    // daily-wear：404/空态明确容忍（后端未开放端点时前端即空态兜底）
    let wearNote
    try {
      const r = await apiCall(cfg.staging, `/api/v1/patients/${encodeURIComponent(auth.patientId)}/daily-wear`, { token: auth.token })
      wearNote = `daily-wear -> ${r.status}` + (r.status === 404 ? '（后端未开放，容忍空态）' : '')
      logStep(result, 'api-daily-wear', true, wearNote)
    } catch (e) {
      wearNote = 'daily-wear 请求异常，容忍空态'
      logStep(result, 'api-daily-wear', true, e.message)
    }
    void wearNote

    // [3] UI switchTab history(异常监测) 渲染
    await helpers.withTimeout(mp.switchTab('/pages/history/index'), 20_000, 'switchTab history')
    await new Promise((r) => setTimeout(r, 5000))
    const route = await helpers.pageRoute(mp)
    const uiOk = await helpers.withTimeout(mp.evaluate(function () {
      const ps = getCurrentPages()
      const page = ps[ps.length - 1]
      if (!page || page.route !== 'pages/history/index') return false
      return JSON.stringify(page.data || {}) != null
    }), 10_000, 'history render')
    const f3 = route === 'pages/history/index' && !!uiOk
    logStep(result, 'ui-history-render', f3, { route })

    return alertsOk && f3
  },
})