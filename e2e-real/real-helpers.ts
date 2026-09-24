import { expect, type Page, type Locator } from '@playwright/test'
// gotoMenu / isLoginPath 在本文件下方要被直接调用，故必须真 import 一份——
// 下面那串 `export { ... } from '../e2e/admin-helpers'` 只转发给消费者，不产生本地绑定。
import { gotoMenu, isLoginPath } from '../e2e/admin-helpers'

/*
 * ⚠️ 本文件为「真实模式」E2E 专用 helper（T053）。
 * 禁止使用 e2e/admin-helpers.ts 中的 adminLogin()（mock 角色下拉登录）。
 * 登录必须使用本文件提供的 realLogin()（用户名 + 密码 → 真实接口）。
 *
 * 真实 staging 预置账号（T051 seed）：
 *   ops_admin / admin123   （运营，全权限）
 */

// ─────────────────────────────────────────────────────────────
// Re-export 通用 selector helper（直接复用 mock admin-helpers）
// 注意：不再 re-export adminRoutes（mock 根路径，真实模式用下方 realRoutes —— 带 /admin/ 前缀，Nginx strip 后 router 见根路径）
// ─────────────────────────────────────────────────────────────
export {
  pickSelectOption,
  tableRows,
  menuItems,
  gotoMenu,
  adminMessage,
  topBarUserName,
  adminLogout,
  adminLogout as realLogout,
  isLoginPath,
  ADMIN_MOUNT as REAL_MOUNT,
} from '../e2e/admin-helpers'

// ─────────────────────────────────────────────────────────────
// 真实模式「staging 路由」全量常量（前端挂在 Nginx /admin/ 下）
//
// 挂载点契约（T336）：admin-web 以 vite base=/admin/ 构建，vue-router 的 history base 取同一
// 前缀（createWebHistory(import.meta.env.BASE_URL)），路由表内部仍是根路径（/patients），
// 浏览器地址是 ${base}patients（/admin/patients）。所以 realRoutes 带 /admin/ 前缀是对的。
//
// 历史口径变更（留档，避免又被改回去）：
//   T279 复跑实测（2026-09-21，staging 当时是 root-base 构建）：
//     /admin/patients、/admin/settings → 回落到 /dashboard
//     /patients、/settings            → 打回 /login?redirect=/dashboard
//   ⇒ 那时「深链只能到登录页或数据概览，进具体页必须登录 → 点侧边栏」（见 gotoMenuAndWaitTable）。
//   那是 root-base 构建挂在 /admin/ 下的必然结果，不是 nginx 的问题（它的 try_files 一直回 index.html）。
//   T336 把前端 base 改成 /admin/ 后深链/刷新/登录后回原页恢复；带标记的部署守卫用例见
//   tests/01-login.spec.ts 的「挂载点与深链」——staging 未部署该构建前按 post-deploy 跳过。
// ─────────────────────────────────────────────────────────────
export const realRoutes = {
  login: '/admin/login',
  dashboard: '/admin/dashboard',
  monitor: '/admin/monitor',
  patients: '/admin/patients',
  teams: '/admin/teams',
  devices: '/admin/devices',
  alerts: '/admin/alerts',
  communication: '/admin/communication',
  orthosisLog: '/admin/orthosis-log',
  installRecords: '/admin/install-records',
  technicians: '/admin/technicians',
  roles: '/admin/roles',
  settings: '/admin/settings',
  forbidden: '/admin/403',
} as const
export type RealRoutesKey = keyof typeof realRoutes

// ─────────────────────────────────────────────────────────────
// 资源命名前缀 + 唯一命名工具（写操作可重放，避免污染 seed）
// ─────────────────────────────────────────────────────────────
export const E2E_PATIENT_NAME_PREFIX = 'T053测试'
export const E2E_TEAM_NAME_PREFIX = 'T053团队'
export const E2E_REPLY_PREFIX = 'T053回复'

/**
 * 生成带时间戳的唯一资源名（6 位秒级后缀，staging 单人测试足够唯一）。
 *   uniqueName(E2E_TEAM_NAME_PREFIX) → "T053团队-123456"
 */
export function uniqueName(prefix: string): string {
  const ts = Date.now().toString().slice(-6)
  return `${prefix}-${ts}`
}

// ─────────────────────────────────────────────────────────────
// 真实模式登录：用户名 + 密码（不是 mock 角色下拉）
// ─────────────────────────────────────────────────────────────

