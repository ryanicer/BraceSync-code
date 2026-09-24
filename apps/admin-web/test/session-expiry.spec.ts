// T357：会话失效（HTTP 401）处置的纯层单测——判定、登录页地址、只跳一次。
// request() 里的「分支可达」判据在 test/request-401.spec.ts（要 stub USE_MOCK，另开一份）。
import { describe, it, expect, beforeEach } from 'vitest'
import {
  isAuthExpired,
  innerPath,
  expiredLoginHref,
  handleAuthExpired,
  resetAuthExpiryLatch,
  consumeAuthExpiredNotice,
  AUTH_EXPIRED_MESSAGE,
  AUTH_EXPIRED_NOTICE_KEY,
  type ExpiryEffects,
} from '../src/utils/sessionExpiry'
import { getToken, removeToken } from '../src/utils/token'
import { ADMIN_BASE } from '../src/utils/mount'

function fakeEffects(init: { token?: string; at?: { pathname: string; search: string } } = {}) {
  localStorage.clear()
  if (init.token) localStorage.setItem('admin_token', init.token)
  const calls: string[] = []
  const at = init.at ?? { pathname: '/admin/patients', search: '?page=2' }
  const fx: ExpiryEffects = {
    // 用真的 token 层：「清没清掉」落在 happy-dom 的 localStorage 上，不是自说自话
    removeToken,
    assign: (href) => {
      calls.push(href)
    },
    readLocation: () => ({ baseUrl: '/admin/', ...at }),
    log: () => undefined,
  }
  return { fx, calls }
}

describe('T357 isAuthExpired（按 HTTP 状态判，业务码兼容）', () => {
  it('网关 HTTP 401 一律判失效（信封 code 是 401，不是旧代码认的 40101）', () => {
    expect(isAuthExpired(401, 401)).toBe(true)
    expect(isAuthExpired(401)).toBe(true)
    expect(isAuthExpired(401, 0)).toBe(true)
  })

  it('服务层 10401 与契约里的 40101 仍判失效（HTTP 200 承载业务码的场景）', () => {
    expect(isAuthExpired(200, 10401)).toBe(true)
    expect(isAuthExpired(200, 40101)).toBe(true)
  })

  it('反证：403/400/500 不判失效（医护 403 那类不能被误踢回登录页）', () => {
    expect(isAuthExpired(403, 403)).toBe(false)
    expect(isAuthExpired(400, 400)).toBe(false)
    expect(isAuthExpired(200, 409)).toBe(false)
    expect(isAuthExpired(500)).toBe(false)
  })
})

describe('T357 登录页地址（T336 挂载前缀 + redirect 回原页口径）', () => {
  it('剥挂载前缀得到 router 内部路径', () => {
    expect(innerPath('/admin/', '/admin/patients')).toBe('/patients')
    expect(innerPath('/admin/', '/admin/')).toBe('/')
    expect(innerPath('/', '/dashboard')).toBe('/dashboard')
  })

  it('跳转地址自带 /admin/ 前缀，redirect 是不含前缀的内部路径（回填时可直接 push）', () => {
    const url = new URL(expiredLoginHref('/admin/', '/admin/patients', '?page=2'), 'http://staging')
    expect(url.pathname).toBe('/admin/login')
    expect(url.searchParams.get('redirect')).toBe('/patients?page=2')
  })

  it('根路径 /admin/ 也要带 redirect（登录后按 landing 回首页不算退化）', () => {
    const url = new URL(expiredLoginHref('/admin/', '/admin/', ''), 'http://staging')
    expect(url.searchParams.get('redirect')).toBe('/')
  })

  it('已经在登录页时不带 redirect（防自跳）', () => {
    expect(expiredLoginHref('/admin/', '/admin/login', '')).toBe('/admin/login')
  })

  it('反证：写死 /login（旧口径）会丢挂载前缀 ⇒ 断言本卡修法确实带了前缀', () => {
    expect(expiredLoginHref('/admin/', '/admin/patients', '')).not.toBe('/login')
  })

  it('用真的挂载点常量拼一遍（BASE_URL 由构建注入，这里对账同一个值）', () => {
    const url = new URL(expiredLoginHref(ADMIN_BASE, `${ADMIN_BASE}patients`, ''), 'http://staging')
    expect(url.pathname).toBe(`${ADMIN_BASE}login`)
    expect(url.searchParams.get('redirect')).toBe('/patients')
  })
})

describe('T357 handleAuthExpired（清凭据 + 只跳一次）', () => {
  beforeEach(() => resetAuthExpiryLatch())

  it('首个失效：清 token 并整页跳登录页（带 redirect）', () => {
    const { fx, calls } = fakeEffects({ token: 'dead-token' })
    const href = handleAuthExpired(fx)
    expect(href).toBe('/admin/login?redirect=%2Fpatients%3Fpage%3D2')
    expect(calls).toEqual([href])
    expect(getToken()).toBe('')
  })

  it('同一轮里并发第二个 401：不再重复跳转（避免重载与报错洪水）', () => {
    const { fx, calls } = fakeEffects({ token: 'dead-token' })
    expect(handleAuthExpired(fx)).not.toBeNull()
    expect(handleAuthExpired(fx)).toBeNull()
    expect(calls).toHaveLength(1)
  })

  it('已在登录页：只清凭据，不再 assign（防自跳循环）', () => {
    const { fx, calls } = fakeEffects({ token: 'dead-token', at: { pathname: '/admin/login', search: '' } })
    expect(handleAuthExpired(fx)).toBeNull()
    expect(calls).toEqual([])
    expect(getToken()).toBe('')
  })
})

// T384：整页 assign 会立刻卸掉当前页，那一刻弹 toast 用户看不见 ⇒ 文案随跳转带走，
// 由登录页落地后取用一次。这是 admin-web 与技师端 T326「同文案 + toast」口径对齐的载体。
describe('T384 失效提示的跨页载体', () => {
  beforeEach(() => {
    resetAuthExpiryLatch()
    sessionStorage.clear()
  })

  it('要跳转的那条分支：先把文案留给登录页，取用即作废', () => {
    const { fx } = fakeEffects({ token: 'dead-token' })
    expect(handleAuthExpired(fx)).not.toBeNull()
    expect(sessionStorage.getItem(AUTH_EXPIRED_NOTICE_KEY)).toBe(AUTH_EXPIRED_MESSAGE)
    expect(consumeAuthExpiredNotice()).toBe(AUTH_EXPIRED_MESSAGE)
    expect(consumeAuthExpiredNotice()).toBeNull()
  })

  it('反证：已在登录页（不跳转、页面还在）时不留载体，免得下次进登录页误弹', () => {
    const { fx } = fakeEffects({ token: 'dead-token', at: { pathname: '/admin/login', search: '' } })
    expect(handleAuthExpired(fx)).toBeNull()
    expect(consumeAuthExpiredNotice()).toBeNull()
  })

  it('同一轮第二个 401 不重复处置，也不覆盖已留下的文案', () => {
    const { fx } = fakeEffects({ token: 'dead-token' })
    expect(handleAuthExpired(fx)).not.toBeNull()
    const { fx: fx2 } = fakeEffects({ token: 'dead-token-2', at: { pathname: '/admin/teams', search: '' } })
    expect(handleAuthExpired(fx2)).toBeNull()
    expect(consumeAuthExpiredNotice()).toBe(AUTH_EXPIRED_MESSAGE)
  })
})
