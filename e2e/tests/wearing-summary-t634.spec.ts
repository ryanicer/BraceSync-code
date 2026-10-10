import { test, expect } from '@playwright/test'
import { setupPatientE2E, ok } from '../helpers'

/**
 * T634 佩戴管理页（统计页落点）按日/按周汇总视图的 H5 实测。
 *
 * 只做 H5 渲染实测，小程序端不冒充已验（口径同 profile.spec）。
 * 夹具自带的是 2026-06/07 的历史日，落不进本自然周的槽位，所以本 spec 自己按
 * 「东八区本周一至今日」造行并覆盖 daily-wear 路由（LIFO：后注册先咨询）。
 * 周界口径＝值班席第 115 轮裁定第四节（周一起算 + Asia/Shanghai）；这里的东八区算法是**独立第二份实现**，
 * 不 import src/utils 那一层 ⇒ 页面把周界算回设备本地时区时，CI（UTC runner）这一格会红而不是跟着错。
 * 派发单第二条「切换只换维度不换数据源」在这一格是被数出来的：切档与换日之后，
 * 页面发出的 daily-wear 请求计数必须还是同一枚。
 */
const WEARING_PATH = '/#/pages/wearing/index'

const MS_PER_DAY = 24 * 60 * 60 * 1000
const MS_PER_HOUR = 60 * 60 * 1000
const pad = (n: number) => (n < 10 ? `0${n}` : `${n}`)

/** 东八区那一天的日历键 + 该日历日零点（UTC 纪元毫秒）+ 星期序号（0=周日） */
function cstDay(d: Date): { key: string; midnightUtc: number; dow: number } {
  const p = new Date(d.getTime() + 8 * MS_PER_HOUR)
  const midnightUtc = Date.UTC(p.getUTCFullYear(), p.getUTCMonth(), p.getUTCDate())
  return {
    key: `${p.getUTCFullYear()}-${pad(p.getUTCMonth() + 1)}-${pad(p.getUTCDate())}`,
    midnightUtc,
    dow: new Date(midnightUtc).getUTCDay(),
  }
}

/** 由「某日历日的 UTC 零点毫秒」还原本东八区日历日键（CST 与 UTC 都无夏令时，差值恰为整日） */
function keyAtMidnight(ms: number): string {
  const p = new Date(ms)
  return `${p.getUTCFullYear()}-${pad(p.getUTCMonth() + 1)}-${pad(p.getUTCDate())}`
}

