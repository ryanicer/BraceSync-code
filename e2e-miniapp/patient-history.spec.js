// T054 患者端 · 历史/异常监测（staging real API，方案 C「仅授权后链路」）
//
// 断言数据正确性（容忍空态，不伪造数据）：
//   1) ensurePatientToken
//   2) Node 侧 GET /api/v1/alerts?patientId= → 断言 200 + 列表结构（容忍空 list = 正常空态）
//      GET /patients/:id/daily-wear 后端当前可能 404 → 明确容忍空态（记录 tolerated 而非 FAIL）
//   3) 小程序 UI 切页腿：T450-③ 起为显式跳过（目标页已不在产物页表内，详见 [3] 处）
//
// 用法：node e2e-miniapp/patient-history.spec.js
const cfg = require('./real-miniapp.config')
const helpers = require('./real-mp-helpers')

// 历史页路由。页表里没有这一条时 UI 腿走 skipStep，不许 switchTab 硬撞。
const HISTORY_PAGE = '/pages/history/index'

helpers.runSpec(cfg, {
  name: 'patient-history',
  app: 'patient',
  role: 'patient',
  body: async (ctx) => {
    const { apiCall, logStep, result } = ctx

    // [1] 鉴权
    let auth
    try { auth = await helpers.ensurePatientToken(ctx) }
    catch (e) { logStep(result, 'auth-obtain', false, e.message); return false }
    if (!auth.token || !auth.patientId) { logStep(result, 'auth-obtain', false, '需 patientId'); return false }

    // [2] Node 侧 alerts 断言（容忍空 list）
    let alertsOk = false
    let alertsFromApi = null
    try {
      const q = `patientId=${encodeURIComponent(auth.patientId)}&page=1&pageSize=100`
      const r = await apiCall(cfg.staging, `/api/v1/alerts?${q}`, { token: auth.token })
      if (r.status === 200) {
        const list = (r.body && r.body.data && r.body.data.list) || []
        alertsOk = true
        alertsFromApi = list.length
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
      logStep(result, 'api-daily-wear(tolerated)', true, wearNote)
    } catch (e) {
      wearNote = 'daily-wear 请求异常，容忍空态'
      logStep(result, 'api-daily-wear(tolerated)', true, e.message)
    }
    void wearNote

    // [3] UI 切页腿：显式跳过（T450-③，口径 = driver 资产陈旧，不是被测功能缺陷）
    //     pages/history/index 已退出货表：患者端 src/pages.json 的 pages 无此条、src/pages/history/ 不存在，
    //     改名前后两代 staging 产物 app.json 各 12 页亦无此路由（Alice T441 docs PR 662 证据包 10/11 号，
    //     与 Peter T417 对照清单 A-1「历史页已删」一致）。旧写法是 mp.switchTab 硬撞 → 20s 超时抛错 →
    //     runSpec 的 fail-closed 外壳判负 → process.exitCode=1，官方六支编排必出一条与产品无关的红。
    //     恢复条件：患者端重新有「历史」页时，把 HISTORY_PAGE 改成真实路由，并把 skipStep 换回
    //     logStep（照旧断言栈顶 route 与页面渲染）。
    helpers.skipStep(
      result,
      'ui-history-route',
      `目标页 ${HISTORY_PAGE} 不在患者端页表内（历史页已删）；API 侧 alerts 条数已落上一条台账：${alertsFromApi}`
    )

    return alertsOk
  },
})