import { expect, type Page, type Locator, type Route } from '@playwright/test'
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

// T502：登录耗时判据与 mock 线同一份实现，真实层用例从这里取，不复制第二套阈值
export { LOGIN_BUDGET_MS, timedLogin, expectLoginWithinBudget } from '../e2e/login-timing'

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
  abnormalReport: '/admin/abnormal-report',
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
/**
 * 本地自验用的 seed 默认口令（值出自 scripts/db/seed 的模拟账号）。
 * T506：CI 不再读它 —— 管理端口令一律经 E2E_REAL_PASSWORD 注入，见 realPassword 的凭据门。
 */
export const DEFAULT_REAL_PASSWORD = 'admin123'

/** 管理端口令的注入变量名（CI 侧同名 Secrets 项在 e2e.yml 的 e2e-real-staging job 注入） */
export const REAL_PASSWORD_ENV = 'E2E_REAL_PASSWORD'

/**
 * 凭据门（T506 乙案），姿势对齐 23-chain-b-technician.spec.ts 的 requireTechCredentials（丙案）：
 * 缺凭据即抛、不 skip、只打印长度、正文写清 CI 该在哪注入 —— 静默跳过的绿不是证据。
 *
 * 为什么 CI 不许回退到源码常量：回退就等于把「这一轮测的是哪一份口令」变成不可知，
 * 口令被改之后照样一片红，而红的是下游断言（见 credentialDriftMessage 那三种面貌）。
 * 本地不设该变量时沿用 DEFAULT_REAL_PASSWORD，否则无 env 的开发自验会全判红（范围 1 允许）。
 */
export function realPassword(why: string): string {
  const injected = process.env[REAL_PASSWORD_ENV] ?? ''
  if (injected) {
    console.log(
      `[e2e-real][凭据门] ${why}: 口令来自 ${REAL_PASSWORD_ENV} 注入（长度 ${injected.length}，值不落日志）`,
    )
    return injected
  }
  if (process.env.CI === 'true') {
    throw new Error(
      `凭据未注入（${why}）：CI 里 ${REAL_PASSWORD_ENV} 未设置或为空，按 T506 口径这里判红而不是回退到源码常量。\n` +
        `修法：1) 在 BraceSync-code 仓库 Settings → Secrets and variables → Actions 配 ${REAL_PASSWORD_ENV}，` +
        `值 = staging 现役管理端口令；2) 确认 .github/workflows/e2e.yml 的 e2e-real-staging job env 块把它注入进来；` +
        `3) 复位类动作走 T504 那条线（staging 四纪律），不要改用例判据去迁就现网口令。`,
    )
  }
  console.log(
    `[e2e-real][凭据门] ${why}: 未设 ${REAL_PASSWORD_ENV}，本地自验沿用 seed 默认口令（长度 ${DEFAULT_REAL_PASSWORD.length}）。CI 不适用本回退。`,
  )
  return DEFAULT_REAL_PASSWORD
}

/**
 * 凭据漂移的显式判语（T506）。
 *
 * 背景（T502 实测）：staging 上 doctor_li 口令被改之后，同一件事在 CI 里露出三副面貌 ——
 * 「ops_admin/doctor_li 登录应 200 实收 401」「应拿到 doctor 的 JWT」「侧栏元素未找到」，
 * 全部长得像代码回归，没有一处说「这是凭据」。这里把 401 当场定性，并给出可执行的自查三步。
 */
export function credentialDriftMessage(username: string, status: number, password: string): string {
  return (
    `凭据漂移（不是代码回归）：${username} 用当前口令登录 staging 实收 HTTP ${status}。` +
    `口令长度 ${password.length}（值不落日志）。\n` +
    `自查：1) 该账号在 staging 的现口令是否被改过（管理端重置、本人自助改密都会在审计留痕里落行）；` +
    `2) CI 的 Secrets（本地则同名环境变量）${REAL_PASSWORD_ENV} 是否配置且与现值一致；` +
    `3) 复位后重跑本用例即转绿 —— 判据本身不许放宽。`
  )
}

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
 * T565 D1：登录分段计时（只打点，不改等待语义）。
 *
 * 为什么要有：timedLogin 只给「整次登录多少毫秒」，拆不出这几十秒花在哪一步。
 * CI 实跑日志里 POST /api/v1/auth/login 之前已经耗掉 ~25s，接口本身只占 ~1.3s，
 * 所以嫌疑在 goto / 渲染 / 表单可交互这一段，而那段此前没有任何逐段读数。
 * 口径：每个 await 完成后记一格，收尾打一行；`+` = 本段耗时，`@` = 自 t0 累计。
 * 派发单第四节明令：不许靠调大 E2E_LOGIN_BUDGET_MS 过关，故此处不碰预算、不碰判据。
 * 失败也要有数：report() 挂在 finally 上，抛错的运行同样留下一整行分段读数。
 */
