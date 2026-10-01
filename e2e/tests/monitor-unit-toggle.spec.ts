import { test, expect, type Page } from '@playwright/test'
import { routes, setupPatientE2E } from '../helpers'
import { realtimeSnapshot, E2E_AREA_CM2 } from '../../apps/patient-miniapp/tests/e2e/fixtures/patient'

/**
 * T513 患者端实时监测页双单位（N / kPa）—— 真实浏览器链路（uni-app H5 + route 拦截）。
 *
 * 为什么单开一份而不是塞进 monitor.spec.ts：本页每条用例都要自己控制「快照里有没有面积」
 * 与「切了几次档」，和 monitor 基线用例的通用 realtime 拦截混在一起会互相遮蔽（同 T322 的
 * route LIFO 坑）。CI 侧它是独立文件 ⇒ 独立计数，判据不会因合并而丢失。
 *
 * 判据映射（派发单 §五）：
 *   1 稿面一致  → 二档分段在标题行右侧、默认 N 档读数逐字未变、副文案按裁定 c 只在 N 档出现
 *   2 后端无新增单位请求 → 下面「切档零请求」条（含正对照：确有请求被记录到）
 *   3 空值两横线 → 面积未配置（contactAreaCm2=null ⇒ 下发 pressureKpa/heatmapMaxKpa 均 null）时
 *                  kPa 档全 --、提示行逐字；N 档不受影响
 *   选型 A 不自算换算 → 已知格 P12＝42.18 N ⇒ 659 kPa（夹具按 model.KpaFromN 下发，页面只读字段）
 *
 * 🔴 微信小程序真机腿本文件覆盖不到（H5 与小程序同为 chromium 内核渲染，但存储与 rpx 换算
 * 不同栈），交件里按「真机未验」登记。
 */

const unitSegBtn = (page: Page, u: 'N' | 'kPa') =>
  page.locator('.segmented.unit-seg .seg-btn', { hasText: u })
const cellValues = (page: Page) =>
  page.locator('.sensor-grid .grid-cell .cell-value')
const cellStyles = (page: Page) =>
  page.locator('.sensor-grid .grid-cell').evaluateAll((els) => els.map((e) => e.getAttribute('style') ?? ''))

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await page.goto(routes.monitor)
  await expect(cellValues(page)).toHaveCount(20)
})

test('默认 N 档：二档分段落在 N，格子读数与改动前同形（PRD 四.2）', async ({ page }) => {
  await expect(unitSegBtn(page, 'N')).toHaveClass(/seg-active/)
  await expect(unitSegBtn(page, 'kPa')).not.toHaveClass(/seg-active/)
  await expect(page.locator('.hero-unit')).toHaveText('N')
  const vals = await cellValues(page).allTextContents()
  expect(vals).toHaveLength(20)
  for (const v of vals) expect(v).toMatch(/^\d+\.\d{2}$/)
  // 副文案「20-60N 正常范围」在 N 档出现（它在 kPa 档会被整块隐藏，见下一条）
  await expect(page.locator('.hero-meta-left')).toBeVisible()
  await expect(page.locator('.hero-meta-left .meta-text')).toHaveText('20-60N 正常范围')
})

test('切到 kPa：数字与单位字母同时换，配色与判档不跟着换（四.9②③）', async ({ page }) => {
  const nVals = await cellValues(page).allTextContents()
  const colorsBefore = await cellStyles(page)

  await unitSegBtn(page, 'kPa').click()
  await expect(unitSegBtn(page, 'kPa')).toHaveClass(/seg-active/)
  await expect(page.locator('.hero-unit')).toHaveText('kPa')

  const kVals = await cellValues(page).allTextContents()
  for (const v of kVals) expect(v).toMatch(/^\d+$/)
  // 反证：kPa 数字必须真的不同于 N 数字，否则「逐格等长」这种断言是恒真的
  expect(kVals).not.toEqual(nVals)
  expect(E2E_AREA_CM2).toBe(0.64) // 夹具面积，659 的推导基准
  // 面积 0.64 下的已知格：P12＝42.18 N ⇒ 后端下发 659 kPa（页面不自算）
  await expect(
    page.locator('.grid-cell', { has: page.locator('.cell-id', { hasText: 'P12' }) }).locator('.cell-value'),
  ).toHaveText('659')
  // 判档恒 N：20 格配色逐格不变
  expect(await cellStyles(page)).toEqual(colorsBefore)
  // 裁定 c：kPa 档不再出现「20-60N 正常范围」
  await expect(page.locator('.hero-meta-left')).toBeHidden()

  await unitSegBtn(page, 'N').click()
  await expect(cellValues(page).first()).toHaveText(nVals[0])
  await expect(page.locator('.hero-meta-left')).toBeVisible()
})

