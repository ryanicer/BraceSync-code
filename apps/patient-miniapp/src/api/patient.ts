import { API_BASE_URL } from '../utils/request'

/**
 * 网关统一响应体。登录/绑定端点返回业务 code（0/10601/10001/...），
 * 不能走 utils/request.ts（其对非 0 code 一律 reject），故此处用 raw uni.request
 * 直接返回 envelope 供状态机 resolveWxLoginResult / resolveBindPhoneResult 处理。
 */
export interface ApiEnvelope<T> {
  code: number
  message: string
  data: T
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
  return new Promise((resolve, reject) => {
    uni.request({
      url: `${API_BASE_URL}/api/v1/patient/wx-login`,
      method: 'POST',
      data: { code },
      header: { 'Content-Type': 'application/json' },
      success: (res) => {
        const raw = res.data as Partial<ApiEnvelope<unknown>> | undefined
        resolve({
          code: typeof raw?.code === 'number' ? raw.code : Number.NaN,
          message: typeof raw?.message === 'string' ? raw.message : '',
          data: (raw?.data as WxLoginData | WxLoginNeedBindData) ?? ({} as never),
        })
      },
      fail: (err) => {
        const errMsg =
          err && typeof err === 'object' && 'errMsg' in err && typeof (err as { errMsg?: unknown }).errMsg === 'string'
            ? (err as { errMsg: string }).errMsg
            : 'Network error'
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
        const raw = res.data as Partial<ApiEnvelope<unknown>> | undefined
        resolve({
          code: typeof raw?.code === 'number' ? raw.code : Number.NaN,
          message: typeof raw?.message === 'string' ? raw.message : '',
          data: (raw?.data as BindPhoneData | BindPhoneFailData) ?? ({} as never),
        })
      },
      fail: (err) => {
        const errMsg =
          err && typeof err === 'object' && 'errMsg' in err && typeof (err as { errMsg?: unknown }).errMsg === 'string'
            ? (err as { errMsg: string }).errMsg
            : 'Network error'
        reject(new Error(`bind-phone request failed: ${errMsg}`))
      },
    })
  })
}
