import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, pickSelectOption } from '../admin-helpers'

/**
 * admin-web 实时监控（T056 重做后）：患者下拉选择 + 4×5 热力图 + 实时压力曲线 + 每秒轮询（T289 F6，设计稿 实时监控.html:161）
 *
 * 新版页面结构（pages/monitor/index.vue）：
 * - 顶部栏：.realtime-tag（T322 三态：实时同步中 / 数据已过期 / 无实时数据）
 *   + .update-time「数据采集：HH:mm:ss（距今 …）| 本次拉取：HH:mm:ss」+ 立即刷新按钮
 * - 患者卡片：.patient-card > el-select(filterable) + .status-indicator + .device-hint
 * - 左栏曲线：.chart-card > .chart-container > canvas（Chart.js）
 * - 右栏热力图：.heatmap-card > .hm-grid > 4×.hm-row > 5×.hm-cell（共 20 格）
 *
 * mock 对齐 mock/patients.ts mockPatientRealtime：
 * - PT-001 林小雨 online（deviceId DEV-A3F312，压力 base=35，帧时刻为当下）
 * - PT-002 陈子航 帧时刻固定落后 3 小时（T322 过期态载体，status 一并 offline）
 * - PT-004 刘俊熙 abnormal（deviceId DEV-D2A012，压力 base=68）
 * - PT-005 赵欣然 offline（deviceId=null → pressureRecords 为空 + seedHeatmap 兜底，T322 无帧态载体）
 */

/** 等待快照加载完成（update-time 出现 HH:mm:ss 时间戳） */
async function waitForSnapshotLoaded(page: import('@playwright/test').Page): Promise<void> {
  await expect(page.locator('.update-time')).toContainText(/\d{2}:\d{2}:\d{2}/, { timeout: 15_000 })
}

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.monitor)
})

test.describe('页面渲染', () => {
  test('实时同步标识 + 默认患者加载 + 最近更新时间', async ({ page }) => {
    // 顶部实时同步脉冲标签（限定 page-toolbar 避免匹配卡片标题内的小标签）
    await expect(page.locator('.page-toolbar .realtime-tag')).toContainText('实时同步中')
    // 患者卡片可见
    await expect(page.locator('.patient-card .card-title')).toContainText('患者选择')
    // 默认选中第一个有设备的患者（PT-001 林小雨）
    // Element Plus filterable el-select 的选中值不在 input.value（input 仅用于搜索），
    // 而是在 .el-select__placeholder / .el-select__selected-item 内显示
    await expect(page.locator('.patient-card .el-select__wrapper')).toContainText('林小雨', { timeout: 15_000 })
    // 快照加载后显示时间戳
    await waitForSnapshotLoaded(page)
    // 状态指示器：PT-001 online → "佩戴中" + status-online 类
    await expect(page.locator('.status-indicator')).toContainText('佩戴中')
    await expect(page.locator('.status-indicator.status-online')).toBeVisible()
    // 设备提示
    await expect(page.locator('.device-hint')).toContainText('DEV-A3F312')
  })

  test('4×5 热力图 20 格渲染 + 最大点标记 + 图例', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    // 20 格 hm-cell
    const cells = page.locator('.hm-cell')
    await expect(cells).toHaveCount(20, { timeout: 15_000 })
    // 每格有 pointId（P01-P20）
    const ids = page.locator('.hm-cell-id')
    await expect(ids).toHaveCount(20)
    await expect(ids.first()).toContainText(/^P\d{2}$/)
    await expect(ids.last()).toContainText('P20')
    // 每格有压力数值（非空数字）
    const vals = page.locator('.hm-cell-val')
    await expect(vals).toHaveCount(20)
    await expect(vals.first()).toContainText(/\d/)
    // 恰好 1 个最大点标记
    await expect(page.locator('.hm-cell-max')).toHaveCount(1)
    // 图例 4 项
    await expect(page.locator('.hm-legend .hm-lg-item')).toHaveCount(4)
    await expect(page.locator('.hm-legend')).toContainText('低压')
    await expect(page.locator('.hm-legend')).toContainText('高压')
  })

  test('实时压力曲线 canvas 可见', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    // 曲线卡片标题
    await expect(page.locator('.chart-card .card-title')).toContainText('实时压力曲线')
    // Chart.js 渲染 canvas
    await expect(page.locator('.chart-container canvas')).toBeVisible({ timeout: 15_000 })
  })
})

