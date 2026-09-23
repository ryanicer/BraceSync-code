import { getToken, removeToken } from './token'
import { AUTH_EXPIRED_MESSAGE, forceRelogin, isAuthFailure } from './authError'
import { logger } from './logger'

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

// 脱敏：密码只记长度
function maskRequestData(data: any): any {
  if (!data) return data
  const masked: Record<string, any> = {}
  for (const [k, v] of Object.entries(data)) {
    if (k.toLowerCase().includes('password') && typeof v === 'string') {
      masked[k] = `len=${v.length}`
    } else {
      masked[k] = v
    }
  }
  return masked
}

export async function request<T>(options: RequestOptions): Promise<T> {
  if (USE_MOCK) {
    throw new Error('Mock mode: use mock data functions directly')
  }

  // T208 修复：登录接口不需要 Authorization，残留的 mock token 会让后端返回 10401 invalid token
  // 仅在非登录请求且有合法 token 时才挂 header
  const isLoginRequest = options.url.includes('/login')
  const token = getToken()
  const header: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  if (token && !isLoginRequest) {
    header['Authorization'] = `Bearer ${token}`
  }

  // T208: 请求日志
  logger.info('[T208]', `request → ${options.method || 'GET'} ${options.url}`, maskRequestData(options.data))

  return new Promise((resolve, reject) => {
    uni.request({
      url: `${API_BASE_URL}${options.url}`,
      method: options.method || 'GET',
      data: options.data || {},
      header,
      success: (res) => {
        const data = res.data as { code: number; message: string; data: T }
        // T326：token 失效（网关 401 / 服务层 10401）一律清凭据回登录页。
        // 登录请求本身除外 —— 密码错同样是 401 + 10401，回登录页会和「手机号或密码错误」提示打架。
        if (!isLoginRequest && isAuthFailure(res.statusCode, data?.code)) {
          // 会话过期是预期分支，用 warn：e2e-miniapp/real-mp-helpers 的 attachConsole 会把
          // 任何 console.error 记进 result.errors，误判成失败（fail-closed）。
          logger.warn('[T326]', `auth failure ← ${options.method || 'GET'} ${options.url}` +
            ` status=${res.statusCode} code=${data?.code}`)
          forceRelogin({
            removeToken,
            toast: (msg) => uni.showToast({ title: msg, icon: 'none' }),
            reLaunch: (url) => uni.reLaunch({ url }),
          })
          reject(new Error(AUTH_EXPIRED_MESSAGE))
          return
        }
        // T208: 响应日志
        if (data.code === 0) {
          logger.info('[T208]', `response ← ${options.method || 'GET'} ${options.url}`, {
            statusCode: res.statusCode, code: data.code, message: data.message,
          })
          resolve(data.data)
        } else {
          logger.error('[T208]', `response fail ← ${options.method || 'GET'} ${options.url}`, {
            statusCode: res.statusCode, code: data.code, message: data.message,
          })
          // T173：透传业务码/HTTP 状态，供调用方区分语义（如基线 409 = 20409）
          // T299：一并透传响应 data，供调用方读结构化附带字段（如 409 的 occupiedDeviceId）而非解析文案
          const err = new Error(data.message || '请求失败') as Error & {
            code?: number
            httpStatus?: number
            data?: unknown
          }
          err.code = data.code
          err.httpStatus = res.statusCode
          err.data = data.data
          reject(err)
        }
      },
      fail: (err) => {
        logger.error('[T208]', `request fail ← ${options.method || 'GET'} ${options.url}`, { errMsg: err.errMsg })
        reject(new Error(err.errMsg || '网络错误'))
      },
    })
  })
}
