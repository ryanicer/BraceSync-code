// auth store 单测：登录/登出/持久化（mock 路径 + T046 真实登录路径）
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from '../src/stores/auth'
import type { AdminLoginResult } from '@bracesync/shared-types'

function loginResult(overrides: Partial<AdminLoginResult> = {}): AdminLoginResult {
  return {
    token: 'jwt-x',
    adminId: 'A0001',
    username: 'ops_admin',
    name: '运营小张',
    roleId: 'ROLE_ADMIN',
    scope: 'all',
    ...overrides,
  }
}

/** 桩 fetch：按响应对象回放（登录接口契约：{code,message,data} 信封） */
function stubFetch(res: { ok: boolean; status?: number; body: unknown }) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok: res.ok,
    status: res.status ?? (res.ok ? 200 : 401),
    json: async () => res.body,
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('useAuthStore', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('初始状态未登录', () => {
    const auth = useAuthStore()
    expect(auth.isLoggedIn).toBe(false)
    expect(auth.role).toBeNull()
  })

  it('login(admin) 后登录态与角色正确并持久化', () => {
    const auth = useAuthStore()
    auth.login('admin')
    expect(auth.isLoggedIn).toBe(true)
    expect(auth.role).toBe('admin')
    expect(auth.token).toMatch(/^mock-token-admin-/)
    expect(localStorage.getItem('admin_token')).toBeTruthy()
    expect(JSON.parse(localStorage.getItem('admin_user') || '{}').role).toBe('admin')
  })

  it('新 store 实例从 localStorage 恢复会话', () => {
    const auth = useAuthStore()
    auth.login('cs')
    // 模拟刷新页面：新建 pinia 重新实例化 store
    setActivePinia(createPinia())
    const restored = useAuthStore()
    expect(restored.isLoggedIn).toBe(true)
    expect(restored.role).toBe('cs')
  })

  it('logout 清空状态与凭据', () => {
    const auth = useAuthStore()
    auth.login('doctor')
    auth.logout()
    expect(auth.isLoggedIn).toBe(false)
    expect(auth.token).toBe('')
    expect(localStorage.getItem('admin_token')).toBeNull()
    expect(localStorage.getItem('admin_user')).toBeNull()
  })
})

describe('loginWithPassword（真实登录，T046）', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('ROLE_ADMIN 成功登录：token/user 持久化，role 映射为 admin', async () => {
    stubFetch({ ok: true, body: { code: 0, message: 'ok', data: loginResult() } })
    const auth = useAuthStore()
    await auth.loginWithPassword('ops_admin', 'admin123')
    expect(auth.isLoggedIn).toBe(true)
    expect(auth.token).toBe('jwt-x')
    expect(auth.role).toBe('admin')
    expect(auth.user?.name).toBe('运营小张')
    expect(auth.user?.roleId).toBe('ROLE_ADMIN')
    expect(auth.user?.scope).toBe('all')
    expect(localStorage.getItem('admin_token')).toBe('jwt-x')
    expect(JSON.parse(localStorage.getItem('admin_user') || '{}').roleId).toBe('ROLE_ADMIN')
  })

  it('ROLE_DOCTOR 映射为 doctor', async () => {
    stubFetch({
      ok: true,
      body: { code: 0, message: 'ok', data: loginResult({ roleId: 'ROLE_DOCTOR', name: '李医生', scope: 'team' }) },
    })
    const auth = useAuthStore()
    await auth.loginWithPassword('doctor_li', 'admin123')
    expect(auth.role).toBe('doctor')
    expect(auth.user?.scope).toBe('team')
  })

  it('ROLE_CS 映射为 cs', async () => {
    stubFetch({
      ok: true,
      body: { code: 0, message: 'ok', data: loginResult({ roleId: 'ROLE_CS', name: '客服小王', adminId: 'A0003', scope: 'all_patients' }) },
    })
    const auth = useAuthStore()
    await auth.loginWithPassword('cs_wang', 'admin123')
    expect(auth.role).toBe('cs')
    expect(auth.user?.adminId).toBe('A0003')
  })

  it('未知 roleId → role=null（fail-closed，守卫将 403）', async () => {
    stubFetch({
      ok: true,
      body: { code: 0, message: 'ok', data: loginResult({ roleId: 'ROLE_X', name: '神秘人' }) },
    })
    const auth = useAuthStore()
    await auth.loginWithPassword('x', 'admin123')
    expect(auth.isLoggedIn).toBe(true)
    expect(auth.role).toBeNull()
  })

  it('凭据错误（10401）→ 抛"用户名或密码错误"，不写入凭据', async () => {
    stubFetch({ ok: false, status: 401, body: { code: 10401, message: 'invalid username or password', data: null } })
    const auth = useAuthStore()
    await expect(auth.loginWithPassword('ops_admin', 'wrong')).rejects.toThrow('用户名或密码错误')
    expect(auth.isLoggedIn).toBe(false)
    expect(auth.token).toBe('')
    expect(localStorage.getItem('admin_token')).toBeNull()
  })

  it('刷新后从 localStorage 恢复真实登录会话', async () => {
    stubFetch({ ok: true, body: { code: 0, message: 'ok', data: loginResult() } })
    const auth = useAuthStore()
    await auth.loginWithPassword('ops_admin', 'admin123')
    setActivePinia(createPinia())
    const restored = useAuthStore()
    expect(restored.isLoggedIn).toBe(true)
    expect(restored.token).toBe('jwt-x')
    expect(restored.role).toBe('admin')
  })
})
