import { API_BASE_URL } from '../utils/request'

/**
 * 网关统一响应体。登录/绑定端点返回业务 code（0/10601/10001/...），
 * 不能走 utils/request.ts（其对非 0 code 一律 reject），故此处用 raw uni.request
 * 直接返回 envelope 供状态机 resolveWxLoginResult / resolveBindPhoneResult 处理。
 *
 * T159-panic-502 排查（v2 部署后验证用，根因定位后回归）：
 *  bindPhone / wxLogin 这两条 raw uni.request 不走 utils/request 的 T080 观测通道，
 *  502 永远不会触发前端观测。临时在 send/recv/fail 三路打 console.warn + emit 到
 *  __E2E_REQUEST_EVENTS__，并附时间戳，便于与 gateway/user-service 日志对时。
 */

export interface ApiEnvelope<T> {
  code: number
  message: string
  data: T
}

// 限长（避免把 token/phoneCode 全打进日志）
function truncate(input: unknown, max = 24): string {
  const s = input == null ? '' : String(input)
  return s.length > max ? `${s.slice(0, max)}…(len=${s.length})` : s
}

// T159-dbg 前端观测：写到 console + window.__E2E_REQUEST_EVENTS__，
// 小程序 window 不可写时静默忽略
function emitBindDbg(payload: Record<string, unknown>): void {
  const line = `[OBS] patient-api ${JSON.stringify(payload)}`
  try {
    // eslint-disable-next-line no-console
    console.warn(line)
  } catch {/* ignore */}
  try {
    // @ts-ignore
    if (typeof window !== 'undefined' && (window as any).__E2E_DEBUG__) {
      // @ts-ignore
      const w = window as any
      if (!Array.isArray(w.__E2E_REQUEST_EVENTS__)) w.__E2E_REQUEST_EVENTS__ = []
      w.__E2E_REQUEST_EVENTS__.push({ t: Date.now(), ...payload })
    }
  } catch {/* ignore */}
}

/** wx-login 成功 data */
export interface WxLoginData {
  token: string
  patientId: string
  name: string
  role: string
}

/** wx-login 未绑定 data（code=10601），bindToken 由后端置于 token 字段 */
export interface WxLoginNeedBindData {
  token: string
}

/** bind-phone 成功 data */
export interface BindPhoneData {
  token: string
  patientId: string
  name: string
  role: string
}

/** bind-phone 失败 data（code=10602/10603） */
export interface BindPhoneFailData {
  phone_token: string
}

/**
 * 患者微信登录。
 * POST /api/v1/patient/wx-login { code }
 * 返回完整 envelope，不 reject 业务码。
 */
export function wxLogin(code: string): Promise<ApiEnvelope<WxLoginData | WxLoginNeedBindData>> {
  // T159-dbg: 前端观测，对照 gateway/user-service 日志
  const sendTs = Date.now()
  emitBindDbg({ event: 'wx-login.send', url: `${API_BASE_URL}/api/v1/patient/wx-login`, code_len: code.length })
  return new Promise((resolve, reject) => {
    uni.request({
      url: `${API_BASE_URL}/api/v1/patient/wx-login`,
      method: 'POST',
      data: { code },
      header: { 'Content-Type': 'application/json' },
      success: (res) => {
        const statusCode = (res as unknown as { statusCode?: number }).statusCode
        const raw = res.data as Partial<ApiEnvelope<unknown>> | undefined
        const envCode = typeof raw?.code === 'number' ? raw.code : Number.NaN
        emitBindDbg({
          event: 'wx-login.recv',
          statusCode: statusCode ?? null,
          code: Number.isFinite(envCode) ? envCode : null,
          message: truncate(raw?.message, 80),
          elapsedMs: Date.now() - sendTs,
        })
        resolve({
          code: envCode,
          message: typeof raw?.message === 'string' ? raw.message : '',
          data: (raw?.data as WxLoginData | WxLoginNeedBindData) ?? ({} as never),
        })
      },
      fail: (err) => {
        const errMsg =
          err && typeof err === 'object' && 'errMsg' in err && typeof (err as { errMsg?: unknown }).errMsg === 'string'
            ? (err as { errMsg: string }).errMsg
            : 'Network error'
        emitBindDbg({
          event: 'wx-login.fail',
          err: truncate(errMsg, 80),
          elapsedMs: Date.now() - sendTs,
        })
        reject(new Error(`wx-login request failed: ${errMsg}`))
      },
    })
  })
}

/**
 * 患者绑定手机号。
 * POST /api/v1/patient/bind-phone
 * header: Authorization: Bearer {bindToken}
 * body: { phone_code } 或 { phone_token }（二选一，phone_token 优先）
 */
export function bindPhone(
  bindToken: string,
  params: { phoneCode?: string; phoneToken?: string }
): Promise<ApiEnvelope<BindPhoneData | BindPhoneFailData>> {
  const data: Record<string, string> = {}
  if (params.phoneToken) {
    data.phone_token = params.phoneToken
  } else if (params.phoneCode) {
    data.phone_code = params.phoneCode
  }
  // T159-dbg: 前端观测 — bind-phone 是 502 排查焦点，必须能从前端确认
  // 1) 请求确实发出 2) statusCode 是不是真 502 3) 后端 envelope 有没有回 4) elapsedMs
  const sendTs = Date.now()
  emitBindDbg({
    event: 'bind-phone.send',
    url: `${API_BASE_URL}/api/v1/patient/bind-phone`,
    has_phone_code: !!params.phoneCode,
    has_phone_token: !!params.phoneToken,
    phone_code_len: params.phoneCode?.length ?? 0,
    phone_token_len: params.phoneToken?.length ?? 0,
    bind_token_prefix: truncate(bindToken, 16),
  })
  return new Promise((resolve, reject) => {
    uni.request({
      url: `${API_BASE_URL}/api/v1/patient/bind-phone`,
      method: 'POST',
      data,
      header: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${bindToken}`,
      },
      success: (res) => {
        const statusCode = (res as unknown as { statusCode?: number }).statusCode
        const raw = res.data as Partial<ApiEnvelope<unknown>> | undefined
        const envCode = typeof raw?.code === 'number' ? raw.code : Number.NaN
        emitBindDbg({
          event: 'bind-phone.recv',
          statusCode: statusCode ?? null,
          code: Number.isFinite(envCode) ? envCode : null,
          message: truncate(raw?.message, 80),
          data_kind: raw?.data == null ? 'null' : typeof raw.data,
          elapsedMs: Date.now() - sendTs,
        })
        resolve({
          code: envCode,
          message: typeof raw?.message === 'string' ? raw.message : '',
          data: (raw?.data as BindPhoneData | BindPhoneFailData) ?? ({} as never),
        })
      },
      fail: (err) => {
        const errMsg =
          err && typeof err === 'object' && 'errMsg' in err && typeof (err as { errMsg?: unknown }).errMsg === 'string'
            ? (err as { errMsg: string }).errMsg
            : 'Network error'
        emitBindDbg({
          event: 'bind-phone.fail',
          err: truncate(errMsg, 80),
          elapsedMs: Date.now() - sendTs,
        })
        reject(new Error(`bind-phone request failed: ${errMsg}`))
      },
    })
  })
}
