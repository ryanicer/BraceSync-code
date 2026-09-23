// T357 回归点：request() 真实模式下遇到 HTTP 401 必须真的走到失效处置出口。
// 旧写法是 `if (!res.ok) throw new Error(...)` 在前、`body.code === 40101` 的清理分支在后，
// 网关又只发 401/401（从不发 40101）⇒ 那段清凭据回登录页的代码在现网产物里是死代码。
// 所以「这一行现在可达」本身就是本卡的验收判据，不能只测纯函数然后宣称已修。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { AUTH_EXPIRED_MESSAGE } from '../src/utils/sessionExpiry'
import { getToken } from '../src/utils/token'

const handled = vi.fn()

vi.mock('../src/utils/sessionExpiry', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../src/utils/sessionExpiry')>()
  return {
    ...actual,
    // 只替身「有副作用的那一步」：整页 assign 在 happy-dom 里不可靠，
    // 判定逻辑与地址拼装仍走真实实现（见 test/session-expiry.spec.ts）
    handleAuthExpired: (deps?: never) => handled(deps),
  }
})

function envelopeFetch(status: number, code: number, message: string) {
  return vi.fn(async () => ({
    ok: status < 400,
    status,
    json: async () => ({ code, message, data: null }),
  }))
}

/**
 * USE_MOCK 是模块级常量，必须 stub 环境变量之后重新求值模块，否则拿到的还是
 * 「Mock mode: use api layer functions」那条 throw。
 */
async function loadRealRequest() {
  vi.stubEnv('VITE_USE_MOCK', 'false')
  vi.resetModules()
  const mod = await import('../src/utils/request')
  return mod.request
}

describe('T357 request() 401 处置分支可达性', () => {
  beforeEach(() => {
    handled.mockClear()
    localStorage.clear()
    localStorage.setItem('admin_token', '格式合法但签名无效的-jwt')
  })
  afterEach(() => {
    vi.unstubAllEnvs()
    vi.unstubAllGlobals()
  })

  it('网关 401 + 信封 code 401：走失效出口，抛中文文案而非网关英文', async () => {
    const request = await loadRealRequest()
    vi.stubGlobal('fetch', envelopeFetch(401, 401, 'unauthorized: invalid or expired token'))

    await expect(request({ url: '/api/v1/admin/patients' })).rejects.toThrow(AUTH_EXPIRED_MESSAGE)
    expect(handled).toHaveBeenCalledTimes(1)
  })

  it('401 且响应体不是 JSON（网关挂在前面的代理回 HTML）：仍然处置，不静默停住', async () => {
    const request = await loadRealRequest()
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: false,
      status: 401,
      json: async () => {
        throw new SyntaxError('Unexpected token < in JSON')
      },
    })))

    await expect(request({ url: '/api/v1/admin/patients' })).rejects.toThrow(AUTH_EXPIRED_MESSAGE)
    expect(handled).toHaveBeenCalledTimes(1)
  })

  it('403 不被误当失效：仍透出后端文案、不走失效出口', async () => {
    const request = await loadRealRequest()
    vi.stubGlobal('fetch', envelopeFetch(403, 403, 'forbidden: out of team scope'))

    await expect(request({ url: '/api/v1/admin/patients' })).rejects.toThrow('forbidden: out of team scope')
    expect(handled).not.toHaveBeenCalled()
    expect(getToken()).toBe('格式合法但签名无效的-jwt')
  })

  it('HTTP 200 + 业务码 40101（契约里那条）也走同一个出口，不再有两套处置', async () => {
    const request = await loadRealRequest()
    vi.stubGlobal('fetch', envelopeFetch(200, 40101, 'token expired'))

    await expect(request({ url: '/api/v1/admin/patients' })).rejects.toThrow(AUTH_EXPIRED_MESSAGE)
    expect(handled).toHaveBeenCalledTimes(1)
  })

  it('成功响应不触发任何处置（防误伤）', async () => {
    const request = await loadRealRequest()
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({ code: 0, message: 'ok', data: { list: [], total: 0 } }),
    })))

    await expect(request({ url: '/api/v1/admin/patients' })).resolves.toEqual({ list: [], total: 0 })
    expect(handled).not.toHaveBeenCalled()
  })
})
