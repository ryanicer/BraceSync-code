// T144 交付② P1：真实后端冒烟 —— 登录 → 列表接口返回非空 → 页面渲染 ≥1 条
//
// T549（2026-10-04）修「列表断言恒假 + 缺凭证静默 skip 假绿」两格：
//   · page.request 是与页面对应的 APIRequestContext，只共享 Cookie，不会自动带上
//     存在 localStorage 里的管理端 JWT（前端的 Authorization 头是 axios 拦截器加的，
//     只管页面自己的请求）。修复前这发裸 GET /api/v1/admin/patients 恒 401 ⇒
//     resp.ok() 恒假（CI run 37133134111 日志原文见 docs/tasks/ella/T549-evidence/）。
//     现在登录成功后从 localStorage 取回 JWT，显式写进 Authorization 头。
//   · CI 里缺凭证不再 skip 而是判红并明示缺哪个变量（静默跳过的绿不是证据，
//     口径对齐 real-helpers 的 T506 凭据门）；本地不设任何凭据变量时沿用 seed 默认口令。
//
// 复用 admin-web E2E 稳定选择器（Element Plus .el-table__body-wrapper tbody tr 等），
// 登录走真实分支（POST /api/v1/auth/login：.login-form 用户名/密码）。
import { test, expect, type Page } from '@playwright/test'
import { isLoginPath, DEFAULT_REAL_USERNAME, realPassword, getAuthToken, realRoutes } from './real-helpers'

const IN_CI = process.env.CI === 'true'
const USERNAME = process.env.ADMIN_USERNAME || DEFAULT_REAL_USERNAME

/**
 * 口令取值链（T549）：ADMIN_PASSWORD → E2E_REAL_PASSWORD（realPassword 的凭据门）。
 * CI 里两级都没有即抛错判红，明示缺哪个变量；不 skip、也不回退到源码常量。
 * 注意 ADMIN_PASSWORD 与 ops_admin 成对 —— 只配 ADMIN_PASSWORD 不配 ADMIN_USERNAME
 * 是本 job 现役的形状（warning 步就是按 ADMIN_USERNAME 探测的），不会错配。
 */
function smokePassword(): string {
  const injected = process.env.ADMIN_PASSWORD ?? ''
  if (injected) {
    console.log(`[e2e-real][contract-smoke] 口令来自 ADMIN_PASSWORD 注入（长度 ${injected.length}，值不落日志）`)
    return injected
  }
  return realPassword('contract-smoke')
}

/**
 * 目标地址门（T549）：CI 里缺 E2E_STAGING_URL 判红，不许回落到 localhost:2080 报「通过」。
 * 判据放在用例运行时而不是 playwright.config.ts 的模块加载期 —— ci-e2e-real-static.yml
 * 会在 CI 里对那份配置跑 `--list`（那个 job 不注入 E2E_STAGING_URL），加载期一抛就变成
 * 「静态门禁红」，把缺变量这件事报到了错误的 job 上。
 */
function assertStagingTarget(): void {
  if (IN_CI && !process.env.E2E_STAGING_URL) {
    throw new Error(
      '[e2e-real][contract-smoke] 缺 E2E_STAGING_URL：CI 不允许回落到本地地址，判红而不是跳过（口径见 e2e.yml 的 E2E_STAGING_URL env 块）',
    )
  }
}

/** admin-web 真实登录（对应用户名/密码双输入，按钮文案含空格的「登 录」） */
async function adminLoginReal(page: Page): Promise<void> {
  // T336：前端以 /admin/ 为 base 构建，登录页在挂载点内（根路径 /login 会被 nginx 302 到 /admin/）
  await page.goto(realRoutes.login)
  await page.locator('.login-form input[autocomplete="username"]').fill(USERNAME)
  await page.locator('.login-form input[autocomplete="current-password"]').fill(smokePassword())
  await page.locator('.login-form').getByRole('button', { name: /登\s*录/ }).click()
  await page.waitForURL((url) => !isLoginPath(url.pathname), { timeout: 15_000 })
}

/** 表格行（Element Plus el-table body 行） */
function tableRows(page: Page) {
  return page.locator('.el-table__body-wrapper tbody tr')
}

// T549 前这里的条件是「缺凭证就 skip」——CI 里造成「1 skipped」的恒绿假面。
// 现在凭据门（smokePassword）在 CI 里缺值直接抛，skip 支路只保留给本地无凭据自验。
test.skip(!IN_CI && !process.env.ADMIN_USERNAME && !process.env.ADMIN_PASSWORD && !process.env.E2E_REAL_PASSWORD,
  '本地未设任何凭据变量（ADMIN_USERNAME/ADMIN_PASSWORD/E2E_REAL_PASSWORD），本用例跳过；CI 不适用本回退')

test('真实后端冒烟：登录→列表接口返回非空→页面渲染≥1条', async ({ page }) => {
  // 0) 地址门先于凭据门：CI 缺 E2E_STAGING_URL 时不许打本地回落地址报绿
  assertStagingTarget()

  // 1) 真实后端登录
  await adminLoginReal(page)

  // 2) 目标列表接口断言「返回非空」：GET /api/v1/admin/patients 分页体 data.list
  //    T549：必须显式带 Authorization —— 取 localStorage 里的 admin_token（与
  //    tests/05/08 等真实用例同一姿势），漏带就是恒 401 的假面断言。
  const token = await getAuthToken(page)
  expect(
    token,
    '登录成功后 localStorage 应存有管理端 JWT（键 admin_token）；取不到则认证姿势已变，判红而不是放行',
  ).toBeTruthy()
  const resp = await page.request.get('/api/v1/admin/patients?page=1&pageSize=10', {
    headers: { Authorization: `Bearer ${token}` },
  })
  expect(resp.status(), '登录+鉴权后列表接口应 200（401=Authorization 头又丢了，见 T549）').toBe(200)
  const body = await resp.json().catch(() => null)
  expect(body).not.toBeNull()
  expect(body.code).toBe(0)
  // 契约字段（PaginatedResponse.list）必须存在且非空 —— 否则即为「后端 200 但空」类问题
  expect(Array.isArray(body.data?.list)).toBe(true)
  expect(body.data.list.length).toBeGreaterThanOrEqual(1)

  // 3) 页面渲染断言「≥1 条」（T144 原意；旧 toHaveCount(1) 把「至少」写成了「恰好」，
  //    staging 多一行 seed 就会恒假，与 2) 同属断言形状缺陷，一并订成 ≥1）
  await page.goto(realRoutes.patients)
  await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
  expect(await tableRows(page).count()).toBeGreaterThanOrEqual(1)
})