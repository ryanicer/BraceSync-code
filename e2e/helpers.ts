import { expect, type Page, type Locator } from '@playwright/test'
import {
  ok, wxLoginResp, realtimeSnapshot, pressureRecords, wearing15,
  pressureAlerts7groups, alertsPage, unbindOk,
  E2E_TOKEN_KEY, E2E_PATIENT_ID_KEY, E2E_TOKEN, E2E_PATIENT_ID,
} from '../apps/patient-miniapp/tests/e2e/fixtures/patient'
import { URL } from 'node:url'

/**
 * 患者端 e2e 公共工具（T016 mock 数据基线 + T074 真实模式 API 拦截基建）
 *
 * 选择器约定：T016 未加 data-testid（红线：不改 apps/patient-miniapp 业务代码），
 * 全部基于 class + 文案定位。Iris 后续补 data-testid 时在此统一迁移。
 *
 * T074 基建新增：setupPatientE2E(page, { withLogin })
 *  - 页面初始化前注入 bracesync_token / bracesync_patient_id（H5 uni.storage = localStorage）
 *  - page.route 拦截 api/v1 全路径请求返回契约 fixture（realtime/records/alerts/daily-wear/unbind/wx-login）
 *  - 属于"测试基建 setup"，不触碰任何断言（断言归 Ella）
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
  wechatBtn: page.locator('.btn-wechat').first(), // T074 微信登录按钮 - FIX: was .wechat-login-btn
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

// ---------------------------------------------------------------------
// T074 真实模式基建：登录态注入 + page.route 拦截（Playwright H5）
// ---------------------------------------------------------------------
/**
 * 在每个 spec 的 beforeEach 第一行调用：
 *   test.beforeEach(async ({ page }) => {
 *     await setupPatientE2E(page, { withLogin: true })   // monitor/device/history 直接进
 *     // 或 { withLogin: false }                         // login.spec 自己测登录
 *     await page.goto(routes.monitor)
 *   })
 *
 * withLogin=true 时：addInitScript 里写入 storage（H5 端 uni.setStorageSync = localStorage），
 *                    utils/token.ts 的读取 key 严格一致。
 */
export async function setupPatientE2E(page: Page, opts: { withLogin?: boolean } = {}) {
  const withLogin = opts.withLogin ?? true

  // 1. storage 注入（在页面任何脚本前执行，保证 authStore 初始化已读登录态）
  await page.addInitScript(
    ({ tokenKey, pidKey, token, pid, login }) => {
      try {
        // uni-app H5: uni.setStorageSync 底层就是 localStorage.setItem（可直接写）
        // 若存在全局 uni 对象则优先调 uni API 保持行为一致
        const write = (k: string, v: string) => {
          try {
            // @ts-ignore
            if (typeof uni !== 'undefined' && uni.setStorageSync) { /* @ts-ignore */ uni.setStorageSync(k, v); return }
          } catch {/* ignore */}
          try { window.localStorage.setItem(k, v) } catch {/* ignore */}
        }
        if (login) {
          write(tokenKey, token)
          write(pidKey, pid)
        }
      } catch {/* ignore */}
    },
    {
      tokenKey: E2E_TOKEN_KEY,
      pidKey: E2E_PATIENT_ID_KEY,
      token: E2E_TOKEN,
      pid: E2E_PATIENT_ID,
      login: withLogin,
    },
  )

  // 2. page.route：真实模式下各页面 API 请求 → 返回 fixture
  //    匹配顺序：精确 path 命中即返回；未命中继续（避免影响未用到的端点）
  await page.route(/\/api\/v1\//, async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    const method = req.method().toUpperCase()
    const q = url.searchParams

    // ——— POST /api/v1/patient/wx-login（登录按钮用）
    if (method === 'POST' && url.pathname === '/api/v1/patient/wx-login') {
      try {
        const body = req.postDataJSON() as { code?: string } | undefined
        return route.fulfill({ json: wxLoginResp(body?.code) })
      } catch {
        return route.fulfill({ json: wxLoginResp() })
      }
    }

    // ——— GET /api/v1/patients/:patientId/realtime
    const mRealtime = url.pathname.match(/^\/api\/v1\/patients\/([^/]+)\/realtime$/)
    if (method === 'GET' && mRealtime) {
      return route.fulfill({ json: realtimeSnapshot() })
    }

    // ——— GET /api/v1/patients/:patientId/records?period=day|week|month&date=YYYY-MM-DD
    const mRecords = url.pathname.match(/^\/api\/v1\/patients\/([^/]+)\/records$/)
    if (method === 'GET' && mRecords) {
      return route.fulfill({ json: ok(pressureRecords(q.get('period') || 'day', q.get('date') || '')) })
    }

    // ——— GET /api/v1/patients/:patientId/daily-wear（history 佩戴）
    const mWear = url.pathname.match(/^\/api\/v1\/patients\/([^/]+)\/daily-wear$/)
    if (method === 'GET' && mWear) {
      return route.fulfill({ json: ok(wearing15()) })
    }

    // ——— GET /api/v1/alerts?patientId=xxx（anomaly + history 压力）
    if (method === 'GET' && url.pathname === '/api/v1/alerts') {
      const pg = parseInt(q.get('page') || '1', 10) || 1
      const psz = parseInt(q.get('pageSize') || '200', 10) || 200
      return route.fulfill({ json: alertsPage(pressureAlerts7groups(), pg, psz) })
    }

    // ——— POST /api/v1/devices/:deviceId/unbind（device 解绑）
    const mUnbind = url.pathname.match(/^\/api\/v1\/devices\/([^/]+)\/unbind$/)
    if (method === 'POST' && mUnbind) {
      return route.fulfill({ json: unbindOk() })
    }

    // 其它端点继续（走网络或 404 —— 符合"只 mock E2E 用到的端点"原则）
    await route.fallback()
  })
}

