import { getToken, removeToken } from './token'

// MOCK 开关：构建时通过 VITE_USE_MOCK 环境变量控制（默认 true=mock，部署构建注入 false 走真实 API）
// 例：VITE_USE_MOCK=false npm run dev:h5 -w apps/tech-miniapp
export const USE_MOCK = import.meta.env.VITE_USE_MOCK !== 'false'
export const API_BASE_URL = '' // 同源代理：nginx /api/ → gateway:8080

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
