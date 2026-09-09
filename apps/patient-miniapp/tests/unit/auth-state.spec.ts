/**
 * T094a KNOWN_RED — 登录绑定状态机单测（PRD §7A.1.1）。
 *
 * 预期红态：stub（src/utils/auth-state.ts）固定返回错误占位值，
 * 以下断言在 Iris 实现真实状态映射前全部 FAIL。
 *
 * 覆盖 7 个分支：
 *   wx-login:  SUCCESS(0) / NEED_BIND(10601) / FAIL(10001,401) / ERROR(10502,502)
 *   bind-phone: BOUND(0) / NO_MATCH(10602) / CONFLICT(10603)
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
})
