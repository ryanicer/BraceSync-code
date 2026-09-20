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
    await expect(first).toContainText('60.00N/68.50N')
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

test.describe('告警规则配置（T253-2.2）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.alerts)
    await page.getByRole('tab', { name: '告警规则配置' }).click()
  })

  test('渲染 4×5 网格 20 点 + 两张规则卡', async ({ page }) => {
    await expect(page.locator('.alert-grid .grid-cell')).toHaveCount(20)
    await expect(page.locator('.card-title').filter({ hasText: '按采集点设置告警阈值' })).toBeVisible()
    await expect(page.locator('.card-title').filter({ hasText: '全局告警规则' })).toBeVisible()
    // 首格 P01/R1C1 默认全选：已选 20 / 20
    await expect(page.locator('.selected-count')).toContainText('已选: 20 / 20')
  })

  test('点击格子切换勾选并更新已选计数', async ({ page }) => {
    const firstCell = page.locator('.alert-grid .grid-cell').first()
    await expect(firstCell).toHaveClass(/monitored/)
    await firstCell.click()
    // 单击 250ms 延迟分派（与双击编辑区分）
    await expect(firstCell).not.toHaveClass(/monitored/, { timeout: 3000 })
    await expect(page.locator('.selected-count')).toContainText('已选: 19 / 20')
  })

  test('全不选 → 已选 0，全选恢复 20', async ({ page }) => {
    await page.getByRole('button', { name: '全不选' }).click()
    await expect(page.locator('.selected-count')).toContainText('已选: 0 / 20')
    await page.getByRole('button', { name: '全选 (20点)' }).click()
    await expect(page.locator('.selected-count')).toContainText('已选: 20 / 20')
  })

  test('双击格子打开独立阈值编辑弹窗并保存', async ({ page }) => {
    const firstCell = page.locator('.alert-grid .grid-cell').first()
    await firstCell.dblclick()
    const dialog = page.locator('.el-dialog').filter({ hasText: '独立阈值' })
    await expect(dialog).toBeVisible()
    // 上限留空 = 跟随统一；下限独立 8N
    await dialog.locator('.el-form-item').nth(1).locator('input').fill('8')
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(dialog).toBeHidden()
    // 首格 chip 显示生效值 45/8N（统一上限 45 / 独立下限 8）
    await expect(page.locator('.point-chips .point-chip').first()).toContainText('P01 45/8N')
  })

  test('保存规则成功', async ({ page }) => {
    await page.getByRole('button', { name: '保存规则' }).click()
    await expect(adminMessage(page)).toContainText('保存成功')
  })

  test('恢复默认成功', async ({ page }) => {
    await page.getByRole('button', { name: '恢复默认' }).click()
    await expect(adminMessage(page)).toContainText('已恢复默认')
  })

  test('保存全局规则成功', async ({ page }) => {
    await page.getByRole('button', { name: '保存全局规则' }).click()
    await expect(adminMessage(page)).toContainText('全局规则保存成功')
  })
})
