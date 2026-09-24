import { test, expect, type Page, type Locator } from '@playwright/test'
import { readFileSync } from 'fs'
import { adminRoutes, adminLogin, menuItems, adminPath, adminMessage } from '../admin-helpers'

/**
 * T372 异常报告独立页（Boss 2026-09-24 裁定 (a)：按设计稿把 T300 的抽屉区块拆成一级页）
 *
 * 设计稿依据 docs/design/admin/异常报告.html：
 *  - :167      顶栏标题「🧾 异常报告」
 *  - :176-219  检索条件卡（患者下拉 / 起止日期 / 快捷范围 chip / 重置·查询·导出）
 *  - :196-201  快捷范围「近 7 天 / 本月至今 / 近 30 天」
 *  - :223-235  患者与设备 8 格，「统计区间」随区间联动
 *  - :238-259  KPI 卡（稿面 5 张，本轮 4 张：第 5 张「最高频采集点」缺读端点维度）
 *  - :262-271  趋势图 + 类型构成图（真 canvas，不用假数据）
 *  - :300-312  文字汇总（稿面固定 6 段中的 ①②⑤⑥ 段 + 抬头「区间异常汇总 · 姓名（ID）· 起 至 止」）
 *  - :350      进入条件①「从患者管理进入 → 患者下拉自动定位」
 *  - :356      错误文案「未找到该患者的绑定记录」
 *
 * 本轮未做（读端点缺维度 ⇒ 拼出来就是编数据，已在卡内登记差值）：
 *  :203-212 异常类型筛选、:273-277 峰值 vs 阈值、:279-296 4×5 采集点网格、
 *  :314-330 异常明细表、:306-307 ③集中点位/④佩戴依从性、:216 PDF 导出与导出弹窗。
 */

const kpiCards = (page: Page): Locator => page.locator('.kpi-row .kpi-card')
const chartCanvases = (page: Page): Locator => page.locator('.chart-row canvas')
const summaryLines = (page: Page): Locator => page.locator('.summary-line')
const infoCard = (page: Page): Locator => page.locator('.page-card').filter({ has: page.locator('.el-descriptions') })
/** 稿面 :223-235「患者与设备」固定 8 格，第 1 格患者 ID、第 8 格统计区间 */
const infoCells = (page: Page): Locator => infoCard(page).locator('.el-descriptions__content')

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  // 走侧栏进入（顺带覆盖稿面「菜单挂载」：侧栏第 4 项，位于患者管理之后）
  await menuItems(page).filter({ hasText: '异常报告' }).click()
  await expect(page).toHaveURL(/\/admin\/abnormal-report$/)
  await expect(kpiCards(page)).toHaveCount(4, { timeout: 15_000 })
  await expect(infoCells(page)).toHaveCount(8)
})