/** 默认 staging 运营账号（T051 seed） */
export const DEFAULT_REAL_USERNAME = 'ops_admin'
export const DEFAULT_REAL_PASSWORD = 'admin123'

/**
 * 把浏览器发出的接口请求打到 stdout（T304）。
 *
 * 为什么要有：CI 里「job 绿」和「真的打了 staging」是两件事——历史上 BASE_URL 是个不存在的
 * 占位域名，用例全 skip / 打空地址也会报绿。这行日志让 run 日志能直接 grep 到
 * `GET http://<staging>/api/v1/...`，证明真实模式确实跑了真环境。
 * 只打方法 + URL（不打 header / body，避免带出 Authorization 与患者字段值）。
 */
export function logRealRequests(page: Page, tag: string): void {
  page.on('request', (req) => {
    const url = req.url()
    if (url.includes('/api/')) console.log(`[e2e-real][${tag}] ${req.method()} ${url}`)
  })
}

/**
 * 真实模式登录（USE_MOCK=false 下的 login 页表单）。
 * 对齐 apps/admin-web/src/pages/login/index.vue 「v-else 真实模式」结构：
 *   - 用户名框：.login-form 下第一个非 password input（el-input 包装 input）
 *   - 密码框：.login-form input[type="password"]
 *   - 登录按钮：文案「登 录」（中间有空格，对齐 T052 实跑成功案例）
 */
export async function realLogin(
  page: Page,
  username: string = DEFAULT_REAL_USERNAME,
  password: string = DEFAULT_REAL_PASSWORD,
): Promise<void> {
  logRealRequests(page, 'login')
  await page.goto(realRoutes.login, { waitUntil: 'domcontentloaded' })
  // 等待登录卡片渲染
  await expect(page.locator('.login-card')).toBeVisible({ timeout: 15_000 })
  await submitRealLoginForm(page, username, password)
}

/**
 * 在「已经停在登录页」的表单上填凭据并提交（不 goto —— T336 深链用例要保住
 * 登录页 URL 上的 redirect 参数，重新 goto 就等于换了个目标页，回填验不准）。
 */
export async function submitRealLoginForm(
  page: Page,
  username: string = DEFAULT_REAL_USERNAME,
  password: string = DEFAULT_REAL_PASSWORD,
): Promise<void> {
  // 用户名：.login-form 下「未带 type=password」的第一个可输入 input
  const usernameInput = page.locator('.login-form input:not([type="password"])').first()
  const passwordInput = page.locator('.login-form input[type="password"]')
  const loginBtn = page
    .locator('.login-form')
    .getByRole('button', { name: '登 录' })

  await expect(usernameInput).toBeVisible()
  await expect(passwordInput).toBeVisible()
  await expect(loginBtn).toBeVisible()

  // 清空前先点击聚焦，再 fill（避免 el-input 残留 value）
  await usernameInput.click()
  await usernameInput.fill('')
  await usernameInput.fill(username)

  await passwordInput.click()
  await passwordInput.fill('')
  await passwordInput.fill(password)

  await loginBtn.click()

  // 登录成功：离开登录页（T336 后浏览器地址是 /admin/dashboard；旧构建是根路径 /dashboard，
  // 故判定写成「路径尾部是 login」而不是 startsWith('/login')——带挂载前缀时也成立）
  // 失败也会变 URL，但这里用 waitForURL 非登录页路径 + 同时用 ElMessage 兜底）
  try {
    await page.waitForURL((url) => !isLoginPath(url.pathname), { timeout: 20_000 })
  } catch {
    // 兜底：如果被 redirect 回 /login（账号异常），不抛，由上层断言判断
  }
}

// ─────────────────────────────────────────────────────────────
// 更多辅助工具
// ─────────────────────────────────────────────────────────────

/** localStorage key（T052 实跑验证：admin_token + admin_user） */
export const LS_TOKEN_KEY = 'admin_token'
export const LS_USER_KEY = 'admin_user'

/** 从 localStorage 取 JWT token（用于 page.request 直接调 API 清理资源） */
export async function getAuthToken(page: Page): Promise<string | null> {
  return page.evaluate((k) => localStorage.getItem(k), LS_TOKEN_KEY)
}

/**
 * 等待表格首行加载完成（列表通用）。
 *
 * `scope` 必传当且仅当该页有第二张也会出数的表（T358）：不带作用域时，条件是
 * 「页面里任意一张表的任意一行可见」，同页另一张表先返回就会误判为已就绪，
 * 而用例读的是自己那张卡的行数 —— 于是读到 0 行判红。
 */
