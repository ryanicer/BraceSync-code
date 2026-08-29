import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, tableRows } from '../admin-helpers'

/**
 * T061 患者沟通 E2E（KNOWN_RED 前 2 条）
 *
 * 设计源：docs/design/admin/患者沟通.html
 * 覆盖 6 条验收用例：
 *   1. 🔴 渲染"打开微信客服后台"按钮  → 按钮不存在 · KNOWN_RED（test.fail）
 *   2. 🔴 点击按钮新窗口打开微信客服 (mpkf.weixin.qq.com) · KNOWN_RED（test.fail）
 *   3. 🟢 列表展示已有反馈（患者/内容/状态 tag）
 *   4. 🟢 详情对话框可打开显示反馈内容
 *   5. 🔴 回复并标记已处理后状态变更 + 列表刷新 · KNOWN_RED（test.fail）
 *        注：mock USE_MOCK 分支 processFeedbackApi 目前仅 delay 不更新 FEEDBACKS 内存。
 *        实现方转绿时需补 mock 内存更新逻辑后移除本条 test.fail。
 *        标记为 test.fail 的原因：避免真实 CI 环境 retries=2 后整体 exit 1，
 *        与 #1 #2 同属 KNOWN_RED（但失败原因不同：缺 UI vs 缺 mock 内存更新）。
 *   6. 🟢 客服角色权限提示 alert 可见
 *
 * 预期红态：#1 #2 因工具栏无按钮，#5 因 mock 不更新内存 → 三者 FAIL。
 * test.fail 标记使 CI 视其为预期失败（绿信号）。
 * 实现方转绿清单：
 *   - apps/admin-web/src/pages/communication/index.vue .page-toolbar 新增
 *     "打开微信客服后台"按钮，click → window.open('https://mpkf.weixin.qq.com/', '_blank')
 *   - mock/communication.ts 或 api/index.ts USE_MOCK 分支补 FEEDBACKS 记录内存更新
 *   - 6/6 全绿后移除 #1 #2 #5 的 test.fail 标记（3 处）
 */

