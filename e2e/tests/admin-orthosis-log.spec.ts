import { test, expect, type Page, type Locator } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

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
    // T344 起工作台第一张卡是「患者基本信息」，方案调整退到内层第一个页签内
    await expect(workspace.locator('.page-card-title').first()).toHaveText('患者基本信息')
    await expect(workspace.locator('.el-tab-pane:visible .page-card-title').first()).toHaveText('方案调整')

    await workspace.getByRole('tab', { name: '佩戴感受' }).click()
    const rows = tableRows(page, workspace.locator('.workspace-feelings-card'))
    await expect(rows).toHaveCount(2)
    await expect(rows.first()).toContainText('贴合')
  })
})

/**
 * T344 患者工作台 4 区块（Boss 2026-09-23 裁定问题 2「本期做」）
 * 设计稿 docs/design/admin/矫形日志.html #tabWorkspace：
 *  - #wsProfile 8 格 = PRD §7D.8 视图② 的 7 字段 + 稿面另加「患者ID」
 *  - #wsTabs 内层页签顺序：矫形方案 → 佩戴感受 → 健康报告 → 数据视图（新增项追加末位不重排）
 *  - #wsData 三块：压力趋势图（7/14/30 天）、每日佩戴时长柱状图、告警记录列表 5 列
 *  - #wsSendAdvice：未选患者禁用 + 点击给占位提示（模板消息端点待 PM 派后端卡）
 * mock 取数：PT-001 林小雨（有设备 DEV-A3F312）、PT-005 赵欣然（deviceId 为 null）
 */
test.describe('工作台 · T344 患者基本信息卡', () => {
  test('8 个字段与稿面同序，取值来自患者详情端点', async ({ page }) => {
    const ws = await openWorkspace(page)
    await pickSelectOption(page, ws.locator('.patient-select'), '林小雨')
    const card = ws.locator('.profile-card')
    const labels = await card.locator('.el-descriptions__label').allTextContents()
    expect(labels.map((t) => t.trim())).toEqual([
      '姓名', '年龄', '性别', '诊断', 'Cobb 角度', '绑定设备', '绑定团队', '患者ID',
    ])
    const body = card.locator('.el-descriptions__body')
    await expect(body).toContainText('林小雨')
    await expect(body).toContainText('13 岁')
    await expect(body).toContainText('女')
    await expect(body).toContainText('青少年特发性脊柱侧弯')
    await expect(body).toContainText('28°')
    await expect(body).toContainText('DEV-A3F312')
    await expect(body).toContainText('脊柱侧弯一组')
    await expect(body).toContainText('PT-001')
  })

  test('未绑定团队/设备的患者显示占位横线，不拿 null 上屏', async ({ page }) => {
    const ws = await openWorkspace(page)
    await pickSelectOption(page, ws.locator('.patient-select'), '王小红')
    const card = ws.locator('.profile-card')
    await expect(card.locator('.el-descriptions__body')).toContainText('PT-007')
    const values = await card.locator('.el-descriptions__content').allTextContents()
    // 第 6 格绑定设备、第 7 格绑定团队在 mock 档案里就是空值 ⇒ 必须落折线而非空白/null
    expect(values[5].trim()).toBe('—')
    expect(values[6].trim()).toBe('—')
  })
})

test.describe('工作台 · T344 发送建议给患者', () => {
  test('未选患者时按钮禁用，选定后可点且只给占位提示', async ({ page }) => {
    const ws = await openWorkspace(page)
    const btn = ws.getByRole('button', { name: '发送建议给患者' })
    await expect(btn).toBeDisabled()
    await pickSelectOption(page, ws.locator('.patient-select'), '林小雨')
    await expect(btn).toBeEnabled()
    await btn.click()
    await expect(adminMessage(page)).toContainText('模板消息通道待后端建端点')
  })
})

