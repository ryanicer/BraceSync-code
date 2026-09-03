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
// T080: 可观测 marker 前缀（CI Playwright 可按 locator / 控制台 warn / 页面 uni-toast 三路取）
const OBS_TAG = '[OBS] request'

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

/**
 * T080：CI 观测通道的最低保障。
 *
 * Playwright CI GitHub reporter 的 stdout 对 console.debug 不采集，且 Vite dev 对
 * 纯副作用 console.debug 可能做 DCE。我们升级为 console.warn（不会被丢弃），
 * 并在 window.__E2E_DEBUG__ 为真时额外把观测事件写到：
 *   - (window as any).__E2E_REQUEST_EVENTS__ 内存数组（Playwright evaluate 可直读）
 *   - uni.showToast icon=none（E2E 再失败时截图里会带 marker 文案；用户肉眼也能看到）
 */
function emit(payload: Record<string, unknown>): void {
  const line = `${OBS_TAG} ${JSON.stringify(payload)}`
  try {
    // 生产构建 terser 也不会 drop console.warn（否则用户代码报错会丢）
    // eslint-disable-next-line no-console
    console.warn(line)
  } catch {/* ignore */}
  try {
    // 仅在 H5 / CI 下生效；小程序 window 可能不可写
    // @ts-ignore
    if (typeof window !== 'undefined' && (window as any).__E2E_DEBUG__) {
      // @ts-ignore
      const w = window as any
      if (!Array.isArray(w.__E2E_REQUEST_EVENTS__)) w.__E2E_REQUEST_EVENTS__ = []
      w.__E2E_REQUEST_EVENTS__.push({ t: Date.now(), ...payload })
      try {
        uni.showToast({
          title: `${OBS_TAG.split(' ')[1]}:${payload.event || 'x'}`,
          icon: 'none',
          duration: 1400,
        })
      } catch {/* ignore */}
    }
  } catch {/* ignore */}
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

  const startTs = Date.now()

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

        const elapsedMs = Date.now() - startTs

        // -------- 1) HTTP 状态码非 2xx：直接判失败，不触发任何页面导航 --------
        //    兼容 H5：uni.request 对 4xx/5xx 仍回调 success。
        //    uni-app statusCode 在 H5/小程序规范中都存在，用 != null 兜底老版本。
        // T080 关键修正：statusCode 只要 >=400（含 4xx/5xx）就**无条件**按"HTTP 失败"处理，
        // 即便 H5 runtime/拦截器把 body 规范化成 {code:0, data:{token,patientId}} 也不再
        // 被当成成功（这是 L3/L6 "mock 500 仍跳 monitor" 的真正兜底）。
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
          emit({
            event: 'http-error',
            url: options.url,
            method: options.method || 'GET',
            statusCode: statusCode ?? null,
            bodyType,
            code: Number.isFinite(code) ? code! : null,
            willNavigate,
            error: truncate(message),
            elapsedMs,
          })
          reject(new Error(message || DEFAULT_ERROR_MESSAGE))
          return
        }

        // -------- 4) HTTP 2xx：按 body.code 分发 --------
        if (Number.isFinite(code) && code === 0) {
          emit({
            event: 'ok',
            url: options.url,
            method: options.method || 'GET',
            statusCode: statusCode ?? null,
            bodyType,
            code: 0,
            willNavigate: false,
            elapsedMs,
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

        emit({
          event: 'biz-error',
          url: options.url,
          method: options.method || 'GET',
          statusCode: statusCode ?? null,
          bodyType,
          code: Number.isFinite(code) ? code! : null,
          willNavigate,
          error: truncate(message),
          elapsedMs,
        })
        reject(new Error(message || DEFAULT_ERROR_MESSAGE))
      },
      fail: (err) => {
        const errMsg = (err && typeof err === 'object' && 'errMsg' in err && typeof (err as { errMsg?: unknown }).errMsg === 'string')
          ? (err as { errMsg: string }).errMsg
          : 'Network error'
        const elapsedMs = Date.now() - startTs
        emit({
          event: 'fail',
          url: options.url,
          method: options.method || 'GET',
          error: truncate(errMsg),
          elapsedMs,
        })
        reject(new Error(`HTTP fail: ${errMsg}`))
      },
    })
  })
}
