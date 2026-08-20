import type { ApiResponse } from '@bracesync/shared-types'
import { getToken, removeToken } from './token'

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
    throw new Error(`HTTP ${res.status}`)
  }
  const body = (await res.json()) as ApiResponse<T>
  if (body.code === 0) {
    return body.data
  }
  if (body.code === 40101) {
    // token 失效：清凭据回登录页（对齐网关鉴权错误码）
    removeToken()
    window.location.href = '/login'
  }
  throw new Error(body.message || 'Request failed')
}
