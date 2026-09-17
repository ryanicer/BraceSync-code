import { test, expect } from '@playwright/test'
import { routes, modal, toast, HOTSPOT_NAME, setupPatientE2E } from '../helpers'

/**
 * device 页：T223 后仅保留「添加新设备」卡片 + 「已添加设备」区
 * （配网交互统一走 wifi-setup 页，原 4 步引导 / WiFi 输入 / 独立按钮均已注释下线）
 * mock 设备：PRS-ML05-RC-001（online）
 */

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await page.goto(routes.device)
  await expect(page.getByText('设备管理').first()).toBeVisible()
})

test('新结构：添加新设备卡片可见', async ({ page }) => {
  await expect(page.locator('.add-card')).toBeVisible()
  await expect(page.locator('.add-title')).toHaveText('添加新设备')
  await expect(page.getByText('开始监测')).toBeVisible()
})

test('设备卡片渲染（mock 设备在线）', async ({ page }) => {
  await expect(page.locator('.device-card')).toBeVisible()
  await expect(page.locator('.device-name')).toHaveText(HOTSPOT_NAME)
  await expect(page.locator('.device-status-text')).toContainText('已添加 · 在线')
})

test('配网入口：添加新设备卡片跳转 wifi-setup', async ({ page }) => {
  await page.locator('.add-card').click()
  await page.waitForURL('**/pages/wifi-setup/**', { timeout: 10_000 })
  // T192：落地到设计稿 01-entry（前置检查页），不再是旧的「WiFi 配网」单页
  await expect(page.getByText('配置前准备')).toBeVisible()
  await expect(page.getByText('开始配置家庭 WiFi')).toBeVisible()
})

test('解绑确认弹窗：取消保留设备', async ({ page }) => {
  await page.locator('.delete-btn').click()
  const m = modal(page)
  await expect(m.root).toBeVisible()
  await expect(m.root).toContainText('确认解绑')
  await expect(m.root).toContainText('确定要解除当前设备绑定吗')
  // 取消：设备仍在
  await m.cancel.click()
  await expect(m.root).toBeHidden()
  await expect(page.locator('.device-card')).toBeVisible()
})

test('解绑确认弹窗：确认后解绑', async ({ page }) => {
  await page.locator('.delete-btn').click()
  const m = modal(page)
  await expect(m.root).toBeVisible()
  await m.confirm.click()
  await expect(toast(page, '设备已解绑')).toBeVisible()
  // 解绑后展示空态卡片
  await expect(page.locator('.empty-card')).toBeVisible()
  await expect(page.locator('.device-card')).toHaveCount(0)
})