export interface LoginSegmentTimer {
  seg: (name: string, extra?: string) => void
  report: () => void
}

export function startLoginSegmentTimer(label: string): LoginSegmentTimer {
  const t0 = Date.now()
  const marks: Array<{ name: string; cum: number; extra?: string }> = []
  let reported = false
  return {
    seg(name, extra) {
      marks.push({ name, cum: Date.now() - t0, extra })
    },
    report() {
      if (reported || marks.length === 0) return
      reported = true
      let prev = 0
      const parts = marks.map((m) => {
        const delta = m.cum - prev
        prev = m.cum
        return `${m.name}=+${delta}@${m.cum}${m.extra ? `:${m.extra}` : ''}`
      })
      console.log(`[login-timing][segments] ${label} ${parts.join(' ')}（ms）`)
    },
  }
}

/**
 * 真实模式登录（USE_MOCK=false 下的 login 页表单）。
 * 对齐 apps/admin-web/src/pages/login/index.vue 「v-else 真实模式」结构：
 *   - 用户名框：.login-form 下第一个非 password input（el-input 包装 input）
 *   - 密码框：.login-form input[type="password"]
 *   - 登录按钮：文案「登 录」（中间有空格，对齐 T052 实跑成功案例）
 */
export function realLogin(
  page: Page,
  username: string = DEFAULT_REAL_USERNAME,
  password: string = realPassword('realLogin'),
): Promise<void> {
  const timer = startLoginSegmentTimer(`realLogin(${username})`)
  return realLoginBody(page, username, password, timer).finally(() => timer.report())
}

async function realLoginBody(
  page: Page,
  username: string,
  password: string,
  timer: LoginSegmentTimer,
): Promise<void> {
  logRealRequests(page, 'login')
  await page.goto(realRoutes.login, { waitUntil: 'domcontentloaded' })
  timer.seg('goto')
  // 等待登录卡片渲染
  await expect(page.locator('.login-card')).toBeVisible({ timeout: 15_000 })
  timer.seg('login-card')
  await submitRealLoginFormBody(page, username, password, timer)
}

/**
 * 在「已经停在登录页」的表单上填凭据并提交（不 goto —— T336 深链用例要保住
 * 登录页 URL 上的 redirect 参数，重新 goto 就等于换了个目标页，回填验不准）。
 */
export function submitRealLoginForm(
  page: Page,
  username: string = DEFAULT_REAL_USERNAME,
  password: string = realPassword('submitRealLoginForm'),
  timer: LoginSegmentTimer = startLoginSegmentTimer(`submitRealLoginForm(${username})`),
): Promise<void> {
  return submitRealLoginFormBody(page, username, password, timer).finally(() => timer.report())
}