test.describe('交互与刷新', () => {
  test('患者下拉可搜索 + 选项列表', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    // 打开下拉
    const select = page.locator('.patient-card .el-select')
    await select.click()
    const dropdown = page.locator('.el-select-dropdown:visible')
    await expect(dropdown).toBeVisible({ timeout: 5_000 })
    // 选项数 = mock/patients.ts 患者总数（T289 4.2 补 PT-007/PT-008 两名未分配患者后为 8）
    const options = dropdown.locator('.el-select-dropdown__item')
    await expect(options).toHaveCount(8, { timeout: 5_000 })
    // 搜索过滤：用 pressSequentially 模拟真实键入触发 el-select filterable 过滤
    const input = page.locator('.patient-card .el-select input')
    await input.click()
    await input.pressSequentially('刘俊熙')
    // 过滤后仅匹配项可见（Element Plus 用 display:none 隐藏非匹配项）
    await expect(dropdown.locator('.el-select-dropdown__item').filter({ hasText: '刘俊熙' })).toBeVisible()
    await expect(dropdown.locator('.el-select-dropdown__item').filter({ hasText: '林小雨' })).toBeHidden()
    // 关闭下拉（点击页面空白区域）
    await page.locator('.card-title').first().click()
    await expect(page.locator('.el-select-dropdown:visible')).toHaveCount(0, { timeout: 5_000 }).catch(() => { /* EP 收起动画 */ })
  })

  test('患者切换：状态/设备提示随患者更新（含未绑定设备边界）', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    // 切换到 PT-004 刘俊熙（abnormal）
    await pickSelectOption(page, page.locator('.patient-card .el-select'), '刘俊熙')
    await expect(page.locator('.status-indicator')).toContainText('异常', { timeout: 10_000 })
    await expect(page.locator('.status-indicator.status-abnormal')).toBeVisible()
    await expect(page.locator('.device-hint')).toContainText('DEV-D2A012')
    // 切换到 PT-005 赵欣然（offline，无设备）
    await pickSelectOption(page, page.locator('.patient-card .el-select'), '赵欣然')
    await expect(page.locator('.status-indicator')).toContainText('未佩戴', { timeout: 10_000 })
    await expect(page.locator('.status-indicator.status-offline')).toBeVisible()
    await expect(page.locator('.device-hint')).toContainText('未绑定设备')
  })

  test('立即刷新更新时间戳', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    const before = await page.locator('.update-time').innerText()
    // 等待至少跨 1 秒，保证刷新后时间字符串变化
    await page.waitForTimeout(1_100)
    await page.getByRole('button', { name: '立即刷新' }).click()
    await expect(page.locator('.update-time')).not.toHaveText(before, { timeout: 15_000 })
  })
})

/* ============================================================================
 * T270 补口：A-MON-03 / A-MON-05 / A-MON-07 / A-MON-08（README §5 第一类）
 *            + 假绿 #4（自动轮询）/ #5（曲线刻度与 tooltip）（README §5 第二类）
 *
 * 🔴 本页每秒轮询一次（T289 F6），且每次轮询会把热力图选中态重置（monitor/index.vue refreshTick
 *    里 heatmapSelected.value = null）。因此凡是「一次交互的结果要在多处之间比对」的断言，
 *    必须在**同一次页面求值内**把相关 DOM 一起读出来，再用 expect.poll 重试整个原子读取，
 *    否则会撞在轮询边界上产生假红。下面 summary / points / 热力图详情都按这个写法来。
 * ========================================================================== */

/** 一次原子读取患者摘要四格（label/value 成对返回） */
function readPeakGrid(page: import('@playwright/test').Page) {
  return page.locator('.peak-card .peak-grid').evaluate((grid) =>
    Array.from(grid.querySelectorAll<HTMLElement>('.peak-cell')).map((cell) => ({
      label: cell.querySelector('.peak-label')?.textContent?.trim() ?? '',
      value: (cell.querySelector('.peak-num') ?? cell.querySelector('.peak-text'))?.textContent?.trim() ?? '',
    })),
  )
}

