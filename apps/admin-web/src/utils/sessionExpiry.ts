// T357：会话失效（令牌过期 / 签名不合法）的判定与处置。
//
// 现网事实（本卡复核过，非沿用卡面口径）：
//  - 网关对鉴权失败统一返回 HTTP 401 + 信封 code 401（services/gateway/cmd/server/middleware.go abortJSON）
//  - 服务层自查凭据返回 10401（adminLogin 里读的正是它）
//  - 业务码 40101 后端从未产出：全仓仅旧 request.ts:57 一处出现它，且它排在 `if (!res.ok) throw` 之后，
//    HTTP 401 一定先在上一行抛出 ⇒ 清凭据回登录页那段是死代码（已部署产物 index-DkQpYhj8.js 里同样如此）。
//  ⇒ 处置收口在 HTTP 状态上，与 T326 技师端 apps/tech-miniapp/src/utils/authError.ts、
//    患者端 apps/patient-miniapp/src/utils/request.ts 的 statusCode === 401 口径一致。
import { removeToken } from './token'

export const AUTH_EXPIRED_MESSAGE = '登录已过期，请重新登录'

/** 会话失效判定：HTTP 401 为准，业务码 10401 / 40101 兼容保留 */
export function isAuthExpired(status: number, code?: number): boolean {
  if (status === 401) return true
  return code === 10401 || code === 40101
}

/** 浏览器地址 → router 内部路径（剥掉挂载前缀）。守卫的 redirect 参数用的就是内部路径 */
export function innerPath(baseUrl: string, pathname: string): string {
  if (!pathname.startsWith(baseUrl)) return pathname
  const rest = pathname.slice(baseUrl.length)
  return rest ? `/${rest}` : '/'
}

/**
 * 失效后整页跳转的登录页地址：必须自带挂载前缀（T336 口径：写死 /login 会被 nginx 302 到 /admin/
 * 而丢掉 redirect），并带 redirect 回原页。已在登录页时不带 redirect，避免自跳。
 */
export function expiredLoginHref(baseUrl: string, pathname: string, search: string): string {
  const base = baseUrl.endsWith('/') ? baseUrl : `${baseUrl}/`
  const login = `${base}login`
  const inner = innerPath(base, pathname)
  if (inner === '/login') return login
  const params = new URLSearchParams()
  params.set('redirect', inner + (search || ''))
  return `${login}?${params.toString()}`
}

export interface ExpiryEffects {
  removeToken: () => void
  assign: (href: string) => void
  readLocation: () => { baseUrl: string; pathname: string; search: string }
  log: (href: string) => void
}

// 一轮失效里页面常有多个并发请求同时 401：只让第一个真的清凭据 + 跳转，
// 其余静默抛错，免得反复 assign 造成重载与报错洪水（验收点 ③）。
let expiryHandled = false

/**
 * 处置一次会话失效：清凭据 + 整页回登录页（带 redirect 回原页）。
 * 返回跳转目标；本轮已处置过、或本来就在登录页（无需跳）时返回 null。
 */
export function handleAuthExpired(deps: ExpiryEffects = windowExpiryEffects): string | null {
  if (expiryHandled) return null
  expiryHandled = true
  deps.removeToken()
  const { baseUrl, pathname, search } = deps.readLocation()
  const base = baseUrl.endsWith('/') ? baseUrl : `${baseUrl}/`
  if (innerPath(base, pathname) === '/login') return null // 已在登录页：只清凭据，不自跳
  const href = expiredLoginHref(baseUrl, pathname, search)
  deps.log(href)
  deps.assign(href)
  return href
}

const windowExpiryEffects: ExpiryEffects = {
  removeToken,
  assign: (href) => window.location.assign(href),
  readLocation: () => ({
    baseUrl: import.meta.env.BASE_URL,
    pathname: window.location.pathname,
    search: window.location.search,
  }),
  // 本包无远程日志通道：控制台留一手现场。串里的 T357-auth-expiry 同时是 e2e-real
  // 部署守卫在「已部署 bundle」里做的存在性标记，删了就变成永远 skip 的假门禁。
  log: (href) => console.warn('[auth] 会话失效，清凭据回登录页 [T357-auth-expiry] →', href),
}

/** 仅测试用：复位「本轮已处置」闩，让每个用例都从干净状态起 */
export function resetAuthExpiryLatch(): void {
  expiryHandled = false
}
