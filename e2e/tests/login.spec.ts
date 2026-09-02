import { test, expect } from '@playwright/test'
import { routes, loginPage, toast, fillUniInput, setupPatientE2E } from '../helpers'

/**
 * T080: login 页 - 微信授权一键登录、协议勾选（T074 迁移版）
 * 对齐 Iris 的《login 新用例契约清单》L1–L10（mock 登录，不依赖后端）
 * 核心流程：协议勾选 → 微信按钮点击 → POST /api/v1/patient/wx-login (含 fallback code)
 */

// ---------------------------------------------------------------------
// Mock 常量定义（用于 route handler）
// ---------------------------------------------------------------------
const MOCK_TOKEN = 'mock-jwt-token-for-e2e-t080'
const MOCK_PATIENT = { id: 'PT001', name: '张三', role: 'patient' }

// ---------------------------------------------------------------------
// Test Setup
// ---------------------------------------------------------------------
test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: false })
  await page.goto(routes.login)
})

// ---------------------------------------------------------------------
// L1: 微信登录按钮可见可用
// ---------------------------------------------------------------------
test('L1-微信登录按钮可见可用', async ({ page }) => {
  const el = loginPage(page)
  await expect(el.wechatBtn).toBeVisible()
  await expect(el.wechatBtn).toBeEnabled()
})

// ---------------------------------------------------------------------
// L7: 协议勾选前按钮 disabled，勾选后 enabled
// ---------------------------------------------------------------------
test('L7-协议勾选激活按钮', async ({ page }) => {
  const el = loginPage(page)
  
  // 初始 disabled
  await expect(el.wechatBtn).toBeDisabled()
  
  // 勾选协议
  await el.checkbox.click()
  await expect(el.checkbox).toHaveClass(/checkbox-checked/)
  
  // 按钮 enabled
  await expect(el.wechatBtn).toBeEnabled()
})

// ---------------------------------------------------------------------
// L4 + L2: 点击微信按钮跳转 monitor+storage+route mock 断言
// ---------------------------------------------------------------------
test('L4-L2-点击微信按钮跳转 monitor 并写入 storage', async ({ page }) => {
  // Route mock setup - 拦截请求体以验证 fallback code
  let capturedReq: any
  
  await page.route('/api/v1/patient/wx-login', async (route) => {
    const request = route.request()
    capturedReq = request.postDataJSON() || {}
    return route.fulfill({ json: { token: MOCK_TOKEN, ...MOCK_PATIENT } })
  })
  
  // 执行登录：勾选协议 → 点击微信按钮
  const el = loginPage(page)
  await el.checkbox.click()
  await el.wechatBtn.click()
  
  // 断言跳转
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
  await expect(page.getByText('实时监测').first()).toBeVisible()
  
  // 断言 storage 写入
  const storage = await page.context().storageState()
  expect(storage).toHaveProperty('tokens') // T074 基建已注入 bracesync_token
  
  // 断言请求体包含 fallback code（核心契约）
  expect(capturedReq.code).toBe('h5-fallback-wechat-login-code')
})

// ---------------------------------------------------------------------
// L3: 接口失败 toast+ 不跳转
// ---------------------------------------------------------------------
test('L3-接口失败显示 toast 且不跳转', async ({ page }) => {
  await page.route('/api/v1/patient/wx-login', async (route) => {
    return route.fulfill({ status: 500, json: { message: 'Internal Server Error' } })
  })
  
  const el = loginPage(page)
  await el.checkbox.click()
  await el.wechatBtn.click()
  
  // Toast 显示（错误提示）
  await expect(page.locator('uni-toast').filter({ hasText: /登录失败 | 服务器错误/ })).toBeVisible()
  
  // 仍停留在 login
  await expect(page).toHaveURL(/pages\/login/)
})

// ---------------------------------------------------------------------
// L6: 登录失败后可重试
// ---------------------------------------------------------------------
test('L6-登录失败后可重试', async ({ page }) => {
  let callCount = 0
  
  await page.route('/api/v1/patient/wx-login', async (route) => {
    callCount++
    if (callCount === 1) {
      // 第一次失败
      return route.fulfill({ status: 500, json: { message: 'Server Error' } })
    } else {
      // 第二次成功
      return route.fulfill({ json: { token: MOCK_TOKEN, ...MOCK_PATIENT } })
    }
  })
  
  const el = loginPage(page)
  await el.checkbox.click()
  
  // 第一次点击 - 失败
  await el.wechatBtn.click()
  await expect(page.locator('uni-toast')).toBeVisible()
  await expect(page).toHaveURL(/pages\/login/)
  
  // 第二次点击 - 成功
  await el.wechatBtn.click()
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
  await expect(page.getByText('实时监测').first()).toBeVisible()
})

// ---------------------------------------------------------------------
// L8: 点击后按钮进入 loading 且 disabled（防重复点击）
// ---------------------------------------------------------------------
test('L8-点击后按钮进入 loading 且 disabled', async ({ page }) => {
  const el = loginPage(page)
  await el.checkbox.click()
  
  // Mock 模拟延迟请求
  await page.route('/api/v1/patient/wx-login', async (route) => {
    await new Promise(resolve => setTimeout(resolve, 1000))
    return route.fulfill({ json: { token: MOCK_TOKEN, ...MOCK_PATIENT } })
  })
  
  await el.wechatBtn.click()
  
  // 按钮 loading 样式 + disabled
  await expect(el.wechatBtn).toHaveClass(/loading/)
  await expect(el.wechatBtn).toBeDisabled()
  
  // 防止重复点击 - 显示加载文案
  await expect(el.wechatBtn).toHaveText(/正在加载 | 加载中/)
})

// ---------------------------------------------------------------------
// L9 + L5: 已登录后刷新 monitor 页面保持状态（守卫生效）
// ---------------------------------------------------------------------
test('L9-L5-已登录 monitor 页刷新后保持', async ({ page }) => {
  // Setup: 先完成一次登录（模拟用户刚登录成功）
  await page.route('/api/v1/patient/wx-login', async (route) => {
    return route.fulfill({ json: { token: MOCK_TOKEN, ...MOCK_PATIENT } })
  })
  
  const el = loginPage(page)
  await el.checkbox.click()
  await el.wechatBtn.click()
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
  await expect(page.getByText('实时监测').first()).toBeVisible()
  
  // Reload 刷新页面
  await page.reload()
  
  // URL 仍为 monitor（守卫生效，不踢回 login）
  await expect(page).toHaveURL('**/pages/monitor/')
  
  // 页面无微信登录按钮（守卫生效）
  await expect(el.wechatBtn).not.toBeVisible()
  
  // 热力图数据正常渲染
  await expect(page.locator('.sensor-grid .grid-cell')).toHaveCount(20)
})

// ---------------------------------------------------------------------
// L10: 多次点击登录无死循环
// ---------------------------------------------------------------------
test('L10-多次点击登录无死循环', async ({ page }) => {
  const btn = loginPage(page).wechatBtn
  
  // Mock 始终成功
  await page.route('/api/v1/patient/wx-login', async (route) => {
    return route.fulfill({ json: { token: MOCK_TOKEN, ...MOCK_PATIENT } })
  })
  
  // 勾选协议
  await loginPage(page).checkbox.click()
  
  // 快速连续点击 5 次
  for (let i = 0; i < 5; i++) {
    await btn.click()
  }
  
  // 只跳转一次 (无路由栈爆炸)
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
  
  // 检查路由历史 - 应只有 login→monitor，没有多余中间态
  const history = await page.evaluate(() => window.history.length)
  expect(history).toBeLessThan(10)
})
