import { test, expect } from '@playwright/test'
import { techRoutes, forceTechLoginMock, uniModal, MOCK_TECH_TOKEN } from '../tech-helpers'

/**
 * tech-home 页（T433 缺陷四 · PM 2026-09-27 21:22 裁定并入本卡：「含登录态退出按钮」）
 *
 * 改前全端没有任何退出通道（Alice T428 走查附带观察：文本普查与元素计数双零），
 * 技师在同一台机器上换人登录只能清浏览器存储。
 * 稿面 docs/design/tech/home.html 也没有这个控件 —— 本格的依据是那条裁定，不是稿面。
 */

test.beforeEach(async ({ page }) => {
  await forceTechLoginMock(page)
  await page.goto(techRoutes.home)
})

test('首页有退出登录入口', async ({ page }) => {
  await expect(page.getByText('开始工作')).toBeVisible()
  await expect(page.locator('.logout-btn')).toHaveText('退出登录')
})

test('确认退出后清本地凭据并回登录页', async ({ page }) => {
  await page.locator('.logout-btn').click()
  const modal = uniModal(page)
  await expect(modal.root).toContainText('确定要退出登录吗？')
  await modal.confirm.click()

  // 落回登录页。不按 URL 断言：uni-app H5 把 pages.json 首页（login）映射成 '#/'，
  // reLaunch 后地址栏读到的是 '#/' 而不是 '#/pages/login/index'，按页面内容认。
  await expect(page.locator('.input-field')).toHaveCount(2)
  const token = await page.evaluate(() => localStorage.getItem('bracesync_tech_token'))
  expect(token).toBeNull()
})

test('取消退出不清凭据', async ({ page }) => {
  await page.locator('.logout-btn').click()
  const modal = uniModal(page)
  await modal.cancel.click()
  await expect(page.locator('.logout-btn')).toBeVisible()
  const token = await page.evaluate(() => localStorage.getItem('bracesync_tech_token'))
  expect(token).toBe(MOCK_TECH_TOKEN)
})
