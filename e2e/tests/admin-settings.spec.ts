import { test, expect, type Locator, type Page } from '@playwright/test'
import { adminRoutes, adminLogin } from '../admin-helpers'

/**
 * admin-web 系统配置输入边界：T270 补齐 README §5 第一类缺口 A-SET-03
 *
 * 需求原文 = T267 docs/tests/acceptance/admin/系统配置.md A-SET-03
 * （设计稿 系统配置.html:90-92 number 输入 + :98-100 `step="0.1" min="0"`）。
 *
 * 🔴 本页是全局配置，A-SET-03 全程**不点「保存配置」**：越界值只在输入框里试，
 * 试完刷新还原（用例 4 就是拿「刷新后回到改动前的值」反证没落库）。
 * 断言取「上界夹取 / 下界夹取 / 小数步进精度 / 刷新还原」四类**契约**，
 * 不写死当前配置值 —— 起始值运行时读一次，收尾比对同一份快照。
 */

/**
 * 8 个数字框的完整标签（含单位）。
 * 按 label 精确匹配而非 form-item 全文 —— 「数据采集间隔」的提示语里就写着
 * 「佩戴中断判定时间须 ≥ 2× 采集间隔」，用 hasText 会串到隔壁字段。
 */
const LABELS = {
  interval: '数据采集间隔（秒）',
  retention: '数据保留天数',
  maxPatients: '最大患者数',
  wearHours: '每日佩戴目标时长（h）',
  pressureHigh: '压力偏高阈值（N）',
  pressureFluct: '压力波动幅度阈值（%）',
  wearInterrupt: '佩戴中断判定时间（分钟）',
  drift: '传感器漂移告警阈值（N）',
} as const

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

test.describe('系统配置 · 输入框上下限与步进（T270 A-SET-03）', () => {
  const ALL = Object.keys(LABELS) as FieldKey[]

  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.settings)
    await expect(page.locator('.settings-form')).toBeVisible({ timeout: 15_000 })
    // 8 个框都在，且标签带单位（设计稿口径：数值不能光秃秃）
    for (const key of ALL) await expect(numInput(page, key)).toBeVisible()
  })

  test('越上限：失焦后夹到各字段 max，夹到顶后「+」禁用', async ({ page }) => {
    const over: Partial<Record<FieldKey, [string, string]>> = {
      interval: ['999999', '3600'],
      retention: ['999999', '3650'],
      maxPatients: ['99999999', '1000000'],
      wearHours: ['300', '24'],
      pressureHigh: ['300', '200'], // 走查步骤 2：填 300 → 留 200
      pressureFluct: ['101', '100'],
      wearInterrupt: ['721', '720'],
      drift: ['999', '20'],
    }
    for (const [key, [raw, clamped]] of Object.entries(over)) {
      expect(await typeAndBlur(numInput(page, key as FieldKey), raw), `「${LABELS[key as FieldKey]}」越上限应夹到 ${clamped}`).toBe(clamped)
    }
    await expect(stepper(page, 'drift', 'increase')).toHaveClass(/is-disabled/)
    await stepper(page, 'drift', 'increase').click({ force: true })
    expect(await numInput(page, 'drift').inputValue(), '已到上限仍点 + 不应继续增大').toBe('20')
  })

  test('越下限：失焦后夹到各字段 min', async ({ page }) => {
    const under: Partial<Record<FieldKey, [string, string]>> = {
      // 断言更新（T269 D2）：采集间隔 min 由 1 改 60 —— 后端 validateSettings 要求
      // collectIntervalSeconds ≥ 60 且为 60 的整数倍，前端 min/step/step-strictly 同口径，
      // 免得把必然 400 的值留给后端拒。
      interval: ['0', '60'],
      retention: ['-5', '1'],
      maxPatients: ['0', '1'],
      wearHours: ['0', '1'],
      pressureHigh: ['-1', '1'],
      pressureFluct: ['0', '1'],
      wearInterrupt: ['3', '10'], // 走查步骤 3：填 3 → 留 10
      drift: ['0', '0.1'],
    }
    for (const [key, [raw, clamped]] of Object.entries(under)) {
      expect(await typeAndBlur(numInput(page, key as FieldKey), raw), `「${LABELS[key as FieldKey]}」越下界应夹到 ${clamped}`).toBe(clamped)
    }
    await expect(stepper(page, 'drift', 'decrease')).toHaveClass(/is-disabled/)
  })

  test('小数步进：step=0.1 递增/递减不产生浮点尾巴', async ({ page }) => {
    const drift = numInput(page, 'drift')
    const startText = await drift.inputValue()
    const start = Number(startText)
    expect(Number.isFinite(start), `默认值应为数字，实际 ${startText}`).toBe(true)
    expect(start, '本条要双向步进，字段须处于 (min, max] 中段').toBeGreaterThan(0.1)

    // 真键盘改值并失焦：值必须提交进 model（否则下一步会看到 +0.1 落在旧值上）
    await typeInto(drift, '5')
    expect(await drift.inputValue()).toBe('5')
    await stepper(page, 'drift', 'increase').click()
    expect(await drift.inputValue(), '步进一次须为 5.1，不得是 5.09999…').toBe('5.1')
    await stepper(page, 'drift', 'increase').click()
    expect(await drift.inputValue()).toBe('5.2')
    await stepper(page, 'drift', 'decrease').click()
    expect(await drift.inputValue(), '回退一次须精确回到 5.1').toBe('5.1')
    // 走查步骤 4 的小数位数口径：界面上不出现第 3 位小数
    expect(await drift.inputValue()).not.toMatch(/\.\d{2,}/)
  })

  test('不点保存：刷新后 8 个字段全部回到改动前的值', async ({ page }) => {
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
    for (const key of ALL) {
      expect(await numInput(page, key).inputValue(), `刷新后「${LABELS[key]}」应还原（未点保存 ⇒ 未落库）`).toBe(before[key])
    }
    await expect(page.locator('.el-message--error')).toHaveCount(0)
  })
})
