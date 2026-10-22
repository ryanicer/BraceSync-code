/**
 * T733（方案A·单位链收口）— 技师端校准矩阵的**唯一展示层换算出口**
 *
 * 口径：采集（B513 raw int16）与存储（POST /baselines、baselines 表）全程 **mN 原值、零换算**；
 * 给技师看的数字（N / kPa、判色档位）只能在渲染前这一刻换算一次，且必须走本模块——
 * 本模块是全 app 唯一出现 `nFromMn`（＝÷MN_PER_N）的地方（各页禁止自写 /1000）。
 *
 * 判色 pressureLevel 恒按 N（PRD 裁定 4「仅展示层换算」：换档只换数字怎么写，
 * 不换格子算什么色），故入参 mN、内部先换 N 再比阈值。
 * kPa 腿复用 shared-utils `kpaFromN`（与后端 model.KpaFromN 同公式），不另开口径。
 */
import { nFromMn } from '@bracesync/constants'
import { kpaFromN, type PressureUnit } from '@bracesync/shared-utils'

/** mN → N 数字文本（展示层唯一换算腿）；非有限值出「--」，不退化成 0.00 */
export function mnToNText(mn: number, decimals = 2): string {
  const n = nFromMn(mn)
  return Number.isFinite(n) ? n.toFixed(decimals) : '--'
}

/**
 * 矩阵单元格文本：N 档 2 位小数（本页既有口径），kPa 档整数（同源派生取整）。
 * 入参是 mN 原值——只有这里是 N/kPa 换算，存储与上报都拿 mN。
 */
export function calibCellText(mn: number, unit: PressureUnit, areaCm2: number | null | undefined): string {
  if (unit !== 'kPa') return mnToNText(mn)
  const n = nFromMn(mn)
  return String(kpaFromN(n, areaCm2) ?? '--')
}

/** 统计行文本（带单位字母）：N 档「0.35N」，kPa 档「5kPa」，不可换算出「--」 */
export function calibStatText(mn: number, unit: PressureUnit, areaCm2: number | null | undefined): string {
  const n = nFromMn(mn)
  if (!Number.isFinite(n)) return '--'
  if (unit !== 'kPa') return `${n.toFixed(2)}N`
  const k = kpaFromN(n, areaCm2)
  return k === null ? '--' : `${k}kPa`
}

/**
 * 判色档位（恒按 N 判，阈值不随档位、也永不改）：入参 mN 先换 N 再比。
 * 阈值表与 T733 之前的 N 口径逐字一致（0.5/1/2/5/10/20）。
 */
export function calibPressureLevel(mn: number): number {
  const abs = Math.abs(nFromMn(mn))
  if (abs < 0.5) return 0
  if (abs < 1) return 1
  if (abs < 2) return 2
  if (abs < 5) return 3
  if (abs < 10) return 4
  if (abs < 20) return 5
  return 6
}
