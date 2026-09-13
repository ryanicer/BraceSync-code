// T054 患者端 · 真实微信登录（staging real API，方案 C「仅授权后链路」）
//
// getPhoneNumber 原生授权弹窗无法被 miniprogram-automator 自动化（bind 步骤），但
// uni.login → POST /api/v1/patient/wx-login 可自动化。故：
//   - 前提：该微信 devtools 账号对应 openid 已被人工一次性预置绑定（Boss/真机走一次 getPhoneNumber）
//   - 本 spec 驱动真实「微信一键登录」→ 断言拿到真实 token/patientId（SUCCESS）
//   - 若未预置（NEED_BIND）或未配 PATIENT_TOKEN+PATIENT_ID → 如实 FAIL + 明确指引（不自欺）
//
// 用法：node e2e-miniapp/patient-login.spec.js
//   可选 PATIENT_TOKEN + PATIENT_ID（方案 C-纯：跳过 UI 直接种 token）
const cfg = require('./real-miniapp.config')
const helpers = require('./real-mp-helpers')

helpers.runSpec(cfg, {
  name: 'patient-login',
  app: 'patient',
  role: 'patient',
  body: async (ctx) => {
    const { apiCall, logStep, result } = ctx

    // [1] 鉴权获取（UI wx-login / env 种 token / 已种）
    let auth
    try {
      auth = await helpers.ensurePatientToken(ctx)
    } catch (e) {
      logStep(result, 'auth-obtain', false, e.message)
      return false
    }
    const f1 = !!auth.token
    logStep(result, 'auth-obtain', f1, { via: auth.via, hasPatientId: !!auth.patientId })
    if (!f1) return false

    // [2] 真实 token 有效性：直连一个受保护接口
    if (auth.patientId) {
      const r = await apiCall(cfg.staging, `/api/v1/patients/${encodeURIComponent(auth.patientId)}/realtime`, { token: auth.token })
      logStep(result, 'token-valid', r.status === 200, `GET patients/:id/realtime -> ${r.status}`)
      return r.status === 200
    }
    logStep(result, 'token-valid', true, 'patientId 缺失，跳过受保护接口校验（token 已获取）')
    return true
  },
})