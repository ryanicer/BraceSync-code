// T384 防回潮：导出 CSV 这条自带 Authorization 的裸 fetch 通道必须走会话失效唯一出口。
//
// 修前现场（Joe 部署后独立复验 §六 建议 1，接口层实证明细 L1.5：死令牌打该端点确实回 401 + 信封 code 401）：
// api/index.ts 的 exportAbnormalReportApi 在 `if (!res.ok)` 里只 throw「导出失败（HTTP 401）」，
// 不调 isAuthExpired / expiredSession ⇒ 页面数据走 request() 会被弹回登录页，但在页上点「导出 CSV」不会：
// 凭据不清、不跳登录，用户对着一个永远失败的按钮反复点。
//
// 所以本文件的判据不是「抛什么文案」，而是「处置出口被调用了几次」——修前该断言恒为 0，必红。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { AUTH_EXPIRED_MESSAGE } from '../src/utils/sessionExpiry'

const handled = vi.fn()

vi.mock('../src/utils/sessionExpiry', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../src/utils/sessionExpiry')>()
  return {
    ...actual,
    // 同 test/request-401.spec.ts：只替身「有整页副作用的那一步」，判定与地址拼装走真实实现
    handleAuthExpired: (deps?: never) => handled(deps),
  }
})

const QUERY = { patientId: 'P20260701ABCDEF01', start: '2026-07-01', end: '2026-07-07' }
const DEAD_TOKEN = '格式合法但签名无效的-jwt'

/** USE_MOCK 是模块级常量，必须 stub 环境变量后重新求值模块，否则拿到的是 mock 分支 */
async function loadRealApi() {
  vi.stubEnv('VITE_USE_MOCK', 'false')
  vi.resetModules()
  return await import('../src/api')
}

function envelopeFetch(status: number, code: number, message: string) {
  return vi.fn(async () => ({
    ok: status < 400,
    status,
    headers: { get: () => null },
    json: async () => ({ code, message, data: null }),
  }))
}

/** 存盘动作的可观测替身：401 时不该走到，成功时该走到 */
function stubDownload() {
  const originalCreate = URL.createObjectURL
  const originalRevoke = URL.revokeObjectURL
  const create = vi.fn(() => 'blob:fake-csv')
  URL.createObjectURL = create
  URL.revokeObjectURL = vi.fn()
  return { create, restore: () => { URL.createObjectURL = originalCreate; URL.revokeObjectURL = originalRevoke } }
}

describe('T384 导出通道 401 处置', () => {
  beforeEach(() => {
    handled.mockClear()
    localStorage.clear()
    localStorage.setItem('admin_token', DEAD_TOKEN)
  })
  afterEach(() => {
    vi.unstubAllEnvs()
    vi.unstubAllGlobals()
    sessionStorage.clear()
  })

  it('死令牌点导出：走唯一出口（清凭据 + 跳登录由处置承担），抛中文文案而非网关英文', async () => {
    const dl = stubDownload()
    const fetchMock = envelopeFetch(401, 401, 'unauthorized: invalid or expired token')
    vi.stubGlobal('fetch', fetchMock)
    const api = await loadRealApi()

    await expect(api.exportAbnormalReportApi(QUERY)).rejects.toThrow(AUTH_EXPIRED_MESSAGE)
    expect(handled).toHaveBeenCalledTimes(1)
    expect(dl.create).not.toHaveBeenCalled() // 错误体是 JSON 信封，不能当 CSV 存盘
    dl.restore()
  })

  it('导出请求确实带了 Bearer 头与查询参数（收口 401 不等于把凭据通道改掉）', async () => {
    const dl = stubDownload()
    const fetchMock = envelopeFetch(401, 401, 'unauthorized')
    vi.stubGlobal('fetch', fetchMock)
    const api = await loadRealApi()

    await expect(api.exportAbnormalReportApi(QUERY)).rejects.toThrow(AUTH_EXPIRED_MESSAGE)
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, { headers: Record<string, string> }]
    expect(url).toContain('/api/v1/admin/abnormal-reports/export?')
    expect(url).toContain('patientId=P20260701ABCDEF01')
    expect(init.headers.Authorization).toBe(`Bearer ${DEAD_TOKEN}`)
    dl.restore()
  })

  it('401 且响应体不是 JSON（挡在前面的代理回 HTML）：仍然处置，不静默停住', async () => {
    const dl = stubDownload()
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: false,
      status: 401,
      headers: { get: () => null },
      json: async () => {
        throw new SyntaxError('Unexpected token < in JSON')
      },
    })))
    const api = await loadRealApi()

    await expect(api.exportAbnormalReportApi(QUERY)).rejects.toThrow(AUTH_EXPIRED_MESSAGE)
    expect(handled).toHaveBeenCalledTimes(1)
    dl.restore()
  })

  it('反证：403 不被误当失效（导出仍透出后端文案、一次都不处置）', async () => {
    const dl = stubDownload()
    vi.stubGlobal('fetch', envelopeFetch(403, 403, 'forbidden: out of team scope'))
    const api = await loadRealApi()

    await expect(api.exportAbnormalReportApi(QUERY)).rejects.toThrow('forbidden: out of team scope')
    expect(handled).not.toHaveBeenCalled()
    dl.restore()
  })

  it('成功回 CSV 时不触发任何处置，并真的走存盘（防「无差别拦成 401」）', async () => {
    const dl = stubDownload()
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      status: 200,
      headers: { get: (k: string) => (k === 'Content-Disposition' ? 'attachment; filename="abnormal-p1.csv"' : null) },
      blob: async () => ({ size: 3 }),
    })))
    const api = await loadRealApi()

    await expect(api.exportAbnormalReportApi(QUERY)).resolves.toBeUndefined()
    expect(handled).not.toHaveBeenCalled()
    expect(dl.create).toHaveBeenCalledTimes(1)
    dl.restore()
  })
})
