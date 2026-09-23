import { test, expect, type Page } from '@playwright/test'
import { realRoutes, submitRealLoginForm, isLoginPath, REAL_MOUNT } from '../real-helpers'
import { requireDeployedBuild } from '../deploy-guard'

/**
 * T355 - 登录后导航恢复（真实模式 · staging）
 *
 * 卡面现场「ops_admin 登录偶发停在登录页、无报错、重跑即通」的自然复现率实测 0/30，
 * 而唯一会「静默停住」的路径是懒加载 chunk 拉取失败让 vue-router 中止导航且无人声张。
 * 本条不赌概率：在 staging 已部署包上掐掉登录后新请求的第一发 JS 模块，强制走该错误路径，
 * 验「整页重试后落到落地页」。这是 mock 用例（本地 dev server 逐模块请求）覆盖不到的
 * 产物形态——hash 命名的 assets + nginx。
 *
 * 部署守卫（T324 共享 helper）：恢复逻辑在入口 chunk 里，标记串随构建产出；
 * staging 未部署本卡构建时本条显式标 post-deploy 跳过，部署后自动转真跑。
 */

/** 已部署 bundle 里是否含标记串：只看入口 script 正文，纯存在性探测（不做业务断言） */
async function markerInDeployedBundle(page: Page, marker: string): Promise<boolean> {
  const srcs = await page.evaluate(() =>
    Array.from(document.querySelectorAll('script[src]')).map((s) => s.getAttribute('src') || ''),
  )
  for (const src of srcs) {
    const res = await page.request.get(new URL(src, page.url()).href)
    if (res.ok() && (await res.text()).includes(marker)) return true
  }
  return false
}

interface ChunkSaboteur {
  /** 开始拦截：调用后第一发「此前没见过」的 .js 请求会被中止一次 */
  arm: () => void
  /** 被中止的 URL（空串 = 一发都没掐到） */
  aborted: () => string
}

function armChunkSaboteur(page: Page): ChunkSaboteur {
  const seen = new Set<string>()
  let armed = false
  let blocked = ''
  page.on('request', (req) => {
    const url = req.url()
    if (!armed && /\.js(\?|$)/.test(url)) seen.add(url)
  })
  page.route('**/*.js', (route) => {
    const url = route.request().url()
    if (!armed || blocked || seen.has(url)) return route.continue()
    blocked = url
    return route.abort()
  })
  return { arm: () => { armed = true }, aborted: () => blocked }
}

test.describe('12-登录后导航恢复（T355）', () => {
  test('12.1 落地页 chunk 拉不到 → 整页重试一次并落到 dashboard，不停在 /login', async ({ page }) => {
    await page.goto(realRoutes.login, { waitUntil: 'domcontentloaded' })
    await expect(page.locator('.login-card')).toBeVisible({ timeout: 20_000 })
    // 登录页自身的模块请求完再登记守卫，避免把「还没落地」误判成「已部署旧包」
    await page.waitForTimeout(1500)

    await requireDeployedBuild(page, {
      marker: 'T355-nav-recovery',
      why: '要 staging 部署含导航恢复的构建（恢复逻辑在入口 chunk）',
      probe: async (p) => markerInDeployedBundle(p, 'T355-nav-recovery'),
    })

    const boom = armChunkSaboteur(page)
    boom.arm()
    await submitRealLoginForm(page)

    expect(boom.aborted(), '一发都没掐到 = 用例没走到目标路径，假绿').toBeTruthy()
    await expect(page).toHaveURL(new RegExp(`${REAL_MOUNT}/dashboard$`), { timeout: 30_000 })
    expect(isLoginPath(new URL(page.url()).pathname), '仍停在登录页 = 恢复未生效').toBe(false)
    // 落地页真的挂载完成（外壳菜单渲染出来），而不是只有地址变了
    await expect(page.locator('.el-menu')).toBeVisible({ timeout: 20_000 })
  })
})
