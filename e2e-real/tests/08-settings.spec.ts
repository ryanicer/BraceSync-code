import { test, expect, type Page } from '@playwright/test'
import { realLogin, gotoMenu, adminMessage, getAuthToken } from '../real-helpers'

/*
 * T279 - 08 系统配置（真实模式）
 *
 * 为什么要单独有一条真实模式的 settings 用例：mock 侧的「保存配置」走
 * e2e/tests/admin-settings.spec.ts → mockSaveSystemSettings()，那个函数只 resolve、
 * 不发 HTTP ⇒ mock 全绿也证明不了「值进了库」。本文件补的就是这一段：
 *   8.1 接口值 → 表单回显逐字段一致（只读，证明 sys_configs 真的在供给页面）
 *   8.2 「保存配置」按钮 → 真发 PUT → GET 回读值确实变了 → 自带还原
 *
 * 🔴 2026-09-21 实跑（staging hbksd.com.cn:81）：8.2 的前置探测 PUT 被后端拒
 *   400 pressureHighThresholdN (5) must be greater than threshold_pressure_low (10)
 *   —— 000019（T203 阈值 ÷10）把 threshold_pressure_high 45→5，漏改 000016 播的
 *      threshold_pressure_low=10，两键一倒挂，validateSettings 就恒不通过，
 *      于是 staging 上「保存配置」点不动。用例按此写成条件跳过（不改 staging 数据去凑绿），
 *      seed 修好后自动恢复真跑，无需改本文件。
 *
 *   PM 裁定（T279 卡内 2026-09-21 13:00）：采纳「条件跳过 + 登记缺陷」，确认是真缺陷，
 *   已另立 **T281 [Winner]** 用新迁移把 threshold_pressure_low 10 改为 1。
 *   ⇒ 本用例 **阻塞于 T281**：T281 合并并部署到 staging 后即自动恢复真跑。
 */

const SETTINGS_API = '/api/v1/admin/settings'

/** GET /api/v1/admin/settings 的响应体（字段口径 = model.SystemSettingsDTO） */
interface Settings {
  dailyWearTargetHours: number
  pressureHighThresholdN: number
  pressureLowThresholdN?: number
  pressureFluctuationPct: number
  wearInterruptMinutes: number
  sensorDriftN: number
  wifiPresets: { ssid: string; password?: string }[]
  collectIntervalSeconds: number
  retentionDays: number
  maxPatients: number
}

/** 用 API 读库（绕开前端状态，作为「真的落库了」的判据） */
async function readSettings(page: Page): Promise<Settings> {
  const token = await getAuthToken(page)
  expect(token, '登录后 localStorage 应有 admin_token').toBeTruthy()
  const res = await page.request.get(SETTINGS_API, {
    headers: { Authorization: `Bearer ${token}` },
  })
  expect(res.status()).toBe(200)
  const body = await res.json()
  expect(body.code, `接口 body: ${JSON.stringify(body)}`).toBe(0)
  return body.data as Settings
}

/** 进系统配置页（staging 深链不可用，只能点侧边栏 —— 见 real-helpers 顶部说明） */
async function openSettings(page: Page): Promise<void> {
  await gotoMenu(page, '系统配置')
  await expect(page).toHaveURL(/\/settings$/, { timeout: 15_000 })
  await expect(page.locator('.settings-form')).toBeVisible({ timeout: 15_000 })
}

/**
 * 取「阈值与参数」Tab 里某个 label 对应的数值框。
 * scope 必须显式选卡：T289 12.4 把压力三档从 .settings-form 拆进独立的 .pressure-tier-form
 * （拆卡是为了让 .settings-form 继续当「全局系统参数」卡的定位锚点，见 settings/index.vue 样式注释），
 * 于是旧写法在 .settings-form 里找「压力偏高阈值（N）」直接超时。
 */
function numberField(page: Page, label: string, scope = '.settings-form') {
  return page
    .locator(`${scope} .el-form-item`)
    .filter({ hasText: label })
    .first()
    .locator('.el-input-number input')
    .first()
}

/** 全局系统参数卡（写 sys_configs 的常规键） */
const FORM_FIELDS: { label: string; key: keyof Settings }[] = [
  { label: '数据采集间隔（秒）', key: 'collectIntervalSeconds' },
  { label: '数据保留天数', key: 'retentionDays' },
  { label: '最大患者数', key: 'maxPatients' },
  { label: '每日佩戴目标时长（h）', key: 'dailyWearTargetHours' },
  { label: '压力波动幅度阈值（%）', key: 'pressureFluctuationPct' },
  { label: '佩戴中断判定时间（分钟）', key: 'wearInterruptMinutes' },
  { label: '传感器漂移告警阈值（N）', key: 'sensorDriftN' },
]

/** 压力阈值配置卡（T289 12.4 三档；label 也随拆卡改名：压力偏高阈值（N）→ 偏高上限（N）） */
const TIER_FIELDS: { label: string; key: keyof Settings }[] = [
  { label: '低压上限（N）', key: 'pressureLowThresholdN' },
  { label: '偏高上限（N）', key: 'pressureHighThresholdN' },
]

