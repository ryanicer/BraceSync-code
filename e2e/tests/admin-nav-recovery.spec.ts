import { test, expect, type Page } from '@playwright/test'
import { adminRoutes, ADMIN_MOUNT, isLoginPath } from '../admin-helpers'

/**
 * T355 登录后不跳转（路由 chunk 拉取失败 → 导航静默中止）回归
 *
 * 卡面现场：ops_admin 登录偶发停在 /login、无任何报错，重跑即通。
 * 排查结论：登录请求成功、token 已落库、错误分支必弹 toast（api/loginWithPassword 全路径 throw），
 * 唯一「静默停住」的代码路径是懒加载路由 chunk 拉取失败——vue-router 会中止本次导航且默认无人声张。
 * 本地 dev server 与线上一致地把 pages/<页>/index.vue 作为独立模块请求，
 * 故 page.route 掐掉这一发请求即可在门禁里复现「点登录 → 停在登录页」，无需等 staging。
 *
 * 三条用例的判别力（都在未修复版上判红，见卡片交件说明）：
 * ① 复现卡面症状 + 修复后落地；② 换页同路径恢复；③ 一直失败时只重试一次（防环）。
 */

/** 目标页 chunk（排除 vite 的 ?vue&type=style 子请求，它不是导航所需的模块） */
const chunkUrl = (dir: string) => new RegExp(`pages/${dir}/index\\.vue(\\?|$)`)

/**
 * 掐掉某页 chunk 的前 n 次请求（n=Infinity 则一直掐），返回计数器与错误日志。
 * blocked 用于反假绿：请求真被中止过，用例才有意义。
 */
function sabotageChunk(page: Page, dir: string, times: number) {
  const state = { requested: 0, blocked: 0, routerErrors: [] as string[] }
  page.on('console', (msg) => {
    if (msg.type() === 'error' && msg.text().includes('[router] 导航失败')) {
      state.routerErrors.push(msg.text().replace(/\s+/g, ' '))
    }
  })
  page.route('**/*', (route) => {
    const url = route.request().url()
    if (!chunkUrl(dir).test(url)) return route.continue()
    state.requested += 1
    if (state.blocked < times) {
      state.blocked += 1
      return route.abort()
    }
    return route.continue()
  })
  return state
}

test.describe('登录后导航恢复（T355）', () => {
  test('登录成功但目标页 chunk 拉不到：不再停在登录页，整页重试后落到落地页', async ({ page }) => {
    await page.goto(adminRoutes.login)
    // dashboard 是 admin 的登录后落地页——只掐第一发（deferred 语义：恢复后的整页加载放行）
    const boom = sabotageChunk(page, 'dashboard', 1)

    await page.locator('.login-form input[type="password"]').fill('mock-password')
    await page.locator('.login-form').getByRole('button', { name: '登录' }).click()

    await page.waitForURL((url) => !isLoginPath(url.pathname), { timeout: 20_000 })
    expect(new URL(page.url()).pathname).toBe(`${ADMIN_MOUNT}/dashboard`)
    await expect(page.locator('.kpi-card')).not.toHaveCount(0, { timeout: 15_000 })

    expect(boom.blocked, '未掐到 chunk = 用例没跑到目标路径').toBe(1)
    expect(boom.routerErrors.length, '静默失败必须留下 console 痕迹（可观测性）').toBeGreaterThanOrEqual(1)
  })

  test('菜单换页 chunk 拉不到：整页恢复后落到目标页，表格数据正常', async ({ page }) => {
    await page.goto(adminRoutes.login)
    await page.locator('.login-form input[type="password"]').fill('mock-password')
    await page.locator('.login-form').getByRole('button', { name: '登录' }).click()
    await page.waitForURL((url) => !isLoginPath(url.pathname), { timeout: 15_000 })

    const boom = sabotageChunk(page, 'patients', 1)
    await page.locator('.el-menu .el-menu-item', { hasText: '患者管理' }).click()

    await expect(page).toHaveURL(new RegExp(`${ADMIN_MOUNT}/patients$`), { timeout: 20_000 })
    await expect(page.locator('.el-table__body-wrapper tbody tr').first()).toBeVisible({ timeout: 15_000 })
    expect(boom.blocked).toBe(1)
  })

  test('chunk 持续拉不到：只整页重试一次，不形成刷新死循环', async ({ page }) => {
    await page.goto(adminRoutes.login)
    await page.locator('.login-form input[type="password"]').fill('mock-password')
    await page.locator('.login-form').getByRole('button', { name: '登录' }).click()
    await page.waitForURL((url) => !isLoginPath(url.pathname), { timeout: 15_000 })

    const boom = sabotageChunk(page, 'patients', Infinity)
    await page.locator('.el-menu .el-menu-item', { hasText: '患者管理' }).click()

    // 恢复用的整页加载再失败一次 ⇒ 防环标志命中，停在目标页不再刷新
    await expect(page).toHaveURL(new RegExp(`${ADMIN_MOUNT}/patients$`), { timeout: 20_000 })
    await page.waitForTimeout(4_000)
    // 恰发 2 次 = SPA 内一次 + 整页恢复一次；3 次以上即死循环，1 次则恢复未生效
    expect(boom.requested, `chunk 请求次数异常：${boom.requested}`).toBe(2)
  })
})
