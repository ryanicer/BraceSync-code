/**
 * STUB: T094a KNOWN_RED scaffold — implementation by Iris Stage 3b.
 *
 * 登录绑定状态机（PRD §7A.1.1）。
 * 本文件仅提供函数签名与错误占位返回值，供 Ella 的 KNOWN_RED 单测断言失败（红）。
 * Iris 转绿时替换函数体为真实状态映射逻辑，无需改动 import 路径。
 *
 * 纯 TS，不依赖 uni.* / DOM / Vue。
 */

/** wx-login 返回的业务状态 */
export type WxLoginState = 'SUCCESS' | 'NEED_BIND' | 'FAIL' | 'ERROR'

/** bind-phone 返回的业务状态
 * RETRY＝留在当前页提示可重试；REBIND＝须清掉 phoneToken 回绑定引导页重新授权（T438 补） */
export type BindPhoneState = 'BOUND' | 'NO_MATCH' | 'CONFLICT' | 'RETRY' | 'REBIND'

/** 状态机解析结果 */
export interface AuthStateResult {
  /** 业务状态枚举值 */
  state: WxLoginState | BindPhoneState
  /** 目标页面路由（空字符串表示不跳转，仅弹提示） */
  targetPage: string
  /** 提示文案（FAIL/ERROR 等非跳转态使用） */
  message?: string
}

/**
 * 解析 wx-login 接口 code → 状态 + 去向页面。
 * PRD §7A.1.1：0→SUCCESS，10601→NEED_BIND，10001/401→FAIL，10502/502→ERROR。
 * T434 补：10401 → FAIL。后端 wxLogin 在 jscode2session 回业务错误时统一产出
 * HTTP 401 + 10401（handler.go:661 / model.CodeUnauthorized），PRD §7A.1.1 通用约束
 * 要求「凭证错误与网络失败分别提示」，故不得与未知码共用服务异常兜底句。
 */
export function resolveWxLoginResult(code: number): AuthStateResult {
  switch (code) {
    case 0:
      return { state: 'SUCCESS', targetPage: '/pages/monitor/index' }
    case 10601:
      return { state: 'NEED_BIND', targetPage: '/pages/login/bind' }
    case 10401:
      return {
        state: 'FAIL',
        targetPage: '',
        message: '授权信息已失效，请重新登录',
      }
    case 10001:
    case 401:
      return {
        state: 'FAIL',
        targetPage: '',
        message: '登录失败，请联系门诊工作人员',
      }
    case 10502:
    case 502:
      return {
        state: 'ERROR',
        targetPage: '',
        message: '微信服务暂不可用，请稍后重试',
      }
    default:
      return {
        state: 'ERROR',
        targetPage: '',
        message: '服务异常，请稍后重试',
      }
  }
}

/**
 * 解析 bind-phone 接口 code → 状态 + 去向页面。
 * PRD §7A.1.1：0→BOUND，10602→NO_MATCH，10603→CONFLICT，
 * 10604→提示重新授权手机号（留在原页可重试），10605→提示后回绑定引导页。
 * T438 修：default 原返回 NO_MATCH，而调用方三个页面都按 state 分发，
 * bind.vue / conflict.vue 的 case 'NO_MATCH' 会把未识别业务码误跳「未匹配」页
 * （对患者断言「没查到你的档案」），且该分支自带的 message 永远读不到。
 * 未识别码不得占用任何会跳转的业务态 ⇒ 改为不跳转的 RETRY。
 */
export function resolveBindPhoneResult(code: number): AuthStateResult {
  switch (code) {
    case 0:
      return { state: 'BOUND', targetPage: '/pages/monitor/index' }
    case 10602:
      return { state: 'NO_MATCH', targetPage: '/pages/login/no-match' }
    case 10603:
      return { state: 'CONFLICT', targetPage: '/pages/login/conflict' }
    case 10604:
      return {
        state: 'RETRY',
        targetPage: '',
        message: '授权信息已失效，请重新授权手机号',
      }
    case 10605:
      return {
        state: 'REBIND',
        targetPage: '/pages/login/bind',
        message: '操作已过期，请重新绑定',
      }
    default:
      return {
        state: 'RETRY',
        targetPage: '',
        message: '绑定失败，请稍后重试',
      }
  }
}
