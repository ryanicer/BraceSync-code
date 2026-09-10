import { test, expect } from '@playwright/test'
import { techRoutes, forceTechLoginMock } from '../tech-helpers'

/**
 * tech-complete 页：安装完成摘要 + 快捷入口
 * 对齐 T089 V2.1 complete 页：移除电子签名/已签署，新增数据可达性状态
 */

test.beforeEach(async ({ page }) => {
  await forceTechLoginMock(page)
  await page.goto(techRoutes.complete)
})

test('页面显示安装完成标题', async ({ page }) => {
  await expect(page.locator('.success-title')).toBeVisible()
  await expect(page.locator('.success-title')).toHaveText('安装完成')
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
  // T089: 电子签名已移除，新增数据可达性
  await expect(page.getByText('数据可达性')).toBeVisible()
})

test('状态标签显示', async ({ page }) => {
  // 基线状态始终显示已保存
  await expect(page.locator('.status-badge', { hasText: '已保存' })).toBeVisible()
  // T089: 数据可达性 badge（默认待验证）
  await expect(page.locator('.status-badge', { hasText: '待验证' })).toBeVisible()
})

test('查看安装记录跳转', async ({ page }) => {
  await page.locator('.action-btn', { hasText: '查看安装记录' }).click()
  await expect(page).toHaveURL(/pages\/records/, { timeout: 10_000 })
  await expect(page.getByText('安装记录').first()).toBeVisible()
})

test('继续安装下一台跳转', async ({ page }) => {
  await page.locator('.action-btn', { hasText: '继续安装下一台' }).click()
  await expect(page).not.toHaveURL(/pages\/complete/, { timeout: 10_000 })
})
