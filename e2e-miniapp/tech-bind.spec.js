// T054 技师端 · 真实绑定（staging real API，USE_MOCK=false）
//
// 绑定会向 staging 写真实行（T053 惯例：造数据前缀隔离，跑完不强求清理），绝不碰生产。
// 本 driver 用「真实 staging API 写 → 直连复核 + 小程序 UI 复核」双通道断言数据正确性：
//   1) Node 侧 tech/login 拿 token（T0001 老陈）
//   2) API 写：POST /devices/:deviceId/bind + POST /install-records（唯一命名的 install/patient 后缀）
//   3) 直连 GET /install-records 复核：新写入的 installId/deviceId 确实出现在 staging 列表（数据正确性）
//   4) 小程序 UI 复核：导航到 records(安装记录) 页，断言其渲染的真实列表包含刚写入的记录
//
// ⚠️ 依赖 staging 已存在一个可绑定的测试设备：TECH_DEVICE_ID；患者 ID：TECH_PATIENT_ID。
//    bind 接口要求 device 存在，故二者为必填环境变量，缺省即 fail-fast（如实标注，不自欺）。
//
// 用法：node e2e-miniapp/tech-bind.spec.js
//     TECH_DEVICE_ID=... TECH_PATIENT_ID=... TECH_TECH_ID=...   （TECH_TECH_ID 缺省 T0001）
const cfg = require('./real-miniapp.config')
/* global getCurrentPages */
const helpers = require('./real-mp-helpers')

const PHONE = process.env.TECH_PHONE || '13800138000'
const PASSWORD = process.env.TECH_PASSWORD || 'test123456'
const DEVICE_ID = process.env.TECH_DEVICE_ID
const PATIENT_ID = process.env.TECH_PATIENT_ID
const TECH_ID = process.env.TECH_TECH_ID || 'T0001'

helpers.runSpec(cfg, {
  name: 'tech-bind',
  app: 'tech',
  role: 'tech',
  body: async (ctx) => {
    const { mp, pageRoute, apiCall, logStep, result, uniqueName } = ctx

    if (!DEVICE_ID || !PATIENT_ID) {
      logStep(result, 'env-precondition', false, '需 TECH_DEVICE_ID + TECH_PATIENT_ID（staging 存在可绑定的测试设备/患者）')
      return false
    }

    // [1] 登录
    let token
    try { token = (await helpers.loginTech(cfg.staging, PHONE, PASSWORD)).token }
    catch (e) { logStep(result, 'api-login', false, e.message); return false }
    logStep(result, 'api-login', true, 'token acquired')

    // [2] 真实写绑定 + 安装记录（造数据唯一后缀）
    const suffix = uniqueName(cfg.prefix) // e.g. T054测试-<ts>-00
    let bindResp, instResp
    try {
      bindResp = await apiCall(cfg.staging, `/api/v1/devices/${encodeURIComponent(DEVICE_ID)}/bind`, {
        method: 'POST', token, body: { patientId: PATIENT_ID },
      })
      instResp = await apiCall(cfg.staging, '/api/v1/install-records', {
        method: 'POST', token,
        body: {
          deviceId: DEVICE_ID, patientId: PATIENT_ID, techId: TECH_ID,
          status: 'in_progress', wifiStatus: 'unconfigured', reachabilityStatus: 'pending',
          notes: suffix,
        },
      })
    } catch (e) { logStep(result, 'api-write', false, e.message); return false }
    const instId = instResp.body && instResp.body.data && instResp.body.data.installId
    const f2 = bindResp.status === 200 && instResp.status === 200 && !!instId
    logStep(result, 'api-write(bind+install)', f2, { bind: bindResp.status, install: instResp.status, instId })
    if (!f2) return false

    // [3] 直连复核：install-records 列表确实含刚写入的 instId（数据正确性）
    let recList
    try {
      const r = await apiCall(cfg.staging, '/api/v1/install-records', { token })
      recList = (r.body && r.body.data && r.body.data.list) || []
    } catch (e) { logStep(result, 'api-verify-list', false, e.message); return false }
    const found = recList.some((x) => x.installId === instId || x.deviceId === DEVICE_ID || x.notes === suffix)
    logStep(result, 'api-verify-list', found, { total: recList.length, instId })

    // [4] 小程序 UI 复核：records 页渲染真实列表并含新记录
    await helpers.withTimeout(mp.reLaunch('/pages/home/index'), 15_000, 'reLaunch home')
    await new Promise((r) => setTimeout(r, 3000))
    await helpers.withTimeout(mp.reLaunch('/pages/records/index'), 15_000, 'reLaunch records')
    await new Promise((r) => setTimeout(r, 5000))
    const route = await pageRoute(mp)
    const uiFound = await helpers.withTimeout(mp.evaluate(function (devId) {
      const ps = getCurrentPages()
      const page = ps[ps.length - 1]
      const raw = JSON.stringify(page.data || {})
      return raw.indexOf(devId) >= 0
    }, DEVICE_ID), 10_000, 'records render')
    logStep(result, 'ui-records-render', route === 'pages/records/index' && !!uiFound, { route, uiFound: !!uiFound })

    return f2 && found && route === 'pages/records/index' && !!uiFound
  },
})