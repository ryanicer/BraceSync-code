import { getToken, removeToken } from './token'

// MOCK 开关：T074 切换真实模式 — 生产部署 USE_MOCK=false，API_BASE_URL 指向网关域名
export const USE_MOCK = false
export const API_BASE_URL = 'https://api.hbksd.com.cn'

interface RequestOptions {
  url: string
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  data?: Record<string, unknown>
}

/**
 * 网关统一响应体（T067 约定）。
 * 防御式解析：H5/小程序端 HTTP 2xx/4xx/5xx 均会走到 success 回调，
 * 因此先按 HTTP 状态码粗分，再按 body.code 细分。
 */
interface GatewayResponse<T> {
  code: number
  message: string
  data: T
}

const DEFAULT_ERROR_MESSAGE = '服务器异常，请稍后重试'

/** 安全地从 uni.request 的 success 回调里取 bodyType 字符串供日志 */
function bodyKind(data: unknown): string {
  if (data === null) return 'null'
  const t = typeof data
  if (t === 'undefined') return 'undefined'
  if (t === 'string') return data.length === 0 ? 'string(empty)' : `string(${Math.min(data.length, 32)}...)`
  if (t === 'object') return Array.isArray(data) ? 'array' : 'object'
  return t
}

/** 限长 80 字符的安全消息（避免日志把整段 HTML 打出来） */
function truncate(input: unknown, max = 80): string {
  const s = input == null ? '' : String(input)
  return s.length > max ? `${s.slice(0, max)}…` : s
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
        const statusCode: number | undefined =
          (res as unknown as { statusCode?: number }).statusCode
        const raw = res.data as unknown
        const bodyType = bodyKind(raw)
        let code: number | undefined
        let message: string | undefined
        let data: unknown

        // -------- 1) HTTP 状态码非 2xx：直接判失败，不触发任何页面导航 --------
        //    兼容 H5：uni.request 对 4xx/5xx 仍回调 success。
        //    uni-app statusCode 在 H5/小程序规范中都存在，用 != null 兜底老版本。
        const httpOk = statusCode != null && statusCode >= 200 && statusCode < 300

        // -------- 2) body 防御式解析 --------
        if (typeof raw === 'object' && raw !== null && !Array.isArray(raw)) {
          const envelope = raw as Partial<GatewayResponse<unknown>>
          code = typeof envelope.code === 'number' ? envelope.code : Number.NaN
          message = typeof envelope.message === 'string' ? envelope.message : undefined
          data = envelope.data
        } else {
          code = Number.NaN
          message = undefined
          data = undefined
        }
        message = message || DEFAULT_ERROR_MESSAGE

        let willNavigate = false

        // -------- 3) 先按 HTTP 粗分；非 2xx 一律 reject，杜绝误走成功分支 --------
        if (!httpOk) {
          // 仅当明确是"鉴权失败"（401 + 鉴权 code）时才允许回登录页，
          // 其他 4xx/5xx（含 Playwright mock 500）一律只抛错、不导航。
          if (statusCode === 401 && Number.isFinite(code) && code! >= 10000 && code! < 20000) {
            willNavigate = true
            removeToken()
            try {
              uni.reLaunch({ url: '/pages/login/index' })
            } catch {
              /* ignore navigation error in sandbox */
            }
          }
          // 诊断日志：statusCode 非 2xx
          console.debug('[request]', {
            url: options.url,
            method: options.method,
            statusCode,
            bodyType,
            code: Number.isFinite(code) ? code : null,
            willNavigate,
            error: truncate(message),
          })
          reject(new Error(message || DEFAULT_ERROR_MESSAGE))
          return
        }

        // -------- 4) HTTP 2xx：按 body.code 分发 --------
        if (Number.isFinite(code) && code === 0) {
          console.debug('[request]', {
            url: options.url,
            method: options.method,
            statusCode,
            bodyType,
            code: 0,
            willNavigate: false,
          })
          resolve(data as T)
          return
        }

        if (Number.isFinite(code) && code! >= 10000 && code! < 20000) {
          willNavigate = true
          removeToken()
          try {
            uni.reLaunch({ url: '/pages/login/index' })
          } catch {
            /* ignore */
          }
        }

        console.debug('[request]', {
          url: options.url,
          method: options.method,
          statusCode,
          bodyType,
          code: Number.isFinite(code) ? code : null,
          willNavigate,
          error: truncate(message),
        })
        reject(new Error(message || DEFAULT_ERROR_MESSAGE))
      },
      fail: (err) => {
        const errMsg = (err && typeof err === 'object' && 'errMsg' in err && typeof (err as { errMsg?: unknown }).errMsg === 'string')
          ? (err as { errMsg: string }).errMsg
          : 'Network error'
        console.debug('[request]', {
          url: options.url,
          method: options.method,
          fail: true,
          error: truncate(errMsg),
        })
        reject(new Error(`HTTP fail: ${errMsg}`))
      },
    })
  })
}
