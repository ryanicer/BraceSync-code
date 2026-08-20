import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * admin-web 告警管理：列表渲染 + 类型/状态筛选 + 处理流程（复用 T019B processAlert 模式）
 * mock 数据对齐 mock/alerts.ts：6 条（pending 3 / processed 3；pressure_high 2 / wear_interrupt 2）
 */

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.alerts)
})

test.describe('告警列表', () => {
  test('渲染 6 条告警且列信息完整', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows).toHaveCount(6)
    // 首行 ALR-001：压力偏高 / 林小雨 / 待处理 / 进行中
    const first = rows.first()
    await expect(first).toContainText('压力偏高')
    await expect(first).toContainText('林小雨')
    await expect(first).toContainText('DEV-A3F312')
    await expect(first).toContainText('60/68.5N')
    await expect(first).toContainText('待处理')
    await expect(first).toContainText('进行中')
  })

  test('已处理告警显示处理人', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: 'P05 压力波动异常' })
    await expect(row).toContainText('已处理')
    await expect(row).toContainText('张建国')
  })

  test('分页组件显示共 6 条', async ({ page }) => {
    await expect(page.locator('.el-pagination')).toContainText('共 6 条')
  })
})

test.describe('筛选', () => {
  test('按类型筛选：压力偏高 → 2 条', async ({ page }) => {
    await pickSelectOption(page, page.locator('.filter-select').first(), '压力偏高')
    await expect(tableRows(page)).toHaveCount(2)
    await expect(tableRows(page).first()).toContainText('压力偏高')
  })

  test('按状态筛选：待处理 → 3 条', async ({ page }) => {
    await pickSelectOption(page, page.locator('.filter-select').nth(1), '待处理')
    await expect(tableRows(page)).toHaveCount(3)
  })

  test('类型 + 状态组合筛选：佩戴中断 × 待处理 → 1 条', async ({ page }) => {
    await pickSelectOption(page, page.locator('.filter-select').first(), '佩戴中断')
    await pickSelectOption(page, page.locator('.filter-select').nth(1), '待处理')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('陈子航')
  })

  test('清空筛选恢复 6 条', async ({ page }) => {
    await pickSelectOption(page, page.locator('.filter-select').first(), '压力偏高')
    await expect(tableRows(page)).toHaveCount(2)
    // clearable：EP 2.14 新 select 的清空图标 hover 才渲染（.el-select__clear）
    const typeSelect = page.locator('.filter-select').first()
    await typeSelect.hover()
    await typeSelect.locator('.el-select__clear').click()
    await expect(tableRows(page)).toHaveCount(6)
  })
})

test.describe('处理流程', () => {
  test('待处理告警可打开处理对话框并确认处理', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: 'P10 压力持续偏高' })
    await row.getByRole('button', { name: '处理' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '处理告警' })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText('压力偏高')
    await dialog.locator('textarea').fill('已通知患者调整佩戴位置（e2e）')
    await dialog.getByRole('button', { name: '确认处理' }).click()
    await expect(adminMessage(page)).toContainText('处理成功')
    await expect(dialog).toBeHidden()
  })

  test('已处理告警无处理按钮', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: 'P05 压力波动异常' })
    await expect(row.getByRole('button', { name: '处理' })).toHaveCount(0)
  })

  test('处理对话框可取消', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: 'P12 传感器数据漂移' })
    await row.getByRole('button', { name: '处理' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '处理告警' })
    await dialog.getByRole('button', { name: '取消' }).click()
    await expect(dialog).toBeHidden()
    // 取消后仍为待处理
    await expect(row).toContainText('待处理')
  })
})
