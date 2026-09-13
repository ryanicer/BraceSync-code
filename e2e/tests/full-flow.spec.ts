import { test, expect } from '@playwright/test'
import { switchTabBy, fillUniInput, HOTSPOT_NAME, loginPage, setupPatientE2E } from '../helpers'

/**
 * T080: 全链路 - 微信登录契约版（T074 迁移）
 * 登录流程：协议勾选 → 微信按钮点击 → POST /api/v1/patient/wx-login
 * T074 真实模式基建：test 顶部注册 route mock（断言体不动）
 */
test('患者端核心全链路：微信登录到配网成功', async ({ page }) => {
  test.setTimeout(120_000)
  await setupPatientE2E(page, { withLogin: false })

  // ===== 1. login (WeChat contract) =====
  await page.goto('/#/pages/login/index')
  const el = loginPage(page)
  
  // agreed 默认 true → 直接点击微信按钮
  await el.wechatBtn.click()
  
  // 成功路径走自定义 toast (login/index.vue L83-86: <view class="toast">
  // 失败路径才走 uni.showToast，所以这里用 .toast-text 断言
  await expect(page.locator('.toast-text').filter({ hasText: /登录成功/ })).toBeVisible({ timeout: 5000 })
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })

  // ===== 2. monitor：热力图 + 点选 + 日/周切换 =====
  await expect(page.locator('.sensor-grid .grid-cell')).toHaveCount(20)
  await page.locator('.grid-cell', { has: page.locator('.cell-id', { hasText: 'P01' }) }).click()
  await expect(page.locator('.hero-label')).toContainText('P01 · 当前压力值')
  await page.locator('.segmented .seg-btn', { hasText: '周' }).click()
  await expect(page.locator('.trend-section .section-title')).toContainText('本周压力趋势')

  // ===== 3. history：tabBar 切换 + 双 Tab =====
  await switchTabBy(page, '异常监测')
  await page.waitForURL('**/pages/history/**', { timeout: 10_000 })
  await expect(page.locator('.wearing-row')).toHaveCount(15)
  await page.locator('.segmented .seg-btn', { hasText: '压力异常' }).click()
  await expect(page.locator('.p-group')).toHaveCount(7)

  // ===== 4. device：tabBar 切换 + 设备卡片 =====
  await switchTabBy(page, '设备管理')
  await page.waitForURL('**/pages/device/**', { timeout: 10_000 })
  await expect(page.locator('.device-name')).toHaveText(HOTSPOT_NAME)

  // ===== 5. wifi-setup：T192 设计稿链路 01→02→03→04→05 =====
  await page.locator('.action-btn', { hasText: '开始添加设备' }).click()
  await page.waitForURL('**/pages/wifi-setup/**', { timeout: 10_000 })

  // 01-entry：前置检查页
  await expect(page.getByText('配置前准备')).toBeVisible()
  await page.getByText('开始配置家庭 WiFi').click()

  // 02-scan：BLE 近场发现，列表只出 BSYNC- 设备
  const deviceItem = page.locator('.device-item').first()
  await expect(deviceItem).toBeVisible({ timeout: 5_000 })
  await expect(deviceItem).toContainText('BSYNC-')
  await deviceItem.click()

  // 03-connect：建立连接后才允许填凭据
  await expect(page.getByText('连接中')).toBeVisible()
  await expect(page.getByText('已连接')).toBeVisible({ timeout: 10_000 })
  await page.getByText('连接成功，下一步').click()
  await expect(page.getByText('家庭 WiFi 名称')).toBeVisible()
  await fillUniInput(page.locator('uni-input.input-field').first().locator('input'), 'My_Custom_WiFi')
  await fillUniInput(page.locator('uni-input.input-field').nth(1).locator('input'), 'secret123')
  await page.getByText('开始配网').click()

  // 04-progress：四步清单（状态 0/1/2/3 的患者口径）
  await expect(page.locator('.step-list .step')).toHaveCount(4)

  // 05-success
  await expect(page.locator('uni-page-body').getByText('配网成功')).toBeVisible({ timeout: 30_000 })

  // ===== 6. 按设计稿「返回设备管理」回设备页 =====
  await page.getByText('返回设备管理').click()
  await page.waitForURL('**/pages/device/**', { timeout: 10_000 })
  await expect(page.locator('.device-card')).toBeVisible()
})
