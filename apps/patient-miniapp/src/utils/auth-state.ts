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

/** bind-phone 返回的业务状态 */
export type BindPhoneState = 'BOUND' | 'NO_MATCH' | 'CONFLICT'

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
 */
export function resolveWxLoginResult(_code: number): AuthStateResult {
  // STUB: 固定返回错误占位值，使单测断言失败（红）。Iris 转绿时实现真实映射。
  return {
    state: 'ERROR',
    targetPage: '',
    message: 'STUB: 未实现',
  }
}

/**
 * 解析 bind-phone 接口 code → 状态 + 去向页面。
 * PRD §7A.1.1：0→BOUND，10602→NO_MATCH，10603→CONFLICT。
 */
export function resolveBindPhoneResult(_code: number): AuthStateResult {
  // STUB: 固定返回错误占位值，使单测断言失败（红）。Iris 转绿时实现真实映射。
  return {
    state: 'CONFLICT',
    targetPage: '',
    message: 'STUB: 未实现',
  }
}
