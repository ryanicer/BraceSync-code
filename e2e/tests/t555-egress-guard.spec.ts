import { test, expect } from '@playwright/test'
import { routes, setupPatientE2E } from '../helpers'

/**
 * T555 · 出网止血 —— 拦截判据自检（验收尺 3）
 *
 * 1) 白名单之外的 /api/v1/ 请求必须被 abort：这条断言在拦截退回旧形态（route.fallback 走真实网络）时变红，
 *    所以它证明「拦截真会咬」，不是恒通过。
 * 2) 白名单之内的请求仍能被 mock 消费：反证拦截没有一把掐死全部，mock 套件本身还活着。
 *
 * 两条都走同源相对路径，任何情况下都不会把请求打到真实后端域名。
 */

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: false })
  await page.goto(routes.login)
})

test('T555.1-非白名单 /api/v1/ 请求被 abort（不落真实网络）', async ({ page }) => {
  const outcome = await page.evaluate(async () => {
    try {
      const res = await fetch('/api/v1/t555-not-whitelisted', { method: 'POST' })
      return `resolved:${res.status}`
    } catch {
      return 'aborted'
    }
  })
  expect(outcome).toBe('aborted')
})

test('T555.2-白名单内写腿仍由 mock 消费（拦截不是全拦）', async ({ page }) => {
  const outcome = await page.evaluate(async () => {
    try {
      const res = await fetch('/api/v1/patient/wx-login', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ code: 't555-probe' }),
      })
      const body = await res.json()
      return `http=${res.status} code=${body.code}`
    } catch {
      return 'aborted'
    }
  })
  expect(outcome).toBe('http=200 code=0')
})