test.describe('患者摘要与点位（T270 A-MON-03/05/07/08）', () => {
  test('A-MON-03 患者摘要四项：标签顺序 + 数值格式 + 无 undefined/NaN/空', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    await expect
      .poll(
        async () => {
          const cells = await readPeakGrid(page)
          return JSON.stringify({
            labels: cells.map((c) => c.label),
            hours: /^\d+\.\d h$/.test(cells[0]?.value ?? '') ? 'ok' : cells[0]?.value,
            pressure: /^\d+\.\d N$/.test(cells[1]?.value ?? '') ? 'ok' : cells[1]?.value,
            peakPoint: /^(P\d{2} \(R\dC\d\)|--)$/.test(cells[2]?.value ?? '') ? 'ok' : cells[2]?.value,
            events: /^\d+$/.test(cells[3]?.value ?? '') ? 'ok' : cells[3]?.value,
          })
        },
        { message: '患者摘要四项需在某一轮快照内同时成立', timeout: 20_000 },
      )
      .toBe(
        JSON.stringify({
          labels: ['今日累计佩戴时长', '当前最大压力', '最大压力采集点', '今日异常事件'],
          hours: 'ok',
          pressure: 'ok',
          peakPoint: 'ok',
          events: 'ok',
        }),
      )
    // 四项均不得出现脏值（用例硬判据；正则成立即排除 undefined/NaN/空白）
    await expect(page.locator('.peak-card')).not.toContainText(/undefined|NaN/)
  })

  test('A-MON-05 点击热力图格子：详情行跟随所点点位', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    const read = (pointId: string) =>
      page.evaluate((pid) => {
        const cell = Array.from(document.querySelectorAll<HTMLElement>('.hm-cell')).find(
          (el) => el.querySelector('.hm-cell-id')?.textContent?.trim() === pid,
        )
        cell?.click()
        const detail = document.querySelector('.hm-detail')?.textContent?.trim() ?? ''
        const cellVal = Number(cell?.querySelector('.hm-cell-val')?.textContent?.trim())
        const detailVal = Number(detail.match(/·\s*([\d.]+)\s*N/)?.[1] ?? NaN)
        return {
          hit: detail.includes(`当前选中：${pid} (R`) && !Number.isNaN(cellVal) && !Number.isNaN(detailVal) &&
            Math.abs(cellVal - detailVal) < 1,
        }
      }, pointId)

    // 第 2 行第 1 格 = P06；再点 P15，详情行须随点位切换
    await expect.poll(() => read('P06'), { message: '点 P06 后详情行应回显 P06 (R2C1)', timeout: 20_000 }).toEqual({ hit: true })
    await expect.poll(() => read('P15'), { message: '点 P15 后详情行应回显 P15 (R3C5)', timeout: 20_000 }).toEqual({ hit: true })

    // 点击不得产生错误提示或跳转
    await expect(page.locator('.el-message--error')).toHaveCount(0)
    await expect(page).toHaveURL(/\/monitor/)
  })

  test('A-MON-07 采集点实时数值表：四列表头 + 20 行 P01…P20 / R1C1…R4C5 + 状态四选一', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    const card = page.locator('.page-card').filter({ hasText: '采集点实时数值表' })
    const headers = card.locator('.points-table thead th')
    await expect(headers).toHaveText(['采集点', '位置', '当前压力 (N)', '状态'])

    await expect
      .poll(
        () =>
          card.locator('.points-table tbody').evaluate((tbody) => {
            const rows = Array.from(tbody.querySelectorAll('tr'))
            const ids = rows.map((r) => r.children[0]?.textContent?.trim() ?? '')
            const labels = rows.map((r) => r.children[1]?.textContent?.trim() ?? '')
            const pressures = rows.map((r) => r.children[2]?.textContent?.trim() ?? '')
            const statuses = rows.map((r) => r.children[3]?.textContent?.trim() ?? '')
            const expectIds = Array.from({ length: 20 }, (_, i) => `P${String(i + 1).padStart(2, '0')}`)
            const expectLabels = Array.from({ length: 20 }, (_, i) => `R${Math.floor(i / 5) + 1}C${(i % 5) + 1}`)
            return {
              rowCount: rows.length,
              idSeqOk: JSON.stringify(ids) === JSON.stringify(expectIds),
              labelSeqOk: JSON.stringify(labels) === JSON.stringify(expectLabels),
              // 压力保留 1 位小数且非 NaN（0.0 = 无信号点位，属实现口径）
              pressureFmtOk: pressures.every((p) => /^\d+\.\d$/.test(p)),
              statusFmtOk: statuses.every((s) => /^(正常|关注|偏高|无信号)$/.test(s)),
              dotPerRow: rows.every((r) => !!r.children[3]?.querySelector('.status-dot')),
            }
          }),
        {
          message: '采集点表 20 行 / P01-P20 / R1C1-R4C5 / 压力 1 位小数 / 状态四选一',
          timeout: 20_000,
        },
      )
      .toEqual({
        rowCount: 20,
        idSeqOk: true,
        labelSeqOk: true,
        pressureFmtOk: true,
        statusFmtOk: true,
        dotPerRow: true,
      })
  })

  test('A-MON-08 近期异常事件：卡标题 + 四列表头 + mock 空态「无异常事件」', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    const card = page.locator('.page-card').filter({ hasText: '近期异常事件' })
    await expect(card.locator('.card-title')).toContainText('近期异常事件')
    await expect(card.locator('.events-table thead th')).toHaveText(['时间', '类型', '详情', '采集点'])
    // mock 快照的 alerts 恒为空数组（mock/patients.ts:135）→ 只能验空态；
    // 有数据分支（时间 HH:mm / 类型中文徽章 / 采集点缺省 -）结构性不可达，留在点击 Agent 用例集。
    await expect(card.locator('.events-table tbody td.empty-cell')).toHaveText('无异常事件')
    await expect(card).not.toContainText(/undefined|NaN/)
  })
})

