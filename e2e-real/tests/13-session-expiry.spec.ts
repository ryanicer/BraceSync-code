import { test, expect, type Page } from '@playwright/test'
import {
  realRoutes, isLoginPath, submitRealLoginForm, LS_TOKEN_KEY, LS_USER_KEY, REAL_MOUNT,
} from '../real-helpers'
import { requireDeployedBuild } from '../deploy-guard'

/**
 * T357 - 令牌失效后回登录页（真实模式 · staging）
 *
 * 卡面现场「令牌失效后不跳登录页」：网关只发 HTTP 401 + 信封 code 401，而旧 request.ts
 * 的清理分支认的是业务码 40101、且排在 `if (!res.ok) throw` 之后 ⇒ 那段代码在现网产物里
 * 永不执行（已在部署包 index-DkQpYhj8.js 上核对过）。本条用 staging 真网关做一次
 * 「格式合法、签名无效」的 JWT，验处置真的发生，且 redirect 回原页符合 T336 深链口径。
 *
 * 部署守卫（T324 共享 helper）：staging 未部署本卡构建时显式标 post-deploy 跳过，部署后转真跑。
 */

/** 三段式、exp 已过期、签名随意 —— 让网关走 ParseJWT 失败分支（HTTP 401 + code 401） */
const DEAD_JWT = [
  'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9',
  'eyJzdWIiOiJET0MtMzU3UFJPQkUiLCJyb2xlIjoiYWRtaW4iLCJleHAiOjE2MDAwMDAwMDB9',
  'c3RhZ2luZy1zaWduYXR1cmUtaXMtZmFrZS0zNTctcHJvYmU',
].join('.')
const DEAD_USER = JSON.stringify({ name: 'T357探针', role: 'admin', adminId: 'DOC-357PROBE', username: 't357-probe' })

/** 已部署 bundle 里是否含标记串：入口 script + 已请求过的 .js 都探一遍（纯存在性，不做业务断言） */
async function markerInDeployedBundle(page: Page, marker: string): Promise<boolean> {
  const urls = await page.evaluate(() => {
    const fromHtml = Array.from(document.querySelectorAll('script[src]'))
      .map((s) => s.getAttribute('src') || '')
    const fromPerf = performance.getEntriesByType('resource').map((e) => e.name)
    return Array.from(new Set([...fromHtml, ...fromPerf].filter((u) => /\.js(\?|$)/.test(u))))
  })
  for (const src of urls) {
    const res = await page.request.get(new URL(src, page.url()).href)
    if (res.ok() && (await res.text()).includes(marker)) return true
  }
  return false
}

test.describe('13-令牌失效处置（T357）', () => {
  test('13.1 签名无效的令牌访问 /admin/patients：清凭据 + 跳登录页并带 redirect，且只处置一次', async ({ page }) => {
    const marks: string[] = []
    page.on('console', (msg) => {
      if (msg.text().includes('T357-auth-expiry')) marks.push(msg.text())
    })

    await page.goto(realRoutes.login, { waitUntil: 'domcontentloaded' })
    await expect(page.locator('.login-card')).toBeVisible({ timeout: 20_000 })
    // 登录页自身模块请求完再探标记，避免把「还没落地」误判成「已部署旧包」
    await page.waitForTimeout(1500)
    await requireDeployedBuild(page, {
      marker: 'T357-auth-expiry',
      why: '要 staging 部署含 401 处置的构建（判定与跳转在入口 chunk）',
      probe: async (p) => markerInDeployedBundle(p, 'T357-auth-expiry'),
    })

    await page.evaluate(
      ([token, user]) => {
        localStorage.setItem('admin_token', token)
        localStorage.setItem('admin_user', user)
      },
      [DEAD_JWT, DEAD_USER],
    )

    // 直接深链受保护页：守卫看 token+user 都在 ⇒ 放行 ⇒ 页面发真实请求 ⇒ 网关 401
    await page.goto(realRoutes.patients, { waitUntil: 'domcontentloaded' })

    // expect.poll 的谓词会撞上本次整页跳转：page.evaluate 在执行上下文被销毁时抛错，
    // 而 poll 不吞错 ⇒ 同一个 run 的两次 attempt 分别红在这行（06:20）与下一行（06:11）——
    // CI run 35926141178 实测。抛错只可能是「正在跳」，返回哨兵值继续轮询，落地后仍能读出真值。
    const readLS = (k: string) =>
      page.evaluate((key) => localStorage.getItem(key), k).catch(() => '__navigating__')

    await expect
      .poll(() => readLS(LS_TOKEN_KEY), {
        message: '失效令牌没被清掉 = 处置分支仍未执行',
        timeout: 20_000,
      })
      .toBeNull()
    await expect(page).toHaveURL(new RegExp(`${REAL_MOUNT}/login`), { timeout: 20_000 })
    // 原判据写成 isLoginPath(...).toBe(false)，与上一行「URL 必须落在 login」互斥 ⇒ 处置真的发生时
    // 必然红：staging 一部署上 T357，本条就永久红（本机 06:3x 对 staging 复现 Expected false /
    // Received true）。改按该行消息的本意出判据：离开了患者管理页。
    expect(new URL(page.url()).pathname, '仍停在患者管理页 = 未跳登录页').not.toBe(realRoutes.patients)

    // T336 深链口径：redirect 是不带挂载前缀的 router 内部路径，登录后可直接回填 push
    expect(new URL(page.url()).searchParams.get('redirect')).toBe('/patients')
    await expect(page.locator('.login-card')).toBeVisible({ timeout: 20_000 })
    expect(await page.evaluate((k) => localStorage.getItem(k), LS_USER_KEY)).toBeNull()

    // 验收点 ③：一页多发并发 401 只处置一次（旧形态是每次请求各刷一条报错）
    expect(marks.length, `失效处置打了 ${marks.length} 次：${marks.join(' | ')}`).toBe(1)
  })

  test('13.2 反证：正常登录的令牌访问同一页不被踢回登录页（处置不是无差别拦 401）', async ({ page }) => {
    await page.goto(realRoutes.login, { waitUntil: 'domcontentloaded' })
    await expect(page.locator('.login-card')).toBeVisible({ timeout: 20_000 })
    await page.waitForTimeout(1500)
    await requireDeployedBuild(page, {
      marker: 'T357-auth-expiry',
      why: '要 staging 部署含 401 处置的构建',
      probe: async (p) => markerInDeployedBundle(p, 'T357-auth-expiry'),
    })

    await submitRealLoginForm(page)
    await expect(page).toHaveURL(new RegExp(`${REAL_MOUNT}/(dashboard|patients|monitor)`), { timeout: 30_000 })

    await page.goto(realRoutes.patients, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(4000)
    expect(isLoginPath(new URL(page.url()).pathname), '正常令牌被误判失效 ⇒ 踢回了登录页').toBe(false)
    expect(await page.evaluate((k) => localStorage.getItem(k), LS_TOKEN_KEY), '正常令牌被清掉了').toBeTruthy()
  })
})