const now = new Date()
const cstToday = cstDay(now)
// 周日回退 6 天，周一为 0
const mondayIdx = cstToday.dow === 0 ? 6 : cstToday.dow - 1
const mondayMidnightUtc = cstToday.midnightUtc - mondayIdx * MS_PER_DAY
/** 本周 7 枚（东八区周一 → 东八区周日），用于周界读数那一行 */
const fullWeekKeys: string[] = Array.from({ length: 7 }, (_, i) => keyAtMidnight(mondayMidnightUtc + i * MS_PER_DAY))
/** 已到期的那几枚（东八区周一 → 东八区今日），页面按日档的日 chip 就这么多 */
const thisWeekKeys = fullWeekKeys.slice(0, mondayIdx + 1)
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
  date: keyAtMidnight(mondayMidnightUtc - MS_PER_DAY),
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
    const wearUrls: string[] = []
    await setupPatientE2E(page, { withLogin: true })
    page.on('request', (r) => {
      if (/\/api\/v1\/patients\/[^/]+\/daily-wear(\?|$)/.test(r.url())) {
        wearReqCount += 1
        wearUrls.push(r.url())
      }
    })
    // 后注册先咨询：覆盖 helpers 里那份历史日夹具
    await page.route(/\/api\/v1\/patients\/[^/]+\/daily-wear/, (route) =>
      route.fulfill({ json: ok([...rowsFor(thisWeekKeys), prevWeekRow]) })
    )
    await page.goto(WEARING_PATH)
    await expect(page.locator('.section-title', { hasText: '汇总视图' })).toBeVisible()

    const initial = wearReqCount
    expect(initial, '页面自己该发的那一发取数要抓到').toBeGreaterThanOrEqual(1)

    // —— 取数窗口两界（裁定第四节：周一起算 + Asia/Shanghai）——
    // 这一格是「页面自己算出来的那两枚键」对「本 spec 独立算的那两枚」：设备本地时区落后于东八区时
    // （UTC runner 的每日 16:00 之后）两算式不同值，页面退回设备本地周界就会在这里红。
    for (const raw of wearUrls) {
      const u = new URL(raw)
      expect(u.searchParams.get('start'), `窗口左界不是东八区周边（${raw}）`).toBe(fullWeekKeys[0])
      expect(u.searchParams.get('end'), `窗口右界不是东八区今日（${raw}）`).toBe(cstToday.key)
    }
    // 夹具自检（先证自己再证页面）：周边那枚必须真的落在周一，今日必须落在算出的下标上
    expect(new Date(mondayMidnightUtc).getUTCDay(), '夹具算出的周边不是周一').toBe(1)
    expect(fullWeekKeys[mondayIdx], '今日在下标上对不回东八区今日键').toBe(cstToday.key)

    // —— 按日档（默认）：日 chip 只到今日，读数等于今日那枚夹具值 ——
    const chips = page.locator('.day-chip')
    await expect(chips).toHaveCount(thisWeekKeys.length)
    const todayKey = cstToday.key
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
    await expect(range).toContainText(`${fullWeekKeys[0].slice(5)} 至 ${fullWeekKeys[6].slice(5)}`)
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

/**
 * 时区轴那一格单独一档：把浏览器的钟钉到 UTC-11（比东八区落后 19 小时），
 * 于是每天必有 19 小时「设备本地今日」≠「东八区今日」⇒ 窗口两界与按日档的默认选中都不许跟着设备的钟走。
 * 默认档（本机 +8 / CI 的 UTC）撞不出这一格：两钟同日时两种算式同值，所以钉钟这一档才是常开的那把尺。
 * 注牙读法（本轮 r97 已证）：把 loadDailyWear 的窗口换回设备本地算式，这一档在 end 那一行红。
 */
test.describe('T634 取数窗口的时区轴（浏览器钟钉在东八区以西）', () => {
  test.use({ timezoneId: 'Pacific/Midway' })

  test('窗口两界按东八区，不按设备本地钟', async ({ page }) => {
    const wearUrls: string[] = []
    await setupPatientE2E(page, { withLogin: true })
    page.on('request', (r) => {
      if (/\/api\/v1\/patients\/[^/]+\/daily-wear(\?|$)/.test(r.url())) wearUrls.push(r.url())
    })
    await page.route(/\/api\/v1\/patients\/[^/]+\/daily-wear/, (route) =>
      route.fulfill({ json: ok([...rowsFor(thisWeekKeys), prevWeekRow]) })
    )
    await page.goto(WEARING_PATH)
    await expect(page.locator('.section-title', { hasText: '汇总视图' })).toBeVisible()

    // 钉钟自证（尺子先证自己在不在线上）：getTimezoneOffset 的单位是「比 UTC 快几分钟」
    expect(await page.evaluate(() => new Date().getTimezoneOffset()), '浏览器钟没钉到 UTC-11').toBe(660)
    expect(wearUrls.length, '这一发取数要抓到').toBeGreaterThanOrEqual(1)
    for (const raw of wearUrls) {
      const u = new URL(raw)
      expect(u.searchParams.get('start'), `窗口左界跟着设备的钟走了（${raw}）`).toBe(fullWeekKeys[0])
      expect(u.searchParams.get('end'), `窗口右界跟着设备的钟走了（${raw}）`).toBe(cstToday.key)
    }
    await expect(page.locator('.day-chip')).toHaveCount(thisWeekKeys.length)
    await expect(page.locator('.day-chip-active')).toContainText(cstToday.key.slice(5))
  })
})
