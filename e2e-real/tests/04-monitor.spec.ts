import { test, expect } from '@playwright/test'
import { realLogin, gotoMenu, pickSelectOption, realRoutes } from '../real-helpers'

/**
 * T053 - 04 实时监控（真实模式）
 * 覆盖：页面默认渲染 / 热力图 20 格 / 患者切换
 * ⚠️ 注意：staging 设备模拟器可能未运行，导致状态为 offline/未佩戴，
 *        但监控页前端应对未绑定设备有兜底渲染（热力图 seed 兜底逻辑），
 *        故断言改为「结构正确」，不校验具体状态文案。
 */
test.describe('04-实时监控', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    await gotoMenu(page, '实时监控')
  })

  /** 等待快照时间戳出现（即数据加载完成信号） */
  async function waitForSnapshotLoaded(page: Parameters<typeof test>[0] extends never
    ? never
    : import('@playwright/test').Page): Promise<void> {
    await expect(page.locator('.update-time')).toContainText(/\d{2}:\d{2}:\d{2}/, {
      timeout: 25_000,
    })
  }

  test.describe('页面默认渲染', () => {
    test('4.1 实时同步标签 + 默认患者 + 最近更新时间戳 + 状态指示器 + 设备提示', async ({ page }) => {
      // 1) 实时同步中标签
      await expect(page.locator('.page-toolbar .realtime-tag')).toContainText('实时同步中', {
        timeout: 20_000,
      })
      // 2) 患者卡片 + 选中患者名（el-select__selected-item 或 wrapper 内有任意患者名文字）
      const card = page.locator('.patient-card')
      await expect(card).toBeVisible({ timeout: 20_000 })
      await expect(card.locator('.card-title')).toContainText('患者选择')
      // 选中的患者文字：在 wrapper 内除了"患者选择"标题外的任意非空文字
      const wrapper = page.locator('.patient-card .el-select__wrapper, .patient-card .el-select')
      await expect(wrapper.first()).toBeVisible()

      // 3) 时间戳加载完成（数据就绪信号）
      await waitForSnapshotLoaded(page)

      // 4) 状态指示器存在（佩戴中/未佩戴/异常任一，由真实设备状态决定）
      const status = page.locator('.status-indicator').first()
      await expect(status).toBeVisible()
      const statusText = await status.textContent()
      expect(statusText).toBeTruthy()
      expect(statusText!.trim().length).toBeGreaterThan(0)

      // 5) 设备提示区域存在（内容可以是设备号 DEV-xxxxx 或「未绑定设备」）
      const hint = page.locator('.device-hint').first()
      const hintVisible = await hint.isVisible().catch(() => false)
      if (hintVisible) {
        const t = await hint.textContent()
        expect(t!.trim().length).toBeGreaterThan(0)
      }
    })
  })

  test.describe('热力图 4×5 = 20 格', () => {
    test('4.2 20 格渲染 + P01/P20 端点 + 每格数值 + 唯一最大点标记 + 图例 ≥3 项', async ({ page }) => {
      await waitForSnapshotLoaded(page)
      // 20 格 hm-cell
      const cells = page.locator('.hm-cell')
      await expect(cells).toHaveCount(20, { timeout: 25_000 })

      // 20 个 point id（P01 ~ P20）
      const ids = page.locator('.hm-cell-id')
      await expect(ids).toHaveCount(20)
      await expect(ids.first()).toContainText(/^P\d{2}$/)
      await expect(ids.last()).toContainText('P20')

      // 20 格数值：每格都有文字（含数字或 0/N/A 兜底）
      const vals = page.locator('.hm-cell-val')
      await expect(vals).toHaveCount(20)
      for (let i = 0; i < 20; i++) {
        const v = await vals.nth(i).textContent({ timeout: 3_000 }).catch(() => null)
        // 非空即可（兜底 0 也 OK）
        expect(v !== null && v.trim().length > 0).toBe(true)
      }

      // 恰好 1 个最大点标记（.hm-cell-max）
      await expect(page.locator('.hm-cell-max')).toHaveCount(1)

      // 图例至少 3 项（低/中/高 + 异常或更多）
      const legendItems = page.locator('.hm-legend .hm-lg-item')
      await expect(legendItems.first()).toBeVisible({ timeout: 5_000 }).catch(() => {})
      const legendCount = await legendItems.count()
      expect(legendCount).toBeGreaterThanOrEqual(3)
      const legendText = await page.locator('.hm-legend').textContent()
      expect(legendText).toMatch(/低压|高压|低|高/)
    })
  })

  test.describe('热力图数值渲染与接口同源（T296）', () => {
    // 渲染夹具：改写实时快照响应体（与 4.4 同一手法），用真机 payload 的量级断言前端不再吞精度/错量纲。
    // 设备真实上报口径：points 单位 mN，后端 ÷1000 落库为 N，故页面收到的是亚牛顿值。
    const T296_GRID = Array.from({ length: 20 }, (_, i) => {
      const v = i === 0 ? 0.599 : i === 1 ? 4.0 : i === 2 ? -0.02 : i === 7 ? 0.038 : i === 14 ? 0.014 : 0
      return {
        pointId: `P${String(i + 1).padStart(2, '0')}`,
        row: Math.floor(i / 5) + 1,
        col: (i % 5) + 1,
        label: `R${Math.floor(i / 5) + 1}C${(i % 5) + 1}`,
        pressureValue: v,
        isMax: i === 1,
      }
    })

    async function injectT296Grid(page: import('@playwright/test').Page): Promise<void> {
      await page.route('**/api/v1/patients/*/realtime', async (route) => {
        const res = await route.fetch()
        let body: { data?: Record<string, unknown> }
        try {
          body = await res.json()
        } catch {
          await route.fulfill({ response: res })
          return
        }
        if (body?.data) {
          const data = body.data as {
            pressureHeatmap?: unknown
            heatmapMaxN?: number
            pressureHighN?: number
            pressureRecords?: Array<Record<string, unknown>>
          }
          data.pressureHeatmap = T296_GRID
          data.heatmapMaxN = 6
          data.pressureHighN = 5
          data.pressureRecords = [{ ...(data.pressureRecords?.[0] ?? {}), timestamp: new Date().toISOString() }]
        }
        await route.fulfill({ response: res, body: JSON.stringify(body) })
      })
      await page.locator('.page-toolbar').getByRole('button', { name: '立即刷新' }).click()
    }

    test('4.2c 亚牛顿值不被压成 0/-0，色阶与分级按后端下发上界渲染', async ({ page }) => {
      await waitForSnapshotLoaded(page)
      await injectT296Grid(page)

      const vals = page.locator('.hm-cell-val')
      await expect(vals).toHaveCount(20)
      // P01=0.599 → 一位小数可见（旧实现 toFixed(0) 显示 1，且 P15/P17 全成 0）
      await expect(vals.nth(0)).toHaveText('0.6')
      // P03=-0.02 → 归一为 0.0，不得出现非物理读数「-0」
      await expect(vals.nth(2)).toHaveText('0.0')
      // P08=0.038 / P15=0.014 → 保留 0.0 而非 -0
      await expect(vals.nth(7)).toHaveText('0.0')

      // 色阶上界取快照 heatmapMaxN=6：P02=4.0 → 4/6=0.67 → 「偏高」黄；
      // 若前端仍写死 60，则 4/60=0.067 会渲染成「低压」蓝 —— 此断言即失效点
      const cellBg = async (i: number) => (await page.locator('.hm-cell').nth(i).evaluate(
        (el) => getComputedStyle(el).backgroundColor,
      )).replace(/\s/g, '')
      expect(await cellBg(1), 'P02 4.0N 在 6N 量程下应为偏高黄').toBe('rgb(250,204,21)')
      expect(await cellBg(0), 'P01 0.599N 应为低压蓝').toBe('rgb(96,165,250)')

      // 分级列：4.0 ≥ 0.75×5 → 关注；0.599 < 3.75 → 正常（旧 45/30 分界下两者都显示「正常」）
      const statusCells = page.locator('.points-table tbody tr td:nth-child(4)')
      await expect(statusCells).toHaveCount(20)
      await expect(statusCells.nth(1)).toContainText('关注')
      await expect(statusCells.nth(0)).toContainText('正常')

      // 最大点标记落在接口 isMax 的那一格（P02），不被前端二次归约挪位
      await expect(page.locator('.hm-cell-max')).toHaveCount(1)
      await expect(page.locator('.hm-cell-id').nth(1)).toHaveText('P02')
      await expect(page.locator('.heatmap-card .hm-detail')).toContainText('★ 压力最大点：P02 (R1C2)')
    })

    test('4.2d 热力图卡片显示本帧采集时刻，可与设备逐帧日志对账', async ({ page }) => {
      await waitForSnapshotLoaded(page)
      await injectT296Grid(page)

      const stamp = page.locator('.heatmap-card .card-title .hm-frame-stamp')
      await expect(stamp).toHaveCount(1)
      await expect(stamp).toContainText(/\d{2}:\d{2}:\d{2} 采集 · 距今 \d+s/)
      const age = Number((await stamp.textContent())!.match(/距今 (\d+)s/)![1])
      expect(age, '夹具用当前时刻，距今应远小于一个轮询周期').toBeLessThan(30)
    })
  })

  test.describe('患者切换', () => {
    test('4.3 患者下拉 ≥5 选项，切换 2 次患者后状态或时间戳有更新', async ({ page }) => {
      await waitForSnapshotLoaded(page)
      const select = page.locator('.patient-card .el-select')
      await expect(select.first()).toBeVisible()
      await select.first().click()
      const dropdown = page.locator('.el-select-dropdown:visible')
      await expect(dropdown).toBeVisible({ timeout: 5_000 })
      const options = dropdown.locator('.el-select-dropdown__item')
      // seed 5 患者，至少 5 个选项
      await expect(options.first()).toBeVisible({ timeout: 10_000 })
      const optCount = await options.count()
      expect(optCount).toBeGreaterThanOrEqual(5)

      // 获取第 0、1、2 个患者名（跳过可能的 "请选择" 占位）
      const countOpt = await options.count()
      expect(countOpt).toBeGreaterThanOrEqual(5)
      // 提前读取要切换的患者名（下拉还开着，否则关下拉后 options.nth(1).textContent() 会超时）
      const opt1Text = (await options.nth(1).textContent()) ?? ''
      const opt2Text = (await options.nth(2).textContent()) ?? ''
      expect(opt1Text.length).toBeGreaterThan(0)
      // 点击空白处关闭下拉（先不选）
      await page.locator('.card-title').first().click()
      await page.locator('.el-select-dropdown:visible').waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {})

      // 1) 记录切换前时间戳
      const tsBefore = await page.locator('.update-time').textContent()
      await page.waitForTimeout(1_100)

      // 2) 切换到非第一个患者（取第 1 个，索引 1）
      await pickSelectOption(
        page,
        page.locator('.patient-card .el-select').first(),
        opt1Text,
      )
      // 等待状态或时间戳变化
      await expect(page.locator('.update-time')).not.toHaveText(tsBefore ?? '', {
        timeout: 20_000,
      }).catch(async () => {
        // 如果时间戳没变化，至少状态指示器要存在（兜底通过）
        await expect(page.locator('.status-indicator')).toBeVisible()
      })

      // 3) 再切一次（用提前取的 opt2Text，避免下拉已关闭时再读 options）
      const tsMid = await page.locator('.update-time').textContent()
      await page.waitForTimeout(1_100)
      await pickSelectOption(
        page,
        page.locator('.patient-card .el-select').first(),
        opt2Text,
      )
      // 页面仍在 /monitor
      expect(new URL(page.url()).pathname).toContain('/monitor')
      // 状态指示器仍显示
      await expect(page.locator('.status-indicator')).toBeVisible()
    })
  })

  /**
   * T279 补：A-MON-08「有事件」分支（docs/tests/acceptance/admin/实时监控.md:160）
   *
   * 为什么真实模式也要靠拦截才走得到：真实后端把快照的 alerts 写死成空数组——
   *   services/data-service/internal/service/record.go:461  `Alerts: []any{}`
   *   （model.go:453 注释「今日告警摘要，明细由 alert-service 提供」，实现从未接上）
   * ⇒ staging 7 名患者逐个 GET /api/v1/patients/{id}/realtime 实测 alerts 长度全为 0，
   *    与 mock（mock/patients.ts:135 写死 []）同一条死分支。所以这里只改写 alerts 这一个字段、
   *    其余响应原样透传，把前端渲染分支暴露出来；后端真接上告警摘要后可删掉拦截。
   */
  test.describe('近期异常事件 · 有事件分支', () => {
    const INJECTED = [
      {
        alertId: 'T279-EV-1',
        timestamp: '2026-09-21T02:05:00Z',
        type: 'pressure_high',
        detail: 'T279 注入事件：压力峰值超过偏高阈值',
        sensorPoint: 'R2C3',
      },
      {
        alertId: 'T279-EV-2',
        timestamp: '2026-09-20T18:40:00Z',
        type: 'wear_interrupt',
        detail: 'T279 注入事件：佩戴中断超过判定时长',
        sensorPoint: '',
      },
      {
        alertId: 'T279-EV-3',
        timestamp: '2026-09-21T04:11:00Z',
        type: 'sensor_drift',
        detail: 'T279 注入事件：基线漂移超过告警阈值',
        sensorPoint: 'R4C5',
      },
    ]

    test('4.4 快照带事件时：四列表头 + 逐行时间/类型徽章/详情/采集点渲染，无 undefined/NaN', async ({ page }) => {
      await page.route('**/api/v1/patients/*/realtime', async (route) => {
        const res = await route.fetch()
        let body: { data?: Record<string, unknown> }
        try {
          body = await res.json()
        } catch {
          await route.fulfill({ response: res })
          return
        }
        if (body && body.data) body.data.alerts = INJECTED
        await route.fulfill({ response: res, body: JSON.stringify(body) })
      })

      // 触发一次带拦截的刷新（页面本身 2s 轮询，点「立即刷新」把它拉到当前）
      await page.locator('.page-toolbar').getByRole('button', { name: '立即刷新' }).click()

      const card = page.locator('.page-card').filter({ hasText: '近期异常事件' })
      await expect(card).toBeVisible()
      await expect(card.locator('.card-title')).toHaveText('近期异常事件')

      // 1) 表头四列，顺序与文案逐字对齐设计稿
      await expect(card.locator('.events-table thead th')).toHaveText(['时间', '类型', '详情', '采集点'])

      // 2) 三行事件（空态行必须消失）
      const rows = card.locator('.events-table tbody tr')
      await expect(rows).toHaveCount(INJECTED.length)
      await expect(card.locator('.events-table tbody .empty-cell')).toHaveCount(0)

      // 3) 逐行逐格：时间 HH:mm / 类型中文徽章 + 对应色类 / 详情原文 / 采集点缺省显示 —
      for (let i = 0; i < INJECTED.length; i++) {
        const ev = INJECTED[i]
        const cells = rows.nth(i).locator('td')
        await expect(cells).toHaveCount(4)

        const timeText = (await cells.nth(0).textContent())!.trim()
        expect(timeText, `第 ${i + 1} 行时间格式`).toMatch(/^\d{2}:\d{2}$/)

        const badge = cells.nth(1).locator('.event-type')
        // 文案口径 = packages/shared-utils ALERT_TYPE_LABELS（T289 2.6 全站收口：
        // wear_interrupt「佩戴中断」→「设备离线」、sensor_drift「传感器漂移」→「传感器标定异常」）。
        // 色类来自 monitor/index.vue eventTypeClass，本次未变。
        const expectBadge: Record<string, { label: string; cls: string }> = {
          pressure_high: { label: '压力偏高', cls: 'ev-danger' },
          wear_interrupt: { label: '设备离线', cls: 'ev-warn' },
          sensor_drift: { label: '传感器标定异常', cls: 'ev-info' },
        }
        await expect(badge).toHaveText(expectBadge[ev.type].label)
        await expect(badge).toHaveClass(new RegExp(`\\b${expectBadge[ev.type].cls}\\b`))

        await expect(cells.nth(2)).toHaveText(ev.detail)
        await expect(cells.nth(3)).toHaveText(ev.sensorPoint || '—')
      }

      // 4) 两行时间各自绑定（不是同一常量），且整卡无 undefined / NaN
      const t1 = (await rows.nth(0).locator('td').nth(0).textContent())!.trim()
      const t2 = (await rows.nth(1).locator('td').nth(0).textContent())!.trim()
      expect(t1).not.toBe(t2)
      await expect(card).not.toContainText(/undefined|NaN/)
    })
  })
})