test.describe('独立页主体（mock 自动完成一次查询）', () => {
  test('四张 KPI 卡 + 两张图 + 文字汇总四段全部落地', async ({ page }) => {
    await expect(page.locator('.top-nav-title')).toContainText('异常报告')
    // 稿面 :240-258 卡序：异常总次数 / 压力偏高 / 设备离线 / 未处理条数
    const labels = await kpiCards(page).locator('.kpi-label').allTextContents()
    expect(labels.map((s) => s.trim())).toEqual(['异常总次数', '压力偏高', '设备离线', '未处理条数'])
    // canvas 真渲染（Chart.js 出图即有画布；无数据时本轮不隐藏卡片，见稿面 :352「不隐藏卡片」）
    await expect(chartCanvases(page)).toHaveCount(2)
    expect(await chartCanvases(page).first().evaluate((c) => (c as HTMLCanvasElement).width)).toBeGreaterThan(0)
    await expect(summaryLines(page)).toHaveCount(4)
    // 稿面 :303 抬头「区间异常汇总 · 姓名（患者ID）· 起 至 止」
    await expect(page.locator('.summary-head')).toHaveText(
      /^区间异常汇总 · .+（PT-\d+）· \d{4}-\d{2}-\d{2} 至 \d{4}-\d{2}-\d{2}$/,
    )
  })

  test('KPI 与文字汇总同源：总次数 / 未处理数 / 已处理+未处理=总数', async ({ page }) => {
    const total = Number((await kpiCards(page).nth(0).locator('.kpi-value').textContent())!.trim())
    const unprocessed = Number((await kpiCards(page).nth(3).locator('.kpi-value').textContent())!.trim())
    expect(Number.isFinite(total), 'mock 下首位患者区间内应有事件').toBeTruthy()
    expect(total).toBeGreaterThan(0)
    const first = (await summaryLines(page).nth(0).textContent())!
    const fifth = (await summaryLines(page).nth(2).textContent())!
    expect(first).toContain(`共产生异常 ${total} 次`)
    expect(fifth).toContain(`未处理 ${unprocessed} 次`)
    const processed = Number(/已处理 (\d+) 次/.exec(fifth)![1])
    expect(processed + unprocessed, '稿面 :257 处理率与未处理数须同源于 byStatus').toBe(total)
  })

  test('日均 = 总次数 / 含端点日历天数（稿面 :241 与统计区间 :233 联动）', async ({ page }) => {
    const total = Number((await kpiCards(page).nth(0).locator('.kpi-value').textContent())!.trim())
    const rangeText = (await infoCells(page).nth(7).textContent())!
    const days = Number(/（(\d+) 天）/.exec(rangeText.trim())![1])
    expect(days, '默认区间 = 稿面「近 7 天」').toBe(7)
    const avg = (await kpiCards(page).nth(0).locator('.kpi-note').textContent())!
    expect(avg.trim()).toBe(`日均 ${Math.round((total / days) * 10) / 10} 次`)
  })

  test('类型构成用新四类，旧词「传感器漂移」不得在本页回潮', async ({ page }) => {
    const compose = (await page.locator('.chart-card').nth(1).textContent())!
    expect(compose).toContain('异常类型构成')
    const body = await page.locator('.abnormal-report').textContent()
    expect(body).not.toContain('传感器漂移')
    expect(body).toContain('传感器标定异常')
  })
})

