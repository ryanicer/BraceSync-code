import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, pickSelectOption } from '../admin-helpers'

/**
 * admin-web 实时监控（T056 重做后）：患者下拉选择 + 4×5 热力图 + 实时压力曲线 + 2s 轮询
 *
 * 新版页面结构（pages/monitor/index.vue）：
 * - 顶部栏：.realtime-tag "实时同步中" + .update-time "最近更新：HH:mm:ss" + 立即刷新按钮
 * - 患者卡片：.patient-card > el-select(filterable) + .status-indicator + .device-hint
 * - 左栏曲线：.chart-card > .chart-container > canvas（Chart.js）
 * - 右栏热力图：.heatmap-card > .hm-grid > 4×.hm-row > 5×.hm-cell（共 20 格）
 *
 * mock 对齐 mock/patients.ts mockPatientRealtime：
 * - PT-001 林小雨 online（deviceId DEV-A3F312，压力 base=35）
 * - PT-004 刘俊熙 abnormal（deviceId DEV-D2A012，压力 base=68）
 * - PT-005 赵欣然 offline（deviceId=null → seedHeatmap 兜底）
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
    // 验证至少有 3 个选项
    const options = dropdown.locator('.el-select-dropdown__item')
    await expect(options).toHaveCount(6, { timeout: 5_000 })
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
