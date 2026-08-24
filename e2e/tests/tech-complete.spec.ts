import { test, expect } from '@playwright/test'
import { techRoutes, forceTechLoginMock } from '../tech-helpers'

/**
 * tech-complete 页：安装完成摘要 + 快捷入口
 * 对齐 T018 技师端 complete 页功能（mock 数据）
 */

test.beforeEach(async ({ page }) => {
  await forceTechLoginMock(page)
  await page.goto(techRoutes.complete)
})

test('页面显示安装完成标题', async ({ page }) => {
  await expect(page.locator('.success-title')).toBeVisible()
  await expect(page.getByText('设备已成功安装并配置完毕')).toBeVisible()
  await expect(page.locator('.success-icon')).toBeVisible()
})

test('安装摘要卡片显示', async ({ page }) => {
  await expect(page.getByText('安装摘要')).toBeVisible()
  await expect(page.getByText('设备 ID')).toBeVisible()
  await expect(page.getByText('患者 ID')).toBeVisible()
  await expect(page.getByText('安装时间')).toBeVisible()
  await expect(page.getByText('基线状态')).toBeVisible()
  await expect(page.getByText('WiFi 状态')).toBeVisible()
  await expect(page.getByText('电子签名')).toBeVisible()
})

test('状态标签显示', async ({ page }) => {
  await expect(page.locator('.status-badge', { hasText: '已保存' })).toBeVisible()
  await expect(page.locator('.status-badge', { hasText: '已签署' })).toBeVisible()
})

test('查看安装记录跳转', async ({ page }) => {
  await page.locator('.action-btn', { hasText: '查看安装记录' }).click()
  // redirectTo records 页
  await expect(page).toHaveURL(/pages\/records/, { timeout: 10_000 })
  await expect(page.getByText('安装记录').first()).toBeVisible()
})

test('继续安装下一台跳转', async ({ page }) => {
  await page.locator('.action-btn', { hasText: '继续安装下一台' }).click()
  // redirectTo 在 H5 中可能导航到 bind 或回到首页（依赖 uni-app 路由栈）
  // 验证页面跳转发生（URL 改变）
  await expect(page).not.toHaveURL(/pages\/complete/, { timeout: 10_000 })
})
