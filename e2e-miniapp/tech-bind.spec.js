// T054 技师端 · 真实绑定（staging real API，USE_MOCK=false）
//
// 绑定会向 staging 写真实行（T053 惯例：造数据前缀隔离，跑完不强求清理），绝不碰生产。
// 本 driver 用「真实 staging API 写 → 直连复核 + 小程序 UI 复核」双通道断言数据正确性：
//   1) Node 侧 tech/login 拿 token（T0001 老陈）
//   2) API 写：POST /devices/:deviceId/bind（T299 规则见下）+ POST /install-records（唯一命名的 install/patient 后缀）
//   3) 直连 GET /install-records 复核：新写入的 installId/deviceId 确实出现在 staging 列表（数据正确性）
//   4) 小程序 UI 复核：导航到 records(安装记录) 页，断言其渲染的真实列表包含刚写入的记录
//
// T299「一患者一设备」下的绑定步骤（本文件第 3 步）：
//   先按无意图发 bind；若 409 则必须从 data.occupiedDeviceId 读到占位设备，再带 confirmSwap=true 重试，
//   并重查响应 patientSwappedFrom 与被解除设备的 patientId 已空。跑的是 staging 真实后端，
//   所以这一支同时钉住「技师端确认换绑」在真实模式下的行为。
//   ⚠️ 前提：staging 上的 device-service 必须已部署 T299 之后的构建。旧构建（静默改绑）永不返回 409，
//     此时本支仍会 PASS，但 path 记为 no-conflict，换绑分支等于没被触发——判定实跑有效性请看这个字段。
//
// ⚠️ 依赖 staging 已存在一个可绑定的测试设备：TECH_DEVICE_ID；患者 ID：TECH_PATIENT_ID。
//    bind 接口要求 device 存在，故二者为必填环境变量，缺省即 fail-fast（如实标注，不自欺）。
//    技师账号 TECH_PHONE / TECH_PASSWORD 亦为必填（runbook 口径），driver 里的默认值是 mock 时代遗留，
//    staging 上不成立（实测回 401 / code 10401）。
//
// 用法：node e2e-miniapp/tech-bind.spec.js
//     TECH_DEVICE_ID=... TECH_PATIENT_ID=... TECH_TECH_ID=...   （TECH_TECH_ID 缺省 T0001）
//     TECH_CONFLICT_DEVICE_ID=...（可选）先带意图把这台设备绑给同一患者，造出确定的占用状态，
//       用来逼出 409 换绑分支；不给则该分支只在「患者本来就已占用别的设备」时才走到。
//       预置那台在换绑里会被重新释放，跑完回到解绑态（起点若是解绑态）。
//     需先起微信开发者工具自动化端口（9420），外壳在连接端口之前不会执行任何业务断言。
const cfg = require('./real-miniapp.config')
/* global getCurrentPages */
const helpers = require('./real-mp-helpers')

