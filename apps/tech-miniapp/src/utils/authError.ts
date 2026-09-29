/**
 * 会话失效判定与处置（T326）
 *
 * 网关对「签名不合法 / token 过期」统一返回 HTTP 401 + 信封 code 401
 * （services/gateway/cmd/server/middleware.go abortJSON），服务层自查凭据时返回 10401。
 * 业务码 40101 后端从未产出（全仓 grep 无出处），而技师端旧代码只认 40101，
 * 于是失效 token 既不清 storage 也不回登录页：页面把网关英文文案原样渲染，
 * 点「重试」永远带同一份死 token —— 死结。这里改按 HTTP 状态判，与患者端
 * apps/patient-miniapp/src/utils/request.ts 的 statusCode === 401 口径一致。
 */

export const AUTH_EXPIRED_MESSAGE = '登录已过期，请重新登录'
export const LOGIN_PAGE = '/pages/login/index'

/** T486 技师自助改密端点（豁免判据与 api/password.ts 共用这一个常量，避免两处字面量漂移） */
export const TECH_CHANGE_PASSWORD_URL = '/api/v1/tech/change-password'

/** 后端「旧口令不匹配」的信封码（user-service model.CodeInvalidCredentials，HTTP 401） */
export const CODE_OLD_PASSWORD_WRONG = 10001

export function isAuthFailure(statusCode: number, code?: number): boolean {
  if (statusCode === 401) return true
  return code === 10401 || code === 40101
}

/**
 * T486 豁免格：改密端点上「原密码填错」也是 HTTP 401（后端与登录口同形、防枚举），
 * 但它不是会话失效。不区分的话技师填错一次口令就被清 token 踢回登录页，
 * 页面那句「原密码不正确」永远渲染不到。
 *
 * 判据只用「这条端点 + 信封码 10001」，不看 HTTP 状态：
 * 会话真失效时网关回的是 code=401、服务层回 10401，两者都不会命中 10001，
 * 所以豁免不会把真失效放成不处理。
 */
export function isOldPasswordCheckFailure(url: string, code?: number): boolean {
  return url === TECH_CHANGE_PASSWORD_URL && code === CODE_OLD_PASSWORD_WRONG
}

/** 清凭据 + 提示 + 回登录页。副作用由调用方注入，便于单测断言「一定清了一定回页」 */
export function forceRelogin(deps: {
  removeToken: () => void
  toast: (msg: string) => void
  reLaunch: (url: string) => void
}): void {
  deps.removeToken()
  deps.toast(AUTH_EXPIRED_MESSAGE)
  deps.reLaunch(LOGIN_PAGE)
}
