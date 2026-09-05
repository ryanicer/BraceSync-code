import { getToken, removeToken } from './token'

// 环境变量通过 vite.config.ts 的 define 静态注入（绕开 uni 插件对 import.meta.env 的破坏）
declare const __API_BASE_URL__: string
declare const __USE_MOCK__: boolean

// MOCK 开关：构建时通过 VITE_USE_MOCK 环境变量控制（默认 true=mock，部署构建注入 false 走真实 API）
// 例：VITE_USE_MOCK=false npm run build:mp-weixin
export const USE_MOCK = __USE_MOCK__
export const API_BASE_URL = __API_BASE_URL__

interface RequestOptions {
  url: string
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  data?: Record<string, unknown>
}

export async function request<T>(options: RequestOptions): Promise<T> {
  if (USE_MOCK) {
    throw new Error('Mock mode: use mock data functions directly')
  }

  const token = getToken()
  const header: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  if (token) {
    header['Authorization'] = `Bearer ${token}`
  }

  return new Promise((resolve, reject) => {
    uni.request({
      url: `${API_BASE_URL}${options.url}`,
      method: options.method || 'GET',
      data: options.data || {},
      header,
      success: (res) => {
        const data = res.data as { code: number; message: string; data: T }
        if (data.code === 0) {
          resolve(data.data)
        } else if (data.code === 40101) {
          // token 失效：清凭据回登录页（对齐网关鉴权错误码）
          removeToken()
          uni.reLaunch({ url: '/pages/login/index' })
          reject(new Error(data.message || '登录已过期，请重新登录'))
        } else {
          reject(new Error(data.message || '请求失败'))
        }
      },
      fail: (err) => {
        reject(new Error(err.errMsg || '网络错误'))
      },
    })
  })
}