test('点选后的详情行两档同量：6 N ↔ 94 kPa，字母挂法按稿面（四.9②）', async ({ page }) => {
  await page.locator('.grid-cell', { has: page.locator('.cell-id', { hasText: 'P12' }) }).click()
  const detail = page.locator('.heatmap-detail')
  await expect(detail).toContainText('P12')
  await expect(detail).toHaveText('P12 · 42.18N · 阈值上限 6N')

  await unitSegBtn(page, 'kPa').click()
  await expect(detail).toHaveText('P12 · 659kPa · 阈值上限 94kPa')
})

test('fail-closed：面积未配置时 kPa 档只出 --，N 档照旧出数（五.3 禁退化 0／禁补默认面积）', async ({ page }) => {
  // 后注册先咨询（helpers 里的通用 realtime 被这条覆盖成「无面积」快照）
  await page.route(/\/api\/v1\/patients\/[^/]+\/realtime$/, (route) =>
    route.fulfill({ json: realtimeSnapshot({ areaCm2: null }) }),
  )
  await page.reload()
  await expect(cellValues(page)).toHaveCount(20)

  // N 档不依赖面积：读数照旧、也不许冒出提示行
  const nVals = await cellValues(page).allTextContents()
  for (const v of nVals) expect(v).toMatch(/^\d+\.\d{2}$/)
  await expect(page.locator('.heatmap-area-warn')).toHaveCount(0)

  await unitSegBtn(page, 'kPa').click()
  const kVals = await cellValues(page).allTextContents()
  expect(kVals).toEqual(Array.from({ length: 20 }, () => '--'))
  expect(kVals).not.toContain('0')
  expect(kVals).not.toContain('0.0')
  await expect(page.locator('.hero-number')).toHaveText('--')
  await expect(page.locator('.heatmap-area-warn')).toHaveText('未配置面积，暂无法换算')
})

test('切档零请求：全程没有「单位」相关端点，档位与派生值都取自同一快照（判据 2）', async ({ page }) => {
  const urls: string[] = []
  page.on('request', (req) => {
    if (new URL(req.url()).pathname.startsWith('/api/')) urls.push(req.url())
  })
  await page.reload()
  await expect(cellValues(page)).toHaveCount(20)

  const before = urls.length
  // 正对照：确有快照请求被记录到，否则「没有单位请求」是空集假绿
  expect(before).toBeGreaterThan(0)
  expect(urls.some((u) => /\/realtime$/.test(new URL(u).pathname))).toBe(true)

  await unitSegBtn(page, 'kPa').click()
  await unitSegBtn(page, 'N').click()
  await unitSegBtn(page, 'kPa').click()
  await expect(page.locator('.hero-unit')).toHaveText('kPa')

  expect(urls).toHaveLength(before)
  expect(urls.filter((u) => /unit/i.test(u))).toEqual([])
})

test('裁定 e：档位记忆只走本地，重新进页仍恢复 kPa（无服务端写入）', async ({ page }) => {
  const writes: string[] = []
  page.on('request', (req) => {
    const m = req.method().toUpperCase()
    if (m !== 'GET' && new URL(req.url()).pathname.startsWith('/api/')) writes.push(`${m} ${req.url()}`)
  })

  await unitSegBtn(page, 'kPa').click()
  await expect(unitSegBtn(page, 'kPa')).toHaveClass(/seg-active/)

  await page.reload()
  await expect(cellValues(page)).toHaveCount(20)
  await expect(unitSegBtn(page, 'kPa')).toHaveClass(/seg-active/)
  await expect(page.locator('.hero-unit')).toHaveText('kPa')
  expect(writes).toEqual([])
})
