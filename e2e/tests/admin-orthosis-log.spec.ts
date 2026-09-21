import { test, expect, type Page, type Locator } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * T301 G1 补齐：矫形日志页（医生工作台）内容级用例
 * 本页此前仅被 admin-permissions 的 goto 判不 403 覆盖，三 Tab 业务断言数为 0。
 *
 * mock 对齐 apps/admin-web/src/mock/orthosis.ts（患者 PT-001 林小雨）：
 * - 方案 2 条（v2.1 / v1.0）；保存新方案 mock 固定返回 v2.2 并前插
 * - 感受 2 条：FL-001（胸段、未回复）/ FL-002（无不适部位、已回复）
 * - 报告 2 份：周报 92.5% / 38.2N / 趋势向好；月报 88.1% / 保持平稳
 * 写操作（保存方案、回复日志）只改浏览器内存里的 mock，每条用例新开 context，不碰共享 seed。
 */

async function openAsAdmin(page: Page): Promise<void> {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.orthosisLog)
}

async function pickPatient(page: Page, name: string): Promise<void> {
  await pickSelectOption(page, page.locator('.patient-select'), name)
}

const tab = (page: Page, label: string): Locator =>
  page.locator('.el-tabs__item').filter({ hasText: label })

const feelingRow = (page: Page, logDate: string): Locator =>
  tableRows(page).filter({ hasText: logDate })

test.describe('矫形日志 · 未选患者', () => {
  test('只有占位空态，不渲染三 Tab', async ({ page }) => {
    await openAsAdmin(page)
    await expect(page.locator('.el-empty')).toContainText('请选择患者开始诊断评估')
    await expect(page.locator('.el-tabs__item')).toHaveCount(0)
  })
})

test.describe('矫形日志 · 矫形方案 Tab', () => {
  test.beforeEach(async ({ page }) => {
    await openAsAdmin(page)
    await pickPatient(page, '林小雨')
  })

  test('历史方案 2 条，时间线含版本号', async ({ page }) => {
    await expect(page.locator('.page-card-title').filter({ hasText: '历史方案' })).toHaveText(
      '历史方案（2）'
    )
    const stamps = page.locator('.el-timeline-item__timestamp')
    await expect(stamps).toHaveCount(2)
    await expect(stamps.first()).toContainText('v2.1')
    await expect(stamps.nth(1)).toContainText('v1.0')
    await expect(page.locator('.el-timeline-item').first()).toContainText('T7-T9')
  })

  test('方案内容为空时「保存新方案」禁用', async ({ page }) => {
    await expect(page.getByRole('button', { name: '保存新方案' })).toBeDisabled()
  })

  test('保存新方案后前插 v2.2，输入框清空', async ({ page }) => {
    await page.locator('.page-card textarea').first().fill('T301 用例：夜间佩戴目标调整')
    await page.getByRole('button', { name: '保存新方案' }).click()
    await expect(adminMessage(page)).toContainText('方案已保存')
    await expect(page.locator('.page-card-title').filter({ hasText: '历史方案' })).toHaveText(
      '历史方案（3）'
    )
    const stamps = page.locator('.el-timeline-item__timestamp')
    await expect(stamps.first()).toContainText('v2.2')
    await expect(page.locator('.el-timeline-item').first()).toContainText('T301 用例：夜间佩戴目标调整')
    await expect(page.locator('.page-card textarea').first()).toHaveValue('')
  })
})

test.describe('矫形日志 · 佩戴感受 Tab', () => {
  test.beforeEach(async ({ page }) => {
    await openAsAdmin(page)
    await pickPatient(page, '林小雨')
    await tab(page, '佩戴感受').click()
  })

  test('2 条日志：日期、不适部位中文映射、无部位显示 -', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows).toHaveCount(2)
    // 列序：0 日期 1 舒适度 2 不适部位 3 患者备注 4 医生回复
    await expect(feelingRow(page, '2026-08-11').locator('td').nth(2)).toHaveText('胸段')
    await expect(feelingRow(page, '2026-08-11')).toContainText('上午有点闷')
    await expect(feelingRow(page, '2026-08-10').locator('td').nth(2)).toHaveText('-')
  })

  test('已回复日志展示回复文本，未回复日志展示输入框', async ({ page }) => {
    await expect(feelingRow(page, '2026-08-10').locator('.reply-content')).toHaveText(
      '继续保持，注意睡姿'
    )
    await expect(feelingRow(page, '2026-08-11').locator('.reply-content')).toHaveCount(0)
    await expect(feelingRow(page, '2026-08-11').locator('textarea')).toHaveCount(1)
  })

  test('医生回复未回复日志：提交后行内出现回复内容', async ({ page }) => {
    const row = feelingRow(page, '2026-08-11')
    await expect(row.getByRole('button', { name: '回复' })).toBeDisabled()
    await row.locator('textarea').fill('已阅，注意调整肩带松紧')
    await row.getByRole('button', { name: '回复' }).click()
    await expect(adminMessage(page)).toContainText('回复成功')
    await expect(row.locator('.reply-content')).toHaveText('已阅，注意调整肩带松紧')
    await expect(row.locator('textarea')).toHaveCount(0)
  })
})

test.describe('矫形日志 · 健康报告 Tab', () => {
  test.beforeEach(async ({ page }) => {
    await openAsAdmin(page)
    await pickPatient(page, '林小雨')
    await tab(page, '健康报告').click()
  })

  test('周报/月报标题与周期正确', async ({ page }) => {
    const titles = page.locator('.report-title')
    await expect(titles).toHaveCount(2)
    await expect(titles.first()).toHaveText('周报：2026-08-04 ~ 2026-08-10')
    await expect(titles.nth(1)).toContainText('月报：2026-07-01 ~ 2026-07-31')
  })

  test('指标渲染：佩戴达标率、平均压力带单位', async ({ page }) => {
    const first = page.locator('.report-desc').first()
    await expect(first).toContainText('92.5%')
    await expect(first).toContainText('38.2N')
    await expect(first).toContainText('佩戴依从性良好')
  })

  test('趋势判定着色：向好=success，平稳=info', async ({ page }) => {
    const tags = page.locator('.report-header .el-tag')
    await expect(tags.first()).toHaveText('趋势向好')
    await expect(tags.first()).toHaveClass(/el-tag--success/)
    await expect(tags.nth(1)).toHaveText('保持平稳')
    await expect(tags.nth(1)).toHaveClass(/el-tag--info/)
  })

  test('切换到无数据患者（PT-005 赵欣然）三个 Tab 均为空态', async ({ page }) => {
    // el-tab-pane 切走后仍在 DOM 内（display:none），空态断言必须限定在当前可见面板
    const activeEmpty = page.locator('.el-tab-pane:visible .el-empty')
    await pickPatient(page, '赵欣然')
    await tab(page, '矫形方案').click()
    await expect(activeEmpty.first()).toContainText('暂无方案记录')
    await tab(page, '佩戴感受').click()
    await expect(activeEmpty.first()).toContainText('暂无感受日志')
    await tab(page, '健康报告').click()
    await expect(activeEmpty.first()).toContainText('暂无健康报告')
  })
})

test.describe('矫形日志 · 角色差异', () => {
  test('医生角色显示「仅本团队患者」提示，运营管理员不显示', async ({ page }) => {
    await adminLogin(page, 'doctor')
    await page.goto(adminRoutes.orthosisLog)
    await expect(page.locator('.page-toolbar .el-tag')).toContainText('医生工作台：仅本团队患者')
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.orthosisLog)
    await expect(page.locator('.page-toolbar .el-tag')).toHaveCount(0)
  })
})
