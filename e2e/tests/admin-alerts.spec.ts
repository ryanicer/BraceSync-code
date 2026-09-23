import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * admin-web 告警管理：列表渲染 + 类型/状态筛选 + 处理流程（复用 T019B processAlert 模式）
 * mock 数据对齐 mock/alerts.ts：7 条（pending 3 / processing 1 / processed 3；
 * pressure_high 2 / wear_interrupt 2 / wear_duration_short 1）
 */

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.alerts)
})

test.describe('告警列表', () => {
  test('渲染 7 条告警且列信息完整', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows).toHaveCount(7)
    // 首行 ALR-001：压力偏高 / 林小雨 / 待处理 / 进行中
    const first = rows.first()
    await expect(first).toContainText('压力偏高')
    await expect(first).toContainText('林小雨')
    await expect(first).toContainText('DEV-A3F312')
    // T289 2.5：阈值与实际值按设计稿 告警管理.html:246 拆成两列（原「阈值/实际」合并列作废）
    await expect(first.locator('td').nth(5)).toHaveText('60.00N')
    await expect(first.locator('td').nth(6)).toHaveText('68.50N')
    await expect(first).toContainText('待处理')
    await expect(first).toContainText('进行中')
  })

  // T289 2.5：设计稿 告警管理.html:246 九列 = 时间/患者/设备/告警类型/采集点/阈值/实际值/状态/操作；
  // 「详情」「恢复态」为 PRD 多出的列，排在设计稿列之后（T245：多出列不自行判删）。
  test('列清单与列序对齐设计稿', async ({ page }) => {
    await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
    const heads = await page
      .locator('.el-table__header-wrapper thead th')
      .evaluateAll((ths) => ths.map((th) => (th.textContent ?? '').trim()).filter(Boolean))
    expect(heads).toEqual(['时间', '患者', '设备', '告警类型', '采集点', '阈值', '实际值', '详情', '状态', '恢复态', '操作'])
  })

  test('已处理告警显示处理人', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: 'P05 压力波动异常' })
    await expect(row).toContainText('已处理')
    await expect(row).toContainText('张建国')
  })

  test('分页组件显示共 7 条', async ({ page }) => {
    await expect(page.locator('.el-pagination')).toContainText('共 7 条')
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

  test('类型 + 状态组合筛选：设备离线 × 待处理 → 1 条', async ({ page }) => {
    await pickSelectOption(page, page.locator('.filter-select').first(), '设备离线')
    await pickSelectOption(page, page.locator('.filter-select').nth(1), '待处理')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('陈子航')
  })

  test('清空筛选恢复 7 条', async ({ page }) => {
    await pickSelectOption(page, page.locator('.filter-select').first(), '压力偏高')
    await expect(tableRows(page)).toHaveCount(2)
    // clearable：EP 2.14 新 select 的清空图标 hover 才渲染（.el-select__clear）
    const typeSelect = page.locator('.filter-select').first()
    await typeSelect.hover()
    await typeSelect.locator('.el-select__clear').click()
    await expect(tableRows(page)).toHaveCount(7)
  })
})

test.describe('处理流程', () => {
  test('待处理告警可打开处理对话框并确认处理', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: 'P10 压力持续偏高' })
    // exact：pending 行现在并列「开始处理」与「处理」，子串匹配会命中两个按钮
    await row.getByRole('button', { name: '处理', exact: true }).click()
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
    await row.getByRole('button', { name: '处理', exact: true }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '处理告警' })
    await dialog.getByRole('button', { name: '取消' }).click()
    await expect(dialog).toBeHidden()
    // 取消后仍为待处理
    await expect(row).toContainText('待处理')
  })
})