test.describe('08-系统配置（真实模式）', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    await openSettings(page)
  })

  test('8.1 表单回显逐字段 == GET /admin/settings（库→接口→页面链路）', async ({ page }) => {
    const api = await readSettings(page)

    // 逐字段比对（不是「页面有数字」这种弱断言）
    const seen: Record<string, { ui: string; api: number }> = {}
    for (const group of [
      { scope: '.settings-form', fields: FORM_FIELDS },
      { scope: '.pressure-tier-form', fields: TIER_FIELDS },
    ]) {
      for (const f of group.fields) {
        const ui = await numberField(page, f.label, group.scope).inputValue()
        seen[f.key] = { ui, api: api[f.key] as number }
        expect(ui.trim(), `字段 ${f.label} 回显`).not.toBe('')
        expect(Number(ui), `字段 ${f.label} 应等于接口值 ${api[f.key]}`).toBe(api[f.key])
      }
    }

    // 接口少给任何一个字段，上面的比对就会 NaN ≠ undefined 而失败
    expect(Object.keys(seen)).toHaveLength(FORM_FIELDS.length + TIER_FIELDS.length)

    // 中间档「正常上限」后端不落库（三档合两键），页面按契约推导 =（偏高上限 + 低压上限）÷ 2；
    // 这条断言守的是「拆卡后推导算法没被顺手改掉」。
    const normalUpperUi = await numberField(page, '正常上限（N）', '.pressure-tier-form').inputValue()
    const derived = (api.pressureHighThresholdN + (api.pressureLowThresholdN ?? 0)) / 2
    expect(Number(normalUpperUi), `正常上限应等于推导值 ${derived}`).toBe(derived)

    // WiFi 预设：接口有 seed 就必须渲染出来（脱敏列不得把 ssid 吞掉）
    if (api.wifiPresets.length > 0) {
      const wifiRows = page
        .locator('.page-card')
        .filter({ hasText: 'WiFi 预设列表' })
        .locator('.el-table__body-wrapper tbody tr')
      await expect(wifiRows).toHaveCount(api.wifiPresets.length)
      await expect(wifiRows.first()).toContainText(api.wifiPresets[0].ssid)
    }
  })

  test('8.2 保存配置 → 真发 PUT → GET 回读值确实变了 → 还原回改前值', async ({ page }) => {
    const baseline = await readSettings(page)

    // 前置探测：把「库里现值」原样 PUT 一次（值不变，只问后端肯不肯收）。
    // 被拒 ⇒ 说明 staging 现存配置自相矛盾，写进去的值再也还原不回来，此时必须跳过而非硬改环境。
    const token = await getAuthToken(page)
    const probe = await page.request.put(SETTINGS_API, {
      data: baseline,
      headers: { Authorization: `Bearer ${token}` },
    })
    if (probe.status() !== 200) {
      const msg = ((await probe.json().catch(() => null))?.message ?? '') as string
      test.skip(
        true,
        `staging 系统配置存值不满足后端不变量，PUT 被拒（HTTP ${probe.status()}）：${msg}。` +
          `此时任何一次保存都还原不回改前值，故跳过。` +
          `🔴 等 T281（新迁移把 threshold_pressure_low 10 改为 1）修复并部署到 staging 后再跑本用例；` +
          `seed 修好后本条自动恢复真跑，无需改本文件。`,
      )
    }

    // 改前 / 改后值：只动 maxPatients（该键目前无业务消费方，改一天也不影响别的用例）
    const target = baseline.maxPatients + 1

    // 记录真实发出的 PUT，证明「保存」不是 mock 那种本地 resolve
    const puts: string[] = []
    page.on('request', (req) => {
      if (req.method() === 'PUT' && req.url().includes('/api/v1/admin/settings')) puts.push(req.url())
    })

    let saved = false
    try {
      const maxPatientsInput = numberField(page, '最大患者数')
      await maxPatientsInput.click()
      await maxPatientsInput.fill(String(target))
      await maxPatientsInput.press('Enter')
      await expect(maxPatientsInput).toHaveValue(String(target))

      await page.locator('.settings-form').getByRole('button', { name: '保存配置' }).click()
      await expect(adminMessage(page)).toContainText('配置已保存', { timeout: 15_000 })
      saved = true

      expect(puts.length, '保存必须真的发出 PUT 请求').toBeGreaterThanOrEqual(1)

      // 落库回读：绕开前端，直接问接口
      const after = await readSettings(page)
      expect(after.maxPatients, '改后 GET 回读应等于新值').toBe(target)
      // 其余字段不得被这次保存顺手改掉
      expect(after.retentionDays).toBe(baseline.retentionDays)
      expect(after.collectIntervalSeconds).toBe(baseline.collectIntervalSeconds)

      // 还原：走接口写回改前值（比再点一遍 UI 稳）
      const restore = await page.request.put(SETTINGS_API, {
        data: baseline,
        headers: { Authorization: `Bearer ${token}` },
      })
      expect(restore.status()).toBe(200)
      const restored = await readSettings(page)
      expect(restored.maxPatients, '还原后应回到改前值').toBe(baseline.maxPatients)
      saved = false
    } finally {
      // 中途任何断言失败都可能把 staging 留成新值 —— 兜底再还原一次
      if (saved) {
        await page.request
          .put(SETTINGS_API, { data: baseline, headers: { Authorization: `Bearer ${token}` } })
          .catch(() => {})
        const guard = await readSettings(page).catch(() => null)
        if (guard && guard.maxPatients !== baseline.maxPatients) {
          console.error(
            `[T279 08.2] staging 系统配置未还原成功：maxPatients=${guard.maxPatients}，应为 ${baseline.maxPatients}`,
          )
        }
      }
    }
  })
})
