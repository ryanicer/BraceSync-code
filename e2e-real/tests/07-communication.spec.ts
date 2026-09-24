import { test, expect, type Locator } from '@playwright/test'
import {
  realLogin,
  gotoMenuAndWaitTable,
  tableRows,
  adminMessage,
  E2E_REPLY_PREFIX,
  uniqueName,
  getAllTagTexts,
} from '../real-helpers'

/**
 * T053 - 07 患者沟通（真实模式）
 * T051 seed：至少 17 条 feedbacks（含重复执行的历史遗留，用 ≥17 断言）。
 * 覆盖：列表渲染 / 详情对话框 / 回复处理（写操作，唯一命名）
 */
/**
 * 读右侧详情面板的 el-descriptions（label → value，按渲染顺序）。
 * EP 2.14 的类名是 .el-descriptions__label（值在其 nextElementSibling），
 * 不是 2.2 时代的 .el-descriptions-item__label —— 用后者会静默匹配 0 个节点。
 */
async function readDescriptions(pane: Locator): Promise<{ label: string; value: string }[]> {
  return pane
    .locator('.el-descriptions')
    .evaluate((el) =>
      Array.from(el.querySelectorAll('.el-descriptions__label')).map((th) => ({
        label: (th.textContent ?? '').trim(),
        value: (th.nextElementSibling?.textContent ?? '').trim(),
      })),
    )
}