test.describe('告警类型术语与三态（T289 2.6 / 2.7）', () => {
  // 2.6：设计稿 告警管理.html:248-251 四类术语（PM 09-21 23:53 答复①）。码值不动，只改显示。
  // 断言只看「告警类型」列（td 第 4 列）——detail 文案里出现「佩戴中断」等字样是后端生成的正文，不属术语口径。
  test('类型下拉五项文案与设计稿一致（压力波动仅历史）', async ({ page }) => {
    await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
    await page.locator('.filter-select').first().click()
    const option = page.locator('.el-select-dropdown:visible .el-select-dropdown__item').first()
    await expect(option).toBeVisible()
    const options = await page
      .locator('.el-select-dropdown:visible .el-select-dropdown__item')
      .evaluateAll((items) => items.map((it) => (it.textContent ?? '').trim()))
    expect(options).toEqual(['压力偏高', '设备离线', '佩戴时长不足', '传感器标定异常', '压力波动'])
    await page.keyboard.press('Escape')
  })

  test('列表按设计稿术语渲染，码值不泄漏', async ({ page }) => {
    await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
    const cells = await tableRows(page).evaluateAll((trs) =>
      trs.map((tr) => Array.from(tr.querySelectorAll('td')).map((td) => (td.textContent ?? '').trim())),
    )
    expect(cells.length, '须有数据行').toBeGreaterThan(0)
    for (const cellsRow of cells) {
      const alertType = cellsRow[3]
      expect(['压力偏高', '设备离线', '佩戴时长不足', '传感器标定异常', '压力波动'], `告警类型列须是设计稿术语，实际「${alertType}」`).toContain(alertType)
    }
    // wear_interrupt 行必须显示「设备离线」，sensor_drift 行必须显示「传感器标定异常」
    expect(cells.find((c) => c[2] === 'DEV-B7E456')?.[3]).toBe('设备离线')
    expect(cells.find((c) => c[2] === 'DEV-C9D789')?.[3]).toBe('传感器标定异常')
  })

  test('状态下拉含「处理中」：处理中筛选 → 1 条', async ({ page }) => {
    await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
    await page.locator('.filter-select').nth(1).click()
    await expect(page.locator('.el-select-dropdown:visible .el-select-dropdown__item').first()).toBeVisible()
    const statusOptions = await page
      .locator('.el-select-dropdown:visible .el-select-dropdown__item')
      .evaluateAll((items) => items.map((it) => (it.textContent ?? '').trim()))
    expect(statusOptions, '状态下拉三档（设计稿 告警管理.html:240；「全部状态」是 placeholder，不作 option）').toEqual(['待处理', '处理中', '已处理'])
    await page.keyboard.press('Escape')

    await pickSelectOption(page, page.locator('.filter-select').nth(1), '处理中')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('佩戴时长不足')
    await expect(rows.first()).toContainText('处理中')
  })

  test('待处理行有「开始处理」，点击后转「处理中」且入口消失', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: '佩戴中断超过 30 分钟' })
    await expect(row.first()).toContainText('待处理')
    await row.first().getByRole('button', { name: '开始处理' }).click()
    await expect(adminMessage(page)).toContainText('已开始处理')
    const after = tableRows(page).filter({ hasText: '佩戴中断超过 30 分钟' })
    await expect(after.first()).toContainText('处理中')
    await expect(after.first().getByRole('button', { name: '开始处理' })).toHaveCount(0)
    // 进入处理中后仍可「处理」→ 已处理
    await after.first().getByRole('button', { name: '处理', exact: true }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '处理告警' })
    await dialog.locator('textarea').fill('已联系技师复检（e2e）')
    await dialog.getByRole('button', { name: '确认处理' }).click()
    await expect(adminMessage(page)).toContainText('处理成功')
  })

  test('已处理行既无「开始处理」也无「处理」（后端 processed 重开会 409）', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: 'P05 压力波动异常' })
    await expect(row.first().getByRole('button', { name: '处理', exact: true })).toHaveCount(0)
    await expect(row.first().getByRole('button', { name: '开始处理' })).toHaveCount(0)
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

/**
 * T351：医护进「告警管理」不再被两张 admin 专属配置 Tab 带出 403。
 *
 * 现场（staging 已部署包 + doctor_li，见 docs/tasks/iris/T351-截图）：进页面即
 * `403 GET /api/v1/admin/alert-rules` → 一条红条 + 控制台一条 403；点「流程配置」再补一条
 * `403 GET /api/v1/admin/flow/templates`。网关那两条是 adminOnlyPatterns 的正确行为，
 * 而 PRD §7D.11 给医护的是「🚨 告警管理 ✅（仅本团队患者）」= 页面级准入 ⇒ 页面要能进、
 * 配置面按角色摘掉。
 *
 * mock 模式下没有真 403 可抓，所以这里锁的是 DOM 形态（Tab 数量 + 配置面板不渲染）；
 * 「Network 无 403」那半条判据在 e2e-real/tests/03-alerts.spec.ts 的 3b 段。
 */
test.describe('医护角色进告警管理（T351）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'doctor')
    await page.goto(adminRoutes.alerts)
  })

  test('列表照常渲染，两张 admin 专属配置 Tab 不出现', async ({ page }) => {
    await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
    await expect(tableRows(page)).toHaveCount(7)

    await expect(page.getByRole('tab', { name: '告警列表' })).toBeVisible()
    await expect(page.getByRole('tab', { name: '处理流程' })).toBeVisible()
    await expect(page.getByRole('tab', { name: '告警规则配置' })).toHaveCount(0)
    await expect(page.getByRole('tab', { name: '流程配置' })).toHaveCount(0)
    // 页面级准入没被一起摘掉 ⇒ 也不该冒出一条错误提示
    await expect(page.locator('.el-message--error')).toHaveCount(0)
  })

  // 反证：上一条的「配置 Tab 数为 0」不能是选择器写错的永真断言 —— 运营角色下这四张 Tab
  // 必须全在（且第一张之外的能点出 20 格网格，见上面「告警规则配置」段）。
  test('反证：运营角色四张 Tab 齐全（同一段选择器在 admin 下数到 4）', async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.alerts)
    await expect(page.getByRole('tab')).toHaveCount(4)
    await page.getByRole('tab', { name: '告警规则配置' }).click()
    await expect(page.locator('.alert-grid .grid-cell')).toHaveCount(20)
  })
})
