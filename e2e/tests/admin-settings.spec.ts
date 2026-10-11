import { test, expect, type Locator, type Page } from '@playwright/test'
import { adminRoutes, adminLogin } from '../admin-helpers'

/**
 * admin-web 系统配置输入边界：T270 补齐 README §5 第一类缺口 A-SET-03
 *
 * 需求原文 = T267 docs/tests/acceptance/admin/系统配置.md A-SET-03。
 *
 * T653（T642 R2/R3 甲，2026-10-11）：本页瘦身至 **3 个平台参数**
 * （数据采集间隔 / 数据保留天数 / 最大患者数）。退场编辑位与页面：
 * - 每日佩戴目标、设备离线判定时间、传感器标定异常阈值 → 唯一编辑位归「告警管理 · Tab2 全局规则」
 *   （同键 wear_target_hours / threshold_wear_interrupt_minutes / threshold_sensor_drift）；
 * - 「压力阈值配置」整卡 → 统一压力上下限归告警页 Tab2（threshold_pressure_high/low 键与患者端
 *   热力图消费方不动）；
 * - 「通知规则」「发送记录」两 Tab 摘 UI 退场。
 * 全部只摘 UI：settings GET 仍回填、PUT 仍原值回传（隐藏键有区间校验，缺键 ⇒ 0 ⇒ 400），
 * 载荷侧由 apps/admin-web/test/settings-visible-terms.spec.ts 钉住（e2e 打 mock dev server，载荷观测不到）。
 *
 * 🔴 本页是全局配置，A-SET-03 全程**不点「保存配置」**：越界值只在输入框里试，
 * 试完刷新还原（用例 4 就是拿「刷新后回到改动前的值」反证没落库）。
 */

/**
 * 可编辑数字框的完整标签（含单位）。
 * 按 label 精确匹配而非 form-item 全文 —— 各字段的 form-hint 里会复述隔壁字段的名词，
 * 用 hasText 会串到别的格。T653 后本表只剩 3 个平台参数；
 * 压力波动幅度阈值（T419 S-6）与佩戴目标/设备离线/传感器/压力三档（T653）均已摘 UI，
 * 缺席由本文件反向断言钉，PUT 仍回传隐藏键由 settings-visible-terms.spec.ts 钉。
 */
const LABELS = {
  interval: '数据采集间隔（秒）',
  retention: '数据保留天数',
  maxPatients: '最大患者数',
} as const

/** T653 摘 UI 的旧标签：整页（含 .settings-form）不得残留 */
const REMOVED_LABELS = [
  '每日佩戴目标时长（h）',
  '设备离线判定时间（分钟）',
  '传感器标定异常告警阈值（N）',
  '低压上限（N）',
  '正常上限（N）',
  '偏高上限（N）',
  '压力波动幅度阈值（%）',
] as const

type FieldKey = keyof typeof LABELS

function formItem(page: Page, key: FieldKey): Locator {
  const label = LABELS[key]
  return page
    .locator('.el-form-item')
    .filter({ has: page.locator(`.el-form-item__label:text-is("${label}")`) })
    .first()
}

function numInput(page: Page, key: FieldKey): Locator {
  return formItem(page, key).locator('.el-input-number input')
}

function stepper(page: Page, key: FieldKey, dir: 'increase' | 'decrease'): Locator {
  return formItem(page, key).locator(`.el-input-number__${dir}`)
}

/** 真键盘输入（fill 只置 DOM 值，EP 的 change 提交在某些字段上不会触发） */
async function typeInto(input: Locator, raw: string): Promise<void> {
  await input.click()
  await input.press('Control+a')
  await input.pressSequentially(raw)
}

/** 输入越界值并失焦，返回输入框最终留住的文本 */
async function typeAndBlur(input: Locator, raw: string): Promise<string> {
  await typeInto(input, raw)
  await input.blur()
  return input.inputValue()
}

/**
 * 等「后端现值已回显」再读框。
 * 🔴 全局参数卡整张挂 v-loading，罩子只在 GET /admin/settings 返回后移除；
 * 不等它就直接读 ⇒ 读到的是 form 里的**初始默认值**，刷新还原类断言会假失败。
 */
async function waitSettingsLoaded(page: Page): Promise<void> {
  await expect(
    page.locator('.page-card:has(.settings-form) .el-loading-mask').first(),
  ).toBeHidden({ timeout: 15_000 })
}