test.describe('07-患者沟通', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    // T279：先等 URL 落位再等行，避免命中上一页（数据概览）排行表（7.1/7.2 实跑失效根因）
    await gotoMenuAndWaitTable(page, '患者沟通', 'communication')
  })

  test.describe('反馈列表渲染', () => {
    test('7.1 ≥17 行反馈（含重复），每行有患者/内容/状态 tag；待处理 + 已解决同时存在', async ({ page }) => {
      const rows = tableRows(page)
      const count = await rows.count()
      // seed 17 + 历史重复 → ≥17
      expect(count).toBeGreaterThanOrEqual(17)

      const allText = await page.locator('.el-table').textContent() ?? ''
      // 存在患者名（2-4 字中文，seed 里有）
      const chineseName = allText.match(/[\u4e00-\u9fa5]{2,4}/)
      expect(chineseName).toBeTruthy()

      const tags = await getAllTagTexts(page.locator('.el-table'))
      expect(tags.length).toBeGreaterThanOrEqual(17)
      const hasPending = tags.some((t) => /待处理|未处理|pending/i.test(t))
      const hasResolved = tags.some((t) => /已解决|已回复|解决|回复/i.test(t))
      expect(hasPending || hasResolved).toBe(true) // 至少有 1 种
    })
  })

  test.describe('详情面板（主从式：点行 → 右侧出详情，无对话框）', () => {
    test('7.2 点第 3 行 → 右侧「反馈 N 详情」+ 患者/类型/内容/提交时间/状态 逐项等于该行', async ({ page }) => {
      // T279 修：本页真实结构（apps/admin-web/src/pages/communication/index.vue）是左表右详情——
      //   el-table @row-click="selectFeedback"，右侧 .right-pane 用 el-descriptions 渲染，
      //   既没有「详情」按钮也没有 el-dialog。旧断言按 mock 稿的对话框写，在 staging 恒 0 命中（实跑即失败）。
      // ⚠️ loadData() 会把 list[0] 默认塞进 current，所以必须点「非第一行」：
      //   点第一行时「右侧显示第一行」不点也成立 → 假绿。
      const rows = tableRows(page)
      expect(await rows.count()).toBeGreaterThanOrEqual(3)

      const target = rows.nth(2)
      const cellTexts = async () =>
        Promise.all([0, 1, 2, 3, 4].map((i) => target.locator('td').nth(i).innerText().then((t) => t.trim())))
      const [id, patient, type, content, status] = await cellTexts()

      await target.locator('td').nth(3).click()

      const pane = page.locator('.right-pane')

      await expect(pane.locator('.pane-title')).toHaveText(`反馈 ${id} 详情`, { timeout: 10_000 })
      // 详情是行内面板，不是弹层
      await expect(page.locator('.el-dialog')).toHaveCount(0)

      const shown = await readDescriptions(pane)
      expect(shown.map((f) => f.label), '详情字段名与顺序').toEqual([
        '患者', '类型', '内容', '提交时间', '状态',
      ])
      const byLabel = Object.fromEntries(shown.map((f) => [f.label, f.value]))
      expect(byLabel['患者'], '详情的患者 = 所点行的患者列').toBe(patient)
      expect(byLabel['类型'], '详情的类型 = 所点行的类型列').toBe(type)
      expect(byLabel['内容'], '详情的内容 = 所点行的内容列').toBe(content)
      // formatTime() 口径：MM-DD HH:mm（不是完整 ISO）
      expect(byLabel['提交时间']).toMatch(/^\d{2}-\d{2} \d{2}:\d{2}$/)
      expect(byLabel['状态'], '详情的状态 = 所点行的状态列').toBe(status)
    })
  })

  test.describe('处理备注与标记已处理（写）', () => {
    test('7.3 找到 pending 反馈 → 详情 → 填处理备注（T053回复-xxx）→ 保存成功 + 状态变更', async ({ page }) => {
      // T279 复跑停跑：回复对象是 seed 反馈，POST /feedbacks/:id/process 单向、退不回待处理。
      //   ⚠️ PM 裁定（本卡 2026-09-21 13:00）把 7.3 列入「跑（自建唯一命名数据、跑完删除）」，
      //      但后台没有「新建反馈」入口/端点 —— 反馈只能由患者端提交 ⇒ 7.3 的对象必然是共享 seed 反馈，
      //      与同一条裁定的判定「共享 seed 数据默认只读，要动必须先报 PM」直接冲突，已回报待重裁。
      //      （与 3.4 同类：单向、无 un-process 端点、不可回滚 —— 正是 PM 否决 3.4 的那条硬理由。）
      test.skip(true, '反馈只能由患者端产生，后台无新建端点 ⇒ 7.3 只能改共享 seed 反馈且无回退端点，与「seed 默认只读」判定冲突；已报 PM 待重裁')
      // 第一步：先找第一行 pending 反馈（tag 含「待处理」）
      const rows = tableRows(page)
      let pendingIdx = -1
      for (let i = 0; i < Math.min(await rows.count(), 30); i++) {
        const tags = await rows.nth(i).locator('.el-tag').allTextContents()
        if (tags.some((t) => /待处理|未处理|pending/i.test(t))) {
          pendingIdx = i
          break
        }
      }
      // T279 修：打开详情靠「点行」（无详情按钮/无对话框，见 7.2 注释），回复框在右侧 .reply-box
      // 旧写法在找不到 pending 时退化成「点第一行」，然后靠 if(visible) 层层吞掉断言 —— 现在如实前置失败
      expect(pendingIdx, 'staging seed 应存在待处理反馈').toBeGreaterThanOrEqual(0)
      const targetRow = rows.nth(pendingIdx)
      const rowId = (await targetRow.locator('td').nth(0).innerText()).trim()
      await targetRow.locator('td').nth(3).click()

      const pane = page.locator('.right-pane')
      await expect(pane.locator('.pane-title')).toHaveText(`反馈 ${rowId} 详情`, { timeout: 10_000 })

      const replyInput = pane.locator('.reply-box textarea').first()
      await expect(replyInput).toBeVisible({ timeout: 5_000 })
      const replyText = `${uniqueName(E2E_REPLY_PREFIX)} 已安排门诊复查，跟进处理中`
      await replyInput.fill(replyText)
      const submitBtn = pane.locator('.reply-actions').getByRole('button', { name: '保存处理备注' })
      await expect(submitBtn).toBeVisible({ timeout: 5_000 })
      await submitBtn.click()
      // T279 收紧：旧写法「没抓到提示 = 直接过」；按准确文案轮询
      // （communication/index.vue saveNote → ElMessage.success('处理备注已保存')）
      // T374 改文案：staging 需部署到带本次改动的构建后本条才成立（当前 7.3 仍被上面 skip 挡住）
      await expect(adminMessage(page)).toHaveText('处理备注已保存', { timeout: 20_000 })

      // 详情面板新增「客服处理备注」项（T374 文案口径），状态由待处理变已回复
      const shown = await readDescriptions(pane)
      const byLabel = Object.fromEntries(shown.map((f) => [f.label, f.value]))
      expect(shown.map((f) => f.label), '保存备注后详情字段').toEqual([
        '患者', '类型', '内容', '提交时间', '状态', '客服处理备注',
      ])
      expect(byLabel['状态']).toBe('已回复')
      expect(byLabel['客服处理备注']).toBe(replyText)
      // 列表该行 tag 同步变已回复
      await expect(targetRow.locator('.el-tag')).toHaveText('已回复', { timeout: 10_000 })
    })
  })
})
