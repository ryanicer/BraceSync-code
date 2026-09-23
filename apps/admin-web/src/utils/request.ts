import type { ApiResponse } from '@bracesync/shared-types'
import { getToken } from './token'
import { AUTH_EXPIRED_MESSAGE, handleAuthExpired, isAuthExpired } from './sessionExpiry'

/**
 * T357：会话失效处置的唯一出口——清凭据 + 整页回登录页（带 redirect 回原页），并抛中文文案，
 * 让调用方把「登录已过期」提示给用户，而不是把网关英文原样渲染（同 T326 技师端口径）。
 */
function expiredSession(): never {
  handleAuthExpired()
  throw new Error(AUTH_EXPIRED_MESSAGE)
}

// MOCK 开关：构建时通过 VITE_USE_MOCK 环境变量控制（默认 true=mock，部署构建注入 false 走真实 API）
// 例：VITE_USE_MOCK=false npm run build -w apps/admin-web
export const USE_MOCK = import.meta.env.VITE_USE_MOCK !== 'false'
export const API_BASE_URL = '' // 同源代理：nginx /api/ → gateway:8080

interface RequestOptions {
  url: string
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  data?: Record<string, unknown>
}

/** 统一 HTTP 请求（USE_MOCK=false 时走 fetch，解包 ApiResponse 信封） */
export async function request<T>(options: RequestOptions): Promise<T> {
  if (USE_MOCK) {
    throw new Error('Mock mode: use api layer functions (mock 分支) instead')
  }

  const token = getToken()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const method = options.method || 'GET'
  const isGet = method === 'GET'
  let url = `${API_BASE_URL}${options.url}`
  if (isGet && options.data) {
    const params = new URLSearchParams()
    for (const [key, value] of Object.entries(options.data)) {
      if (value !== undefined && value !== null && value !== '') {
        params.append(key, String(value))
      }
    }
    const qs = params.toString()
    if (qs) url += `?${qs}`
  }

  const res = await fetch(url, {
    method,
    headers,
    body: !isGet && options.data ? JSON.stringify(options.data) : undefined,
  })
  if (!res.ok) {
    // 后端校验失败以 HTTP 4xx + 信封返回，文案（如「collectIntervalSeconds must be...」）要透出给用户
    const errBody = (await res.json().catch(() => null)) as ApiResponse<unknown> | null
    // T357：401 必须先于「透出后端文案」处置掉——旧写法在这里无条件 throw，
    // 把下面那段清凭据回登录页变成了死代码。
    if (isAuthExpired(res.status, errBody?.code)) expiredSession()
    throw new Error(errBody?.message || `HTTP ${res.status}`)
  }
  const body = (await res.json()) as ApiResponse<T>
  if (body.code === 0) {
    return body.data
  }
  // 200 + 业务鉴权码（服务层可能这样回）走同一个出口；401 的判定在上面 !res.ok 分支已完成
  if (isAuthExpired(res.status, body.code)) expiredSession()
  throw new Error(body.message || 'Request failed')
}
