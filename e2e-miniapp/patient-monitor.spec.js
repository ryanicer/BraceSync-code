// T054 患者端 · 实时监测（staging real API，方案 C「仅授权后链路」）
//
// 断言数据正确性（容忍新患者空态，但不伪造数据）：
//   1) ensurePatientToken（UI wx-login 或 PATIENT_TOKEN+PATIENT_ID 种入）
//   2) Node 侧 GET /api/v1/patients/:id/realtime → 断言 200 + data 必须为对象
//      空 pressureRecords 视为合法空态（不误判 FAIL），data 缺失/为 null 则 FAIL
//   3) 小程序 UI：switchTab 到 monitor(实时监测) → 断言路由正确（页面 data 字段级断言需 devtools
//      侧先核对编译后 data 键名，暂不伪造；已落 API 返回条数供人工比对）
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
    const dataOk = snap != null && typeof snap === 'object' // data 必须为对象；空 pressureRecords 才是合法空态
    const recs = snap && Array.isArray(snap.pressureRecords) ? snap.pressureRecords : []
    logStep(result, 'api-realtime', dataOk, { dataType: snap === null ? 'null' : typeof snap, pressureRecords: recs.length })

    // [3] UI switchTab monitor 渲染
    await helpers.withTimeout(mp.switchTab('/pages/monitor/index'), 20_000, 'switchTab monitor')
    await new Promise((r) => setTimeout(r, 5000))
    const route = await helpers.pageRoute(mp)
    const uiRouteOk = await helpers.withTimeout(mp.evaluate(function () {
      const ps = getCurrentPages()
      const page = ps[ps.length - 1]
      return !!page && page.route === 'pages/monitor/index'
    }), 10_000, 'monitor route')
    const f3 = route === 'pages/monitor/index' && !!uiRouteOk
    logStep(result, 'ui-monitor-route', f3, { route, pressureRecordsFromApi: recs.length })

    return dataOk && f3
  },
})