import { test, expect } from '@playwright/test'
import { routes, setupPatientE2E } from '../helpers'

/**
 * T620 患者端趋势「取数契约」浏览器格。
 *
 * 断的是请求面，不是像素面：页面必须向后端要桶（interval），
 * 而不是把明细分页翻成一堆零星孤点。渲染面（X 轴按真实时间定位、
 * 零值不丢）由 apps/patient-miniapp/tests/unit/trend-window.spec.ts 的纯层用例守着；
 * 现网那一格（真机 P20260005 日/周/月连续曲线）要等部署，见卡内交件说明。
 */

/** 独立算出的北京日期（en-CA + Asia/Shanghai），不引实现里的 util —— 实现错了这里要响 */
function cstToday(offsetMs = 0): string {
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date(Date.now() + offsetMs))
}

async function recordQueries(page) {
  const seen: URL[] = []
  page.on('request', req => {
    if (/\/api\/v1\/patients\/[^/]+\/records(\?|$)/.test(req.url())) seen.push(new URL(req.url()))
  })
  return seen
}

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
})

test('日档要 30 分钟桶、周/月要 1 天桶，且不再发翻页参数', async ({ page }) => {
  const seen = await recordQueries(page)
  const before = cstToday()
  await page.goto(routes.monitor)
  const segBtn = (label: string) => page.locator('.segmented .seg-btn', { hasText: label })
  await segBtn('周').click()
  await segBtn('月').click()
  const after = cstToday()

  // 三档各至少一次请求
  const byPeriod = (p: string) => seen.filter(u => u.searchParams.get('period') === p)
  await expect.poll(() => byPeriod('day').length, { timeout: 15_000 }).toBeGreaterThan(0)
  expect(byPeriod('week').length).toBeGreaterThan(0)
  expect(byPeriod('month').length).toBeGreaterThan(0)

  // 桶宽：日=30m，周/月=1d（与后端 historyIntervals 白名单同名）
  for (const u of byPeriod('day')) expect(u.searchParams.get('interval')).toBe('30m')
  for (const u of [...byPeriod('week'), ...byPeriod('month')]) expect(u.searchParams.get('interval')).toBe('1d')

  // 不再自己凑明细：桶读路不分页
  for (const u of seen) {
    expect(u.searchParams.has('page')).toBe(false)
    expect(u.searchParams.has('pageSize')).toBe(false)
  }

  // date = 北京的某一天（跨北京零点时允许前后各一枚），🔴 不是设备本地/UTC 的那天
  const allowed = [before, after]
  for (const u of seen) {
    const d = u.searchParams.get('date') || ''
    expect(/^\d{4}-\d{2}-\d{2}$/.test(d), `date 形状不对：${d}`).toBe(true)
    expect(allowed, `date 不在北京日范围内：${d}（允许 ${allowed.join('/')}）`).toContain(d)
  }

  // 图表确实渲染出来了（有桶就不出「暂无数据」）
  await expect(page.locator('.curve-card .curve-canvas')).toBeVisible()
  await expect(page.getByText('暂无数据')).toHaveCount(0)
})
