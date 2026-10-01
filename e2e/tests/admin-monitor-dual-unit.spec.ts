import { test, expect, type Page } from '@playwright/test'
import { adminRoutes, adminLogin, pickSelectOption } from '../admin-helpers'
import { AREA_MISSING_HINT } from '@bracesync/shared-utils'
import {
  mockPatientRealtime,
  AREA_UNSET_PATIENT_ID,
  MOCK_AREA_CM2,
  type PressureHeatmapPoint,
} from '../../apps/admin-web/src/mock/patients'

/**
 * T513 admin 实时监控双单位（N / kPa）—— 真实浏览器链路（dev server + 轮询）。
 *
 * 与 apps/admin-web/test/monitor-dual-unit.spec.ts（mount）的分工：
 * - mount 那份掌握数据与切档时机，做「同一份快照在两档下各呈现成什么」的成对比较（确定性）；
 * - 本份证明真实页在真实 DOM / 真实轮询下确实接上了档位（选择器与浏览器渲染不是纸面接线）。
 * 🔴 判据 2「后端无新增单位请求、无落库」在本文件里不成立也不假充：mock 模式下 api 层在浏览器
 * 进程内直接 return 快照，全程本就没有 /api 请求可数（空集断言＝假绿）。该腿由两处承担：
 * 患者端 H5 e2e（monitor-unit-toggle.spec.ts 的「切档零请求」，route 拦截下确有请求可数）
 * ＋ admin 侧源码级门禁（unitPref 只碰 localStorage、switchUnit 不取数）。staging 网络面板归 O3。
 *
 * 夹具锚点：mock/patients.ts 是「服务端替身」，其 pressureKpa/heatmapMaxKpa 由 mockKpaOf 下发；
 * 本用例只把页面文本与这些**下发值**对平，不在用例里再实现一遍换算公式。
 */

const seg = (page: Page, u: 'N' | 'kPa') => page.locator('.unit-seg-btn', { hasText: u })
const activeSegText = (page: Page) => page.locator('.unit-seg-btn.unit-seg-active')
const cellVals = (page: Page) => page.locator('.hm-cell-val')
const cellStyles = (page: Page) =>
  page.locator('.hm-cell').evaluateAll((els) => els.map((e) => e.getAttribute('style') ?? ''))
const maxCellIdx = (page: Page) =>
  page.locator('.hm-cell').evaluateAll((els) => els.reduce<number[]>((acc, e, i) => (e.classList.contains('hm-cell-max') ? [...acc, i] : acc), []))
const tableVals = (page: Page) => page.locator('.points-table tbody tr td:nth-child(3)')
const statusCells = (page: Page) => page.locator('.points-table tbody tr td:nth-child(4)')
const headers = (page: Page) => page.locator('.points-table thead th')
const detail = (page: Page) => page.locator('.hm-detail')
const peak = (page: Page) => page.locator('.peak-cell.peak-value .peak-num')

/** 等首帧落地（20 格渲染完），避免与每秒轮询抢读 */
async function waitForFrame(page: Page): Promise<void> {
  await expect(cellVals(page)).toHaveCount(20, { timeout: 15_000 })
}

async function openMonitor(page: Page): Promise<void> {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.monitor)
  await waitForFrame(page)
}

async function switchUnit(page: Page, u: 'N' | 'kPa'): Promise<void> {
  await seg(page, u).click()
  await expect(activeSegText(page)).toHaveText(u)
}

/** 下发值即页面应呈现的 kPa 文本（逐格、逐行） */
function kpaTexts(ht: PressureHeatmapPoint[]): string[] {
  return ht.map((p) => String(p.pressureKpa))
}
function nTexts(ht: PressureHeatmapPoint[]): string[] {
  return ht.map((p) => Number(p.pressureValue).toFixed(1))
}

test('默认 N 档：落 N、四列表头带 (N)、格子与表列逐格等于快照下发值（PRD 四.2）', async ({ page }) => {
  await openMonitor(page)
  await expect(activeSegText(page)).toHaveText('N')
  await expect(headers(page)).toHaveText(['采集点', '位置', '当前压力 (N)', '状态'])
  const ht = mockPatientRealtime('PT-001').pressureHeatmap ?? []
  await expect(cellVals(page)).toHaveText(nTexts(ht))
  await expect(tableVals(page)).toHaveText(nTexts(ht))
  await expect(peak(page)).toContainText(' N')
  await expect(page.locator('.hm-area-warn')).toHaveCount(0)
})

