import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * admin-web 关键页抽查：设备管理（筛选）/ 技师管理（启停）/ 系统配置（阈值+通知规则）
 * mock 对齐：devices.ts（6 台，online 3 / abnormal 1 / offline 1 / unbound 1）、
 * org.ts（4 技师，冯师傅禁用）、system.ts（阈值默认值 + 4 条通知规则）
 */

test.describe('设备管理', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.devices)
  })

  test('渲染 6 台设备且状态 tag 正确', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows).toHaveCount(6)
    const onlineRow = rows.filter({ hasText: 'DEV-A3F312' })
    await expect(onlineRow.locator('.el-tag--success')).toContainText('在线')
    await expect(rows.filter({ hasText: 'DEV-C9D789' }).locator('.el-tag--danger')).toContainText('异常')
    await expect(rows.filter({ hasText: 'DEV-D2A012' }).locator('.el-tag--warning')).toContainText('离线')
    await expect(rows.filter({ hasText: 'DEV-F8C590' }).locator('.el-tag--info')).toContainText('未绑定')
  })

  test('状态筛选：在线 → 3 台', async ({ page }) => {
    await pickSelectOption(page, page.locator('.status-select'), '在线')
    await expect(tableRows(page)).toHaveCount(3)
  })

  test('状态筛选：异常 → 1 台（DEV-C9D789）', async ({ page }) => {
    await pickSelectOption(page, page.locator('.status-select'), '异常')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('DEV-C9D789')
  })

  test('关键词搜索设备ID（回车触发）', async ({ page }) => {
    await page.locator('.search-input input').fill('DEV-B7E456')
    await page.locator('.search-input input').press('Enter')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('陈子航')
  })
})

test.describe('技师管理', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.technicians)
  })

  test('渲染 4 名技师及认证/账号状态', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows).toHaveCount(4)
    await expect(rows.filter({ hasText: '周师傅' })).toContainText('已认证')
    await expect(rows.filter({ hasText: '郑师傅' })).toContainText('未认证')
    await expect(rows.filter({ hasText: '冯师傅' })).toContainText('禁用')
  })

  test('禁用技师：popconfirm 确认后状态翻转', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: '周师傅' })
    await row.getByRole('button', { name: '禁用' }).click()
    await expect(page.locator('.el-popconfirm')).toContainText('确认禁用技师 周师傅？')
    await page.locator('.el-popconfirm').getByRole('button', { name: '确定' }).click()
    await expect(adminMessage(page)).toContainText('已禁用')
    await expect(row).toContainText('禁用')
    await expect(row.getByRole('button', { name: '启用' })).toBeVisible()
  })

  test('启用技师：冯师傅 禁用 → 启用', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: '冯师傅' })
    await row.getByRole('button', { name: '启用' }).click()
    await page.locator('.el-popconfirm').getByRole('button', { name: '确定' }).click()
    await expect(adminMessage(page)).toContainText('已启用')
    await expect(row).toContainText('启用')
  })

  test('popconfirm 取消不改变状态', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: '吴师傅' })
    await row.getByRole('button', { name: '禁用' }).click()
    await page.locator('.el-popconfirm').getByRole('button', { name: '取消' }).click()
    await expect(row).toContainText('启用')
  })
})

test.describe('系统配置', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.settings)
  })

  test('阈值表单加载默认值（对齐 DEFAULT_THRESHOLDS）', async ({ page }) => {
    await expect(page.getByText('全局系统参数')).toBeVisible()
    await expect(page.locator('.settings-form')).toContainText('每日佩戴目标时长')
    await expect(page.locator('.settings-form')).toContainText('压力偏高阈值')
    await expect(page.locator('.settings-form')).toContainText('佩戴中断判定时间')
    // mock 默认每日佩戴目标 22h（el-input-number 值在 input value 上，非文本节点）
    await expect(page.locator('.settings-form .el-input-number').first().locator('input')).toHaveValue('22')
  })

  test('保存配置成功提示', async ({ page }) => {
    await page.locator('.settings-form').getByRole('button', { name: '保存配置' }).click()
    await expect(adminMessage(page)).toContainText('配置已保存')
  })

  test('WiFi 预设列表展示脱敏密码', async ({ page }) => {
    await expect(page.getByText('WiFi 预设列表')).toBeVisible()
    await expect(page.getByText('Hospital-WiFi').first()).toBeVisible()
    await expect(page.getByText('********').first()).toBeVisible()
  })

  test('通知规则 tab：4 类告警规则与渠道/对象勾选', async ({ page }) => {
    await page.getByRole('tab', { name: '通知规则' }).click()
    const card = page.locator('.page-card').filter({ hasText: '告警通知规则' })
    await expect(card.locator('.el-table__body-wrapper tbody tr')).toHaveCount(4)
    // 压力偏高：微信+短信 双渠道勾选
    const pressureRow = card.locator('tbody tr').filter({ hasText: '压力偏高' })
    await expect(pressureRow.locator('.el-checkbox.is-checked')).toHaveCount(4) // 渠道2 + 对象2
  })

  test('通知规则切换勾选后提示更新成功', async ({ page }) => {
    await page.getByRole('tab', { name: '通知规则' }).click()
    const card = page.locator('.page-card').filter({ hasText: '告警通知规则' })
    const wearRow = card.locator('tbody tr').filter({ hasText: '佩戴中断' })
    // 佩戴中断默认仅微信 + 患者；追加勾选短信渠道
    await wearRow.locator('.el-checkbox').filter({ hasText: '短信' }).click()
    await expect(adminMessage(page)).toContainText('通知渠道已更新')
  })

  test('发送记录 tab：4 条记录与状态 tag', async ({ page }) => {
    await page.getByRole('tab', { name: '发送记录' }).click()
    // el-tabs 三个 pane 同时挂载，仅可见 pane 的表格参与断言
    const rows = page.locator('.el-table:visible .el-table__body-wrapper tbody tr')
    await expect(rows).toHaveCount(4)
    await expect(rows.filter({ hasText: 'NTF-001' })).toContainText('已发送')
    await expect(rows.filter({ hasText: 'NTF-002' })).toContainText('失败')
    await expect(rows.filter({ hasText: 'NTF-003' })).toContainText('降级短信')
  })
})
