import { test, expect, type Page } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * T057 患者管理写功能 E2E
 *
 * 设计源：docs/design/admin/患者管理.html
 * 覆盖 3 个写功能：添加患者 / 分配团队 / 批量分配
 *
 * T057 的四条入口（工具栏添加患者、详情抽屉分配团队、三个 el-dialog）已全部实现并转绿，
 * 原 KNOWN_RED 标记与「实现方转绿清单」删除。
 * T289 4.2 改版：批量分配不再是「列表勾选 + 单一目标团队弹窗」，而是设计稿 :98-108 的
 * 独立卡片（只列未分配患者 + 逐行「分配至」下拉），所以列表已无 selection 列。
 * mock 的 batch-bind 只对不存在的 patientId 计 failures（卡片只列真实存在的患者），
 * 故旧「部分失败明细」用例无法再由 UI 触发，已随之删除。
 */

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.patients)
})

// ─────────────────────────────────────────────────────────────
// 添加患者
// ─────────────────────────────────────────────────────────────

test.describe('添加患者', () => {
  test('点击添加患者按钮打开新建对话框', async ({ page }) => {
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

  test('填写患者表单提交成功后列表刷新', async ({ page }) => {
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

  test('姓名为空时表单校验拦截提交', async ({ page }) => {
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

  test('手机号为空时表单校验拦截提交', async ({ page }) => {
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
  test('详情抽屉中分配团队成功', async ({ page }) => {
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

  test('分配相同团队（幂等）返回成功不报错', async ({ page }) => {
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
// 批量分配（T289 4.2：设计稿 患者管理.html:98-108 独立卡片，取代旧「批量绑定」弹窗）
// ─────────────────────────────────────────────────────────────

test.describe('批量分配', () => {
  test('旧「批量绑定」工具栏入口与弹窗已撤（设计稿列表卡片无该按钮）', async ({ page }) => {
    await expect(page.locator('.page-toolbar').getByRole('button', { name: '批量绑定' })).toHaveCount(0)
    await expect(page.locator('.el-dialog').filter({ hasText: '批量绑定' })).toHaveCount(0)
  })

  test('卡片内勾选未分配患者并逐行选团队，确认后 mock 落库', async ({ page }) => {
    const card = page.locator('.batch-bind-card')
    const row = tableRows(page, card).filter({ hasText: 'PT-007' })
    await expect(row).toBeVisible({ timeout: 15_000 })
    await row.locator('.el-checkbox').click()
    await pickSelectOption(page, row.locator('.batch-team-select'), '术后康复治疗组')
    await card.getByRole('button', { name: '确认分配' }).click()
    await expect(adminMessage(page)).toContainText('批量分配成功 1 条')

    // 写通道真落库：列表行的绑定团队列取到新团队，而不是只弹个提示
    const listRow = tableRows(page, page.locator('.patient-list-card')).filter({ hasText: '王小红' })
    await expect(listRow).toContainText('术后康复治疗组')
  })
})

// ─────────────────────────────────────────────────────────────
// T432 档案写三入口：编辑档案 / 改手机号 / 解绑微信
//
// 为什么这三条要落在 Playwright 而不是 vitest：admin-web 的 vitest 环境是 happy-dom，
// EP 2.14.4 的 FormItem.validate 在该环境下以 undefined 作 rejection 值（实测 validateState
// 停在 is-validating），Form 侧收集到的 validationErrors 为空 ⇒ validateField 对非法值返回 true，
// 「不过校验就不发请求」这一格在 jsdom 类环境里量不出来（同环境直跑 async-validator 是正常的）。
// 真实浏览器里 EP 的规则渲染与拦截都成立，正是本文件 姓名/手机号 必填两格一直在绿的东西。
// ─────────────────────────────────────────────────────────────

/** 打开林小雨（PT-001）详情抽屉 */
async function openDetail(page: Page, name = '林小雨') {
  await tableRows(page).filter({ hasText: name }).click()
  const drawer = page.locator('.el-drawer')
  await expect(drawer).toBeVisible()
  return drawer
}

test.describe('编辑档案', () => {
  test('详情抽屉给出三个新入口', async ({ page }) => {
    const drawer = await openDetail(page)
    await expect(drawer.getByRole('button', { name: '编辑档案' })).toBeVisible()
    await expect(drawer.getByRole('button', { name: '改手机号' })).toBeVisible()
    await expect(drawer.getByRole('button', { name: '解绑微信' })).toBeVisible()
  })

  test('改诊断保存后列表行取到新值（真落库，不是只弹提示）', async ({ page }) => {
    const drawer = await openDetail(page)
    await drawer.getByRole('button', { name: '编辑档案' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '编辑档案' })
    await expect(dialog).toBeVisible()
    // 打开时按当前档案预填
    await expect(dialog.locator('input[placeholder="请输入诊断"]')).toHaveValue('青少年特发性脊柱侧弯')
    await dialog.locator('input[placeholder="请输入诊断"]').fill('姿势性脊柱侧弯E2E')
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(adminMessage(page)).toContainText('档案已保存')
    await expect(tableRows(page).filter({ hasText: '林小雨' })).toContainText('姿势性脊柱侧弯E2E')
  })

  test('一个字都没改 ⇒ 保存置灰（空编辑后端判 400，不该发出去）', async ({ page }) => {
    const drawer = await openDetail(page)
    await drawer.getByRole('button', { name: '编辑档案' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '编辑档案' })
    const save = dialog.getByRole('button', { name: '保存' })
    await expect(save).toBeDisabled()
  })

  test('清空姓名拦在提交前：档案编辑没有「置为空」通道', async ({ page }) => {
    const drawer = await openDetail(page)
    await drawer.getByRole('button', { name: '编辑档案' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '编辑档案' })
    await dialog.locator('input[placeholder="请输入姓名"]').fill('')
    await expect(dialog.locator('.form-error')).toContainText('姓名不可清空')
    await expect(dialog.getByRole('button', { name: '保存' })).toBeDisabled()
  })

  test('Cobb 角越界（后端值域 0-180）拦在提交前', async ({ page }) => {
    const drawer = await openDetail(page)
    await drawer.getByRole('button', { name: '编辑档案' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '编辑档案' })
    await dialog.locator('input[placeholder="请输入Cobb角"]').fill('200')
    await expect(dialog.locator('.form-error')).toContainText('Cobb角需为 0-180 之间的数字')
    await expect(dialog.getByRole('button', { name: '保存' })).toBeDisabled()
  })
})

test.describe('改手机号', () => {
  test('输入框不预填（读侧 phone 恒空，预填＝把空值伪装成原号）', async ({ page }) => {
    const drawer = await openDetail(page)
    await drawer.getByRole('button', { name: '改手机号' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '修改手机号' })
    await expect(dialog.locator('input[placeholder="请输入11位新手机号"]')).toHaveValue('')
    await expect(dialog).toContainText('原因随请求写入服务端审计日志')
  })

  test('号码格式错 ⇒ Element Plus 规则文案 + 弹窗不关（不发出请求）', async ({ page }) => {
    const drawer = await openDetail(page)
    await drawer.getByRole('button', { name: '改手机号' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '修改手机号' })
    await dialog.locator('input[placeholder="请输入11位新手机号"]').fill('139000011')
    await dialog.locator('textarea[placeholder="例如：患者换号，本人来电申请"]').fill('患者换号')
    await dialog.getByRole('button', { name: '确定' }).click()
    await expect(dialog.locator('.el-form-item__error')).toContainText('手机号需为 11 位、以 1 开头的数字')
    await expect(dialog).toBeVisible()
  })

  test('不填原因 ⇒ 拦下（后端不校验 reason，但没理由的改号无从追责）', async ({ page }) => {
    const drawer = await openDetail(page)
    await drawer.getByRole('button', { name: '改手机号' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '修改手机号' })
    await dialog.locator('input[placeholder="请输入11位新手机号"]').fill('13900001234')
    await dialog.getByRole('button', { name: '确定' }).click()
    await expect(dialog.locator('.el-form-item__error')).toContainText('请填写变更原因')
    await expect(dialog).toBeVisible()
  })

  test('号码 + 原因齐 ⇒ 提交成功', async ({ page }) => {
    const drawer = await openDetail(page)
    await drawer.getByRole('button', { name: '改手机号' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '修改手机号' })
    await dialog.locator('input[placeholder="请输入11位新手机号"]').fill('13900001234')
    await dialog.locator('textarea[placeholder="例如：患者换号，本人来电申请"]').fill('本人来电换号')
    await dialog.getByRole('button', { name: '确定' }).click()
    await expect(adminMessage(page)).toContainText('手机号已更新')
    await expect(dialog).toBeHidden()
  })
})

test.describe('解绑微信', () => {
  test('取消 ⇒ 不提示、不改档案', async ({ page }) => {
    const drawer = await openDetail(page)
    await drawer.getByRole('button', { name: '解绑微信' }).click()
    const box = page.locator('.el-message-box')
    await expect(box).toBeVisible()
    // 后果文案只写实测得到的那一条（解绑后再用微信登录进「重新绑定手机号」流程）
    await expect(box).toContainText('重新绑定手机号')
    await box.getByRole('button', { name: '取消' }).click()
    await expect(box).toBeHidden()
    await expect(page.locator('.el-message')).toHaveCount(0)
  })

  test('确认解绑 ⇒ 提交成功（后端无条件置空 openid，未绑过也回 200）', async ({ page }) => {
    const drawer = await openDetail(page)
    await drawer.getByRole('button', { name: '解绑微信' }).click()
    const box = page.locator('.el-message-box')
    await expect(box).toContainText('PT-001')
    await box.getByRole('button', { name: '确认解绑' }).click()
    await expect(adminMessage(page)).toContainText('已解绑微信')
  })
})
