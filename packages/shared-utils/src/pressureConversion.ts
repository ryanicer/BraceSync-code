/**
 * T662（T643 二期）：N ↔ kPa 数值换算（编辑输入 / 配置表单专用）。
 *
 * 🔴 使用约束（与「同源派生」口径对齐，避免造出第二条口径线）：
 *   1. 仅用于「前端必须自算」的场景：告警阈值输入框、系统配置阈值表单、
 *      矫形日志虚线 kPa 位、技师端校准矩阵 —— 这些位置后端不下发 kPa 派生值，
 *      只能前端按同一公式换算。
 *   2. 展示层（热力图、趋势曲线、详情行、状态标签）一律读后端在同响应里下发的
 *      派生值（pressureKpa / heatmapMaxKpa / avgPressureKpa 等），不得调用本模块，
 *      不得用本模块结果做展示渲染。否则就是 T203 同型坑（前端一条口径、后端一条
 *      口径，后端改了前端不会跟着错）。
 *   3. 公式与后端 data-service `model.KpaFromN` 逐字对齐：
 *        kPa = round( max(0, N) / area_cm2 * 10 )
 *      反向 nFromKpa = kPa / 10 * area_cm2（不归零、不取整，编辑态保留精度）。
 *   4. 面积缺失 / ≤ 0 / 非有限值 → 返回 null（fail-closed，与展示层同语义：
 *      不可换算就显示 --，不许退化成 0 或 N 原值）。
 *   5. 默认面积 0.64 cm²（PRD §7A.2.1 二.5，Boss 2026-09-28 裁定）是**后台配置项**
 *      的默认值。调用方若拿不到设备真实面积，可用此兜底，但必须明确登记这是
 *      「默认面积兜底」，且当真实面积可用时优先用真实面积。
 */

/** 后台配置项默认值（PRD §7A.2.1 二.5）：0.64 cm² */
export const DEFAULT_SENSOR_AREA_CM2 = 0.64

/**
 * N → kPa 换算，与后端 `model.KpaFromN` 同公式（归零 → 除面积乘 10 → 四舍五入取整）。
 *
 * 用于编辑输入/配置表单等前端必须自算的场景。展示层禁用，见文件顶部约束。
 *
 * @param n N 值
 * @param areaCm2 有效面积 cm²；null / ≤0 / 非有限值 → 返回 null
 * @returns 整数 kPa；不可换算返回 null
 */
export function kpaFromN(n: number, areaCm2: number | null | undefined): number | null {
  if (areaCm2 === null || areaCm2 === undefined) return null
  if (!Number.isFinite(areaCm2) || !(areaCm2 > 0)) return null
  if (!Number.isFinite(n)) return null
  const zeroed = n < 0 ? 0 : n
  const k = (zeroed / areaCm2) * 10
  if (!Number.isFinite(k)) return null
  return Math.round(k)
}

/**
 * kPa → N 换算（反向）。不归零、不取整，编辑态保留精度。
 *
 * 用于：当用户在 kPa 档输入一个值，要转回 N 真值存库/发请求。
 *
 * @param kPa kPa 值
 * @param areaCm2 有效面积 cm²；null / ≤0 / 非有限值 → 返回 null
 * @returns N 值（保留原精度）；不可换算返回 null
 */
export function nFromKpa(kPa: number, areaCm2: number | null | undefined): number | null {
  if (areaCm2 === null || areaCm2 === undefined) return null
  if (!Number.isFinite(areaCm2) || !(areaCm2 > 0)) return null
  if (!Number.isFinite(kPa)) return null
  if (kPa < 0) return null // kPa 物理上非负，负值当不可换算
  const n = (kPa / 10) * areaCm2
  if (!Number.isFinite(n)) return null
  return n
}

/**
 * 编辑场景用的「当前档位下的数值与单位文本」。
 *
 * 展示层不要用这个 —— 展示层用 `unitValueText`（读后端派生值）。
 * 这个是给输入框旁边的单位标签、表单 label 用的。
 */
export function editingValueText(
  unit: 'N' | 'kPa',
  valueInN: number,
  areaCm2: number | null | undefined,
  decimalsInN = 1,
): string {
  if (unit === 'N') {
    return `${valueInN.toFixed(decimalsInN)} N`
  }
  const k = kpaFromN(valueInN, areaCm2)
  return k === null ? '--' : `${k} kPa`
}
