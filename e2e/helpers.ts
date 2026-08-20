import { expect, type Page, type Locator } from '@playwright/test'

/**
 * 患者端 e2e 公共工具（T016 mock 数据基线）
 *
 * 选择器约定：T016 未加 data-testid（红线：不改 apps/patient-miniapp 业务代码），
 * 全部基于 class + 文案定位。Iris 后续补 data-testid 时在此统一迁移。
 */

export const TEST_PHONE = '13800138000'
export const TEST_SMS_CODE = '1234'
export const HOTSPOT_NAME = 'PRS-ML05-RC-001'

/** uni-app H5 hash 路由直达（tab 页用 switchTab 语义，普通页 navigateTo） */
export const routes = {
  login: '/#/pages/login/index',
  monitor: '/#/pages/monitor/index',
  history: '/#/pages/history/index',
  device: '/#/pages/device/index',
  wifiSetup: '/#/pages/wifi-setup/index',
} as const

/** 登录页元素（uni-app H5：placeholder 为覆盖层 div，需定位 uni-input 内层原生 input） */
export const loginPage = (page: Page) => ({
  phone: page.locator('uni-input.input-field:not(.input-sms) input').first(),
  smsCode: page.locator('uni-input.input-sms input').first(),
  smsBtn: page.locator('.sms-btn').first(),
  checkbox: page.locator('.checkbox').first(),
  loginBtn: page.locator('.btn-primary').first(),
})

/** 完成一次 mock 登录并等待跳转到 monitor */
export async function doLogin(page: Page) {
  await page.goto(routes.login)
  const el = loginPage(page)
  await fillUniInput(el.phone, TEST_PHONE)
  await fillUniInput(el.smsCode, TEST_SMS_CODE)
  await expect(el.checkbox).toHaveClass(/checkbox-checked/)
  await el.loginBtn.click()
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
}

/** uni.showToast 的 H5 实现为 uni-toast 自定义元素 */
export function toast(page: Page, text: string | RegExp) {
  return page.locator('uni-toast').filter({ hasText: text })
}

/** uni.showModal 的 H5 实现为 uni-modal 自定义元素，按钮为 __btn_default/__btn_primary */
export function modal(page: Page) {
  const m = page.locator('uni-modal')
  return {
    root: m,
    cancel: m.locator('.uni-modal__btn_default'),
    confirm: m.locator('.uni-modal__btn_primary'),
  }
}

/**
 * uni-app H5 输入填充：原生 input 被渲染为高度≈0（placeholder 为覆盖层 div），
 * Playwright 可见性检查过不去，用 force fill + 值断言保证 v-model 生效。
 */
export async function fillUniInput(input: Locator, value: string) {
  await input.fill(value, { force: true })
  await expect(input).toHaveValue(value)
}

/** tabBar（uni-tabbar）内按文案点击，用于 tab 页切换 */
export async function switchTabBy(page: Page, text: string) {
  await page.locator('uni-tabbar').getByText(text, { exact: true }).click()
}