test.describe('工作台 · T344 数据视图', () => {
  test('内层页签顺序：三块照实现现状，数据视图追加末位', async ({ page }) => {
    const ws = await openWorkspace(page)
    await pickSelectOption(page, ws.locator('.patient-select'), '林小雨')
    const names = (await ws.locator('.el-tabs__item').allTextContents()).map((t) => t.trim())
    expect(names).toEqual(['矫形方案', '佩戴感受', '健康报告', '数据视图'])
  })

  test('两张图 + 告警 5 列：区间切到 14 天后图表仍在，虚线取值来自系统配置', async ({ page }) => {
    const ws = await openWorkspace(page)
    await pickSelectOption(page, ws.locator('.patient-select'), '林小雨')
    await ws.getByRole('tab', { name: '数据视图' }).click()
    const pane = ws.locator('.el-tab-pane:visible')
    await expect(pane.locator('canvas')).toHaveCount(2)
    // 画布的父容器必须定高：chart.js responsive 按父级内容高排版，父级若被标题/注记一起撑开就会每轮 resize 长一截
    // （staging 实测旧写法涨到 2137px/6595px，见 T344 截图台账）
    const parentHeights = await pane.locator('canvas').evaluateAll((els) =>
      els.map((c) => Math.round((c.parentElement ?? c).getBoundingClientRect().height)))
    expect(parentHeights).toHaveLength(2)
    for (const h of parentHeights) {
      expect(h).toBeGreaterThan(100)
      expect(h).toBeLessThanOrEqual(260)
    }
    // 稿面：两条虚线不写死数值 ⇒ 注记里回显的是 §7D.12 当前配置值
    await expect(pane.locator('.axis-note').first()).toContainText('当前取系统配置')
    // 点 label 而非 input：el-radio-button 的真 radio 被 __inner span 遮住，点 input 会被判 intercept
    await pane.locator('label.el-radio-button').filter({ hasText: '14 天' }).click()
    await expect(pane.getByRole('radio', { name: '14 天' })).toBeChecked()
    await expect(pane.locator('canvas')).toHaveCount(2)

    const heads = await pane.locator('.el-table__header-wrapper thead th')
      .evaluateAll((ths) => ths.map((th) => (th.textContent ?? '').trim()).filter(Boolean))
    expect(heads).toEqual(['时间', '告警类型', '等级', '处理状态', '说明'])
    // 等级列：Alert 契约无该字段（卡内已报 PM）⇒ 只出占位，不用类型着色冒充
    const firstAlert = pane.locator('.el-table__body-wrapper tbody tr').first()
    await expect(firstAlert.locator('td').nth(2)).toHaveText('—')
    await expect(firstAlert.locator('td').nth(1)).toContainText('压力偏高')
  })

  test('无设备患者（PT-005 赵欣然）走空态，不画一条全 0 的假曲线', async ({ page }) => {
    const ws = await openWorkspace(page)
    await pickSelectOption(page, ws.locator('.patient-select'), '赵欣然')
    await ws.getByRole('tab', { name: '数据视图' }).click()
    const pane = ws.locator('.el-tab-pane:visible')
    await expect(pane.locator('.el-empty').first()).toContainText('暂无日佩戴统计')
    await expect(pane.locator('canvas')).toHaveCount(0)
  })
})

/**
 * T301 G1 内容级用例（origin/main PR #156 带入，合并时按 8.1 改版重定位）
 *
 * 🔴 判据一条没删，只改了三处定位：
 *  - 工作台不再是页面默认视图 ⇒ 每条用例先点「患者工作台」页签；
 *  - 外层 el-tabs 也在 DOM 里 ⇒ 页签数 / 空态 / 表格一律 scope 到 .view-workspace；
 *  - 感受表新增「佩戴感受」列 ⇒ 不适部位从 td[2] 变成 td[3]。
 * mock 对齐 apps/admin-web/src/mock/orthosis.ts（PT-001 林小雨）：
 *  方案 2 条（v2.1 / v1.0，保存新方案 mock 固定前插 v2.2）；
 *  感受 2 条 FL-001（胸段、未回复）/ FL-002（无不适部位、已回复）；
 *  报告 周报 92.5% / 38.2N、月报 88.1%。
 * 写操作只改浏览器内存里的 mock，每条用例新开 context，不碰共享 seed。
 */
