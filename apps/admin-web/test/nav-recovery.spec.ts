// T355：跳转可靠性收口的纯层单测（isChunkLoadError / shouldRecoverTo / clearRecoverFlag）。
// 只测纯逻辑，不实例化 vue-router——装配在 router/index.ts，DOM 判据交给 Playwright。
import { describe, it, expect, beforeEach } from 'vitest'
import {
  isChunkLoadError,
  shouldRecoverTo,
  clearRecoverFlag,
  NAV_RECOVER_KEY,
} from '../src/router/navRecovery'

function fakeStore(init: Record<string, string> = {}) {
  const m = new Map<string, string>(Object.entries(init))
  return {
    getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
    _has: (k: string) => m.has(k),
  }
}

describe('T355 isChunkLoadError', () => {
  it('认得各浏览器/vite 的懒加载失败文案', () => {
    const msgs = [
      'Failed to fetch dynamically imported module: http://h/admin/assets/dashboard-abc123.js',
      'error loading dynamically imported module: http://h/assets/x.js',
      'Importing a module script failed.',
      'Loading chunk dashboard-abc123 failed.',
      'Loading CSS chunk 7 failed.',
      'failed to fetch dynamically imported module',
    ]
    for (const m of msgs) expect(isChunkLoadError(new Error(m))).toBe(true)
  })

  it('普通运行时异常不误判为可重载的 chunk 错误', () => {
    expect(isChunkLoadError(new Error('Cannot read properties of undefined (reading name)'))).toBe(false)
    expect(isChunkLoadError(new Error('登录失败（HTTP 500）'))).toBe(false)
    expect(isChunkLoadError(new TypeError('x is not a function'))).toBe(false)
  })

  it('空/字符串输入不炸', () => {
    expect(isChunkLoadError(null)).toBe(false)
    expect(isChunkLoadError(undefined)).toBe(false)
    expect(isChunkLoadError('')).toBe(false)
    expect(isChunkLoadError('Failed to fetch dynamically imported module: u')).toBe(true)
  })
})

describe('T355 shouldRecoverTo 单次防环', () => {
  let store: ReturnType<typeof fakeStore>
  beforeEach(() => { store = fakeStore() })

  it('同一目标第一次允许恢复并占位，第二次判红不再循环', () => {
    expect(shouldRecoverTo(store, '/dashboard')).toBe(true)
    expect(store.getItem(NAV_RECOVER_KEY)).toBe('/dashboard')
    expect(shouldRecoverTo(store, '/dashboard')).toBe(false)
  })

  it('不同目标各自有一次恢复额度', () => {
    expect(shouldRecoverTo(store, '/dashboard')).toBe(true)
    expect(shouldRecoverTo(store, '/alerts')).toBe(true)
    expect(shouldRecoverTo(store, '/alerts')).toBe(false)
  })

  it('成功落地清占位后，同目标重新获得一次额度', () => {
    expect(shouldRecoverTo(store, '/dashboard')).toBe(true)
    expect(shouldRecoverTo(store, '/dashboard')).toBe(false)
    clearRecoverFlag(store)
    expect(store._has(NAV_RECOVER_KEY)).toBe(false)
    expect(shouldRecoverTo(store, '/dashboard')).toBe(true)
  })
})
