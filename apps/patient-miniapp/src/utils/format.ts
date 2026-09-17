/**
 * 患者端数值格式化工具（T206）
 *
 * 用于 monitor 热力图/单元格/hero 等压力值显示，避免长小数溢出。
 * 按数值量级自适应精度：大值整数、中等 1 位小数、小值 2 位小数。
 */

/**
 * 压力值格式化：按量级自适应精度，避免长小数撑破 UI。
 *
 * | 输入范围    | 输出示例 | 说明                     |
 * |-------------|----------|--------------------------|
 * | null/undef  | `'--'`   | 无数据                   |
 * | ≥ 10        | `'75'`   | 整数（旧 mN 当 N 显示）  |
 * | 1 ~ 10      | `'7.5'`  | 1 位小数                 |
 * | 0.1 ~ 1     | `'0.75'` | 2 位小数（N 口径典型值） |
 * | < 0.1       | `'0.08'` | 2 位小数                 |
 *
 * 注：这个函数是显示层格式化，**不改变实际数值**；
 * 阈值比较、颜色判定等仍用原始 value。
 */
export function formatPressureValue(val: number | undefined | null): string {
  if (val === undefined || val === null || isNaN(val)) return '--'
  const abs = Math.abs(val)
  if (abs >= 10) return val.toFixed(0)
  if (abs >= 1) return val.toFixed(1)
  return val.toFixed(2)
}