// ─────────────────────────────────────────────────────────────
// Admin 视角（前 5 条用例）
// ─────────────────────────────────────────────────────────────
test.describe('患者沟通 · admin 视角', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.communication)
  })

  // ── 打开微信客服后台入口（KNOWN_RED 2 条） ───────────────
  test.describe('打开微信客服后台入口（KNOWN_RED）', () => {
    test.fail('页面工具栏渲染"打开微信客服后台"按钮', async ({ page }) => {
      // 按钮应位于 .page-toolbar（与"查询"同排）
      const toolbar = page.locator('.page-toolbar')
      const btn = toolbar.getByRole('button', { name: '打开微信客服后台' })
      await expect(btn).toBeVisible()
      await expect(btn).toContainText('打开微信客服后台')
    })

    test.fail('点击按钮新窗口打开微信客服 URL 含 mpkf.weixin.qq.com', async ({ page, browserName }) => {
      // 双断言策略：
      // 1) 优先用 waitForEvent('popup') 抓新窗口
      // 2) 兜底 spy window.open 调用参数（mock/无头环境可能拦截真实 popup）
      const btn = page.locator('.page-toolbar').getByRole('button', { name: '打开微信客服后台' })
      await expect(btn).toBeVisible()

      // Spy window.open（兜底）
      await page.addInitScript(() => {
        ;(window as unknown as { __openCalls: unknown[][] }).__openCalls = []
        const origOpen = window.open.bind(window)
        window.open = function (...args: unknown[]) {
          ;(window as unknown as { __openCalls: unknown[][] }).__openCalls.push(args)
          // 尝试调用原方法，失败也返回 stub 对象
          try {
            return origOpen(...(args as [string, string?, string?])) ?? { closed: false }
          } catch {
            return { closed: false } as unknown as Window
          }
        }
      })

      // 重新触发（page 在 addInitScript 后刷新要 re-login；此处在同一页面复用前一步上下文，
      // 故直接点击，不对 popup 做阻塞等待；用 race 任一断言成立即可）

      const popupPromise = page.waitForEvent('popup').catch(() => null)
      await btn.click()
      const popup = await popupPromise

      if (popup && !browserName.toLowerCase().includes('webkit')) {
        // 路径 A：真 popup 拿到 URL
        await popup.waitForLoadState('domcontentloaded', { timeout: 10_000 }).catch(() => {})
        expect(popup.url()).toContain('mpkf.weixin.qq.com')
      } else {
        // 路径 B：兜底 spy
        const calls = await page.evaluate(
          () => (window as unknown as { __openCalls: string[][] }).__openCalls,
        )
        expect(calls.length).toBeGreaterThanOrEqual(1)
        const [url, target] = calls[0] ?? []
        expect(String(url ?? '')).toContain('mpkf.weixin.qq.com')
        // target 可空但默认 _blank
        if (target !== undefined && target !== null) {
          expect(String(target)).toEqual('_blank')
        }
      }
    })
  })

  // ── 反馈列表与详情 ──────────────────────────────────────
  test.describe('反馈列表与详情', () => {
    test('列表展示已有反馈行（患者名 / 内容 / 状态 tag）', async ({ page }) => {
      // 首屏已自动 loadData（onMounted），等待 loading 结束
      await page.waitForSelector('.el-table__body-wrapper tbody tr', { timeout: 10_000 })
      const rows = tableRows(page)
      // mock 数据有 4 条反馈
      await expect(rows).toHaveCount(4)

      // 第一行是 FB-001 林小雨（佩戴不适 / pending）
      const firstRow = rows.nth(0)
      await expect(firstRow).toContainText('林小雨')
      await expect(firstRow).toContainText('佩戴不适')

      // 状态 tag 类型：pending = warning(el-tag type=warning 会有 class 或颜色，但最稳是直接看文案映射)
      const statusTags = page.locator('.el-table__body-wrapper .el-tag')
      // 至少有 pending(待处理) + resolved(已解决) 两种
      const allTags = await statusTags.allTextContents()
      expect(allTags.some((t) => t.includes('待处理'))).toBe(true)
      expect(allTags.some((t) => t.includes('已解决'))).toBe(true)
    })

    test('点击详情按钮打开对话框显示反馈内容', async ({ page }) => {
      const rows = tableRows(page)
      await expect(rows).toHaveCount(4)

      // 点首行"详情"按钮
      const detailBtn = rows.nth(0).getByRole('button', { name: '详情' })
      await expect(detailBtn).toBeVisible()
      await detailBtn.click()

      const dialog = page.locator('.el-dialog').filter({ hasText: '反馈' })
      await expect(dialog).toBeVisible({ timeout: 5_000 })

      // el-descriptions 含关键字段
      const descriptions = dialog.locator('.el-descriptions')
      await expect(descriptions).toBeVisible()
      await expect(descriptions).toContainText('患者')
      await expect(descriptions).toContainText('类型')
      await expect(descriptions).toContainText('内容')
      await expect(descriptions).toContainText('提交时间')
    })
  })

  // ── 回复与处理流程 ──────────────────────────────────────
  test.describe('回复与处理流程', () => {
    test.fail('填写回复并提交后 ElMessage 成功 + 状态变更 + 列表刷新', async ({ page }) => {
      const rows = tableRows(page)
      await expect(rows).toHaveCount(4)

      // 找 pending 行（林小雨，第 0 行）点详情
      const firstRow = rows.nth(0)
      await expect(firstRow).toContainText('林小雨')
      await firstRow.getByRole('button', { name: '详情' }).click()

      const dialog = page.locator('.el-dialog').filter({ hasText: '反馈' })
      await expect(dialog).toBeVisible()

      // status=pending 时应有回复输入框 + 按钮
      const replyInput = dialog.locator('textarea.reply-input, .el-textarea textarea')
      const submitBtn = dialog.getByRole('button', { name: '回复并标记' })
      await expect(replyInput).toBeVisible()
      await expect(submitBtn).toBeVisible()

      await replyInput.fill('已安排调整支具，明日下午门诊复查确认')
      await submitBtn.click()

      // ElMessage 成功提示
      await expect(adminMessage(page)).toContainText('成功', { timeout: 10_000 })
      // 对话框关闭
      await expect(dialog).toBeHidden({ timeout: 10_000 })

      // 列表对应行状态变更（变为 replied=已回复 或 resolved=已解决 任一即可）
      // 注意：mock 未补内存更新时，loadData() 拉回的数据不会反映此变更 → 本断言可能 FAIL
      // 实现方转绿补 mock 内存更新后必过
      const rowsAfter = tableRows(page)
      await expect(rowsAfter.nth(0).locator('.el-tag')).toContainText(/已回复|已解决/, {
        timeout: 15_000,
      })
    })
  })
})

// ─────────────────────────────────────────────────────────────
// 客服角色视角（权限提示 alert）
// ─────────────────────────────────────────────────────────────
test.describe('患者沟通 · 客服角色权限提示', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'cs')
    await page.goto(adminRoutes.communication)
  })

  test('客服角色登录显示权限提示 alert（含"仅可查看反馈与标记处理状态"）', async ({ page }) => {
    // 角色提示 el-alert（.role-hint class）
    const alert = page.locator('.role-hint, .el-alert').first()
    await expect(alert).toBeVisible({ timeout: 10_000 })
    await expect(alert).toContainText('客服角色')
    await expect(alert).toContainText('仅可查看反馈与标记处理状态')
  })
})