async function submitRealLoginFormBody(
  page: Page,
  username: string,
  password: string,
  timer: LoginSegmentTimer,
): Promise<void> {
  // 用户名：.login-form 下「未带 type=password」的第一个可输入 input
  const usernameInput = page.locator('.login-form input:not([type="password"])').first()
  const passwordInput = page.locator('.login-form input[type="password"]')
  const loginBtn = page
    .locator('.login-form')
    .getByRole('button', { name: '登 录' })

  await expect(usernameInput).toBeVisible()
  timer.seg('user-visible')
  await expect(passwordInput).toBeVisible()
  timer.seg('pass-visible')
  await expect(loginBtn).toBeVisible()
  timer.seg('btn-visible')

  // 清空前先点击聚焦，再 fill（避免 el-input 残留 value）
  await usernameInput.click()
  timer.seg('user-click')
  await usernameInput.fill('')
  timer.seg('user-clear')
  await usernameInput.fill(username)
  timer.seg('user-fill')

  await passwordInput.click()
  timer.seg('pass-click')
  await passwordInput.fill('')
  timer.seg('pass-clear')
  await passwordInput.fill(password)
  timer.seg('pass-fill')

  // T506：先把登录接口的响应挂上观察，再点提交。登录非 200 时当场定性成「凭据漂移」——
  // 否则上层只会看到「应拿到 xxx 的 JWT」「侧栏元素未找到」这类下游噪音（T502 在 CI 里实测过三种面貌）。
  const loginResponse = page
    .waitForResponse(
      (r) => r.url().includes('/api/v1/auth/login') && r.request().method() === 'POST',
      { timeout: 20_000 },
    )
    .catch(() => null)
  timer.seg('post-armed')

  await loginBtn.click()
  timer.seg('submit-click')

  const resp = await loginResponse
  timer.seg('post-response', resp ? `http${resp.status()}` : 'none')
  if (resp && resp.status() !== 200) {
    throw new Error(credentialDriftMessage(username, resp.status(), password))
  }

  // 登录成功：离开登录页（T336 后浏览器地址是 /admin/dashboard；旧构建是根路径 /dashboard，
  // 故判定写成「路径尾部是 login」而不是 startsWith('/login')——带挂载前缀时也成立）
  // 失败也会变 URL，但这里用 waitForURL 非登录页路径 + 同时用 ElMessage 兜底）
  try {
    await page.waitForURL((url) => !isLoginPath(url.pathname), { timeout: 20_000 })
    timer.seg('left-login')
  } catch {
    // 兜底：如果被 redirect 回 /login（账号异常），不抛，由上层断言判断
    timer.seg('left-login-timeout')
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
 * T450 把上面那句「照样会撞上」收掉：冷缓存首包这一发跨 await 的窗口压不到零（ prefetch 需要
 * 具体 URL 与鉴权头，而拦截器只有通配模式，拿不到），故改为「收尾竞态 ⇒ 吞掉这一次出口」。
 * 吞的判据只有 Playwright 的三条报错原文（见 isTeardownRace），宽一格就会把真缺陷静音。
 * 同一发被处置时页面也已经不在读它了，所以吞掉不影响用例判据；served 改为「真的填出去了一发」
 * 才计数，用例末尾 realtimeServed() >= 1 那条反证锁因此仍成立（且比改前更严）。
 *
 * 第三条原文 "Test ended" 由 T450 探针补上：本地对拍（HEAD 版 helper 与本版跑同一收尾时机）里
 * 旧写法抛的是 route.fetch: Test ended.，与 04-monitor.spec.ts afterEach 记过的那句同源（那条注释
 * 当时就写过「会被判给同 worker 的下一条用例」）。判据表只留前两条 ⇒ 旧写法照样红，故这句必须进表。
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
      let res: Awaited<ReturnType<Route['fetch']>>
      try {
        res = await route.fetch()
      } catch (err) {
        if (isTeardownRace(err)) return // 收尾竞态：这一发请求已经没有读者，不 fulfill 也不抛
        throw err
      }
      let body: { data?: Record<string, unknown> }
      try {
        body = await res.json()
      } catch {
        await fulfillOrDrop(route, { response: res })
        return // 非 JSON 不建缓存，下一发轮询再取（与 T365 版一致）
      }
      if (body?.data) mutate(body.data)
      cached = { status: res.status(), body: JSON.stringify(body) }
    }
    if (await fulfillOrDrop(route, { status: cached.status, contentType: 'application/json', body: cached.body })) {
      served++
    }
  })
  return () => served
}

// isTeardownRace 只认这三类 Playwright 报错原文：route 已被处置（同一请求被别处答过）、
// 目标页面/上下文/浏览器已关、用例已结束导致的回调中止。三者都只在收尾窗口出现，
// 不是被测功能的行为。
// 🔴 别放宽成「任何异常都吞」——那会把真实的拦截器缺陷伪装成绿。
const teardownRaceMarks = ['Route is already handled', 'Target page, context or browser has been closed', 'Test ended']

function isTeardownRace(err: unknown): boolean {
  const msg = err instanceof Error ? err.message : String(err)
  return teardownRaceMarks.some((mark) => msg.includes(mark))
}

// fulfillOrDrop 填响应；被处置则返回 false（调用方据此不计数），其余异常照抛。
async function fulfillOrDrop(route: Route, opts: Parameters<Route['fulfill']>[0]): Promise<boolean> {
  try {
    await route.fulfill(opts)
    return true
  } catch (err) {
    if (isTeardownRace(err)) return false
    throw err
  }
}
