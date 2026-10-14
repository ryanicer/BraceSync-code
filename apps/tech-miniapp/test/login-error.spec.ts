import { describe, it, expect } from 'vitest'
import { classifyLoginError } from '../src/utils/loginError'

/**
 * T677：登录错误分级——区分「网络失败」与「服务端凭证错误」，不再一锅烩。
 * 契约出处：PRD §7C.1「网络失败与凭证错误必须分别提示，禁止一锅烩兜底文案」。
 * 展示面口径：T465 门禁规定展示汇点不得出现技术原文（errMsg 只进日志），故 modal 正文
 * 走共享文案层（带 NET 码位），原始 errMsg 由 login/index.vue 的 logger 单独留痕。
 */
describe('T677 登录错误分级', () => {
  it('传输层失败（无码无状态）判 network，弹网络 modal 且不含技术原文', () => {
    const err = new Error('request:fail url not in domain list')
    const p = classifyLoginError(err)
    expect(p.kind).toBe('network')
    expect(p.modal).toBe(true)
    expect(p.title).toBe('网络连接失败')
    expect(p.content).toContain('登录请求未能送达服务器')
    expect(p.content).toContain('错误码 NET')
    expect(p.content).not.toContain('request:fail')
  })

  it('断网空错误也判 network，正文退化到页面兜底句不空窗', () => {
    const p = classifyLoginError(new Error(''))
    expect(p.kind).toBe('network')
    expect(p.content).toContain('登录请求未能送达服务器')
    expect(p.content).toContain('错误码 NET')
  })

  it('服务端 10401（HTTP 401）判 credential，逐字保持既有句', () => {
    const p = classifyLoginError({ message: 'invalid phone or password', code: 10401, httpStatus: 401 })
    expect(p.kind).toBe('credential')
    expect(p.modal).toBe(false)
    expect(p.title).toBe('手机号或密码错误')
  })

  it('仅 HTTP 401（无信封码）同样判 credential', () => {
    const p = classifyLoginError({ message: 'unauthorized', httpStatus: 401 })
    expect(p.kind).toBe('credential')
    expect(p.title).toBe('手机号或密码错误')
  })

  it('其它已知服务端码走共享文案层', () => {
    const p = classifyLoginError({ message: 'internal error', code: 90001, httpStatus: 500 })
    expect(p.kind).toBe('server')
    expect(p.modal).toBe(false)
    expect(p.title).toBe('服务暂时异常，请稍后重试')
  })

  it('有 HTTP 状态无码（如网关 502）判 server，不当网络失败', () => {
    const p = classifyLoginError({ message: 'Bad Gateway', httpStatus: 502 })
    expect(p.kind).toBe('server')
    expect(p.title).toContain('登录失败')
    expect(p.title).toContain('HTTP502')
  })
})
