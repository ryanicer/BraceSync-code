import { test, expect, type Page, type Locator } from '@playwright/test'
import { adminRoutes, adminLogin, tableRows } from '../admin-helpers'

/**
 * admin-web 安装记录：列表列序 / 校准状态三态 / 详情抽屉 20 点偏移值网格（T289 批次三 B 批 9.1~9.3）
 *
 * 设计稿依据 docs/design/admin/安装记录.html：
 *  - :105 搜索框 placeholder「搜索患者/设备ID/技师」
 *  - :108 列头 患者/设备ID/技师/安装时间/WiFi/校准/操作
 *  - :140 列表校准短词 正常/异常；:175 详情全称 校准正常/校准异常
 *  - :168-197 详情抽屉（患者/设备ID/技师/安装时间/WiFi + 传感器偏移值网格 + 安装备注）
 *  - :62 偏移网格 5 列；:189 编号 S01…S20
 *
 * mock 数据 apps/admin-web/src/mock/org.ts INSTALL_RECORDS（INS-001~005）：
 *  calibStatus = 正常(001/002/004) / 异常(003) / 未校准(005，无基线 ⇒ offsetValues 为 [])
 */
const listCard = (page: Page): Locator => page.locator('.install-list-card')
const listRows = (page: Page): Locator => tableRows(page, listCard(page))
const listHeads = (page: Page): Locator => listCard(page).locator('.el-table__header-wrapper thead th')
const drawerBody = (page: Page): Locator => page.locator('.el-drawer .detail')

async function openDetail(page: Page, installId: string): Promise<void> {
  const row = listRows(page).filter({ hasText: installId })
  await expect(row).toHaveCount(1)
  await row.locator('button', { hasText: '详情' }).click()
  await expect(drawerBody(page)).toBeVisible()
}

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.installRecords)
  await expect(listRows(page).first()).toBeVisible({ timeout: 15_000 })
})

test.describe('列表（T289 9.3）', () => {
  // T245 口径：设计稿没有的列（安装ID/基线/备注）不自行判删，只保证设计稿列相对同序
  test('设计稿列名与列序（患者→设备ID→技师→安装时间→WiFi→校准状态）', async ({ page }) => {
    const heads = await listHeads(page).evaluateAll((ths) =>
      ths.map((th) => (th.textContent ?? '').trim()).filter(Boolean),
    )
    expect(heads).toEqual(['安装ID', '患者', '设备ID', '技师', '安装时间', '基线', 'WiFi', '校准状态', '备注', '操作'])
  })

  test('校准状态三态徽标：正常 / 异常 / 未校准', async ({ page }) => {
    await expect(listRows(page).filter({ hasText: 'INS-001' })).toContainText('正常')
    await expect(listRows(page).filter({ hasText: 'INS-003' })).toContainText('异常')
    await expect(listRows(page).filter({ hasText: 'INS-005' })).toContainText('未校准')
  })

  test('搜索框按患者姓名可命中（设计稿 :105 / 契约 keyword 口径）', async ({ page }) => {
    await page.locator('.search-input input').fill('王梓萌')
    await page.getByRole('button', { name: '查询' }).click()
    const rows = listRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('INS-003')
    await expect(rows.first()).toContainText('王梓萌')
  })
})

test.describe('详情抽屉（T289 9.1/9.2）', () => {
  test('已校准记录展示 20 点偏移值网格（S01…S20，单位 N）', async ({ page }) => {
    await openDetail(page, 'INS-001')
    await expect(page.locator('.el-drawer__title')).toHaveText('安装详情')
    await expect(drawerBody(page).locator('.detail-calib')).toHaveText('校准正常')
    const cells = drawerBody(page).locator('.offset-cell')
    await expect(cells).toHaveCount(20)
    await expect(cells.first()).toContainText('S01')
    await expect(cells.first()).toContainText('0.12')
    await expect(cells.nth(11)).toContainText('S12')
    await expect(cells.last()).toContainText('S20')
    await expect(cells.last()).toContainText('0.05')
    await expect(drawerBody(page).locator('.section-title').first()).toHaveText('传感器偏移值 (N)')
    await expect(drawerBody(page).getByText('首次安装，空载校准通过')).toBeVisible()
  })

  test('校准异常记录：详情用全称徽标（设计稿 :175）', async ({ page }) => {
    await openDetail(page, 'INS-003')
    await expect(drawerBody(page).locator('.detail-calib')).toHaveText('校准异常')
    await expect(drawerBody(page).locator('.offset-cell')).toHaveCount(20)
  })

  test('未校准记录：契约口径 offsetValues 为空数组 ⇒ 网格空态', async ({ page }) => {
    await openDetail(page, 'INS-005')
    await expect(drawerBody(page).locator('.detail-calib')).toHaveText('未校准')
    await expect(drawerBody(page).locator('.offset-cell')).toHaveCount(0)
    await expect(drawerBody(page).getByText('尚未保存基线，无偏移值')).toBeVisible()
  })
})
