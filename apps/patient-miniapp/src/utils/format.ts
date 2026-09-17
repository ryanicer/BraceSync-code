/**
 * 患者端数值格式化工具（T206）
 *
 * 用于 monitor 热力图/单元格/hero 等压力值显示，避免长小数溢出。
 * 统一输出 2 位小数，所有值 ≤ 4 字符，不会撑破 100rpx × 100rpx 单元格。
 *
 * 注：这个函数是显示层格式化，**不改变实际数值**；
 * 阈值比较、颜色判定等仍用原始 value。
 * T233：显示层对负值归零（物理上压力不可为负），
 *   val < 0 时显示 0.00；颜色/趋势图/后端逻辑均不受影响。
 */
export function formatPressureValue(val: number | undefined | null): string {
  if (val === undefined || val === null || isNaN(val)) return '--'
  return Math.max(0, val).toFixed(2)
}
