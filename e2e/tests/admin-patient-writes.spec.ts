import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * T057 患者管理写功能 E2E（KNOWN_RED）
 *
 * 设计源：docs/design/admin/患者管理.html
 * 覆盖 3 个写功能：添加患者 / 分配团队 / 批量绑定
 *
 * 预期红态：admin-web 患者管理页当前仅列表+搜索+详情（无写操作 UI），
 * 所有 test.fail 用例因找不到"添加患者"按钮、"分配团队"入口、"批量绑定"按钮等
 * 而 FAIL。test.fail 标记使 CI 将其视为预期失败（绿信号）。
 *
 * 实现方转绿清单：
 *   1. .page-toolbar 增加添加患者 / 批量绑定按钮
 *   2. el-table 增加 selection 列（批量绑定前置）
 *   3. el-drawer 详情增加分配团队按钮 + 弹窗
 *   4. 新建 / 分配 / 批量绑定 三个 el-dialog 表单（新建表单含手机号必填字段）
 *   5. mock 层补 POST /api/v1/admin/patients、PUT .../team、POST .../batch-bind
 * 届时移除 test.fail 标记，用例转绿。
 */

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.patients)
})

// ─────────────────────────────────────────────────────────────
// 添加患者
// ─────────────────────────────────────────────────────────────

test.describe('添加患者', () => {
  test.fail('点击添加患者按钮打开新建对话框', async ({ page }) => {
    // 工具栏应有"添加患者"按钮（当前不存在 → KNOWN_RED）
    const btn = page.locator('.page-toolbar').getByRole('button', { name: '添加患者' })
    await btn.click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '新建患者' })
    await expect(dialog).toBeVisible()
    // 对话框应含姓名、手机号、性别、年龄、诊断、Cobb角、设备ID、团队、医生字段
    await expect(dialog).toContainText('姓名')
    await expect(dialog).toContainText('手机号')
    await expect(dialog).toContainText('诊断')
    await expect(dialog).toContainText('团队')
  })

  test.fail('填写患者表单提交成功后列表刷新', async ({ page }) => {
    await page.locator('.page-toolbar').getByRole('button', { name: '添加患者' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '新建患者' })
    await expect(dialog).toBeVisible()

    // 填写表单（含手机号必填字段）
    await dialog.locator('input[placeholder*="姓名"]').fill('测试患者E2E')
    await dialog.locator('input[placeholder*="手机号"]').fill('13800138000')
    await dialog.locator('input[placeholder*="年龄"]').fill('15')
    await dialog.locator('input[placeholder*="诊断"]').fill('青少年特发性脊柱侧弯')
    // 选择团队
    await pickSelectOption(page, dialog.locator('.el-select').first(), '脊柱侧弯一组')

    // 提交
    await dialog.getByRole('button', { name: '确定' }).click()
    // 成功提示
    await expect(adminMessage(page)).toContainText('成功')
    // 对话框关闭
    await expect(dialog).toBeHidden()
    // 列表含新患者
    await expect(tableRows(page).filter({ hasText: '测试患者E2E' })).toHaveCount(1)
  })

  test.fail('姓名为空时表单校验拦截提交', async ({ page }) => {
    await page.locator('.page-toolbar').getByRole('button', { name: '添加患者' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '新建患者' })
    await expect(dialog).toBeVisible()

    // 不填姓名直接提交
    await dialog.getByRole('button', { name: '确定' }).click()
    // 应显示校验错误（姓名必填）
    await expect(dialog.locator('.el-form-item__error')).toContainText('姓名')
    // 对话框仍可见（未关闭）
    await expect(dialog).toBeVisible()
  })

  test.fail('手机号为空时表单校验拦截提交', async ({ page }) => {
    await page.locator('.page-toolbar').getByRole('button', { name: '添加患者' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '新建患者' })
    await expect(dialog).toBeVisible()

    // 填姓名但不填手机号直接提交
    await dialog.locator('input[placeholder*="姓名"]').fill('有姓名无手机号')
    await dialog.getByRole('button', { name: '确定' }).click()
    // 应显示校验错误（手机号必填）
    await expect(dialog.locator('.el-form-item__error')).toContainText('手机号')
    // 对话框仍可见（未关闭）
    await expect(dialog).toBeVisible()
  })
})

// ─────────────────────────────────────────────────────────────
// 分配团队
// ─────────────────────────────────────────────────────────────

test.describe('分配团队', () => {
  test.fail('详情抽屉中分配团队成功', async ({ page }) => {
    // 打开详情抽屉
    await tableRows(page).filter({ hasText: '林小雨' }).click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible()

    // 点击"分配团队"按钮（当前不存在 → KNOWN_RED）
    await drawer.getByRole('button', { name: '分配团队' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '分配团队' })
    await expect(dialog).toBeVisible()

    // 选择目标团队
    await pickSelectOption(page, dialog.locator('.el-select').first(), '脊柱侧弯二组')
    await dialog.getByRole('button', { name: '确定' }).click()

    // 成功提示
    await expect(adminMessage(page)).toContainText('成功')
    // 抽屉中团队名更新
    await expect(drawer).toContainText('脊柱侧弯二组')
  })

  test.fail('分配相同团队（幂等）返回成功不报错', async ({ page }) => {
    await tableRows(page).filter({ hasText: '林小雨' }).click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible()

    // 林小雨当前所属团队为"脊柱侧弯一组"
    await drawer.getByRole('button', { name: '分配团队' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '分配团队' })
    await expect(dialog).toBeVisible()

    // 选择相同团队（幂等）
    await pickSelectOption(page, dialog.locator('.el-select').first(), '脊柱侧弯一组')
    await dialog.getByRole('button', { name: '确定' }).click()

    // 幂等：仍返回成功（非 409）
    await expect(adminMessage(page)).toContainText('成功')
  })
})

// ─────────────────────────────────────────────────────────────
// 批量绑定
// ─────────────────────────────────────────────────────────────

test.describe('批量绑定', () => {
  test.fail('选择多个患者批量绑定到团队', async ({ page }) => {
    // el-table 应有 selection 列（当前不存在 → KNOWN_RED）
    // 勾选前 2 行
    const checkboxes = page.locator('.el-table__body-wrapper .el-checkbox')
    await checkboxes.nth(0).click()
    await checkboxes.nth(1).click()

    // 点击"批量绑定"按钮
    await page.locator('.page-toolbar').getByRole('button', { name: '批量绑定' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '批量绑定' })
    await expect(dialog).toBeVisible()

    // 选择目标团队
    await pickSelectOption(page, dialog.locator('.el-select').first(), '脊柱侧弯一组')
    await dialog.getByRole('button', { name: '确定' }).click()

    // 成功提示
    await expect(adminMessage(page)).toContainText('成功')
  })

  test.fail('批量绑定部分失败显示失败明细', async ({ page }) => {
    const checkboxes = page.locator('.el-table__body-wrapper .el-checkbox')
    await checkboxes.nth(0).click()
    await checkboxes.nth(1).click()

    await page.locator('.page-toolbar').getByRole('button', { name: '批量绑定' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '批量绑定' })
    await expect(dialog).toBeVisible()

    await pickSelectOption(page, dialog.locator('.el-select').first(), '脊柱侧弯一组')
    await dialog.getByRole('button', { name: '确定' }).click()

    // 部分失败：提示中含失败计数或明细（mock 支撑由实现方补）
    const msg = adminMessage(page)
    await expect(msg).toContainText(/成功|失败/)
  })
})