async function openWorkspace(page: Page): Promise<Locator> {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.orthosisLog)
  await expect(listRows(page).first()).toBeVisible({ timeout: 15_000 })
  await page.getByRole('tab', { name: '患者工作台' }).click()
  const ws = page.locator('.view-workspace')
  await expect(ws).toBeVisible()
  return ws
}

test.describe('工作台 · 未选患者与角色差异（T301 G1）', () => {
  test('未选患者只有占位空态，不渲染三 Tab', async ({ page }) => {
    const ws = await openWorkspace(page)
    await expect(ws.locator('.empty-placeholder')).toContainText('请选择患者开始诊断评估')
    await expect(ws.locator('.el-tabs__item')).toHaveCount(0)
  })

  test('医生角色显示「仅本团队患者」提示，运营管理员不显示', async ({ page }) => {
    await adminLogin(page, 'doctor')
    await page.goto(adminRoutes.orthosisLog)
    await page.getByRole('tab', { name: '患者工作台' }).click()
    await expect(page.locator('.view-workspace .page-toolbar .el-tag')).toContainText('医护工作台：仅本团队患者')

    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.orthosisLog)
    await page.getByRole('tab', { name: '患者工作台' }).click()
    await expect(page.locator('.view-workspace .page-toolbar .el-tag')).toHaveCount(0)
  })
})

test.describe('工作台 · 矫形方案 Tab（T301 G1）', () => {
  let ws: Locator

  test.beforeEach(async ({ page }) => {
    ws = await openWorkspace(page)
    await pickSelectOption(page, ws.locator('.patient-select'), '林小雨')
  })

  test('历史方案 2 条，时间线含版本号', async () => {
    await expect(ws.locator('.page-card-title').filter({ hasText: '历史方案' })).toHaveText('历史方案（2）')
    const stamps = ws.locator('.el-timeline-item__timestamp')
    await expect(stamps).toHaveCount(2)
    await expect(stamps.first()).toContainText('v2.1')
    await expect(stamps.nth(1)).toContainText('v1.0')
    await expect(ws.locator('.el-timeline-item').first()).toContainText('T7-T9')
  })

  test('方案内容为空时「保存新方案」禁用', async () => {
    await expect(ws.getByRole('button', { name: '保存新方案' })).toBeDisabled()
  })

  test('保存新方案后前插 v2.2，输入框清空', async ({ page }) => {
    await ws.locator('.page-card textarea').first().fill('T301 用例：夜间佩戴目标调整')
    await ws.getByRole('button', { name: '保存新方案' }).click()
    await expect(adminMessage(page)).toContainText('方案已保存')
    await expect(ws.locator('.page-card-title').filter({ hasText: '历史方案' })).toHaveText('历史方案（3）')
    const stamps = ws.locator('.el-timeline-item__timestamp')
    await expect(stamps.first()).toContainText('v2.2')
    await expect(ws.locator('.el-timeline-item').first()).toContainText('T301 用例：夜间佩戴目标调整')
    await expect(ws.locator('.page-card textarea').first()).toHaveValue('')
  })
})

