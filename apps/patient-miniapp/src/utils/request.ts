import { getToken, removeToken } from './token'

// MOCK 开关：接口就绪后设为 false 并填写 API_BASE_URL
export const USE_MOCK = true
export const API_BASE_URL = '' // 接口就绪后填，如 'https://api.bracesync.com'

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
        } else if (data.code >= 10000 && data.code < 20000) {
          removeToken()
          uni.reLaunch({ url: '/pages/login/index' })
          reject(new Error(data.message))
        } else {
          reject(new Error(data.message))
        }
      },
      fail: (err) => {
        reject(new Error(err.errMsg || 'Network error'))
      },
    })
  })
}