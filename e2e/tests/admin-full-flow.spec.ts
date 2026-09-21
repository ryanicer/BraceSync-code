import { test, expect } from '@playwright/test'
import {
  adminRoutes, adminLogin, adminLogout, adminMessage, gotoMenu, tableRows, topBarUserName,
} from '../admin-helpers'

/**
 * admin-web 全链路：登录 → 首页 Dashboard → 告警处理 → 患者管理（→ 退出）
 * 验证运营管理员跨页面核心工作流（全部走菜单导航，模拟真实操作路径）
 */

test('登录 → Dashboard → 告警处理 → 患者管理 → 退出', async ({ page }) => {
  // 1. 登录
  await adminLogin(page, 'admin')
  await expect(page).toHaveURL(/\/dashboard/)
  await expect(topBarUserName(page)).toHaveText('运营管理员')

  // 2. 首页 Dashboard：6 KPI + 图表就绪
  await expect(page.locator('.kpi-card')).toHaveCount(6)
  await expect(page.locator('.kpi-card').filter({ hasText: '累计患者' }).locator('.kpi-value')).toHaveText('1256')
  await expect(page.getByText('团队佩戴达标排行')).toBeVisible()

  // 3. 菜单进入告警管理，处理一条待处理告警
  await gotoMenu(page, '告警管理')
  await expect(page).toHaveURL(/\/alerts/)
  const alertRow = tableRows(page).filter({ hasText: 'P10 压力持续偏高' })
  await expect(alertRow).toContainText('待处理')
  await alertRow.getByRole('button', { name: '处理', exact: true }).click()
  const dialog = page.locator('.el-dialog').filter({ hasText: '处理告警' })
  await dialog.locator('textarea').fill('全链路：已通知患者调整佩戴位置')
  await dialog.getByRole('button', { name: '确认处理' }).click()
  await expect(adminMessage(page)).toContainText('处理成功')

  // 4. 菜单进入患者管理，搜索并查看该告警关联患者
  await gotoMenu(page, '患者管理')
  await expect(page).toHaveURL(/\/patients/)
  await page.locator('.search-input input').fill('林小雨')
  await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).click()
  // T289 4.2：本页另有「批量患者-团队绑定」卡片（不受列表搜索影响），必须 scope 到列表卡片
  const patientRow = tableRows(page, page.locator('.patient-list-card'))
  await expect(patientRow).toHaveCount(1)
  await patientRow.first().click()
  const drawer = page.locator('.el-drawer')
  await expect(drawer).toContainText('林小雨（PT-001）')
  await drawer.locator('.el-drawer__close-btn').click()

  // 5. 退出登录，回到登录页且受保护页重新被拦截
  await adminLogout(page)
  await expect(page.locator('.login-title')).toBeVisible()
  await page.goto(adminRoutes.dashboard)
  await expect(page).toHaveURL(/\/login/)
})

test('医生工作链路：登录 → Dashboard → 告警查看 → 实时监控', async ({ page }) => {
  await adminLogin(page, 'doctor')
  await expect(page).toHaveURL(/\/dashboard/)
  await expect(topBarUserName(page)).toHaveText('张建国医生')

  // 医生可见菜单仅 4 项，告警管理在其中
  await gotoMenu(page, '告警管理')
  await expect(page).toHaveURL(/\/alerts/)
  await expect(tableRows(page)).toHaveCount(7) // T289 2.6/2.7：mock 告警补到 7 条（新增 wear_duration_short + processing 行）

  await gotoMenu(page, '实时监控')
  await expect(page).toHaveURL(/\/monitor/)
  await expect(page.locator('.page-toolbar .realtime-tag')).toContainText('实时同步中')
  await expect(page.locator('.update-time')).toContainText(/\d{2}:\d{2}:\d{2}/, { timeout: 15_000 })

  // 医生无权进入患者管理（菜单不可见，直达被拦）
  await expect(page.locator('.el-menu .el-menu-item').filter({ hasText: '患者管理' })).toHaveCount(0)
  await page.goto(adminRoutes.patients)
  await expect(page).toHaveURL(/\/403/)
})

test('客服工作链路：登录 → 患者沟通 → 标记处理', async ({ page }) => {
  await adminLogin(page, 'cs')
  await page.goto(adminRoutes.communication)
  await expect(page).toHaveURL(/\/communication/)
  await expect(page.locator('.role-hint')).toBeVisible()

  // 反馈列表 4 条，待处理可标记（T247: 左右布局，点击行在右侧操作）
  const rows = tableRows(page)
  await expect(rows).toHaveCount(4)
  const pendingRow = rows.filter({ hasText: 'FB-001' })
  await expect(pendingRow).toContainText('待处理')
  await pendingRow.click()

  // 右侧面板点"仅标记已处理"
  const rightPane = page.locator('.right-pane')
  await rightPane.getByRole('button', { name: '仅标记已处理' }).click()
  await expect(adminMessage(page)).toContainText('已标记处理')
  await expect(pendingRow).toContainText('已解决')

  // 查看已回复反馈 FB-002（点击行，右侧面板显示回复内容）
  await rows.filter({ hasText: 'FB-002' }).click()
  await expect(rightPane).toContainText('已核查，昨夜数据完整补传成功')
})