test.describe('工作台 · 佩戴感受 Tab（T301 G1）', () => {
  let ws: Locator
  let rows: Locator

  test.beforeEach(async ({ page }) => {
    ws = await openWorkspace(page)
    await pickSelectOption(page, ws.locator('.patient-select'), '林小雨')
    await ws.getByRole('tab', { name: '佩戴感受' }).click()
    rows = tableRows(page, ws.locator('.workspace-feelings-card'))
  })

  test('2 条日志：感受两档、不适部位中文映射、无部位显示 -', async () => {
    await expect(rows).toHaveCount(2)
    // 列序：0 日期 1 佩戴感受 2 舒适度 3 不适部位 4 患者备注 5 医生回复
    const d11 = rows.filter({ hasText: '2026-08-11' })
    await expect(d11).toContainText('贴合')
    await expect(d11.locator('td').nth(3)).toHaveText('胸段')
    await expect(d11).toContainText('上午有点闷')
    await expect(rows.filter({ hasText: '2026-08-10' }).locator('td').nth(3)).toHaveText('-')
  })

  test('已回复日志展示回复文本，未回复日志展示输入框', async () => {
    await expect(rows.filter({ hasText: '2026-08-10' }).locator('.reply-content')).toHaveText('继续保持，注意睡姿')
    const pending = rows.filter({ hasText: '2026-08-11' })
    await expect(pending.locator('.reply-content')).toHaveCount(0)
    await expect(pending.locator('textarea')).toHaveCount(1)
  })

  test('医生回复未回复日志：提交后行内出现回复内容', async ({ page }) => {
    const row = rows.filter({ hasText: '2026-08-11' })
    await expect(row.getByRole('button', { name: '回复' })).toBeDisabled()
    await row.locator('textarea').fill('已阅，注意调整肩带松紧')
    await row.getByRole('button', { name: '回复' }).click()
    await expect(adminMessage(page)).toContainText('回复成功')
    await expect(row.locator('.reply-content')).toHaveText('已阅，注意调整肩带松紧')
    await expect(row.locator('textarea')).toHaveCount(0)
  })
})

test.describe('工作台 · 健康报告 Tab（T301 G1）', () => {
  let ws: Locator

  test.beforeEach(async ({ page }) => {
    ws = await openWorkspace(page)
    await pickSelectOption(page, ws.locator('.patient-select'), '林小雨')
    await ws.getByRole('tab', { name: '健康报告' }).click()
  })

  test('周报/月报标题与周期正确', async () => {
    const titles = ws.locator('.report-title')
    await expect(titles).toHaveCount(2)
    await expect(titles.first()).toContainText('周报：2026-08-04 ~ 2026-08-10')
    await expect(titles.nth(1)).toContainText('月报：2026-07-01 ~ 2026-07-31')
  })

  test('指标渲染：佩戴达标率、平均压力带单位', async () => {
    const first = ws.locator('.report-desc').first()
    await expect(first).toContainText('92.5%')
    await expect(first).toContainText('38.2N')
    await expect(first).toContainText('佩戴依从性良好')
  })

  test('趋势判定着色：向好=success，平稳=info', async () => {
    const tags = ws.locator('.report-header .el-tag')
    await expect(tags.first()).toHaveText('趋势向好')
    await expect(tags.first()).toHaveClass(/el-tag--success/)
    await expect(tags.nth(1)).toHaveText('保持平稳')
    await expect(tags.nth(1)).toHaveClass(/el-tag--info/)
  })

  test('切换到无数据患者（PT-005 赵欣然）三个 Tab 均为空态', async ({ page }) => {
    // el-tab-pane 切走后仍在 DOM 内（display:none），空态断言须限定当前可见面板
    const activeEmpty = ws.locator('.el-tab-pane:visible .el-empty')
    await pickSelectOption(page, ws.locator('.patient-select'), '赵欣然')
    await ws.getByRole('tab', { name: '矫形方案' }).click()
    await expect(activeEmpty.first()).toContainText('暂无方案记录')
    await ws.getByRole('tab', { name: '佩戴感受' }).click()
    await expect(activeEmpty.first()).toContainText('暂无感受日志')
    await ws.getByRole('tab', { name: '健康报告' }).click()
    await expect(activeEmpty.first()).toContainText('暂无健康报告')
  })
})