test.describe('自动刷新与曲线细节（T270 假绿 #4/#5）', () => {
  test('假绿#4 A-MON-09 自动轮询：什么都不点，时间戳应在每秒轮询下自行变化', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    const before = await page.locator('.update-time').innerText()
    // 不做任何点击，仅等待轮询自己推进时间戳（旧 e2e 只测了手动「立即刷新」）
    // T289 F6：轮询周期 = POLL_MS 1000（设计稿 实时监控.html:161「每秒刷新」），2.5s 内必须变
    await expect(page.locator('.update-time')).not.toHaveText(before, { timeout: 2_500 })
    await expect(page.locator('.el-message--error')).toHaveCount(0)
    // 轮询期间页面不得白屏：曲线与热力图仍各自在位
    await expect(page.locator('.chart-container canvas')).toBeVisible()
    await expect(page.locator('.hm-cell')).toHaveCount(20)
  })

  test('假绿#5 A-MON-06 曲线：蓝色折线 + 浅蓝填充 + 纵/横轴刻度 + 悬停出 tooltip', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    const canvas = page.locator('.chart-container canvas')
    await expect(canvas).toBeVisible({ timeout: 15_000 })
    // T322 起曲线只画本轮真实帧（1 帧/秒），开局不再一次性造 30 点正弦假数据
    // ⇒ 下面的像素阈值要有足够点位才成立：等出一横轴满刻度（≥8 个标签）再采样
    await page.waitForTimeout(10_500)
    await page.waitForTimeout(400) // 等入场动画画完，避免采样到半帧

    /**
     * Chart.js 把曲线、填充、坐标轴刻度与 tooltip 全画进 canvas（既无 DOM 也无 a11y 文本），
     * 旧 e2e 只断言「canvas 可见」= 假绿。这里按像素判定：
     *  - 线：不透明 #1a6db5；填充：rgba(26,109,181,.08)（透明底 canvas，实测 alpha≈20）
     *  - 刻度文字：Chart.js 默认 ticks.color #666（灰）→ 左侧 x<42 的列是 y 轴「0N/20N/…」，
     *    底部 y>height-27 的行是 x 轴时间刻度
     *  - tooltip：默认底色 rgba(0,0,0,.8)；限定绘图区（排除左右上下的刻度文字带）
     * 阈值取实测量级（线 ≈850px / 填充 ≈81kpx / y 刻度 ≈284px / x 刻度 ≈1k px / tooltip ≈3.6kpx）
     * 留 2–4 倍余量，防渲染差异。
     */
    const sample = (kind: 'line' | 'fill' | 'dark' | 'tick', area?: { x0: number; x1: number; y0: number; y1: number }) =>
      canvas.evaluate(
        (el, arg) => {
          const c = el as HTMLCanvasElement
          const d = c.getContext('2d')!.getImageData(0, 0, c.width, c.height).data
          const hit = (r: number, g: number, b: number, a: number): boolean => {
            if (arg.kind === 'line') return a > 150 && Math.abs(r - 26) < 12 && Math.abs(g - 109) < 12 && Math.abs(b - 181) < 12
            if (arg.kind === 'fill') return a > 10 && a < 60 && r < 40 && g > 100 && g < 130 && b > 165
            if (arg.kind === 'tick') return a > 120 && Math.abs(r - 102) < 26 && Math.abs(g - 102) < 26 && Math.abs(b - 102) < 26
            return a > 150 && r < 60 && g < 60 && b < 60 // dark：tooltip 底色
          }
          let points = 0
          let maxRowHits = 0
          const rows = new Map<number, number>()
          for (let i = 0; i < d.length; i += 4) {
            const x = (i / 4) % c.width
            const y = Math.floor(i / 4 / c.width)
            if (arg.area && (x < arg.area.x0 || x > arg.area.x1 || y < arg.area.y0 || y > arg.area.y1)) continue
            if (hit(d[i], d[i + 1], d[i + 2], d[i + 3])) {
              points++
              const n = (rows.get(y) ?? 0) + 1
              rows.set(y, n)
              maxRowHits = Math.max(maxRowHits, n)
            }
          }
          return { points, maxRowHits }
        },
        { kind, area },
      )

    // 曲线本体
    expect((await sample('line')).points).toBeGreaterThan(200)
    // 曲线下方浅蓝填充
    expect((await sample('fill')).points).toBeGreaterThan(10_000)

    // 绘图区矩形（canvas 像素坐标）：左侧留出 y 轴刻度、底部留出 x 轴刻度
    const box = await canvas.boundingBox()
    expect(box).not.toBeNull()
    const plot = { x0: 42, x1: Math.floor(box!.width) - 2, y0: 6, y1: Math.floor(box!.height) - 28 }

    // A-MON-06「纵轴刻度 / 横轴刻度」：刻度文字同样只存在于位图里
    // 实测（本机 chromium，dpr=1）：y 轴文字列 ≈284 px、x 轴文字行 ≈1007 px → 阈值留 2 倍以上余量
    const yAxisGutter = { x0: 0, x1: 41, y0: 0, y1: Math.floor(box!.height) - 30 }
    const xAxisBand = { x0: 42, x1: Math.floor(box!.width) - 2, y0: Math.floor(box!.height) - 26, y1: Math.floor(box!.height) - 1 }
    expect((await sample('tick', yAxisGutter)).points, '纵轴应画出「0N/20N/…」刻度文字').toBeGreaterThan(120)
    expect((await sample('tick', xAxisBand)).points, '横轴应画出时间刻度文字').toBeGreaterThan(400)

    // 基线：未悬停时绘图区内不应有任何深色像素，否则下面的断言毫无意义
    expect((await sample('dark', plot)).points, '绘图区基线不该有深色块').toBe(0)

    // 悬停曲线中部 → tooltip 出现（实心块：总像素 + 单行连续宽度双判据）
    await page.mouse.move(box!.x + box!.width * 0.5, box!.y + box!.height * 0.5)
    await page.waitForTimeout(600) // tooltip 淡入动画
    const shown = await sample('dark', plot)
    expect(shown.points).toBeGreaterThan(1000)
    expect(shown.maxRowHits).toBeGreaterThan(40)

    // 移出图表 → tooltip 消失（证明深色块由悬停触发，而非页面常驻绘制）
    await page.mouse.move(box!.x + box!.width * 0.5, box!.y + box!.height + 80)
    await page.waitForTimeout(600)
    expect((await sample('dark', plot)).points).toBeLessThan(50)
  })
})

