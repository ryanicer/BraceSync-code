import { test, expect, type Page, type Locator } from '@playwright/test'
import { adminRoutes, adminLogin, tableRows } from '../admin-helpers'

/**
 * T301 G1 补齐：安装记录页内容级用例（本页此前仅被 admin-permissions 的 goto 判不 403 覆盖）
 *
 * mock 对齐 apps/admin-web/src/mock/org.ts INSTALL_RECORDS（5 条）：
 * - INS-001..004 baselineId 非空 → 基线列「已保存」；INS-005 baselineId=null →「待保存」
 * - INS-003 / INS-005 wifiStatus=unconfigured →「未配网」，其余「已配网」
 * 本页为只读列表（无新增/编辑入口），不写共享 seed。
 */

const rowOf = (page: Page, installId: string): Locator =>
  tableRows(page).filter({ hasText: installId })

// 列序：0 安装ID 1 设备 2 患者 3 技师 4 校准时间 5 基线 6 WiFi 7 备注
const cellTag = (row: Locator, col: number): Locator => row.locator('td').nth(col).locator('.el-tag')

test.describe('安装记录', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.installRecords)
  })

  test('渲染 5 条记录，患者/技师列显示姓名而非 ID', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows).toHaveCount(5)
    const first = rowOf(page, 'INS-001')
    await expect(first).toContainText('DEV-A3F312')
    await expect(first).toContainText('林小雨')
    await expect(first).toContainText('周师傅')
    // join 反证：列表里不应把患者主键当展示值（T269 D1 / T278 同一口径）
    await expect(first).not.toContainText('PT-001')
  })

  test('校准时间渲染为「日期 时:分」', async ({ page }) => {
    await expect(rowOf(page, 'INS-003').locator('td').nth(4)).toHaveText('2026-05-18 15:30')
  })

  test('基线列：有 baselineId 显示已保存，INS-005 显示待保存', async ({ page }) => {
    await expect(cellTag(rowOf(page, 'INS-001'), 5)).toHaveClass(/el-tag--success/)
    await expect(cellTag(rowOf(page, 'INS-001'), 5)).toHaveText('已保存')
    await expect(cellTag(rowOf(page, 'INS-005'), 5)).toHaveClass(/el-tag--warning/)
    await expect(cellTag(rowOf(page, 'INS-005'), 5)).toHaveText('待保存')
  })

  test('WiFi 列：未配网的两条记录标 info，其余标 success', async ({ page }) => {
    await expect(cellTag(rowOf(page, 'INS-003'), 6)).toHaveText('未配网')
    await expect(cellTag(rowOf(page, 'INS-003'), 6)).toHaveClass(/el-tag--info/)
    await expect(cellTag(rowOf(page, 'INS-002'), 6)).toHaveText('已配网')
    await expect(cellTag(rowOf(page, 'INS-002'), 6)).toHaveClass(/el-tag--success/)
  })

  test('关键词搜索设备 ID → 只剩 1 条', async ({ page }) => {
    const search = page.locator('.search-input input')
    await search.fill('DEV-B7E456')
    await search.press('Enter')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('INS-002')
  })

  test('关键词搜索安装 ID 大小写不敏感', async ({ page }) => {
    const search = page.locator('.search-input input')
    await search.fill('ins-004')
    await search.press('Enter')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('DEV-D2A012')
  })

  test('清空关键词恢复全量，分页汇总显示共 5 条', async ({ page }) => {
    const search = page.locator('.search-input input')
    await expect(page.locator('.el-pagination__total')).toHaveText('共 5 条')
    await search.fill('INS-005')
    await search.press('Enter')
    await expect(tableRows(page)).toHaveCount(1)
    await search.fill('')
    await search.press('Enter')
    await expect(tableRows(page)).toHaveCount(5)
  })
})
