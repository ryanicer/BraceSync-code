/**
 * T298 — 患者端「异常监测」告警文本化（口径来源：docs/design/patient/anomaly.html + PRD §7D.6）
 *
 * 从 pages/anomaly/index.vue 抽出为纯函数：三端共用的显示规则要能被 vitest 直接跑到，
 * 不能只在 SFC 里内联（SFC 不在 vitest 的解析范围内）。
 */
import type { Alert } from '@bracesync/shared-types'
import { alertTypeLabel, formatAlertValue } from '@bracesync/shared-utils'

export interface PressureAnomalyItem {
  /** 采集点；按日类告警（设备离线 / 佩戴时长不足）无点位，为空串 */
  point: string
  /** 中文类型名，唯一来源 ALERT_TYPE_LABELS */
  type: string
  level: 'warn' | 'error'
  /** 后端 Alert.detail 原文（自述句，前端不再拼数） */
  detail: string
  /** 阈值文本，无单位口径保障的类型为空串 */
  threshold: string
  /** 由 actualValue + 处理/恢复态推出的补充行 */
  meta: string
}

// engine.go EvaluateWearDurationShort 写入的 thresholdValue/actualValue 是**分钟**
// （need = targetHours*60、actual = wearMinutes），而 shared-utils formatAlertValue 按**小时**标 'h'
// ⇒ 该类型的数值单位口径未定，患者端不显示裸数字（detail 自述句里已带小时数），缺口已随 T298 报 PM。
const HIDDEN_VALUE_TYPES = new Set(['wear_duration_short'])

/** Alert → 压力异常列表行（4 类共用一套文本化规则） */
export function toPressureAnomalyItem(a: Alert): PressureAnomalyItem {
  const label = alertTypeLabel(a.type)
  const showValue = !HIDDEN_VALUE_TYPES.has(a.type)
  const actualTxt = showValue && a.actualValue != null ? formatAlertValue(a.type, a.actualValue) : ''
  const thresholdTxt =
    showValue && a.thresholdValue != null ? formatAlertValue(a.type, a.thresholdValue, { prefix: '>' }) : ''
  // Alert 无 severity 字段（shared-types 用 type + actualValue/thresholdValue + resolvedStatus 表达）
  // level：pressure_high 且实际/阈值达 60N → error，其余 warn（T221 日历圆点口径）
  const level: PressureAnomalyItem['level'] =
    a.type === 'pressure_high' && (a.actualValue >= 60 || a.thresholdValue >= 60) ? 'error' : 'warn'
  const statusTxt = a.processNote?.trim() || (a.resolvedStatus === 'resolved' ? '已恢复' : '关注中')
  return {
    point: a.sensorPoint || '',
    type: label,
    level,
    detail: a.detail?.trim() || label,
    threshold: thresholdTxt,
    meta: actualTxt ? `实际值 ${actualTxt} · ${statusTxt}` : statusTxt,
  }
}

/** 按日期分组（date 键 = timestamp 前 10 位）；设备离线不在本页承载（PRD §7D.6 归技师端现场知晓） */
export function alertsToPressureMap(alerts: Alert[]): Map<string, PressureAnomalyItem[]> {
  const byDate = new Map<string, PressureAnomalyItem[]>()
  for (const a of alerts) {
    if (a.type === 'wear_interrupt') continue
    const date = a.timestamp ? a.timestamp.slice(0, 10) : new Date().toISOString().slice(0, 10)
    if (!byDate.has(date)) byDate.set(date, [])
    byDate.get(date)!.push(toPressureAnomalyItem(a))
  }
  return byDate
}
