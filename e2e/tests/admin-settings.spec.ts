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
 * 可编辑数字框的完整标签（含单位）。
 * 按 label 精确匹配而非 form-item 全文 —— 各字段的 form-hint 里会复述隔壁字段的名词
 * （如「设备离线判定时间」下方的「必须 ≥ 2×采集间隔」），用 hasText 会串到采集间隔那格。
 * T419 G-6/S-6：下两项随 PRD §7D.12 V3.20「只改显示文案、键名不动」改名；
 * 原 pressureFluct「压力波动幅度阈值（%）」表单项已下线（T419 S-6），不在此表内
 * ⇒ 缺席由本文件「压力波动幅度阈值表单项已下线」用例钉， PUT 仍回传该键由
 * apps/admin-web/test/settings-visible-terms.spec.ts 钉（e2e 打 mock dev server，载荷观测不到）。
 */
const LABELS = {
  interval: '数据采集间隔（秒）',
  retention: '数据保留天数',
  maxPatients: '最大患者数',
  wearHours: '每日佩戴目标时长（h）',
  // T289 12.4：设计稿 系统配置.html:95-102 把压力阈值独立成「压力阈值配置」卡，
  // 字段名随设计稿改为 低压上限 / 偏高上限（原「压力偏高阈值（N）」并入后者）。
  pressureLow: '低压上限（N）',
  pressureHigh: '偏高上限（N）',
  wearInterrupt: '设备离线判定时间（分钟）',
  drift: '传感器标定异常告警阈值（N）',
} as const

/** 只读回显档，不参与越界/步进/还原三类断言（契约 api-contracts.ts:303-305 前端推导） */
const NORMAL_UPPER_LABEL = '正常上限（N）'

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
 * T301（PR #156）先实测到这条竞态（--workers=4 稳定红、serially 绿），当时用
 * 「连续两次读数一致」轮询收口；本页现值一律走后端回显 ⇒ 罩子消失即等价信号，
 * 合并两条卡时统一改成认 v-loading，不再轮询。
 * （T289 12.4 加了压力阈值 / 医生默认阈值两张卡后本页渲染变重，此竞态又红过一次。）
 */
async function waitSettingsLoaded(page: Page): Promise<void> {
  await expect(
    page.locator('.page-card:has(.settings-form) .el-loading-mask').first(),
  ).toBeHidden({ timeout: 15_000 })
}

test.describe('系统配置 · 输入框上下限与步进（T270 A-SET-03）', () => {
  const ALL = Object.keys(LABELS) as FieldKey[]

  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.settings)
    await expect(page.locator('.settings-form')).toBeVisible({ timeout: 15_000 })
    await waitSettingsLoaded(page)
    // 8 个可编辑框都在，且标签带单位（设计稿口径：数值不能光秃秃）
    for (const key of ALL) await expect(numInput(page, key)).toBeVisible()
    // T419 S-6：已下线的那一格连标签都不该出现（载荷侧的回传由 Vitest 页测钉住）
    await expect(page.locator('.settings-form')).not.toContainText('压力波动幅度阈值')
  })

  test('越上限：失焦后夹到各字段 max，夹到顶后「+」禁用', async ({ page }) => {
    const over: Partial<Record<FieldKey, [string, string]>> = {
      interval: ['999999', '3600'],
      retention: ['999999', '3650'],
      maxPatients: ['99999999', '1000000'],
      wearHours: ['300', '24'],
      pressureLow: ['300', '200'],
      pressureHigh: ['300', '200'], // 走查步骤 2：填 300 → 留 200
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
      pressureLow: ['-5', '0'], // 设计稿 系统配置.html:98 min="0"
      pressureHigh: ['-1', '1'],
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
    await waitSettingsLoaded(page)
    for (const key of ALL) {
      expect(await numInput(page, key).inputValue(), `刷新后「${LABELS[key]}」应还原（未点保存 ⇒ 未落库）`).toBe(before[key])
    }
    await expect(page.locator('.el-message--error')).toHaveCount(0)
  })
})

/**
 * T289 12.4（设计稿 系统配置.html:95-102 压力阈值配置 + :103-137 医生默认阈值）
 *
 * 🔴 三条判据都不写死数值：三档现值、20 点表的上下限全部**从页面读回来再比**，
 *    这样换 staging 现值（T203 ÷10 后 high=5 / low=1）也不会假绿或假红。
 *    设计稿样例里的 20/40/60、45/10 只是稿面值，PM 裁定不许硬编。
 * 全程不点「保存阈值」，改完即弃。
 */
test.describe('系统配置 · 压力阈值三档与医生默认阈值（T289 12.4）', () => {
  const tierCard = (page: Page) => page.locator('.pressure-tier-card')
  const pointCard = (page: Page) => page.locator('.default-threshold-card')

  function tierInput(page: Page, label: string): Locator {
    return tierCard(page)
      .locator('.el-form-item')
      .filter({ has: page.locator(`.el-form-item__label:text-is("${label}")`) })
      .first()
      .locator('.el-input-number input')
  }

  async function num(page: Page, label: string): Promise<number> {
    const raw = await tierInput(page, label).inputValue()
    const n = Number(raw)
    expect(Number.isFinite(n), `「${label}」应读出数字，实际 ${raw}`).toBe(true)
    return n
  }

  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.settings)
    await expect(tierCard(page)).toBeVisible({ timeout: 15_000 })
    await waitSettingsLoaded(page)
    // 设计稿 :98-100 的三档顺序：低压上限 → 正常上限 → 偏高上限
    const labels = await tierCard(page).locator('.el-form-item__label').allInnerTexts()
    expect(labels.map((t) => t.trim()).filter((t) => t.includes('（N）'))).toEqual([
      '低压上限（N）',
      '正常上限（N）',
      '偏高上限（N）',
    ])
  })

  test('中间档只读回显，且恒等于上下两档的算术中值（契约 :303-305）', async ({ page }) => {
    await expect(tierInput(page, '正常上限（N）')).toBeDisabled()
    const low0 = await num(page, '低压上限（N）')
    const high0 = await num(page, '偏高上限（N）')
    expect(await num(page, '正常上限（N）'), '进页面即须满足 (偏高 + 低压) ÷ 2').toBe((high0 + low0) / 2)

    // 改低压 ⇒ 中间档跟着变（前端推导，不是第二个后端键）
    await typeInto(tierInput(page, '低压上限（N）'), '2')
    const high = await num(page, '偏高上限（N）')
    expect(await num(page, '正常上限（N）'), '低压改成 2 后中间档须为 (偏高 + 2) ÷ 2').toBe((high + 2) / 2)
  })

  // T633（Boss 2026-10-09 报单）：「医生默认阈值」整卡不再挂载 ⇒ 原来逐点比稿面的那条判据作废，
  // 换成「卡、标题与表格都不在场」的反向断言。压力三档那张卡的判据（beforeEach 与中间档推导）一条没动。
  test('T633：医生默认阈值卡不挂载（标题与表格都不在场，页面无残留）', async ({ page }) => {
    await expect(pointCard(page)).toHaveCount(0)
    await expect(page.getByText('医生默认阈值')).toHaveCount(0)
    await expect(page.locator('.point-table')).toHaveCount(0)
  })

})
