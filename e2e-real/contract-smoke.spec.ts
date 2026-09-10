// T144 交付② P1：真实后端冒烟 —— 登录 → 列表接口返回非空 → 页面渲染 ≥1 条
//
// 阻塞说明：本用例需真实 staging 联调环境 + 只读凭证（ADMIN_USERNAME / ADMIN_PASSWORD，找 Andy）。
// 凭证缺失时自动 skip（不进 PR 门禁、不影响提交）；具备条件后由 nightly / 手动触发跑通。
//
// 复用 admin-web E2E 稳定选择器（Element Plus .el-table__body-wrapper tbody tr 等），
// 登录走真实分支（POST /api/v1/auth/login：.login-form 用户名/密码）。
import { test, expect, type Page } from '@playwright/test'

const USERNAME = process.env.ADMIN_USERNAME
const PASSWORD = process.env.ADMIN_PASSWORD

/** admin-web 真实登录（对应用户名/密码双输入，按钮文案含空格的「登 录」） */
async function adminLoginReal(page: Page): Promise<void> {
  await page.goto('/login')
  await page.locator('.login-form input[autocomplete="username"]').fill(USERNAME)
  await page.locator('.login-form input[autocomplete="current-password"]').fill(PASSWORD)
  await page.locator('.login-form').getByRole('button', { name: /登\s*录/ }).click()
  await page.waitForURL((url) => !url.pathname.startsWith('/login'), { timeout: 15_000 })
}

/** 表格行（Element Plus el-table body 行） */
function tableRows(page: Page) {
  return page.locator('.el-table__body-wrapper tbody tr')
}

test.skip(!USERNAME || !PASSWORD, '缺少真实后端冒烟凭证（ADMIN_USERNAME/ADMIN_PASSWORD，找 Andy 提供），本用例跳过')

test('真实后端冒烟：登录→列表接口返回非空→页面渲染≥1条', async ({ page }) => {
  // 1) 真实后端登录
  await adminLoginReal(page)

  // 2) 目标列表接口断言「返回非空」：GET /api/v1/admin/patients 分页体 data.list
  const resp = await page.request.get('/api/v1/admin/patients?page=1&pageSize=10')
  expect(resp.ok()).toBeTruthy()
  const body = await resp.json().catch(() => null)
  expect(body).not.toBeNull()
  expect(body.code).toBe(0)
  // 契约字段（PaginatedResponse.list）必须存在且非空 —— 否则即为「后端 200 但空」类问题
  expect(Array.isArray(body.data?.list)).toBe(true)
  expect(body.data.list.length).toBeGreaterThanOrEqual(1)

  // 3) 页面渲染断言「≥1 条」
  await page.goto('/patients')
  await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
  await expect(tableRows(page).first()).toHaveCount(1)
})