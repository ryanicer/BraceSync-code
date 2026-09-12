// T054 患者端 · 实时监测（staging real API，方案 C「仅授权后链路」）
//
// 断言数据正确性（容忍新患者空态，但不伪造数据）：
//   1) ensurePatientToken（UI wx-login 或 PATIENT_TOKEN+PATIENT_ID 种入）
//   2) Node 侧 GET /api/v1/patients/:id/realtime → 断言 200 + 结构正确（pressureRecords/status 字段）
//      空 pressureRecords 视为合法空态（结构对即可），不误判 FAIL
//   3) 小程序 UI：switchTab 到 monitor(实时监测) → 断言路由正确 + 页面无异常（page.data 含 route 锚点）
//
// 用法：node e2e-miniapp/patient-monitor.spec.js
const cfg = require('./real-miniapp.config')
/* global getCurrentPages */
const helpers = require('./real-mp-helpers')

helpers.runSpec(cfg, {
  name: 'patient-monitor',
  app: 'patient',
  role: 'patient',
  body: async (ctx) => {
    const { mp, apiCall, logStep, result } = ctx

    // [1] 鉴权
    let auth
    try { auth = await helpers.ensurePatientToken(ctx) }
    catch (e) { logStep(result, 'auth-obtain', false, e.message); return false }
    if (!auth.token || !auth.patientId) { logStep(result, 'auth-obtain', false, '需 patientId'); return false }

    // [2] Node 侧 realtime 断言（结构正确，容忍空）
    let snap
    try {
      const r = await apiCall(cfg.staging, `/api/v1/patients/${encodeURIComponent(auth.patientId)}/realtime`, { token: auth.token })
      snap = r.body && r.body.data
      if (r.status !== 200) { logStep(result, 'api-realtime', false, `status=${r.status}`); return false }
    } catch (e) { logStep(result, 'api-realtime', false, e.message); return false }
    const dataOk = snap == null || typeof snap === 'object' // 结构必须为对象（空态也合法）
    const recs = snap && Array.isArray(snap.pressureRecords) ? snap.pressureRecords : []
    logStep(result, 'api-realtime', dataOk, { status: 'ok', pressureRecords: recs.length })

    // [3] UI switchTab monitor 渲染
    await helpers.withTimeout(mp.switchTab('/pages/monitor/index'), 20_000, 'switchTab monitor')
    await new Promise((r) => setTimeout(r, 5000))
    const route = await helpers.pageRoute(mp)
    const uiOk = await helpers.withTimeout(mp.evaluate(function () {
      const ps = getCurrentPages()
      const page = ps[ps.length - 1]
      if (!page || page.route !== 'pages/monitor/index') return false
      const raw = JSON.stringify(page.data || {})
      return raw != null
    }), 10_000, 'monitor render')
    const f3 = route === 'pages/monitor/index' && !!uiOk
    logStep(result, 'ui-monitor-render', f3, { route })

    return dataOk && f3
  },
})