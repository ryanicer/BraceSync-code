import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * T059 团队管理写功能 E2E（KNOWN_RED）
 *
 * 设计源：docs/design/admin/团队管理.html
 * 覆盖 5 个写功能：新建团队 / 编辑团队 / 删除团队 / 成员添加 / 成员移除
 *
 * 预期红态：admin-web 团队管理页当前为 59 行纯只读（el-table 列表 + 团队排行），
 * 顶部无"新建团队"按钮，表格无"操作"列（成员/编辑/删除），无成员管理面板，
 * 无团队/成员对话框。所有 test.fail 用例因找不到"新建团队"按钮、"编辑/删除/成员"
 * 操作按钮、"添加成员"入口等而 FAIL。test.fail 标记使 CI 将其视为预期失败（绿信号）。
 *
 * 实现方转绿清单（对齐设计源团队管理.html）：
 *   1. .page-toolbar 增加"新建团队"按钮（顶部 top-nav 右侧）
 *   2. el-table 增加"操作"列：成员 / 编辑 / 删除 三个按钮
 *   3. 新建/编辑团队 el-dialog（团队名称 + 负责人 select + 描述）
 *   4. 成员管理面板（点"成员"切换显示）：搜索 + 角色筛选 + "添加成员"按钮 + 成员表
 *   5. 添加成员 el-dialog（选择成员 select + 分配角色 select）
 *   6. 成员表"移除"按钮 + 删除团队二次确认 ElMessageBox
 *   7. mock 层补 POST /api/v1/teams、PUT .../teams/:teamId、DELETE .../teams/:teamId、
 *      POST .../teams/:teamId/members、PUT .../teams/:teamId/members/:memberId、
 *      DELETE .../teams/:teamId/members/:memberId
 * 届时移除 test.fail 标记，用例转绿。
 *
 * 删除约束策略（Ella 推荐，待 Boss 评审）：
 *   团队被患者/成员引用时拒绝删除（reject-if-referenced），返回 409 携带引用计数，
 *   前端在删除按钮的二次确认后捕获失败并展示"团队被引用：N 患者 / M 成员"提示。
 */

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.teams)
})

// ─────────────────────────────────────────────────────────────
// 新建团队
// ─────────────────────────────────────────────────────────────

test.describe('新建团队', () => {
  test('点击新建团队按钮打开新建对话框', async ({ page }) => {
    // 顶部工具栏应有"新建团队"按钮（当前不存在 → KNOWN_RED）
    const btn = page.locator('.page-toolbar').getByRole('button', { name: '新建团队' })
    await btn.click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '新建团队' })
    await expect(dialog).toBeVisible()
    // 对话框应含团队名称、负责人、描述字段
    await expect(dialog).toContainText('团队名称')
    await expect(dialog).toContainText('负责人')
    await expect(dialog).toContainText('描述')
  })

  test('填写团队表单提交成功后列表刷新', async ({ page }) => {
    await page.locator('.page-toolbar').getByRole('button', { name: '新建团队' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '新建团队' })
    await expect(dialog).toBeVisible()

    // 填写表单
    await dialog.locator('input[placeholder*="团队名称"]').fill('康复二组')
    // 选择负责人
    await pickSelectOption(page, dialog.locator('.el-select').first(), '王康复师')
    // 描述（可选）
    await dialog.locator('input[placeholder*="描述"]').fill('康复诊疗二组')

    // 提交
    await dialog.getByRole('button', { name: '保存' }).click()
    // 成功提示
    await expect(adminMessage(page)).toContainText('成功')
    // 对话框关闭
    await expect(dialog).toBeHidden()
    // 列表含新团队
    await expect(tableRows(page).filter({ hasText: '康复二组' })).toHaveCount(1)
  })

  test('团队名称为空时表单校验拦截提交', async ({ page }) => {
    await page.locator('.page-toolbar').getByRole('button', { name: '新建团队' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '新建团队' })
    await expect(dialog).toBeVisible()

    // 不填团队名称直接提交
    await dialog.getByRole('button', { name: '保存' }).click()
    // 应显示校验错误（团队名称必填）
    await expect(dialog.locator('.el-form-item__error')).toContainText('团队名称')
    // 对话框仍可见（未关闭）
    await expect(dialog).toBeVisible()
  })

  test('团队名称重复时后端返回 409 拒绝', async ({ page }) => {
    await page.locator('.page-toolbar').getByRole('button', { name: '新建团队' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '新建团队' })
    await expect(dialog).toBeVisible()

    // 填入已存在的团队名"骨科一组"
    await dialog.locator('input[placeholder*="团队名称"]').fill('骨科一组')
    await pickSelectOption(page, dialog.locator('.el-select').first(), '张主任')
    await dialog.getByRole('button', { name: '保存' }).click()
    // 应提示团队名重复（409 映射为业务错误提示）
    await expect(adminMessage(page)).toContainText(/重复|已存在|占用/)
  })
})

