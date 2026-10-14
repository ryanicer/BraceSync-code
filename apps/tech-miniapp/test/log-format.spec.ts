import { describe, it, expect } from 'vitest'
import { toLogText, formatLogArgs } from '../src/utils/logFormat'

/**
 * T677：logger 对象序列化——对象不再被隐式 String() 成 [object Object]。
 * 真机取证时 loginWithPassword / request fail / doLogin catch 三行日志全是
 * `[object Object]`，errMsg/url 关键信息丢失。
 */
describe('T677 日志参数序列化', () => {
  it('字符串原样返回', () => {
    expect(toLogText('hello')).toBe('hello')
  })

  it('对象序列化为 JSON，而非 [object Object]', () => {
    const out = toLogText({ errMsg: 'request:fail', url: '/api/v1/tech/login' })
    expect(out).toBe('{"errMsg":"request:fail","url":"/api/v1/tech/login"}')
    expect(out).not.toContain('[object Object]')
  })

  it('嵌套对象也整体 JSON 化', () => {
    const out = toLogText({ a: { b: 1 }, c: [1, 2] })
    expect(out).toBe('{"a":{"b":1},"c":[1,2]}')
  })

  it('Error 取 stack（无 stack 时取 name: message）', () => {
    const err = new Error('request:fail url not in domain list')
    const out = toLogText(err)
    expect(out).toContain('request:fail url not in domain list')
  })

  it('undefined 显式成串，不丢位', () => {
    expect(toLogText(undefined)).toBe('undefined')
  })

  it('循环引用不抛错，退化 String()', () => {
    const a: Record<string, unknown> = {}
    a.self = a
    expect(() => toLogText(a)).not.toThrow()
    expect(toLogText(a)).toBe('[object Object]')
  })

  it('多参数拼装：对象与字符串同级序列化', () => {
    const out = formatLogArgs(['[T208]', 'request fail', { errMsg: 'request:fail' }])
    expect(out).toBe('[T208] request fail {"errMsg":"request:fail"}')
    expect(out).not.toContain('[object Object]')
  })
})
