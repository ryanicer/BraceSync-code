import { expect, type Page, type Locator } from '@playwright/test'

/**
 * 技师端 e2e 公共工具（T027 tech-miniapp mock 数据基线）
 *
 * 选择器约定：tech-miniapp 的 <input class="form-input"> 在 H5 下被 uni-app
 * 渲染为 <uni-input class="form-input"> 包裹的原生 <input>。
 * 原生 input 可能不在 DOM 中（shadow DOM 或动态渲染），使用 evaluate 兜底。
 */

export const TECH_PHONE = '13900000001' // T041 播种技师账号（T037 联调验证通过）
export const MOCK_TECH_TOKEN = 'mock-tech-token-ci-001'
export const MOCK_TECH_ID = 'TECH_CI_001'
export const MOCK_DEVICE_ID = 'PRS-ML05-RC-001'

/** uni-app H5 hash 路由直达（技师端无 tabBar） */
export const techRoutes = {
  login: '/#/pages/login/index',
  bind: '/#/pages/bind/index',
  matrix: '/#/pages/matrix/index',
  saveBaseline: '/#/pages/save-baseline/index',
  wifiConfig: '/#/pages/wifi-config/index',
  records: '/#/pages/records/index',
  complete: '/#/pages/complete/index',
} as const

/**
 * uni-app H5 输入填充：uni-input 内部原生 input 可能不在 CSS 可达的 DOM 中，
 * 优先尝试原生 input 定位，不可用时通过 evaluate + querySelector 兜底。
 */
export async function fillTechInput(locator: Locator, value: string) {
  // 策略 1：尝试直接 fill（适用于原生 input 元素）
  const tag = await locator.evaluate(el => el.tagName.toLowerCase()).catch(() => '')
  if (tag === 'input' || tag === 'textarea') {
    await locator.fill(value, { force: true })
    await expect(locator).toHaveValue(value)
    return
  }
  // 策略 2：uni-input 组件 → 查找内部原生 input
  const innerInput = locator.locator('input')
  const innerCount = await innerInput.count()
  if (innerCount > 0) {
    await innerInput.first().fill(value, { force: true })
    await expect(innerInput.first()).toHaveValue(value)
    return
  }
  // 策略 3：通过 evaluate 直接操作 DOM
  await locator.evaluate((el, val) => {
    const input = el.querySelector('input') || el.shadowRoot?.querySelector('input')
    if (input) {
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype, 'value'
      )?.set
      nativeInputValueSetter?.call(input, val)
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
    }
  }, value)
}

/**
 * 强制 Mock 技师登录（登录态注入）
 * Key 参照 apps/tech-miniapp/src/utils/token.ts
 *
 * 为什么用 addInitScript 而不是 goto 后 evaluate：
 * bind/matrix 页 onMounted 有登录守卫（未登录 reLaunch 到 login），
 * 且 pinia authStore 的 isLoggedIn 只在初始化时读一次。若先加载页面
 * 再写 token，store 已初始化为 false，写 token 不会更新，守卫仍拦截。
 * addInitScript 在页面任何脚本执行前注入 localStorage（H5 端
 * uni.setStorageSync 底层即 localStorage，key 原样），确保 app 初始化时读到。
 * @param page Playwright Page 实例
 */
export async function forceTechLoginMock(page: Page) {
  await page.addInitScript(({ token, id }) => {
    try {
      localStorage.setItem('bracesync_tech_token', token)
      localStorage.setItem('bracesync_tech_id', id)
    } catch {
      // 忽略非浏览器上下文异常
    }
  }, { token: MOCK_TECH_TOKEN, id: MOCK_TECH_ID })
  await page.goto(techRoutes.bind) // 加载即已登录
}

/**
 * 执行真实登录流程（仅用于本地验证登录页 UI，CI 跳过）
 * @description 对齐 T037 独立登录页交互：导航/login → 填手机号/密码 → 勾选协议 → 点击登录 → 跳转 bind
 * @param page Playwright Page 实例
 */
export async function doTechRealLogin(page: Page) {
  await page.goto(techRoutes.login)
  
  // 填充手机号（第一个 .input-field）
  const phoneInput = page.locator('.input-field').first()
  await fillTechInput(phoneInput, TECH_PHONE)
  
  // 填充密码（第二个 .input-field）
  const pwdInput = page.locator('.input-field').nth(1)
  await fillTechInput(pwdInput, 'admin123') // T041 播种密码
  
  // 勾选协议（必须）
  await page.locator('.agree-row .checkbox').click()
  
  // 点击登录按钮
  await page.locator('.btn-primary').click()
  
  // 断言跳转至 bind 页
  await page.waitForURL('**/pages/bind/**', { timeout: 10_000 })
  await expect(page.getByText('扫码绑定')).toBeVisible({ timeout: 10_000 })
}



/** uni.showToast 渲染为 uni-toast 自定义元素 */
export function uniToast(page: Page, text: string | RegExp) {
  return page.locator('uni-toast').filter({ hasText: text })
}

/** bind 页自实现 toast（非 uni.showToast，v-if 控制 .toast/.toast-text） */
export function bindToast(page: Page) {
  return {
    root: page.locator('.toast'),
    text: page.locator('.toast-text'),
  }
}

/** uni.showModal 渲染为 uni-modal 自定义元素 */
export function uniModal(page: Page) {
  const m = page.locator('uni-modal')
  return {
    root: m,
    cancel: m.locator('.uni-modal__btn_default'),
    confirm: m.locator('.uni-modal__btn_primary'),
  }
}


