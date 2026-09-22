import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, tableRows } from '../admin-helpers'

/**
 * T061 患者沟通 E2E（6/6 已转绿 · Winner 实现）
 *
 * 设计源：docs/design/admin/患者沟通.html
 * 覆盖 6 条验收用例：
 *   1. 🟢 渲染"打开微信客服后台"按钮
 *   2. 🟢 点击按钮新窗口打开微信客服 (mpkf.weixin.qq.com)
 *   3. 🟢 列表展示已有反馈（患者/内容/状态 tag）
 *   4. 🟢 详情对话框可打开显示反馈内容
 *   5. 🟢 回复并标记已处理后状态变更 + 列表刷新
 *        实现：mock/communication.ts 补 mockProcessFeedback 更新 FEEDBACKS 内存
 *   6. 🟢 客服角色权限提示 alert 可见
 *
 * 实现方转绿补全（Winner T061）：
 *   - apps/admin-web/src/pages/communication/index.vue .page-toolbar 新增
 *     "打开微信客服后台"按钮，click → window.open('https://mpkf.weixin.qq.com/', '_blank')
 *   - mock/communication.ts + api/index.ts USE_MOCK 分支补 FEEDBACKS 记录内存更新
 *   - 原 #1 #2 #5 的 test.fail 标记已移除（3 处）
 *
 * T310 / F8（Ella 在 T301 覆盖矩阵 §四 登记）：上面第 2 条用例改前用「真实导航成功」当判据，
 *   外网不可达时 popup.url() 落在 chrome-error://chromewebdata/ ⇒ 假红。现已改为本地拦截。
 */

// ─────────────────────────────────────────────────────────────
// Admin 视角（前 5 条用例）
// ─────────────────────────────────────────────────────────────
test.describe('患者沟通 · admin 视角', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.communication)
  })

  // ── 打开微信客服后台入口 ───────────────────────────────
  test.describe('打开微信客服后台入口', () => {
    test('页面工具栏渲染"打开微信客服后台"按钮', async ({ page }) => {
      // 按钮应位于 .page-toolbar（与"查询"同排）
      const toolbar = page.locator('.page-toolbar')
      const btn = toolbar.getByRole('button', { name: '打开微信客服后台' })
      await expect(btn).toBeVisible()
      await expect(btn).toContainText('打开微信客服后台')
    })

    test('点击按钮新窗口打开微信客服 URL 含 mpkf.weixin.qq.com（本地拦截，不出网）', async ({ page }) => {
      // T310/F8：改前把「真实导航成功」当判据 —— 外网不可达时 popup.url() 是
      // chrome-error://chromewebdata/，用例直接假红（实测：断外网 3/3 红、联网 5/5 绿）。
      // 现在域名导航由测试侧本地应答，判据换成三件本地可判的事：
      //   ① 确实开了新窗口 ② 开的是微信客服域名 ③ window.open 的 target 是 _blank。
      // 拦截是兜底式的（凡非本机请求一律记账 + 本地应答）⇒ 域名将来被人改走，
      // 记到的 host 就会变（用例仍红），且任何情况下都不会真的出网。
      const external: string[] = []
      await page.context().route('**/*', (route) => {
        const u = route.request().url()
        if (u.startsWith('http://localhost') || u.startsWith('http://127.0.0.1')) return route.continue()
        external.push(u)
        return route.fulfill({
          status: 200,
          contentType: 'text/html; charset=utf-8',
          body: '<!doctype html><title>offline stub for e2e</title>',
        })
      })

      const btn = page.locator('.page-toolbar').getByRole('button', { name: '打开微信客服后台' })
      await expect(btn).toBeVisible()

      // spy window.open 调用参数。改前用 addInitScript 注入 —— 但它在 page.goto 之后才注册，
      // 对当前页根本不生效，兜底路径一旦走到就只是换个报错（T310 顺带修）。页面已加载，直接改活页。
      await page.evaluate(() => {
        const w = window as unknown as { __openCalls?: unknown[][] }
        w.__openCalls = []
        const origOpen = window.open.bind(window)
        window.open = function (...args: unknown[]) {
          w.__openCalls!.push(args)
          return origOpen(...(args as [string, string | undefined, string | undefined]))
        }
      })

      const popupPromise = page.waitForEvent('popup')
      await btn.click()
      const popup = await popupPromise

      expect(popup.url()).toContain('mpkf.weixin.qq.com')
      expect([...new Set(external.map((u) => new URL(u).host))]).toEqual(['mpkf.weixin.qq.com'])

      const calls = await page.evaluate(
        () => (window as unknown as { __openCalls: unknown[][] }).__openCalls,
      )
      expect(calls.length).toBe(1)
      expect(String(calls[0]?.[0])).toBe('https://mpkf.weixin.qq.com/')
      expect(String(calls[0]?.[1])).toBe('_blank')
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

    test('点击反馈行在右侧面板显示反馈内容', async ({ page }) => {
      // T247: 改为左右布局，左侧列表点击行 → 右侧详情面板
      const rows = tableRows(page)
      await expect(rows).toHaveCount(4)

      // 点首行（FB-001 林小雨）
      await rows.nth(0).click()

      // 右侧面板显示 el-descriptions
      const rightPane = page.locator('.right-pane')
      const descriptions = rightPane.locator('.el-descriptions')
      await expect(descriptions).toBeVisible({ timeout: 5_000 })
      await expect(descriptions).toContainText('患者')
      await expect(descriptions).toContainText('类型')
      await expect(descriptions).toContainText('内容')
      await expect(descriptions).toContainText('提交时间')
    })
  })

  // ── 回复与处理流程 ──────────────────────────────────────
  test.describe('回复与处理流程', () => {
    test('填写回复并提交后 ElMessage 成功 + 状态变更 + 列表刷新', async ({ page }) => {
      // T247: 改为左右布局，回复在右侧面板内联操作
      const rows = tableRows(page)
      await expect(rows).toHaveCount(4)

      // 点 pending 行（林小雨，第 0 行）
      const firstRow = rows.nth(0)
      await expect(firstRow).toContainText('林小雨')
      await firstRow.click()

      const rightPane = page.locator('.right-pane')
      // status=pending 时应有回复输入框 + 按钮
      const replyInput = rightPane.locator('.reply-box textarea, .reply-box .el-textarea textarea')
      const submitBtn = rightPane.getByRole('button', { name: '回复并标记' })
      await expect(replyInput).toBeVisible()
      await expect(submitBtn).toBeVisible()

      await replyInput.fill('已安排调整支具，明日下午门诊复查确认')
      await submitBtn.click()

      // ElMessage 成功提示
      await expect(adminMessage(page)).toContainText('成功', { timeout: 10_000 })

      // 列表对应行状态变更（变为 replied=已回复 或 resolved=已解决 任一即可）
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
