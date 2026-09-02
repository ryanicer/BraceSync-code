import { test, expect } from '@playwright/test'
import { routes, modal, toast, fillUniInput, HOTSPOT_NAME, setupPatientE2E } from '../helpers'

/**
 * device 页：设备卡片、配网步骤入口、WiFi 输入、解绑确认弹窗
 * mock 设备：PRS-ML05-RC-001（online）
 * T074 真实模式基建：beforeEach 顶部注入登录态 + route 拦截
 */

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await page.goto(routes.device)
  await expect(page.getByText('设备管理').first()).toBeVisible()
})

test('设备卡片渲染（mock 设备在线）', async ({ page }) => {
  await expect(page.locator('.device-card')).toBeVisible()
  await expect(page.locator('.device-name')).toHaveText(HOTSPOT_NAME)
  await expect(page.locator('.device-status-text')).toContainText('已添加 · 在线')
})

test('添加流程步骤展示（4 步）', async ({ page }) => {
  await expect(page.locator('.step-row')).toHaveCount(4)
  await expect(page.locator('.step-text', { hasText: '打开手机蓝牙' })).toBeVisible()
  await expect(page.locator('.step-text', { hasText: '等待配对完成' })).toBeVisible()
})

test('WiFi 网络信息输入', async ({ page }) => {
  const ssid = page.locator('.input-group', { hasText: 'SSID' }).locator('input')
  const pwd = page.locator('.input-group', { hasText: '密码' }).locator('input')
  await fillUniInput(ssid, 'Home_WiFi_5G')
  await fillUniInput(pwd, 'secret123')
})

test('配网步骤入口跳转 wifi-setup', async ({ page }) => {
  await page.locator('.action-btn', { hasText: '开始添加设备' }).click()
  await page.waitForURL('**/pages/wifi-setup/**', { timeout: 10_000 })
  await expect(page.getByText('WiFi 配网')).toBeVisible()
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
