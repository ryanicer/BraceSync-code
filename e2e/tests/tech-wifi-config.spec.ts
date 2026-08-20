import { test, expect } from '@playwright/test'
import { techRoutes, fillTechInput } from '../tech-helpers'

/**
 * tech-wifi-config 页：WiFi 配网（步骤条 + WiFi 选择 + 密码输入 + 进度 + 完成）
 * 对齐 T018 技师端 wifi-config 页功能（mock 数据，BLE 流程 H5 mock）
 */

test.beforeEach(async ({ page }) => {
  await page.goto(techRoutes.wifiConfig)
})

test('页面标题与步骤条', async ({ page }) => {
  await expect(page.getByText('WiFi 配置').first()).toBeVisible()
  await expect(page.locator('.step')).toHaveCount(4)
  await expect(page.locator('.step-label').first()).toHaveText('连接设备')
  await expect(page.locator('.step-label').last()).toHaveText('完成')
})

test('WiFi 列表显示 3 个网络', async ({ page }) => {
  await expect(page.locator('.wifi-item')).toHaveCount(3)
  await expect(page.locator('.wifi-ssid').first()).toHaveText('Home_WiFi_5G')
  await expect(page.locator('.wifi-ssid').nth(1)).toHaveText('Office_Net')
  await expect(page.locator('.wifi-ssid').nth(2)).toHaveText('Guest_WiFi')
  await expect(page.locator('.wifi-item').first()).toHaveClass(/wifi-selected/)
})

test('切换 WiFi 选择', async ({ page }) => {
  await page.locator('.wifi-item').nth(1).click()
  await expect(page.locator('.wifi-item').nth(1)).toHaveClass(/wifi-selected/)
  await expect(page.locator('.wifi-item').first()).not.toHaveClass(/wifi-selected/)
})

test('手动输入 SSID', async ({ page }) => {
  const ssidInput = page.locator('.manual-wifi uni-input.form-input').first()
  await fillTechInput(ssidInput, 'My_Custom_WiFi')
})

test('密码输入与切换显示', async ({ page }) => {
  const pwdInput = page.locator('uni-input.password-input').first()
  await fillTechInput(pwdInput, 'test1234')
  // 密码默认为隐藏模式（内部原生 input type=password）
  const innerInput = pwdInput.locator('input')
  await expect(innerInput).toHaveAttribute('type', 'password')
  // 点击切换显示密码
  await page.locator('.password-toggle').click()
  await expect(innerInput).toHaveAttribute('type', 'text')
})

test('配网全流程到成功', async ({ page }) => {
  test.setTimeout(60_000)
  await page.locator('.btn-primary', { hasText: '开始配网' }).click()
  await expect(page.locator('.progress-card')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('.progress-fill')).toBeVisible()
  await expect(page.getByText('配网成功!')).toBeVisible({ timeout: 30_000 })
  await expect(page.getByText('Home_WiFi_5G')).toBeVisible()
})
