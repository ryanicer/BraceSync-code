import { test, expect, type Page } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * T275 2.3 告警处理流程画布（运行态，USE_MOCK=true）
 * mock 数据见 src/mock/flow.ts：
 * - ALR-001「P10 压力持续偏高」→ 在途实例，r1/r2 done、r3 current
 * - ALR-002「佩戴中断超过 30 分钟」→ 已完成实例，r6 skipped（验四色齐全）
 * - ALR-004「P12 传感器数据漂移」→ 无实例，走「选模板启动」空态
 */

async function openFlowTab(page: Page, rowText: string) {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.alerts)
  await tableRows(page).filter({ hasText: rowText }).getByRole('button', { name: '流程' }).click()
  await expect(page.getByRole('tab', { name: '处理流程' })).toHaveClass(/is-active/)
}

/** 按计算后的描边色统计画布上的连线段（连线色写在 model properties，最终以 SVG 呈现属性落地） */
function edgeStrokeCount(page: Page, color: string): Promise<number> {
  return page.locator('.rp-canvas').evaluate((root, c) => {
    const shapes = root.querySelectorAll('.lf-edge polyline, .lf-edge path')
    let hit = 0
    shapes.forEach((el) => {
      if (getComputedStyle(el).stroke === c) hit += 1
    })
    return hit
  }, color)
}

