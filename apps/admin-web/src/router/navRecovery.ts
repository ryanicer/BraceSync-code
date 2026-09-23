// T355：登录后「跳转不落地」的跳转可靠性收口（只做可靠性，不改登录 UX / 错误文案口径）。
//
// 背景：admin-web 所有业务页都是懒加载 chunk（router/index.ts 的 `() => import(...)`）。
// 登录成功后 redirectAfterLogin push 到目标页会触发该 chunk 的动态 import；一旦这次
// 网络取 chunk 失败（冷首载竞态 / 部署后 chunk hash 换代命中旧 index），vue-router 会
// 静默中断这次导航——页面停在 /login 且没有任何 ElMessage。现场两条独立记录都是
// 「第一次失败、重跑即通」，正是这个「首载取不到 chunk、重试时已缓存」的特征。
//
// 抽成纯模块（不 import vue-router / 不碰 DOM）是必须的：本包在 CI 的 Node18 下
// 直接测路由实例很脆，判据落纯逻辑单测、装配在 router/index.ts（与 alertPageAccess 同法）。

/** vue-router 懒加载导航失败时的错误文案特征（跨浏览器）。 */
const CHUNK_LOAD_ERROR_RE =
  /(dynamically imported module|error loading dynamically imported module|importing a module script failed|loading (css )?chunk|failed to fetch dynamically)/i

/**
 * 是否「路由块加载失败」这一类可安全重试的导航错误。
 * 只认动态 import / chunk 拉取失败；普通运行时异常不在此列（不该靠 reload 掩盖真 bug）。
 */
export function isChunkLoadError(error: unknown): boolean {
  if (!error) return false
  const msg = typeof error === 'string' ? error : String((error as Error).message ?? error)
  return CHUNK_LOAD_ERROR_RE.test(msg)
}

/** 会话内记「最近一次为哪个目标路径做过一次恢复重载」的 key。 */
export const NAV_RECOVER_KEY = 'admin_nav_recover'

type SessionStore = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

/**
 * 单次防环：同一目标路径只在「上一次成功导航之前」允许恢复重载一次。
 * 返回 true=可以重载（并占位），false=该目标刚重载过一次仍失败，别再循环。
 * 用注入的 store 以便单测；生产传 sessionStorage。
 */
export function shouldRecoverTo(store: SessionStore, targetPath: string): boolean {
  if (store.getItem(NAV_RECOVER_KEY) === targetPath) return false
  store.setItem(NAV_RECOVER_KEY, targetPath)
  return true
}

/** 成功落地后清占位，使后续独立失败仍各有一次恢复额度。 */
export function clearRecoverFlag(store: SessionStore): void {
  store.removeItem(NAV_RECOVER_KEY)
}
