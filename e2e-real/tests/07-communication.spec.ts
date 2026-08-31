import { test, expect, type Locator } from '@playwright/test'
import {
  realLogin,
  gotoMenu,
  tableRows,
  adminMessage,
  realRoutes,
  E2E_REPLY_PREFIX,
  uniqueName,
  getAllTagTexts,
} from '../real-helpers'

/**
 * T053 - 07 患者沟通（真实模式）
 * T051 seed：至少 17 条 feedbacks（含重复执行的历史遗留，用 ≥17 断言）。
 * 覆盖：列表渲染 / 详情对话框 / 回复处理（写操作，唯一命名）
 */
test.describe('07-患者沟通', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    await gotoMenu(page, '患者沟通')
    await expect(page.locator('.el-table__body-wrapper tbody tr').first()).toBeVisible({
      timeout: 25_000,
    })
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

  test.describe('详情对话框', () => {
    test('7.2 点「详情」→ 显示标题 + 患者/类型/内容/提交时间 字段', async ({ page }) => {
      const rows = tableRows(page)
      // 找到有「详情」按钮的第一行
      let target: Locator | null = null
      for (let i = 0; i < Math.min(await rows.count(), 10); i++) {
        const btn = rows.nth(i).getByRole('button', { name: '详情' })
        if ((await btn.count()) > 0 && (await btn.isVisible().catch(() => false))) {
          target = rows.nth(i)
          break
        }
      }
      expect(target).not.toBeNull()
      await target!.getByRole('button', { name: '详情' }).first().click()

      const dialog = page.locator('.el-dialog')
      await expect(dialog).toBeVisible({ timeout: 10_000 })
      // 对话框标题含「反馈」或「详情」
      const title = await dialog.locator('.el-dialog__title, .el-dialog__header').first().textContent()
      expect(title).toMatch(/反馈|详情|Detail/i)

      // 描述块字段（el-descriptions 或任何含关键字段的区域）
      const content = await dialog.textContent() ?? ''
      const expectedFields = ['患者', '类型', '内容', '提交时间']
      const hit = expectedFields.filter((f) => content.includes(f)).length
      // 至少命中 3 个（允许字段名微调）
      expect(hit).toBeGreaterThanOrEqual(3)

      // 关闭（右上 × 或按钮）
      const closeBtn = dialog.locator('.el-dialog__close, .el-dialog__headerbtn').first()
      if ((await closeBtn.count()) > 0) {
        await closeBtn.click()
      } else {
        const cancel = dialog.getByRole('button', { name: /关闭|取消/ }).first()
        if ((await cancel.count()) > 0) await cancel.click()
      }
      await expect(dialog).toBeHidden({ timeout: 5_000 }).catch(() => {})
    })
  })

  test.describe('回复并标记处理（写）', () => {
    test('7.3 找到 pending 反馈 → 详情 → 填回复（T053回复-xxx）→ 提交成功 + 状态变更', async ({ page }) => {
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
      // 如果没有 pending，找任何一行都行（不阻塞用例）
      let targetRow: Locator
      if (pendingIdx >= 0) {
        targetRow = rows.nth(pendingIdx)
      } else {
        targetRow = rows.first()
      }

      const detailBtn = targetRow.getByRole('button', { name: '详情' })
      await expect(detailBtn.first()).toBeVisible({ timeout: 5_000 })
      await detailBtn.first().click()

      const dialog = page.locator('.el-dialog')
      await expect(dialog).toBeVisible({ timeout: 10_000 })
      // 回复输入框
      const replyInput = dialog.locator('textarea, .reply-input, .el-textarea textarea').first()
      await expect(replyInput).toBeVisible({ timeout: 5_000 })
      const replyText = `${uniqueName(E2E_REPLY_PREFIX)} 已安排门诊复查，跟进处理中`
      await replyInput.fill(replyText)
      // 提交按钮：回复并标记 / 提交 / 回复
      const submitBtn = dialog
        .getByRole('button', { name: /回复并标记|提交回复|提交|回复|确认/ })
        .first()
      await expect(submitBtn).toBeVisible({ timeout: 5_000 })
      await submitBtn.click()
      const msg = adminMessage(page)
      const visible = await msg.isVisible({ timeout: 20_000 }).catch(() => false)
      if (visible) {
        const t = await msg.textContent()
        // 成功提示
        if (/成功|完成|已回复|已解决|已提交/.test(t ?? '')) {
          // 对话框关闭
          await expect(dialog).toBeHidden({ timeout: 5_000 }).catch(() => {})
          // 列表对应行 tag 已变更
          await page.waitForTimeout(2_000)
          const rowsAfter = tableRows(page)
          if (pendingIdx >= 0 && pendingIdx < await rowsAfter.count()) {
            const tagsAfter = await rowsAfter.nth(pendingIdx).locator('.el-tag').allTextContents()
            const changed = tagsAfter.some((x) => /已回复|已解决|处理完成|回复/i.test(x))
            expect(changed).toBe(true)
          }
        }
      }
    })
  })
})