// ─────────────────────────────────────────────────────────────
// 编辑团队
// ─────────────────────────────────────────────────────────────

test.describe('编辑团队', () => {
  test('点击编辑按钮打开对话框并预填表单', async ({ page }) => {
    // el-table 应有"操作"列含"编辑"按钮（当前无操作列 → KNOWN_RED）
    const editBtn = tableRows(page)
      .filter({ hasText: '骨科一组' })
      .getByRole('button', { name: '编辑' })
    await editBtn.click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '编辑团队' })
    await expect(dialog).toBeVisible()
    // 表单应预填当前团队信息
    await expect(dialog.locator('input[placeholder*="团队名称"]')).toHaveValue('骨科一组')
  })

  test('修改团队名称提交成功后列表刷新', async ({ page }) => {
    tableRows(page).filter({ hasText: '骨科一组' }).getByRole('button', { name: '编辑' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '编辑团队' })
    await expect(dialog).toBeVisible()

    // 修改团队名称
    await dialog.locator('input[placeholder*="团队名称"]').fill('骨科一组（改）')
    await dialog.getByRole('button', { name: '保存' }).click()

    // 成功提示
    await expect(adminMessage(page)).toContainText('成功')
    // 对话框关闭
    await expect(dialog).toBeHidden()
    // 列表中团队名已更新
    await expect(tableRows(page).filter({ hasText: '骨科一组（改）' })).toHaveCount(1)
  })
})

// ────────────────────────────────────────────────────────
// 删除团队
// ────────────────────────────────────────────────────────

test.describe('删除团队', () => {
  test('点击删除按钮二次确认后成功删除', async ({ page }) => {
    // el-table 应有"操作"列含"删除"按钮（当前无操作列 → KNOWN_RED）
    const deleteBtn = tableRows(page)
      .filter({ hasText: '康复组' })
      .getByRole('button', { name: '删除' })
    await deleteBtn.click()

    // ElMessageBox 二次确认
    const msgBox = page.locator('.el-message-box')
    await expect(msgBox).toBeVisible()
    await msgBox.getByRole('button', { name: '确定' }).click()

    // 成功提示
    await expect(adminMessage(page)).toContainText('成功')
    // 列表中该团队已消失
    await expect(tableRows(page).filter({ hasText: '康复组' })).toHaveCount(0)
  })

  test('团队被引用时删除被拒绝并展示引用计数', async ({ page }) => {
    // 骨科一组被患者/成员引用（reject-if-referenced 策略）
    tableRows(page).filter({ hasText: '骨科一组' }).getByRole('button', { name: '删除' }).click()
    const msgBox = page.locator('.el-message-box')
    await expect(msgBox).toBeVisible()
    await msgBox.getByRole('button', { name: '确定' }).click()

    // 应提示团队被引用（409 携带计数）
    await expect(adminMessage(page)).toContainText(/引用|占用|患者|成员/)
  })

  test('取消删除二次确认则团队保留', async ({ page }) => {
    tableRows(page).filter({ hasText: '康复组' }).getByRole('button', { name: '删除' }).click()
    const msgBox = page.locator('.el-message-box')
    await expect(msgBox).toBeVisible()
    // 取消
    await msgBox.getByRole('button', { name: '取消' }).click()
    // 列表中团队仍存在
    await expect(tableRows(page).filter({ hasText: '康复组' })).toHaveCount(1)
  })
})

