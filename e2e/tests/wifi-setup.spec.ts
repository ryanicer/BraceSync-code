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

test('4 步引导展示', async ({ page }) => {
  await expect(page.locator('.steps .step')).toHaveCount(4)
  for (const label of ['设备开机', '连接设备热点', '配置WiFi', '完成绑定']) {
    await expect(page.locator('.steps .step-label', { hasText: label })).toBeVisible()
  }
  // 当前处于第 1 步
  await expect(page.locator('.steps .step').first()).toHaveClass(/step-active/)
})

test('设备热点名称展示与复制', async ({ context, page }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await expect(page.locator('.hotspot-name')).toHaveText(HOTSPOT_NAME)
  await page.locator('.btn-copy').click()
  await expect
    .poll(async () => page.evaluate(() => navigator.clipboard.readText()), { timeout: 5_000 })
    .toBe(HOTSPOT_NAME)
})

test('WiFi 列表选择与手动输入', async ({ page }) => {
  // 默认选中 Home_WiFi_5G
  await expect(page.locator('.wifi-item', { hasText: 'Home_WiFi_5G' })).toHaveClass(/wifi-selected/)
  // 切换选中 Office_Net
  await page.locator('.wifi-item', { hasText: 'Office_Net' }).click()
  await expect(page.locator('.wifi-item', { hasText: 'Office_Net' })).toHaveClass(/wifi-selected/)
  await expect(page.locator('.wifi-item', { hasText: 'Home_WiFi_5G' })).not.toHaveClass(/wifi-selected/)
  // 手动输入 SSID
  const manual = page.locator('.manual-wifi input')
  await fillUniInput(manual, 'My_Custom_WiFi')
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

  await fillUniInput(page.locator('uni-input.password-input input'), 'secret123')
  await page.locator('.btn-primary', { hasText: '开始配网' }).click()

  // Step 3：配置进度卡片
  await expect(page.locator('.progress-card')).toBeVisible()
  await expect(page.locator('.progress-text')).toHaveText('正在配置 WiFi...')
  await expect(page.locator('.progress-step')).toContainText('连接设备热点')

  // 进度推进到完成（约 6s）后进入 Step 4 成功页（再 +0.8s）
  await expect(page.locator('.success-text')).toHaveText('配网成功!', { timeout: 30_000 })
  await expect(page.locator('.success-sub')).toContainText(HOTSPOT_NAME)

  // 返回设备管理（有真实历史栈，navigateBack 生效）
  await page.locator('.btn-primary', { hasText: '返回设备管理' }).click()
  await page.waitForURL('**/pages/device/**', { timeout: 20_000 })
})
