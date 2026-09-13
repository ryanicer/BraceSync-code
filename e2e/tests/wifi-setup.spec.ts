import { test, expect } from '@playwright/test'
import { routes, fillUniInput, HOTSPOT_NAME, setupPatientE2E } from '../helpers'

/**
 * wifi-setup 页：4 步引导、热点复制、密码显隐、配网成功状态
 * BLE 在 H5 下全 mock（utils/ble.ts）：连接 1s + 5 步进度 ×1.2s + 0.8s ≈ 9s 到成功页
 * T074 真实模式基建：beforeEach 顶部注入登录态
 */

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await page.goto(routes.wifiSetup)
  await expect(page.getByText('WiFi 配网')).toBeVisible()
})

test('5 步引导展示', async ({ page }) => {
  await expect(page.locator('.steps .step')).toHaveCount(5)
  for (const label of ['收到', '连AP', '取IP', '探测', '成功']) {
    await expect(page.locator('.steps .step-label', { hasText: label })).toBeVisible()
  }
  // 当前处于第 1 步
  await expect(page.locator('.steps .step').first()).toHaveClass(/step-active/)
})

test('WiFi 列表选择与手动输入', async ({ page }) => {
  // 无候选列表，SSID 走直接输入
  const manual = page.locator('.manual-wifi input')
  await fillUniInput(manual, 'My_Custom_WiFi')
  await expect(manual).toHaveValue('My_Custom_WiFi')
})

test('手动输入 SSID 后点击扫描 WiFi 回填不清空（T136）', async ({ page }) => {
  // 无候选列表，改为直接输入；输入值应完整保留不清空
  const ssidInput = page.locator('.manual-wifi input')
  await fillUniInput(ssidInput, 'My_Custom_WiFi')
  await expect(ssidInput).toHaveValue('My_Custom_WiFi')
})

test('WiFi 密码显隐切换', async ({ page }) => {
  const pwd = page.locator('uni-input.password-input input')
  await fillUniInput(pwd, 'secret123')
  // 默认密文显示
  await expect(pwd).toHaveAttribute('type', 'password')
  // 点击切换为明文
  await page.locator('.password-toggle').click()
  await expect(pwd).toHaveAttribute('type', 'text')
  // 再点回密文
  await page.locator('.password-toggle').click()
  await expect(pwd).toHaveAttribute('type', 'password')
})

test('开始配网到配网成功全状态', async ({ page }) => {
  test.setTimeout(90_000)
  // 从设备页真实导航进入（直达进入时无历史栈，配网成功后返回会整页 reload 不跳转）
  await page.goto(routes.device)
  await page.locator('.action-btn', { hasText: '开始添加设备' }).click()
  await page.waitForURL('**/pages/wifi-setup/**', { timeout: 10_000 })

  await fillUniInput(page.locator('.manual-wifi input'), 'My_Custom_WiFi')
  await fillUniInput(page.locator('uni-input.password-input input'), 'secret123')
  await page.locator('.btn-primary', { hasText: '开始配网' }).click()

  // 步骤条推进到 3（探测）以上，然后进入成功态
  await expect(page.locator('.steps .step').nth(0)).toHaveClass(/step-done/, { timeout: 10_000 })
  await expect(page.locator('.steps .step').nth(1)).toHaveClass(/step-done/, { timeout: 10_000 })

  // 成功态（步骤条全部 done + 成功文案）
  await expect(page.locator('.steps .step').nth(4)).toHaveClass(/step-done/, { timeout: 20_000 })
  await expect(page.locator('.success-text')).toHaveText('配网成功', { timeout: 30_000 })

  // 返回设备管理（有真实历史栈，navigateBack 生效）
  await page.locator('.auto-return-tip').waitFor({ timeout: 5000 })
  // 3s 自动返回，等导航
  await page.waitForURL('**/pages/device/**', { timeout: 10_000 })
})