// ────────────────────────────────────────────────────────
// 成员添加
// ────────────────────────────────────────────────────────

test.describe('成员添加', () => {
  test('点击成员按钮打开成员管理面板', async ({ page }) => {
    // el-table 应有"操作"列含"成员"按钮（当前无操作列 → KNOWN_RED）
    const memberBtn = tableRows(page)
      .filter({ hasText: '骨科一组' })
      .getByRole('button', { name: '成员' })
    await memberBtn.click()

    // 成员管理面板应可见
    const memberPanel = page.locator('.member-panel').filter({ hasText: '成员管理' })
    await expect(memberPanel).toBeVisible()
    // 面板标题含团队名
    await expect(memberPanel).toContainText('骨科一组')
  })

  test('在成员管理面板添加成员成功', async ({ page }) => {
    tableRows(page).filter({ hasText: '骨科一组' }).getByRole('button', { name: '成员' }).click()
    const memberPanel = page.locator('.member-panel').filter({ hasText: '成员管理' })
    await expect(memberPanel).toBeVisible()

    // 点击"添加成员"按钮
    await memberPanel.getByRole('button', { name: '添加成员' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '添加成员' })
    await expect(dialog).toBeVisible()

    // 选择成员（医生/技师）
    await pickSelectOption(page, dialog.locator('.el-select').first(), '刘医生')
    // 分配角色
    await pickSelectOption(page, dialog.locator('.el-select').nth(1), '主治医师')
    await dialog.getByRole('button', { name: '确认添加' }).click()

    // 成功提示
    await expect(adminMessage(page)).toContainText('成功')
    // 成员表中含新成员
    await expect(tableRows(page, memberPanel).filter({ hasText: '刘医生' })).toHaveCount(1)
  })
})

// ────────────────────────────────────────────────────────
// 成员移除
// ────────────────────────────────────────────────────────

test.describe('成员移除', () => {
  test('在成员管理面板移除成员成功', async ({ page }) => {
    // 进入骨科一组成员管理面板
    tableRows(page).filter({ hasText: '骨科一组' }).getByRole('button', { name: '成员' }).click()
    const memberPanel = page.locator('.member-panel').filter({ hasText: '成员管理' })
    await expect(memberPanel).toBeVisible()

    // 成员表"移除"按钮（当前不存在 → KNOWN_RED）
    const removeBtn = tableRows(page, memberPanel)
      .filter({ hasText: '王护士' })
      .getByRole('button', { name: '移除' })
    await removeBtn.click()

    // 二次确认
    const msgBox = page.locator('.el-message-box')
    await expect(msgBox).toBeVisible()
    await msgBox.getByRole('button', { name: '确定' }).click()

    // 成功提示
    await expect(adminMessage(page)).toContainText('成功')
    // 成员表中该成员已移除
    await expect(tableRows(page, memberPanel).filter({ hasText: '王护士' })).toHaveCount(0)
  })

  test('移除成员二次确认取消则成员保留', async ({ page }) => {
    tableRows(page).filter({ hasText: '骨科一组' }).getByRole('button', { name: '成员' }).click()
    const memberPanel = page.locator('.member-panel').filter({ hasText: '成员管理' })
    await expect(memberPanel).toBeVisible()

    tableRows(page, memberPanel)
      .filter({ hasText: '王护士' })
      .getByRole('button', { name: '移除' })
      .click()
    const msgBox = page.locator('.el-message-box')
    await expect(msgBox).toBeVisible()
    // 取消
    await msgBox.getByRole('button', { name: '取消' }).click()

    // 成员仍存在
    await expect(tableRows(page, memberPanel).filter({ hasText: '王护士' })).toHaveCount(1)
  })
})