test.describe('检索条件', () => {
  test('快捷范围三枚 chip，点「近 30 天」后统计区间与日期输入同步收窄为 30 天（稿面 :196-201）', async ({ page }) => {
    const chips = page.locator('.chips .chip')
    const texts = (await chips.allTextContents()).map((s) => s.trim())
    expect(texts).toEqual(['近 7 天', '本月至今', '近 30 天'])
    await expect(chips.nth(0)).toHaveClass(/chip-on/) // 默认区间 = 稿面「近 7 天」
    await chips.nth(2).click()
    await expect(chips.nth(2)).toHaveClass(/chip-on/)
    const dates = page.locator('.filter-row .el-date-editor input')
    await expect(dates.nth(1)).toHaveValue(/\d{4}-\d{2}-\d{2}/)
    const from = await dates.nth(0).inputValue()
    const to = await dates.nth(1).inputValue()
    expect(Math.round((Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86400000) + 1).toBe(30)
    await page.locator('.filter-row').getByRole('button', { name: '查询' }).click()
    await expect(infoCells(page).nth(7)).toContainText('（30 天）')
  })
})

test.describe('两个进入入口（稿面 :350 进入条件）', () => {
  test('患者管理抽屉「异常报告」→ 独立页并把患者带过去', async ({ page }) => {
    await page.goto(adminRoutes.patients)
    // 取第 2 行而非首行：直接访问时页面默认选中可见列表首位，用首行这条断言就分不出「定位」还是「默认」
    const row = page.locator('.el-table__body-wrapper tbody tr').nth(1)
    await expect(row).toBeVisible({ timeout: 15_000 })
    const pid = (await row.locator('td').nth(0).textContent())!.trim()
    expect(pid, '首列是患者ID').toMatch(/^PT-/)
    await row.click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible()
    await drawer.getByRole('button', { name: '异常报告' }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/abnormal-report\\?patient=${pid.replace(/-/g, '\\-')}$`))
    // 「自动定位」= 页面选中的就是抽屉里那位患者
    await expect(infoCells(page).nth(0)).toHaveText(pid)
  })

  test('URL 指定不可见患者 → 明写未找到，不悄悄换成别的患者（稿面 :356）', async ({ page }) => {
    await page.goto(`${adminPath('/abnormal-report')}?patient=PT-NOPE`)
    await expect(page.locator('.empty-tip')).toHaveText('未找到该患者的绑定记录')
    await expect(kpiCards(page)).toHaveCount(0)
    await expect(infoCard(page)).toHaveCount(0)
  })
})

/**
 * T300 断言随页迁移（Boss 裁定 (a) 把抽屉区块拆成本页后，原抽屉里的 6 条 e2e 判据必须跟过来）。
 *
 * 🔴 迁移而不是删除：删掉 = 覆盖丢失 = 假绿。逐条去向 ——
 *  - 「汇总自洽」/「导出行数 = 总数」/「区间真的参与筛选」/「空区间行为」/「清空区间不崩」：本 describe 五条，全保留。
 *  - 「三视图（按类型 + 按日期）次数之和 = 总数」：页面无这两张表（稿面这里是两张图），
 *    同源关系改由 chartSeriesOf() 从图数据里读，口径不变（各视图次数之和仍须等于 total）。
 */

/**
 * 往两个独立的 el-date-picker(type=date) 写起止日期。
 * 每个都要 click → fill → Enter 提交；两端之间点一次标题把浮层收起来，
 * 否则开始日的日历面板盖住结束日输入框（点击落空 = 用例「莫名不生效」）。
 */
async function setRange(page: Page, start: string, end: string) {
  const dates = page.locator('.filter-row .el-date-editor input')
  await dates.nth(0).click()
  await dates.nth(0).fill(start)
  await dates.nth(0).press('Enter')
  await page.locator('.page-card-title').first().click()
  await dates.nth(1).click()
  await dates.nth(1).fill(end)
  await dates.nth(1).press('Enter')
  await page.locator('.page-card-title').first().click()
}

/** 清空两端日期：EP 的日期框「填空 + 回车」即把 modelValue 置 null（不依赖 hover 才现身的清除图标） */
async function clearRange(page: Page) {
  const dates = page.locator('.filter-row .el-date-editor input')
  for (const index of [0, 1]) {
    const input = dates.nth(index)
    await input.click()
    await input.fill('')
    await input.press('Enter')
  }
  await page.locator('.page-card-title').first().click()
}

/** KPI 第 1 卡的总次数（页面上唯一被各视图引用的计数真源） */
async function kpiTotal(page: Page): Promise<number> {
  const raw = (await kpiCards(page).nth(0).locator('.kpi-value').textContent())!.trim()
  expect(raw).toMatch(/^\d+$/)
  return Number(raw)
}

const exportButton = (page: Page): Locator => page.getByRole('button', { name: '导出 CSV' })
const queryButton = (page: Page): Locator => page.locator('.filter-row').getByRole('button', { name: '查询' })

test.describe('导出与区间筛选（T300 判据随页迁移）', () => {
  test('默认区间导出 CSV：BOM + 16 列 + 明细行数 = KPI 总次数（汇总与明细同源）', async ({ page }) => {
    const total = await kpiTotal(page)
    expect(total, 'mock 下默认区间应有事件，否则本条没有判别力').toBeGreaterThan(0)
    const start = await page.locator('.filter-row .el-date-editor input').nth(0).inputValue()
    const end = await page.locator('.filter-row .el-date-editor input').nth(1).inputValue()

    const download = page.waitForEvent('download')
    await exportButton(page).click()
    const file = await download
    expect(file.suggestedFilename()).toBe(`abnormal-report-PT-001-${start}_${end}.csv`)

    const raw = readFileSync(await file.path())
    expect(raw.subarray(0, 3).toString('hex'), 'Excel 打开不乱码需 UTF-8 BOM').toBe('efbbbf')
    const lines = raw.toString('utf8').slice(1).trim().split('\r\n')
    expect(lines[0].split(',')).toHaveLength(16)
    expect(lines.length - 1, 'CSV 明细行数 = KPI 总次数').toBe(total)
  })

  test('区间真的参与筛选：换成 2020-03-01~03 后总次数、统计区间、导出文件名同步变', async ({ page }) => {
    const defaultTotal = await kpiTotal(page)
    expect(defaultTotal, '默认窗口应非空，否则下面的「变了」没有对照').toBeGreaterThan(0)

    await setRange(page, '2020-03-01', '2020-03-03')
    await queryButton(page).click()
    // 该窗口内 mock 按患者+日序确定性生成：PT-001 在 03-01 为 0 条、03-02 一条、03-03 两条
    await expect(kpiCards(page).nth(0).locator('.kpi-value')).toHaveText('3')
    expect(await kpiTotal(page)).not.toBe(defaultTotal)
    await expect(infoCells(page).nth(7)).toContainText('2020-03-01 ~ 2020-03-03')
    await expect(infoCells(page).nth(7)).toContainText('（3 天）')
    // ① 段文字汇总与 KPI 同源于同一次响应
    await expect(summaryLines(page).nth(0)).toContainText('共产生异常 3 次')
    // 导出文件名带的是**所填区间**而非默认窗口 ⇒ 证明 start/end 一路传到下载
    const download = page.waitForEvent('download')
    await exportButton(page).click()
    expect((await download).suggestedFilename()).toBe('abnormal-report-PT-001-2020-03-01_2020-03-03.csv')
  })

  test('区间内无异常：总次数 0 + 明写无记录，但 KPI 卡不隐藏（稿面 :352），CSV 只剩表头', async ({ page }) => {
    await setRange(page, '2020-03-01', '2020-03-01') // mock 对 PT-001 该日确定性给 0 条
    await queryButton(page).click()
    await expect(kpiCards(page).nth(0).locator('.kpi-value')).toHaveText('0')
    await expect(page.locator('.empty-tip')).toHaveText('所选区间内该患者无异常记录')
    await expect(kpiCards(page)).toHaveCount(4) // 不隐藏卡片：避免空区间被读成「页面坏了」
    // ② 段（构成）在 total=0 时明写无记录，而不是留下一串顿号或除零出的 NaN
    await expect(summaryLines(page).nth(1)).toHaveText('② 构成：区间内无异常记录')

    const download = page.waitForEvent('download')
    await exportButton(page).click()
    const raw = readFileSync(await (await download).path())
    const lines = raw.toString('utf8').slice(1).trim().split('\r\n')
    expect(lines).toHaveLength(1)
    expect(lines[0].split(',')).toHaveLength(16)
  })

  test('清空日期区间：提示「请选择起止日期」且无未捕获异常（G13 空区间不崩的唯一 E2E 判据）', async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', (e) => errors.push(String(e)))
    await clearRange(page)
    const dates = page.locator('.filter-row .el-date-editor input')
    await expect(dates.nth(0)).toHaveValue('')
    await expect(dates.nth(1)).toHaveValue('')

    await queryButton(page).click()
    await exportButton(page).click()
    expect(errors, `清空区间后点两个按钮不应有未捕获异常，实际：${errors.join(' | ')}`).toHaveLength(0)
    await expect(adminMessage(page)).toContainText('请选择起止日期')
    // 不崩之后还要真的拦住：既不能静默沿用上次汇总，也不能把 KPI 改成半截数
    await expect(kpiTotal(page)).resolves.toBeGreaterThan(0)
  })

  test('起止倒挂：明写「结束日期不能早于开始日期」，不拿倒挂区间去查', async ({ page }) => {
    await setRange(page, '2020-03-05', '2020-03-01')
    await queryButton(page).click()
    await expect(adminMessage(page)).toContainText('结束日期不能早于开始日期')
    await expect(summaryLines(page).nth(0)).toContainText('共产生异常') // 仍是修前的那份汇总
  })

  test('按类型视图与总数自洽：② 构成各类型次数之和 = KPI 总次数（T300「三视图自洽」的等价判据）', async ({ page }) => {
    const total = await kpiTotal(page)
    expect(total).toBeGreaterThan(0)
    // 稿面这里是饼图（canvas 内部数据在 DOM 里读不到），② 构成段是同一份 byType 的文本视图，
    // 用它做求和判据；按日分桶的求和已由 vitest 对 chartDataFromReport 覆盖。
    const compose = (await summaryLines(page).nth(1).textContent())!
    const counts = [...compose.matchAll(/(\d+) 次（/g)].map((m) => Number(m[1]))
    expect(counts.length, `② 段应列出至少一类异常，实际「${compose}」`).toBeGreaterThan(0)
    expect(counts.reduce((n, c) => n + c, 0), '按类型次数不重不漏 = 总次数').toBe(total)
  })
})
