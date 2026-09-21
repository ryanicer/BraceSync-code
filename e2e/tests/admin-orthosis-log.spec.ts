import { test, expect, type Page, type Locator } from '@playwright/test'
import { adminRoutes, adminLogin, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * admin-web 矫形日志（T289 批次三 B 批 8.1 跨患者视图 + 8.2 佩戴感受两档）
 *
 * 设计稿依据 docs/design/admin/矫形日志.html：
 *  - :110-118 筛选条件（搜索患者姓名 / ID、日期范围、全部感受下拉、重置）
 *  - :116 感受下拉两档 贴合 / 不适
 *  - :127 列头 患者/日期/佩戴感受/备注/提交时间/操作，患者列两行显示姓名 + 编号
 *  - :121 「共 N 条记录」
 *  - :204-221 日志详情弹窗
 * PM 裁定 ⑤：跨患者视图为默认，PRD §7D.11「选择患者」工作台并存保留（本文件最后一个 describe）。
 * PM 裁定 ⑥：前端只出两档徽标；真库 discomfort 因 comfort_level VARCHAR(8) 装不下而恒空（T302/待 Boss）。
 */
const listCard = (page: Page): Locator => page.locator('.log-list-card')
const listRows = (page: Page): Locator => tableRows(page, listCard(page))
const listHeads = (page: Page): Locator => listCard(page).locator('.el-table__header-wrapper thead th')
const dialog = (page: Page): Locator => page.locator('.el-dialog:visible')

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.orthosisLog)
  await expect(listRows(page).first()).toBeVisible({ timeout: 15_000 })
})

test.describe('跨患者日志列表（默认视图）', () => {
  test('列名与列序对齐设计稿 :127', async ({ page }) => {
    const heads = await listHeads(page).evaluateAll((ths) =>
      ths.map((th) => (th.textContent ?? '').trim()).filter(Boolean),
    )
    expect(heads).toEqual(['患者', '日期', '佩戴感受', '备注', '提交时间', '操作'])
  })

  test('共 8 条记录，患者列两行显示姓名 + 编号', async ({ page }) => {
    await expect(listRows(page)).toHaveCount(8)
    await expect(listCard(page).locator('.list-count')).toHaveText('共 8 条记录')
    const row = listRows(page).filter({ hasText: '林小雨' }).first()
    await expect(row).toContainText('林小雨')
    await expect(row).toContainText('PT-001')
  })

  // T289 8.2：徽标两档 + 未评（真库种子 comfort_level 为 NULL）
  test('佩戴感受徽标：贴合 / 不适 / 未评', async ({ page }) => {
    const wang = listRows(page).filter({ hasText: '王梓萌' })
    await expect(wang.first()).toContainText('贴合') // FL-004 · 2026-08-11
    await expect(wang.nth(1)).toContainText('不适') // FL-005 · 2026-08-10
    await expect(listRows(page).filter({ hasText: '孙浩然' })).toContainText('未评')
  })

  test('提交时间列在后端未下发 created_at 前显示占位，不用日期列冒充', async ({ page }) => {
    await expect(listRows(page).first().locator('td').nth(4)).toHaveText('—')
  })

  test('按患者姓名搜索后只剩该患者，重置恢复全量', async ({ page }) => {
    await page.locator('.log-search input').fill('王梓萌')
    await page.keyboard.press('Enter')
    await expect(listRows(page)).toHaveCount(2)
    await expect(listRows(page).filter({ hasText: 'PT-003' })).toHaveCount(2)

    await page.getByRole('button', { name: '重置' }).click()
    await expect(listRows(page)).toHaveCount(8)
  })

  test('感受筛选「不适」只出 discomfort 行', async ({ page }) => {
    await pickSelectOption(page, page.locator('.feeling-filter'), '不适')
    await expect(listRows(page)).toHaveCount(2)
    await expect(listRows(page).filter({ hasText: '陈子航' })).toHaveCount(1)
    await expect(listRows(page).filter({ hasText: '王梓萌' })).toHaveCount(1)
    await expect(listCard(page).locator('.list-count')).toHaveText('共 2 条记录')
  })
})

test.describe('日志详情弹窗', () => {
  test('点「查看」打开弹窗，字段与设计稿 :206-211 同序', async ({ page }) => {
    await listRows(page).filter({ hasText: '陈子航' }).first().locator('button', { hasText: '查看' }).click()
    await expect(dialog(page).locator('.el-dialog__title')).toHaveText('矫形日志详情')
    const labels = await dialog(page).locator('.el-descriptions__label').allTextContents()
    expect(labels.map((t) => t.trim())).toEqual(['患者姓名', '患者ID', '日期', '佩戴感受', '提交时间'])
    await expect(dialog(page)).toContainText('陈子航')
    await expect(dialog(page)).toContainText('PT-002')
    await expect(dialog(page)).toContainText('2026-08-11')
    await expect(dialog(page).locator('.detail-note-body')).toHaveText('腰部压得比较疼')
    // exact：el-dialog 自带的关闭按钮 aria-label 是「关闭此对话框」，子串匹配会撞车
    await dialog(page).getByRole('button', { name: '关闭', exact: true }).click()
    await expect(dialog(page)).toHaveCount(0)
  })
})

test.describe('患者工作台（PM 裁定 ⑤ 并存保留）', () => {
  test('切到工作台可继续按患者看方案 / 感受 / 报告，感受表新增两档列', async ({ page }) => {
    // el-tab-pane 的非激活页签仍在 DOM 内 ⇒ 本 describe 一律 scope 到 .view-workspace
    const workspace = page.locator('.view-workspace')
    await page.getByRole('tab', { name: '患者工作台' }).click()
    await expect(workspace.getByText('请选择患者开始诊断评估')).toBeVisible()
    await pickSelectOption(page, workspace.locator('.patient-select'), '林小雨')
    await expect(workspace.locator('.page-card-title').first()).toHaveText('方案调整')

    await workspace.getByRole('tab', { name: '佩戴感受' }).click()
    const rows = tableRows(page, workspace.locator('.workspace-feelings-card'))
    await expect(rows).toHaveCount(2)
    await expect(rows.first()).toContainText('贴合')
  })
})