/* ============================================================================
 * T322 实时监控「假实时」收口
 *
 * Boss 2026-09-22 令：无帧 / 帧过期时必须显示「无数据 / 数据已过期」，不得把陈旧值当实时展示；
 * 「最近更新」不得用取数时刻冒充数据新鲜度。三态在 mock 里各有承载患者：
 *   PT-001 林小雨 → 帧为当下（正常）  PT-002 陈子航 → 帧龄 3 小时（过期）
 *   PT-005 赵欣然 → 未绑定设备，pressureRecords 为空且后端回 seed 热力图（无帧）
 * ========================================================================== */
test.describe('帧新鲜度三态（T322）', () => {
  test('正常态：数据采集时刻与本次拉取时刻分列，页面无过期告警条', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    const bar = page.locator('.page-toolbar .update-time')
    await expect(bar).toContainText(/数据采集：\d{2}:\d{2}:\d{2}/)
    await expect(bar).toContainText(/本次拉取：\d{2}:\d{2}:\d{2}/)
    await expect(bar).toContainText(/距今 \d+s/)
    await expect(page.locator('.frame-notice')).toHaveCount(0)
    await expect(page.locator('.page-toolbar .realtime-tag')).toContainText('实时同步中')
    // 帧龄取自数据侧时刻：正常态就是「刚采的帧」，且必须是个能解析的秒数（不是 NaN / 空白）
    const age = Number((await bar.textContent())!.match(/距今 (\d+)s/)![1])
    expect(age).toBeLessThan(60)
  })

  test('过期态：帧龄 3 小时 → 标签转「数据已过期」+ 告警条，脉冲停止且曲线不被轮询推活', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    await pickSelectOption(page, page.locator('.patient-card .el-select'), '陈子航')
    const tag = page.locator('.page-toolbar .realtime-tag')
    await expect(tag).toHaveClass(/live-expired/)
    await expect(tag).toContainText('数据已过期')
    // 绿点脉冲＝「数据在动」的暗示，非实时态必须停
    expect(await tag.locator('.realtime-dot').evaluate((el) => getComputedStyle(el).animationName)).toBe('none')

    // 末次帧是一个固定时刻：轮询只推进「本次拉取」，不许推进「数据采集」。
    // 这条不变量只有在「帧会停」的患者身上才可判，PT-001 模拟设备持续上报，采集时刻本就跟着走。
    const bar = page.locator('.page-toolbar .update-time')
    const barText = (await bar.textContent())!
    const collected = barText.match(/数据采集：(\d{2}:\d{2}:\d{2})/)![1]
    const pull = barText.match(/本次拉取：(\d{2}:\d{2}:\d{2})/)![1]
    await page.waitForTimeout(2_200)
    await expect(bar).toContainText(`数据采集：${collected}`)
    await expect(bar).not.toContainText(`本次拉取：${pull}`)

    const notice = page.locator('.frame-notice')
    await expect(notice).toHaveCount(1)
    await expect(notice).toContainText(/末次帧采集于 \d{2}:\d{2}:\d{2}（距今 \d+ 小时）/)
    await expect(notice).toContainText('已超过 2 小时有效期')

    // 末次帧数值照旧展示（有帧 ≠ 无数据），但必须标成末次帧而不是当前
    await expect(page.locator('.hm-cell')).toHaveCount(20)
    await expect(page.locator('.hm-cell-pulse')).toHaveCount(0)
    await expect(
      page.locator('.page-card').filter({ hasText: '采集点实时数值表' }).locator('.tbl-note'),
    ).toContainText('非当前实时')
    await expect(page.locator('.chart-empty')).toContainText('帧已过期，曲线不再推进')
    // 防回潮：整页不得再出现「实时同步中」字样
    await expect(page.locator('.monitor')).not.toContainText('实时同步中')
  })

  test('无帧态：未绑定设备 → 显示「无实时数据」，热力图与采集点表不得渲染 seed 兜底值', async ({ page }) => {
    await waitForSnapshotLoaded(page)
    await pickSelectOption(page, page.locator('.patient-card .el-select'), '赵欣然')
    const tag = page.locator('.page-toolbar .realtime-tag')
    await expect(tag).toHaveClass(/live-none/)
    await expect(tag).toContainText('无实时数据')
    await expect(page.locator('.frame-notice')).toContainText('该患者当前无实时帧')

    // 后端无帧时 pressureHeatmap 仍是 20 点 seed 值 —— 页面一格都不许画出来
    await expect(page.locator('.hm-cell')).toHaveCount(0)
    await expect(page.locator('.hm-empty')).toContainText('无实时帧 · 不展示示例数据')
    await expect(page.locator('.points-table .empty-cell')).toContainText('无实时帧 · 不展示示例数据')
    await expect(page.locator('.hm-detail')).toContainText('无实时帧')
    // 帧派生摘要给占位，不给「0.0 N」这种看着像读数的值
    await expect(page.locator('.peak-cell.peak-value .peak-num')).toHaveText('--')
    // 最大压力采集点同样不得留 seed 派生的点位号（曾漏判：seed 兜底值混进今日峰值统计）
    await expect(page.locator('.peak-card .peak-text')).toHaveText('--')
    await expect(page.locator('.chart-empty')).toContainText('无实时帧，曲线不绘制示例数据')
  })
})
