/**
 * T094a KNOWN_RED — 登录绑定状态机单测（PRD §7A.1.1）。
 *
 * 预期红态：stub（src/utils/auth-state.ts）固定返回错误占位值，
 * 以下断言在 Iris 实现真实状态映射前全部 FAIL。
 *
 * 覆盖分支：
 *   wx-login:  SUCCESS(0) / NEED_BIND(10601) / FAIL(10001,401,10401) / ERROR(10502,502)
 *   bind-phone: BOUND(0) / NO_MATCH(10602) / CONFLICT(10603)
 *               / RETRY(10604 与未识别码，不外跳) / REBIND(10605，回绑定引导页)
 */
import { describe, it, expect } from 'vitest'
import {
  resolveWxLoginResult,
  resolveBindPhoneResult,
  type WxLoginState,
  type BindPhoneState,
} from '../../src/utils/auth-state'

describe('登录绑定状态机 — wx-login（PRD §7A.1.1）', () => {
  it('code=0 → SUCCESS，跳转首页 monitor', () => {
    const r = resolveWxLoginResult(0)
    expect(r.state as WxLoginState).toBe('SUCCESS')
    expect(r.targetPage).toBe('/pages/monitor/index')
  })

  it('code=10601 → NEED_BIND，跳转绑定页', () => {
    const r = resolveWxLoginResult(10601)
    expect(r.state as WxLoginState).toBe('NEED_BIND')
    expect(r.targetPage).toBe('/pages/login/bind')
  })

  it('code=10001 → FAIL（凭证错误），不跳转仅提示', () => {
    const r = resolveWxLoginResult(10001)
    expect(r.state as WxLoginState).toBe('FAIL')
    expect(r.targetPage).toBe('')
    expect(r.message).toBeTruthy()
  })

  it('code=401 → FAIL（凭证错误），不跳转仅提示', () => {
    const r = resolveWxLoginResult(401)
    expect(r.state as WxLoginState).toBe('FAIL')
    expect(r.targetPage).toBe('')
  })

  it('code=10502 → ERROR（服务异常），不跳转仅提示', () => {
    const r = resolveWxLoginResult(10502)
    expect(r.state as WxLoginState).toBe('ERROR')
    expect(r.targetPage).toBe('')
    expect(r.message).toBeTruthy()
  })

  // T434：后端 wxLogin 在 jscode2session 返回业务错误时回 HTTP 401 + code 10401
  // （services/user-service/internal/handler/handler.go:661，model.CodeUnauthorized）。
  // 修复前它落 default，提示「服务异常，请稍后重试」，把患者侧可重试的授权码失效
  // 说成后端故障，违反 PRD §7A.1.1「网络失败与凭证错误必须分别提示，禁止一锅烩」。
  it('code=10401 → FAIL（凭据/授权码类），不跳转仅提示', () => {
    const r = resolveWxLoginResult(10401)
    expect(r.state as WxLoginState).toBe('FAIL')
    expect(r.targetPage).toBe('')
    expect(r.message).toBe('授权信息已失效，请重新登录')
  })

  it('code=10401 的提示不得是 default 的服务异常兜底句（T434 判据）', () => {
    const r = resolveWxLoginResult(10401)
    expect(r.message).not.toBe('服务异常，请稍后重试')
    // 与真正的未知码（走 default）必须不同，否则等于没识别
    const unknown = resolveWxLoginResult(10999)
    expect(r.message).not.toBe(unknown.message)
    expect(r.state).not.toBe(unknown.state)
  })

  it('10401（授权码失效）与 10502（微信服务不可用）不得同一处置', () => {
    expect(resolveWxLoginResult(10401).state).not.toBe(resolveWxLoginResult(10502).state)
  })

  it('code=502 → ERROR（服务异常），不跳转仅提示', () => {
    const r = resolveWxLoginResult(502)
    expect(r.state as WxLoginState).toBe('ERROR')
    expect(r.targetPage).toBe('')
  })

  it('FAIL(10001) 与 ERROR(10502) 状态不同，区分凭证错误与服务异常', () => {
    const fail = resolveWxLoginResult(10001)
    const error = resolveWxLoginResult(10502)
    expect(fail.state).not.toBe(error.state)
  })
})

describe('登录绑定状态机 — bind-phone（PRD §7A.1.1）', () => {
  it('code=0 → BOUND，绑定成功跳转首页 monitor', () => {
    const r = resolveBindPhoneResult(0)
    expect(r.state as BindPhoneState).toBe('BOUND')
    expect(r.targetPage).toBe('/pages/monitor/index')
  })

  it('code=10602 → NO_MATCH（未匹配档案），跳转 no-match 页', () => {
    const r = resolveBindPhoneResult(10602)
    expect(r.state as BindPhoneState).toBe('NO_MATCH')
    expect(r.targetPage).toBe('/pages/login/no-match')
  })

  it('code=10603 → CONFLICT（档案已被绑定），跳转 conflict 页', () => {
    const r = resolveBindPhoneResult(10603)
    expect(r.state as BindPhoneState).toBe('CONFLICT')
    expect(r.targetPage).toBe('/pages/login/conflict')
  })

  it('code=10604 → RETRY：提示重新授权手机号且不外跳（PRD §7A.1.1 补充状态码）', () => {
    const r = resolveBindPhoneResult(10604)
    expect(r.state as BindPhoneState).toBe('RETRY')
    expect(r.targetPage).toBe('')
    expect(r.message).toBe('授权信息已失效，请重新授权手机号')
  })

  it('code=10605 → REBIND：提示后回绑定引导页（PRD §7A.1.1 补充状态码）', () => {
    const r = resolveBindPhoneResult(10605)
    expect(r.state as BindPhoneState).toBe('REBIND')
    expect(r.targetPage).toBe('/pages/login/bind')
    expect(r.message).toBe('操作已过期，请重新绑定')
  })

  it('T438 核心：未识别业务码不得判成会跳转的业务态', () => {
    // 候选码取自 services/user-service/internal/model/model.go 的 bind-phone 可达码
    const unknown = [10400, 10401, 10403, 10502, 40301, 90001, 50000, -1]
    for (const code of unknown) {
      const r = resolveBindPhoneResult(code)
      expect(r.state, `code=${code} 不得落 NO_MATCH`).not.toBe('NO_MATCH')
      expect(r.state, `code=${code} 不得落 CONFLICT`).not.toBe('CONFLICT')
      expect(r.state, `code=${code} 不得落 BOUND`).not.toBe('BOUND')
      expect(r.targetPage, `code=${code} 不应有跳转落点`).toBe('')
      expect(r.message, `code=${code} 必须有患者可见文案`).toBeTruthy()
    }
  })

  it('防回潮：NO_MATCH 只由 10602 产出、REBIND 只由 10605 产出', () => {
    const codes = [0, 10400, 10401, 10403, 10502, 10601, 10602, 10603, 10604, 10605, 40301, 90001]
    const statesOf = (want: BindPhoneState) =>
      codes.filter((c) => resolveBindPhoneResult(c).state === want)
    expect(statesOf('NO_MATCH')).toEqual([10602])
    expect(statesOf('CONFLICT')).toEqual([10603])
    expect(statesOf('BOUND')).toEqual([0])
    expect(statesOf('REBIND')).toEqual([10605])
  })
})