test.describe('系统配置 · 输入框上下限与步进（T270 A-SET-03；T653 后只剩 3 个平台参数）', () => {
  const ALL = Object.keys(LABELS) as FieldKey[]

  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.settings)
    await expect(page.locator('.settings-form')).toBeVisible({ timeout: 15_000 })
    await waitSettingsLoaded(page)
    // 3 个可编辑框都在，且标签带单位（设计稿口径：数值不能光秃秃）
    for (const key of ALL) await expect(numInput(page, key)).toBeVisible()
    // T653/T419：已摘 UI 的旧编辑位连标签都不该出现（载荷侧的回传由 Vitest 页测钉住）
    for (const label of REMOVED_LABELS) {
      await expect(page.locator('.el-form-item__label', { hasText: label })).toHaveCount(0)
    }
  })

  test('越上限：失焦后夹到各字段 max，夹到顶后「+」禁用', async ({ page }) => {
    const over: Partial<Record<FieldKey, [string, string]>> = {
      interval: ['999999', '3600'],
      retention: ['999999', '3650'],
      maxPatients: ['99999999', '1000000'],
    }
    for (const [key, [raw, clamped]] of Object.entries(over)) {
      expect(await typeAndBlur(numInput(page, key as FieldKey), raw), `「${LABELS[key as FieldKey]}」越上限应夹到 ${clamped}`).toBe(clamped)
    }
    await expect(stepper(page, 'maxPatients', 'increase')).toHaveClass(/is-disabled/)
    await stepper(page, 'maxPatients', 'increase').click({ force: true })
    expect(await numInput(page, 'maxPatients').inputValue(), '已到上限仍点 + 不应继续增大').toBe('1000000')
  })

  test('越下限：失焦后夹到各字段 min', async ({ page }) => {
    const under: Partial<Record<FieldKey, [string, string]>> = {
      // 采集间隔 min 60 —— 后端 validateSettings 要求 ≥60 且为 60 的整数倍，前端同口径
      interval: ['0', '60'],
      retention: ['-5', '1'],
      maxPatients: ['0', '1'],
    }
    for (const [key, [raw, clamped]] of Object.entries(under)) {
      expect(await typeAndBlur(numInput(page, key as FieldKey), raw), `「${LABELS[key as FieldKey]}」越下界应夹到 ${clamped}`).toBe(clamped)
    }
    await expect(stepper(page, 'interval', 'decrease')).toHaveClass(/is-disabled/)
  })

  test('采集间隔 60 整数倍步进（step-strictly）', async ({ page }) => {
    const interval = numInput(page, 'interval')
    await typeInto(interval, '1800')
    await interval.blur()
    await stepper(page, 'interval', 'decrease').click()
    expect(await interval.inputValue(), '步进一次须为 1740').toBe('1740')
    await stepper(page, 'interval', 'increase').click()
    expect(await interval.inputValue(), '回退一次须精确回到 1800').toBe('1800')
  })

  test('不点保存：刷新后 3 个字段全部回到改动前的值', async ({ page }) => {
    const before: Record<string, string> = {}
    for (const key of ALL) before[key] = await numInput(page, key).inputValue()
    for (const key of ALL) {
      expect(before[key], `默认值「${LABELS[key]}」须是可读数字`).toMatch(/^-?\d+(\.\d+)?$/)
    }

    for (const key of ALL) {
      await typeAndBlur(numInput(page, key), '999999')
      expect(await numInput(page, key).inputValue(), `改动后「${LABELS[key]}」不应仍等于默认值`).not.toBe(before[key])
    }

    await page.reload()
    await expect(page.locator('.settings-form')).toBeVisible({ timeout: 15_000 })
    await waitSettingsLoaded(page)
    for (const key of ALL) {
      expect(await numInput(page, key).inputValue(), `刷新后「${LABELS[key]}」应还原（未点保存 ⇒ 未落库）`).toBe(before[key])
    }
    await expect(page.locator('.el-message--error')).toHaveCount(0)
  })
})

/**
 * T653（T642 R2 甲）：压力阈值配置整卡摘 UI（唯一编辑位归告警页 Tab2）。
 * T633（Boss 2026-10-09 报单）：医生默认阈值卡不挂载。
 * 两张卡的数据面（sys_configs threshold_pressure_high/low、患者端热力图消费）一律不动，
 * 这里只钉「界面不在场」，载荷回传由 Vitest 页测钉。
 */
test.describe('系统配置 · T653 摘 UI 反向断言（压力卡 / 通知两 Tab）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.settings)
    await expect(page.locator('.settings-form')).toBeVisible({ timeout: 15_000 })
    await waitSettingsLoaded(page)
  })

  test('压力阈值配置整卡不在场（三档输入与保存按钮都不存在）', async ({ page }) => {
    await expect(page.locator('.pressure-tier-card')).toHaveCount(0)
    for (const label of ['低压上限（N）', '正常上限（N）', '偏高上限（N）']) {
      await expect(page.locator('.el-form-item__label', { hasText: label })).toHaveCount(0)
    }
    await expect(page.getByText('压力阈值配置')).toHaveCount(0)
  })

  test('T633：医生默认阈值卡不挂载（标题与表格都不在场，页面无残留）', async ({ page }) => {
    await expect(page.locator('.default-threshold-card')).toHaveCount(0)
    await expect(page.getByText('医生默认阈值')).toHaveCount(0)
    await expect(page.locator('.point-table')).toHaveCount(0)
  })

  test('通知规则 / 发送记录两 Tab 不在场（只剩阈值与参数、操作日志）', async ({ page }) => {
    const tabs = await page.locator('.el-tabs__item').allInnerTexts()
    expect(tabs.map((t) => t.trim())).not.toContain('通知规则')
    expect(tabs.map((t) => t.trim())).not.toContain('发送记录')
  })
})