export async function waitForTableLoaded(page: Page, scope?: Locator): Promise<void> {
  const root = scope ?? page
  await expect(root.locator('.el-table__body-wrapper tbody tr').first()).toBeVisible({
    timeout: 20_000,
  })
}

/**
 * 点侧边栏菜单进入某页，并等「本页」表格出首行。
 *
 * 为什么不能沿用 `gotoMenu()` + 裸等 `.el-table__body-wrapper tbody tr` 可见：
 * 上一页（数据概览）自带团队/医生两张排行表，点击菜单后那一瞬旧 DOM 仍在，
 * 旧表格的行会被当成本页「加载完成」信号，于是本页表格还没出数就开始断言
 * （T279 实跑 5.1 / 5.2 / 7.1 / 7.2 四条因此读到 0 行）。
 * 先按 URL 落位、再等行，才是本页的就绪信号。
 *
 * T358：URL 落位只堵住了「上一页残留」这一维，没堵住作用域那一维。
 * T289 给患者管理页加了第二张表（批量患者-团队绑定）后，两张表各发各的请求
 * （列表 pageSize=10 / 批量卡 pageSize=100），谁先返回谁就满足全局等待条件，
 * 批量卡先返回时 5.1 仍读到 0 行 ⇒ 同一条竞态从作用域维度漏了回来。
 * 故调用方一旦断言的是某张卡内的表，就把那张卡作为 `tableScope` 传进来。
 */
export async function gotoMenuAndWaitTable(
  page: Page,
  title: string,
  routePath: string,
  tableScope?: Locator,
): Promise<void> {
  await gotoMenu(page, title)
  await expect(page).toHaveURL(new RegExp(`/${routePath}$`), { timeout: 15_000 })
  await waitForTableLoaded(page, tableScope)
}

/** 取所有表格行中可见的 tag 文本（用于状态存在性验证） */
export async function getAllTagTexts(scope: Locator | Page): Promise<string[]> {
  // Page 与 Locator 都有 .locator()，无需再分支到 body（分支写法会被 TS 收窄成 never）
  return scope.locator('.el-tag').allTextContents()
}

/**
 * T365 · 改写「实时快照」响应的 route handler 收口：只第一条请求走异步取包，
 * 之后的轮询一律拿缓存包同步出口。
 *
 * 换掉的是旧写法「每次拦截都 await route.fetch() 再 fulfill」。监控页每 2s 轮询同一端点，
 * 于是用例收尾时可能仍有 handler 挂在那次 fetch 上；等 fetch 回来时该 route 已被 Playwright
 * 处置掉，fulfill 就抛 "route.fulfill: Route is already handled!"，并被记到本条用例名下
 * （CI 实测 4 个 attempt 红 2 次；本地探针「点完刷新不等断言就收尾」100% 复现同一句）。
 * 收尾那发 page.unrouteAll({ behavior: 'wait' }) 拦不住它 —— 它等的是 handler 返回，
 * 而 route 被处置恰好发生在 handler 还悬着的那段时间里。
 *
 * 第一条请求为什么可以留 await：它必然被用例的可见断言等到（断言读的就是它的产物），
 * 不会跨到收尾；handler 里此后不再有 await 网络，窗口从「一次公网往返」缩到「同一次调用内」。
 *
 * 边界（探针实测，写给后来人）：缓存还冷时那条 handler 仍跨一次 await —— 一条「连自己的首包
 * 都不等就收尾」的用例照样会撞上同一句报错。所以接了这个守卫的用例必须保留「等可见数据到位」
 * 的断言，别把它换成裸等时长。
 *
 * mutate 只在填缓存那一次执行 —— 帧时刻因此被钉死，与本文件顶部对拦截的口径一致。
 */
export async function stubRealtimeSnapshot(
  page: Page,
  mutate: (data: Record<string, unknown>) => void,
): Promise<() => number> {
  let cached: { status: number; body: string } | null = null
  let served = 0
  await page.route('**/api/v1/patients/*/realtime', async (route) => {
    if (cached === null) {
      const res = await route.fetch()
      let body: { data?: Record<string, unknown> }
      try {
        body = await res.json()
      } catch {
        await route.fulfill({ response: res })
        return
      }
      if (body?.data) mutate(body.data)
      cached = { status: res.status(), body: JSON.stringify(body) }
    }
    served++
    await route.fulfill({ status: cached.status, contentType: 'application/json', body: cached.body })
  })
  return () => served
}
