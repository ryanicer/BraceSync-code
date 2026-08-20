import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * admin-web 患者管理：列表 / 关键词搜索 / 团队筛选 / 分页 / 详情抽屉
 * mock 数据对齐 mock/patients.ts：6 名患者（PT-001~PT-006，TEAM-001 有 2 名）
 */

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.patients)
})

test.describe('列表渲染', () => {
  test('渲染 6 名患者且列信息完整', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows).toHaveCount(6)
    const first = rows.first()
    await expect(first).toContainText('PT-001')
    await expect(first).toContainText('林小雨')
    await expect(first).toContainText('28°')
    await expect(first).toContainText('脊柱侧弯一组')
    await expect(first).toContainText('张建国')
    await expect(first).toContainText('DEV-A3F312')
    await expect(first).toContainText('活跃')
  })

  test('未绑定设备患者显示未绑定与待分配', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: '赵欣然' })
    await expect(row).toContainText('未绑定')
    await expect(row).toContainText('待分配')
  })

  test('分页组件显示共 6 条', async ({ page }) => {
    await expect(page.locator('.el-pagination')).toContainText('共 6 条')
  })
})

test.describe('搜索与筛选', () => {
  test('按姓名搜索：林 → 仅林小雨', async ({ page }) => {
    await page.locator('.search-input input').fill('林')
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).click()
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('林小雨')
  })

  test('按患者ID搜索：PT-002 → 陈子航', async ({ page }) => {
    await page.locator('.search-input input').fill('PT-002')
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).click()
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('陈子航')
  })

  test('回车触发搜索', async ({ page }) => {
    await page.locator('.search-input input').fill('王梓萌')
    await page.locator('.search-input input').press('Enter')
    await expect(tableRows(page)).toHaveCount(1)
  })

  test('按团队筛选：脊柱侧弯一组 → 2 名', async ({ page }) => {
    await pickSelectOption(page, page.locator('.team-select'), '脊柱侧弯一组')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(2)
    await expect(rows.nth(0)).toContainText('林小雨')
    await expect(rows.nth(1)).toContainText('陈子航')
  })

  test('清空搜索恢复 6 名', async ({ page }) => {
    await page.locator('.search-input input').fill('林')
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).click()
    await expect(tableRows(page)).toHaveCount(1)
    await page.locator('.search-input input').clear()
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).click()
    await expect(tableRows(page)).toHaveCount(6)
  })
})

test.describe('详情抽屉', () => {
  test('点击行打开详情抽屉', async ({ page }) => {
    await tableRows(page).filter({ hasText: '林小雨' }).click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible()
    await expect(drawer).toContainText('林小雨（PT-001）')
    await expect(drawer).toContainText('青少年特发性脊柱侧弯')
    await expect(drawer).toContainText('28°')
    await expect(drawer).toContainText('脊柱侧弯一组')
    await expect(drawer).toContainText('建档时间')
  })

  test('抽屉可关闭', async ({ page }) => {
    await tableRows(page).filter({ hasText: '陈子航' }).click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible()
    await drawer.locator('.el-drawer__close-btn').click()
    await expect(drawer).toBeHidden()
  })
})
