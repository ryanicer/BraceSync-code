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

/**
 * T235：按告警 type 格式化 actualValue / thresholdValue 显示口径
 *
 * Alert.actualValue 是多义字段，含义随 type 变：
 *   pressure_high        → 压力（N）
 *   pressure_fluctuation → 百分比（后端 engine.go 明确用 %%）
 *   sensor_drift         → 空载读数（N，可能负，复用 formatPressureValue 归零）
 *   wear_interrupt       → 分钟数（本页已过滤，但函数兜底）
 *   未知 type            → 原样数字，不硬编码单位
 *
 * @param type Alert.type
 * @param value actualValue 或 thresholdValue
 * @param opts.prefix 给 thresholdValue 传 '>'，actualValue 不传
 */
export function formatAlertValue(
  type: string,
  value: number,
  opts?: { prefix?: string },
): string {
  const { prefix = '' } = opts ?? {}
  switch (type) {
    case 'pressure_high':
    case 'sensor_drift':
      return `${prefix}${formatPressureValue(value)}N`
    case 'pressure_fluctuation':
      return `${prefix}${value.toFixed(1)}%`
    case 'wear_interrupt':
      return `${prefix}${Math.max(0, Math.round(value))}min`
    default:
      return `${prefix}${value}`
  }
}
