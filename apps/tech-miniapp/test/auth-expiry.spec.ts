import { describe, it, expect, vi } from 'vitest'
import { AUTH_EXPIRED_MESSAGE, LOGIN_PAGE, forceRelogin, isAuthFailure } from '../src/utils/authError'

/**
 * T326 安装记录页 unauthorized：会话失效判据与处置
 *
 * 判据来源是 staging 实测（2026-09-23，见卡内交件）：
 *   残留 mock token / 篡改签名 → HTTP 401 + 信封 code 401「unauthorized: invalid or expired token」
 *   密码错（登录请求）           → HTTP 401 + 信封 code 10401「invalid phone or password」
 *   合法 technician token        → HTTP 200 + code 0（total=31）
 */
describe('T326 会话失效判据', () => {
  it('网关 401（信封 code 也是 401）判为失效', () => {
    expect(isAuthFailure(401, 401)).toBe(true)
  })

  it('服务层 10401 判为失效', () => {
    expect(isAuthFailure(200, 10401)).toBe(true)
  })

  it('旧代码唯一认的 40101 仍判失效（防回退）', () => {
    expect(isAuthFailure(200, 40101)).toBe(true)
  })

  it('非鉴权类失败不得判失效：403/409/500 与无 code 的响应体', () => {
    expect(isAuthFailure(403, 10403)).toBe(false)
    expect(isAuthFailure(409, 20409)).toBe(false)
    expect(isAuthFailure(500, undefined)).toBe(false)
    expect(isAuthFailure(200, undefined)).toBe(false)
    expect(isAuthFailure(200, 0)).toBe(false)
  })
})

describe('T326 会话失效处置', () => {
  it('清 token + 提示 + 回登录页，三件事一次做全', () => {
    const removeToken = vi.fn()
    const toast = vi.fn()
    const reLaunch = vi.fn()

    forceRelogin({ removeToken, toast, reLaunch })

    expect(removeToken).toHaveBeenCalledTimes(1)
    expect(toast).toHaveBeenCalledWith(AUTH_EXPIRED_MESSAGE)
    expect(reLaunch).toHaveBeenCalledWith(LOGIN_PAGE)
  })

  it('给用户的是中文文案，不是网关英文原文', () => {
    expect(AUTH_EXPIRED_MESSAGE).toBe('登录已过期，请重新登录')
  })
})
