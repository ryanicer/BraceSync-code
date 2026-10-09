import { test, expect } from '@playwright/test'
import { setupPatientE2E, ok } from '../helpers'

/**
 * T634 佩戴管理页（统计页落点）按日/按周汇总视图的 H5 实测。
 *
 * 只做 H5 渲染实测，小程序端不冒充已验（口径同 profile.spec）。
 * 夹具自带的是 2026-06/07 的历史日，落不进本自然周的槽位，所以本 spec 自己按
 * 「本周一至今日」造行并覆盖 daily-wear 路由（LIFO：后注册先咨询）。
 * 派发单第二条「切换只换维度不换数据源」在这一格是被数出来的：切档与换日之后，
 * 页面发出的 daily-wear 请求计数必须还是同一枚。
 */
const WEARING_PATH = '/#/pages/wearing/index'

const pad = (n: number) => (n < 10 ? `0${n}` : `${n}`)
const keyOf = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`

/** 与页面同一枚周界算式：周日回退 6 天，周一为 0 */
function weekMonday(now: Date): Date {
  const dow = now.getDay()
  const idx = dow === 0 ? 6 : dow - 1
  return new Date(now.getFullYear(), now.getMonth(), now.getDate() - idx)
}

function todayIndex(now: Date): number {
  const dow = now.getDay()
  return dow === 0 ? 6 : dow - 1
}

const now = new Date()
const monday = weekMonday(now)
const thisWeekKeys: string[] = []
for (let i = 0; i <= todayIndex(now); i++) {
  thisWeekKeys.push(keyOf(new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + i)))
}
// 每日一枚可辨的值：10.0、11.0……今日那枚由此可唯一反推
const hoursByKey = new Map(thisWeekKeys.map((k, i) => [k, 10 + i]))
const rowsFor = (keys: string[]) =>
  keys.map((date) => ({
    date,
    wearMinutes: Math.round((hoursByKey.get(date) as number) * 60),
    avgPressure: 28.5,
    maxPressure: 42.18,
    maxPoint: 'P12',
    frameCount: 1000 + keys.indexOf(date),
    abnormalCount: 2 + keys.indexOf(date),
  }))
// 上一周的一枚：证明按周累计不会把窗口外的行算进来
const prevWeekRow = {
  date: keyOf(new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() - 1)),
  wearMinutes: 9999,
  avgPressure: 1,
  maxPressure: 1,
  maxPoint: 'P01',
  frameCount: 1,
  abnormalCount: 1,
}

test.describe('T634 按日/按周汇总视图（H5）', () => {
  test('按日与按周共用一份取数面：切档与换日都不再发 daily-wear', async ({ page }) => {
    let wearReqCount = 0
    await setupPatientE2E(page, { withLogin: true })
    page.on('request', (r) => {
      if (/\/api\/v1\/patients\/[^/]+\/daily-wear(\?|$)/.test(r.url())) wearReqCount += 1
    })
    // 后注册先咨询：覆盖 helpers 里那份历史日夹具
    await page.route(/\/api\/v1\/patients\/[^/]+\/daily-wear/, (route) =>
      route.fulfill({ json: ok([...rowsFor(thisWeekKeys), prevWeekRow]) })
    )
    await page.goto(WEARING_PATH)
    await expect(page.locator('.section-title', { hasText: '汇总视图' })).toBeVisible()

    const initial = wearReqCount
    expect(initial, '页面自己该发的那一发取数要抓到').toBeGreaterThanOrEqual(1)

    // —— 按日档（默认）：日 chip 只到今日，读数等于今日那枚夹具值 ——
    const chips = page.locator('.day-chip')
    await expect(chips).toHaveCount(thisWeekKeys.length)
    const todayKey = keyOf(now)
    const todayHours = (hoursByKey.get(todayKey) as number).toFixed(1)
    await expect(page.locator('.stat', { hasText: '当日佩戴' })).toContainText(todayHours)
    await expect(page.locator('.day-chip-active')).toContainText(todayKey.slice(5))

    // —— 换日：读数跟着变，取数计数不变（维度换了，数据源没换）——
    if (thisWeekKeys.length > 1) {
      const otherKey = thisWeekKeys[0] // 本周一
      await page.locator('.day-chip', { hasText: otherKey.slice(5) }).click()
      await expect(page.locator('.stat', { hasText: '当日佩戴' })).toContainText(
        (hoursByKey.get(otherKey) as number).toFixed(1)
      )
      await expect(page.locator('.compose-row', { hasText: '采集帧数' })).toContainText(
        String(1000 + thisWeekKeys.indexOf(otherKey))
      )
    }
    expect(wearReqCount, '换日不许再发取数').toBe(initial)

    // —— 切按周：柱状图格与周界读数出现，取数计数仍不变 ——
    await page.locator('.seg-btn', { hasText: '按周' }).click()
    await expect(page.locator('.chart-card')).toBeVisible()
    const sum = thisWeekKeys.reduce((a, k) => a + (hoursByKey.get(k) as number), 0)
    const range = page.locator('.week-range')
    await expect(range).toContainText('本周（周一开始）')
    await expect(range).toContainText(`${thisWeekKeys[0].slice(5)} 至 ${keyOf(new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + 6)).slice(5)}`)
    await expect(range).toContainText(`有记录 ${thisWeekKeys.length} 天`)
    await expect(page.locator('.stat', { hasText: '累计佩戴' })).toContainText(String(Math.round(sum)))
    expect(wearReqCount, '切档不许再发取数').toBe(initial)

    // —— 切回按日：今日读数还在原位（两档读的是同一份派生）——
    await page.locator('.seg-btn', { hasText: '按日' }).click()
    await expect(page.locator('.stat', { hasText: '当日佩戴' })).toContainText(
      (hoursByKey.get(thisWeekKeys[0]) as number).toFixed(1)
    )
    expect(wearReqCount, '来回切档都不许再发取数').toBe(initial)
  })

  test('今日环形图那一格不受切档影响（既有目标行仍常显）', async ({ page }) => {
    await setupPatientE2E(page, { withLogin: true })
    await page.route(/\/api\/v1\/patients\/[^/]+\/daily-wear/, (route) =>
      route.fulfill({ json: ok(rowsFor(thisWeekKeys)) })
    )
    await page.goto(WEARING_PATH)
    const targetLine = page.locator('.ring-target')
    await expect(targetLine).toBeVisible()
    await expect(targetLine).toContainText('目标:')
    await expect(targetLine).toContainText('达标率')
    await page.locator('.seg-btn', { hasText: '按周' }).click()
    await expect(targetLine).toBeVisible()
  })
})