test('切到 kPa：表头字母/格子/表列/摘要/详情行/悬浮 title 六面同换，数字真的变了（四.9②⑤）', async ({ page }) => {
  await openMonitor(page)
  const ht = mockPatientRealtime('PT-001').pressureHeatmap ?? []
  expect(ht).toHaveLength(20)
  expect(MOCK_AREA_CM2).toBeGreaterThan(0)
  const nVals = await cellVals(page).allTextContents()

  await switchUnit(page, 'kPa')
  await expect(headers(page)).toHaveText(['采集点', '位置', '当前压力 (kPa)', '状态'])
  // 表头四列不变宽 —— 数值列就地换读法，不并列成第二列
  await expect(headers(page)).toHaveCount(4)
  await expect(cellVals(page)).toHaveText(kpaTexts(ht))
  await expect(tableVals(page)).toHaveText(kpaTexts(ht))
  // 反证：kPa 档数字确实不同于 N 档（面积在位、量级差约 15.6 倍），否则上面的对平是恒真
  expect(await cellVals(page).allTextContents()).not.toEqual(nVals)

  const maxPt = ht.find((p) => p.isMax)
  expect(maxPt, '夹具应标出一个最大点').toBeTruthy()
  await expect(peak(page)).toHaveText(`${maxPt!.pressureKpa} kPa`)
  await expect(detail(page)).toContainText(`${maxPt!.pressureKpa} kPa`)
  const title = await page.locator('.hm-cell').first().getAttribute('title')
  expect(title ?? '').toMatch(/: \d+ kPa$/)

  // 切回 N：数字面逐字复原（切档是显示态，不是数据变更）
  await switchUnit(page, 'N')
  await expect(cellVals(page)).toHaveText(nVals)
  await expect(peak(page)).toContainText(' N')
})

test('判档恒 N：切档后配色 / 最大点 / 状态列逐格不变（四.9③）', async ({ page }) => {
  await openMonitor(page)
  const before = {
    color: await cellStyles(page),
    isMax: await maxCellIdx(page),
    status: await statusCells(page).allTextContents(),
  }
  expect(before.color).toHaveLength(20)
  expect(before.isMax).toHaveLength(1)
  expect(before.status).toHaveLength(20)

  await switchUnit(page, 'kPa')
  // 数字面必须真的动了，否则「三面相同」可能只是页面没重渲染
  const ht = mockPatientRealtime('PT-001').pressureHeatmap ?? []
  await expect(cellVals(page)).toHaveText(kpaTexts(ht))
  expect(await cellStyles(page)).toEqual(before.color)
  expect(await maxCellIdx(page)).toEqual(before.isMax)
  expect(await statusCells(page).allTextContents()).toEqual(before.status)
  // 状态单元格前面有状态圆点的空白，判据按去空白后的四选一
  for (const s of before.status) expect(s.trim()).toMatch(/^(正常|关注|偏高|无信号)$/)
})

test('fail-closed：面积未配置的患者（PT-003）kPa 档只出 --，N 档照旧出数（五.3）', async ({ page }) => {
  await openMonitor(page)
  // N 档不依赖面积：PT-001（面积在位）此刻也不许冒出提示行
  await expect(page.locator('.hm-area-warn')).toHaveCount(0)

  await pickSelectOption(page, page.locator('.patient-card .el-select'), '王梓萌')
  await waitForFrame(page)
  // 换成 PT-003（contactAreaCm2=null）：N 档照旧逐格有读数，且仍无提示行
  await expect(cellVals(page)).toHaveText(nTexts(mockPatientRealtime(AREA_UNSET_PATIENT_ID).pressureHeatmap ?? []))
  await expect(page.locator('.hm-area-warn')).toHaveCount(0)

  await switchUnit(page, 'kPa')
  await expect(cellVals(page)).toHaveText(Array.from({ length: 20 }, () => '--'))
  await expect(tableVals(page)).toHaveText(Array.from({ length: 20 }, () => '--'))
  // 不许退化成 0（0 会被读成「压力为零」）
  expect((await cellVals(page).allTextContents()).join(',')).not.toMatch(/(^|[,.\s])0([,.\s]|$)/)
  await expect(peak(page)).toHaveText('--')
  await expect(detail(page)).toContainText('· --')
  await expect(page.locator('.hm-area-warn')).toHaveText(AREA_MISSING_HINT)

  // 反证（同档两患者对照）：面积在位的 PT-001 在同一 kPa 档下既有数字也无提示行 ⇒ 上面两条判据不恒真
  await pickSelectOption(page, page.locator('.patient-card .el-select'), '林小雨')
  await waitForFrame(page)
  await expect(activeSegText(page)).toHaveText('kPa')
  await expect(page.locator('.hm-area-warn')).toHaveCount(0)
  expect((await cellVals(page).allTextContents()).some((t) => t !== '--')).toBe(true)
  await expect(peak(page)).toContainText(' kPa')
})

test('裁定 e：档位记忆只走本地，刷新页面仍恢复 kPa', async ({ page }) => {
  await openMonitor(page)
  await switchUnit(page, 'kPa')
  expect(await page.evaluate(() => localStorage.getItem('admin_monitor_unit'))).toBe('kPa')

  await page.reload()
  await waitForFrame(page)
  await expect(activeSegText(page)).toHaveText('kPa')
  await expect(headers(page)).toHaveText(['采集点', '位置', '当前压力 (kPa)', '状态'])

  // 反证：把记忆键删掉再进页回 N，证明上一条的 kPa 真来自记忆而非默认值
  await page.evaluate(() => localStorage.removeItem('admin_monitor_unit'))
  await page.reload()
  await waitForFrame(page)
  await expect(activeSegText(page)).toHaveText('N')
})

test('脏记忆值回落 N：本地存储被写脏时不开在无法识别的档上', async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.evaluate(() => localStorage.setItem('admin_monitor_unit', 'KG'))
  await page.goto(adminRoutes.monitor)
  await waitForFrame(page)
  await expect(activeSegText(page)).toHaveText('N')
  await expect(headers(page)).toHaveText(['采集点', '位置', '当前压力 (N)', '状态'])
})