test.describe('处理流程画布', () => {
  test('① 只读画布：按模板渲染 7 节点 7 连线', async ({ page }) => {
    await openFlowTab(page, 'P10 压力持续偏高')
    await expect(page.locator('.rp-template-bar')).toContainText('默认告警流程')
    await expect(page.locator('.rp-canvas .lf-graph')).toBeVisible()
    await expect(page.locator('.rp-canvas .lf-flow-node')).toHaveCount(7)
    await expect(page.locator('.rp-canvas .lf-edge')).toHaveCount(7)
    // 顶部信息条带出告警编号与触发时间
    await expect(page.locator('.rp-info-row')).toContainText('ALR-001')
    await expect(page.locator('.rp-info-row')).toContainText('2026-08-11 14:30')
  })

  test('② 节点四态着色：done 绿 / current 蓝脉冲 / todo 灰 / skipped 灰虚线', async ({ page }) => {
    await openFlowTab(page, 'P10 压力持续偏高')
    await expect(page.locator('.rp-canvas .lf-flow-node.is-done')).toHaveCount(2)
    await expect(page.locator('.rp-canvas .lf-flow-node.is-current')).toHaveCount(1)
    await expect(page.locator('.rp-canvas .lf-flow-node.is-todo')).toHaveCount(4)
    const shape = (cls: string) => page.locator(`.rp-canvas .lf-flow-node.${cls} .lf-basic-shape`).first()
    await expect(shape('is-done')).toHaveCSS('stroke', 'rgb(16, 185, 129)')
    await expect(shape('is-current')).toHaveCSS('stroke', 'rgb(59, 130, 246)')
    await expect(shape('is-current')).toHaveCSS('animation-name', 'rpNodePulse')
    await expect(shape('is-todo')).toHaveCSS('stroke', 'rgb(203, 213, 225)')

    // 已完成实例才有 skipped 态（灰色 + 虚线）
    await openFlowTab(page, '佩戴中断超过 30 分钟')
    await expect(page.locator('.rp-canvas .lf-flow-node.is-skipped')).toHaveCount(1)
    await expect(page.locator('.rp-canvas .lf-flow-node.is-done')).toHaveCount(6)
    await expect(shape('is-skipped')).toHaveCSS('stroke', 'rgb(203, 213, 225)')
    await expect(shape('is-skipped')).toHaveCSS('stroke-dasharray', '6px, 4px')
    await expect(page.locator('.rp-template-bar')).toContainText('已完成')
  })

  test('③ 已执行连线高亮为绿色，未到达连线保持灰色', async ({ page }) => {
    await openFlowTab(page, 'P10 压力持续偏高')
    // e1(r1→r2)、e2(r2→r3) 两条已执行；其余为未到达灰线
    await expect.poll(() => edgeStrokeCount(page, 'rgb(16, 185, 129)')).toBeGreaterThanOrEqual(2)
    await expect.poll(() => edgeStrokeCount(page, 'rgb(148, 163, 184)')).toBeGreaterThanOrEqual(1)
  })

  test('④ 右侧操作面板：当前节点信息 + 四个操作 + 意见 + 附件 + 提交', async ({ page }) => {
    await openFlowTab(page, 'P10 压力持续偏高')
    const panel = page.locator('.rp-op-panel')
    await expect(panel.locator('.rp-op-name')).toHaveText('医生确认')
    await expect(panel.locator('.rp-op-info')).toContainText('王医生')
    await expect(panel.locator('.rp-op-info')).toContainText('2小时内')
    for (const label of ['确认处理', '驳回', '转派', '加急']) {
      await expect(panel.getByRole('button', { name: label })).toBeVisible()
    }
    await expect(panel.locator('.op-upload')).toContainText('点击上传附件')
    await expect(panel.getByRole('button', { name: '提交处理' })).toBeVisible()
  })

  test('④ 确认处理后流程推进，判断节点要求选分支', async ({ page }) => {
    await openFlowTab(page, 'P10 压力持续偏高')
    const panel = page.locator('.rp-op-panel')
    await panel.locator('textarea').fill('已让患者调整松紧（e2e）')
    await panel.getByRole('button', { name: '提交处理' }).click()
    await expect(adminMessage(page)).toContainText('处理已提交')
    // r3 done → r4（判断分支）current，两条出边要求选分支
    await expect(page.locator('.rp-canvas .lf-flow-node.is-done')).toHaveCount(3)
    await expect(panel.locator('.rp-op-name')).toHaveText('判断分支')
    await expect(panel.locator('.rp-branch')).toBeVisible()
    await panel.getByRole('button', { name: '提交处理' }).click()
    await expect(adminMessage(page)).toContainText('请先选择走哪条分支')
    await panel.locator('.rp-branch label').filter({ hasText: '常规处理' }).click()
    await panel.getByRole('button', { name: '提交处理' }).click()
    await expect(adminMessage(page)).toHaveText('处理已提交')
    // 只走选中分支：r5 current，r6 仍 todo
    await expect(panel.locator('.rp-op-name')).toHaveText('常规处理')
    await expect(page.locator('.rp-canvas .lf-flow-node.is-todo')).toHaveCount(2)
  })

  test('④ 转派只改指派不改流程位置', async ({ page }) => {
    await openFlowTab(page, 'P10 压力持续偏高')
    const panel = page.locator('.rp-op-panel')
    await panel.getByRole('button', { name: '转派' }).click()
    await panel.getByRole('button', { name: '提交（转派）' }).click()
    await expect(adminMessage(page)).toContainText('转派需要选择目标处理人')
    await pickSelectOption(page, panel.locator('.rp-transfer .el-select'), '张建国')
    await panel.getByRole('button', { name: '提交（转派）' }).click()
    await expect(adminMessage(page)).toContainText('处理已提交')
    await expect(panel.locator('.rp-op-name')).toHaveText('医生确认')
    await expect(page.locator('.rp-canvas .lf-flow-node.is-current')).toHaveCount(1)
  })

  test('④ 点击已完成节点显示只读历史，不显示操作按钮', async ({ page }) => {
    await openFlowTab(page, 'P10 压力持续偏高')
    await page.locator('.rp-canvas .lf-flow-node.is-done').first().click({ force: true })
    const panel = page.locator('.rp-op-panel')
    await expect(panel.locator('.rp-hist')).toBeVisible()
    await expect(panel).toContainText('已接收告警通知，进入处理流程')
    await expect(panel.getByRole('button', { name: '确认处理' })).toHaveCount(0)
  })

  test('⑤ 处理时间线按序展示节点与动作', async ({ page }) => {
    await openFlowTab(page, 'P10 压力持续偏高')
    const items = page.locator('.rp-timeline .el-timeline-item')
    await expect(items).toHaveCount(3)
    await expect(items.first()).toContainText('系统自动')
    await expect(items.first()).toContainText('告警触发')
    await expect(items.nth(1)).toContainText('通知推送')
  })

  test('⑥ 图例覆盖层四色说明', async ({ page }) => {
    await openFlowTab(page, 'P10 压力持续偏高')
    const legend = page.locator('.rp-legend')
    await expect(legend).toBeVisible()
    await expect(legend.locator('.rp-legend-item')).toHaveCount(4)
    await expect(legend).toContainText('已完成')
    await expect(legend).toContainText('被跳过')
  })

  test('无流程告警：选模板启动后进入画布，首个节点即当前节点', async ({ page }) => {
    await openFlowTab(page, 'P12 传感器数据漂移')
    await expect(page.locator('.rp-start')).toContainText('该告警还没有处理流程')
    await page.getByRole('button', { name: '启动流程' }).click()
    await expect(adminMessage(page)).toContainText('流程已启动')
    await expect(page.locator('.rp-canvas .lf-flow-node')).toHaveCount(7)
    await expect(page.locator('.rp-canvas .lf-flow-node.is-current')).toHaveCount(1)
    await expect(page.locator('.rp-canvas .lf-flow-node.is-todo')).toHaveCount(6)
    await expect(page.locator('.rp-timeline')).toHaveCount(0)
  })

  test('未选中告警时提示从列表进入', async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.alerts)
    await page.getByRole('tab', { name: '处理流程' }).click()
    await expect(page.locator('.flow-empty')).toContainText('请先在「告警列表」')
  })
})
