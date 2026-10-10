/**
 * 热力图双单位（N / kPa）显示层判据（T513）。
 *
 * 真源分工：换算在 data-service（T508 `model.KpaFromN`），本模块**只做文本渲染**，
 * 一律读快照里已经算好的派生值（`pressureHeatmap[].pressureKpa` / `heatmapMaxKpa`），
 * 绝不在前端重算 —— 重算就等于第二条口径线（T203 写死 60/45 滞后的同型坑）。
 *
 * 判档恒为 N：颜色、`isMax`、图例、状态列、趋势纵轴、事件流读数都不接这里的文本，
 * 换档只换「数字怎么写」，不换「格子算什么色」（Boss 裁定 4「仅展示层换算」）。
 */

export type PressureUnit = 'N' | 'kPa'

export const PRESSURE_UNITS: readonly PressureUnit[] = ['N', 'kPa'] as const

/** 默认档 = N：PRD §7A.2.1 四.2「现状读数不变，避免一上线就看到量级突变」 */
export const DEFAULT_PRESSURE_UNIT: PressureUnit = 'N'

/** 面积缺失时的页内提示，逐字取 PRD §7A.2.1 五.3 与两端稿面 */
export const AREA_MISSING_HINT = '未配置面积，暂无法换算'

/** 只有稿面演示用的 'N' / 'kPa' 两个拼写算合法档；其余（含旧记忆里的脏值）回 null */
export function normalizePressureUnit(raw: unknown): PressureUnit | null {
  return raw === 'N' || raw === 'kPa' ? raw : null
}

/**
 * 起版档取序 = 本地记忆档 → 默认 N。
 * 稿面注记里的 `?unit=` 是评审复现钩子、非实现契约（PRD §7A.2.1 四.8①「仅稿面」），
 * 正式实现不读 URL。
 */
export function pickStartUnit(stored: unknown): PressureUnit {
  return normalizePressureUnit(stored) ?? DEFAULT_PRESSURE_UNIT
}

/**
 * kPa 档的数字文本：只渲染后端派生值。
 * null / undefined（面积未配置、≤0、非数，或后端未下发该字段）一律「--」，
 * 不许退化成 0 —— 0 kPa 是「读到零压力」，与「配置缺失」是两回事。
 */
export function kpaNumberText(kpa: number | null | undefined): string {
  return kpa === null || kpa === undefined || !Number.isFinite(kpa) ? '--' : String(kpa)
}

/**
 * 当前档的数字文本。N 档文本由调用方按本页既有口径先格式化（患者端 2 位小数、
 * admin 单元格 1 位小数），本模块不统一两端的小数位 —— 那是 PRD V3.37 待裁四条里的
 * 「两端小数位差异」，不在本卡裁围。
 */
export function unitNumberText(
  unit: PressureUnit,
  numberTextInN: string,
  kpa: number | null | undefined,
): string {
  return unit === 'N' ? numberTextInN : kpaNumberText(kpa)
}

/**
 * 带单位字母的压力文本（admin 稿面在患者摘要、详情行与悬浮 title 里带字母，形如
 * 「4.22 N」/「66 kPa」，且不可换算时只出「--」不再挂单位字母）。
 * 患者端详情行是「--kPa」形态、不带这层规则，由 `heatmapDetailLine` 自己拼。
 */
export function unitValueText(
  unit: PressureUnit,
  numberTextInN: string,
  kpa: number | null | undefined,
): string {
  if (unit === 'N') return numberTextInN
  const num = kpaNumberText(kpa)
  return num === '--' ? '--' : `${num} kPa`
}

/**
 * 「未配置面积」提示行是否出现：kPa 档 且 快照里的换算不可用。
 * 可用性判据取 `heatmapMaxKpa`（后端 `KpaFromN(色阶上界, 面积)` 的结果）——
 * 它与每点 `pressureKpa` 同一次换算、同一份面积，同源故不出现「提示说有、数字说无」。
 */
export function areaHintVisible(unit: PressureUnit, heatmapMaxKpa: number | null | undefined): boolean {
  return unit === 'kPa' && (heatmapMaxKpa === null || heatmapMaxKpa === undefined)
}

/** hero 副文案「正常范围」只在 N 档出现（Boss 2026-09-30 08:58:12 裁定 c） */
export function heroRangeHintVisible(unit: PressureUnit): boolean {
  return unit === 'N'
}

/**
 * hero 副文案「正常范围」的数值段（T601）：从快照下发的两条配置边界派生
 * （pressureLowN / pressureHighN，与告警引擎同一条 sys_configs 链）。
 * 🔴 绝不写死数值 —— 写死会与配置漂移（T601 现网页面写死「二十到六十 N」对配置 1N 到 5N
 * 差一个量级，患者会把正常读数看成异常）。
 * 任一边界缺失（快照未到 / 字段缺席）返回空串：调用方按空串隐藏该行，
 * 不猜值、不回落旧字面量（旧字面量本身就是本卡缺陷）。
 */
export function heroRangeText(
  low: number | null | undefined,
  high: number | null | undefined,
): string {
  if (low === null || low === undefined || !Number.isFinite(low)) return ''
  if (high === null || high === undefined || !Number.isFinite(high)) return ''
  return `${low}-${high}N 正常范围`
}