const PHONE = process.env.TECH_PHONE || '13800138000'
const PASSWORD = process.env.TECH_PASSWORD || 'test123456'
const DEVICE_ID = process.env.TECH_DEVICE_ID
const PATIENT_ID = process.env.TECH_PATIENT_ID
const TECH_ID = process.env.TECH_TECH_ID || 'T0001'
// 可选：先把这台设备绑给同一患者，制造确定性的「患者已占用设备」状态，逼出 409 换绑分支。
// 不给该变量时行为不变（患者空闲则走幂等/直通分支）。跑完那台设备会回到解绑态，与预置前一致。
const CONFLICT_DEVICE_ID = process.env.TECH_CONFLICT_DEVICE_ID

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

    // [2] 绑定前快照：install-records 列表当前长度
    let preCount = -1
    try {
      const pre = await apiCall(cfg.staging, '/api/v1/install-records', { token })
      preCount = ((pre.body && pre.body.data && pre.body.data.list) || []).length
    } catch (e) { logStep(result, 'api-snapshot-pre', false, e.message); return false }
    logStep(result, 'api-snapshot-pre', preCount >= 0, { preCount })

    // [3] 真实写绑定 + 安装记录（造数据唯一后缀）
    //     T299：一个患者同一时刻只有一台生效设备。先按「无换绑意图」发一次 bind：
    //       · 200 → 该患者当时空闲（或同设备幂等），换绑分支本轮未被触发，如实记 path=no-conflict；
    //       · 409 → 该患者名下已有生效设备，必须从 data.occupiedDeviceId 读到那台设备号（读不到即 FAIL，
    //               不重试、不猜），再带 confirmSwap=true 重试，并回查那台设备确已被解除归属。
    const suffix = uniqueName(cfg.prefix) // e.g. T054测试-<ts>-00
    const bindPath = `/api/v1/devices/${encodeURIComponent(DEVICE_ID)}/bind`

    // [3a] 可选预置占用：把另一台设备先绑给同一患者，下面的无意图 bind 就必须撞 409。
    //      带 confirmSwap 预置，是因为患者此刻可能正占着本次要绑的这台（staging 现状即如此），
    //      无意图预置会被本卡自己的规则拒掉；预置换掉的设备在下面的换绑里会再被释放，跑完回到起点。
    //      不给 TECH_CONFLICT_DEVICE_ID 时本步整体跳过，行为与改动前一致。
    let expectConflict = false
    if (CONFLICT_DEVICE_ID && CONFLICT_DEVICE_ID !== DEVICE_ID) {
      let pre
      try {
        pre = await apiCall(cfg.staging, `/api/v1/devices/${encodeURIComponent(CONFLICT_DEVICE_ID)}/bind`, {
          method: 'POST', token, body: { patientId: PATIENT_ID, confirmSwap: true },
        })
      } catch (e) { logStep(result, 'api-preseed-conflict-device', false, e.message); return false }
      expectConflict = pre.status === 200
      logStep(result, 'api-preseed-conflict-device', pre.status === 200, {
        deviceId: CONFLICT_DEVICE_ID, httpStatus: pre.status, code: pre.body && pre.body.code,
      })
      if (pre.status !== 200) return false
    }

    let bindResp, instResp, swapFrom = null
    try {
      bindResp = await apiCall(cfg.staging, bindPath, {
        method: 'POST', token, body: { patientId: PATIENT_ID },
      })
      if (expectConflict && bindResp.status !== 409) {
        // 已预置占用却拿不到 409：staging 上跑的还是 T299 之前的构建，患者占用被静默改绑吞掉了。
        // 这正是要抓的情形，判 FAIL；被静默解绑的那台需人工重新 bind 回去。
        logStep(result, 'api-bind-expects-409', false, {
          httpStatus: bindResp.status, preseededDevice: CONFLICT_DEVICE_ID,
        })
        return false
      }
      if (bindResp.status === 409) {
        const bd = bindResp.body && bindResp.body.data
        swapFrom = bd && bd.occupiedDeviceId
        const conflictOk = !!swapFrom && bindResp.body.code === 20409 && swapFrom !== DEVICE_ID
        logStep(result, 'api-bind-409-occupied-device', conflictOk, {
          code: bindResp.body && bindResp.body.code, occupiedDeviceId: swapFrom || null,
        })
        if (!conflictOk) { swapFrom = null; return false } // 409 却给不出占位设备 = 契约破坏，不许蒙过去重试
        bindResp = await apiCall(cfg.staging, bindPath, {
          method: 'POST', token, body: { patientId: PATIENT_ID, confirmSwap: true },
        })
      }
    } catch (e) { logStep(result, 'api-write', false, e.message); return false }

    const bindData = bindResp.body && bindResp.body.data
    const bindOk = bindResp.status === 200 && !!bindData && bindData.deviceId === DEVICE_ID
    logStep(result, 'api-bind-path', bindOk, {
      path: swapFrom ? 'conflict-then-confirmSwap' : 'no-conflict',
      httpStatus: bindResp.status, swapped: bindData && bindData.swapped, patientSwappedFrom: bindData && bindData.patientSwappedFrom,
    })
    if (!bindOk) { logStep(result, 'api-write(bind)', false, { bind: bindResp.status, raw: bindResp.raw }); return false }

    // 患者级确认换绑的结果必须点名被解除的那台设备；没换绑则不得凭空冒出一个设备号
    const swapFieldOk = swapFrom
      ? bindData.swapped === true && bindData.patientSwappedFrom === swapFrom
      : !bindData.patientSwappedFrom
    logStep(result, 'api-bind-confirm-swap-fields', swapFieldOk, {
      expectedSwapFrom: swapFrom, swapped: bindData.swapped, patientSwappedFrom: bindData.patientSwappedFrom,
    })
    if (!swapFieldOk) return false

    // 数据面复核（不止看返回字段）：被解除的设备在 devices 里确实不再归属该患者
    if (swapFrom) {
      let prev
      try { prev = await apiCall(cfg.staging, `/api/v1/devices/${encodeURIComponent(swapFrom)}`, { token }) }
      catch (e) { logStep(result, 'api-prev-device-released', false, e.message); return false }
      const prevData = prev.body && prev.body.data
      const released = prev.status === 200 && !!prevData && !prevData.patientId
      logStep(result, 'api-prev-device-released', released, {
        deviceId: swapFrom, httpStatus: prev.status, patientId: prevData && prevData.patientId,
      })
      if (!released) return false
    }

    try {
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
    const f2 = instResp.status === 200 && !!instId
    logStep(result, 'api-write(install)', f2, { install: instResp.status, instId })
    if (!f2) return false

    // [4] 绑定后快照：列表应 +1 且含 notes=suffix 的新行
    let recList
    try {
      const r = await apiCall(cfg.staging, '/api/v1/install-records', { token })
      recList = (r.body && r.body.data && r.body.data.list) || []
    } catch (e) { logStep(result, 'api-snapshot-post', false, e.message); return false }
    const postCount = recList.length
    const newRow = recList.find((x) => x.notes === suffix || x.installId === instId)
    const snapOk = newRow && postCount === preCount + 1
    logStep(result, 'api-snapshot-post', snapOk, { preCount, postCount, delta: postCount - preCount, newRowFound: !!newRow, instId, suffix })

    // [5] 小程序 UI 复核：records 页渲染真实列表并含新记录
    await helpers.withTimeout(mp.reLaunch('/pages/home/index'), 15_000, 'reLaunch home')
    await new Promise((r) => setTimeout(r, 3000))
    await helpers.withTimeout(mp.reLaunch('/pages/records/index'), 15_000, 'reLaunch records')
    await new Promise((r) => setTimeout(r, 5000))
    const route = await pageRoute(mp)
    const uiFound = await helpers.withTimeout(mp.evaluate(function (uniq) {
      const ps = getCurrentPages()
      const page = ps[ps.length - 1]
      const raw = JSON.stringify(page.data || {})
      return raw.indexOf(uniq) >= 0
    }, suffix), 10_000, 'records render')
    logStep(result, 'ui-records-render', route === 'pages/records/index' && !!uiFound, { route, uiFound: !!uiFound, suffix })

    return f2 && snapOk && route === 'pages/records/index' && !!uiFound
  },
})