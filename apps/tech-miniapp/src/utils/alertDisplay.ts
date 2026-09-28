/**
 * T433 缺陷二：技师端告警「数值位」的显示判据（纯层，vitest 可直接测）。
 *
 * 改前页面用真值判断决定渲不渲染：
 *   apps/tech-miniapp/src/pages/alerts/index.vue:49 v-if="alert.actualValue"
 *   同文件 :143/:144 alert.thresholdValue ? ... : ''，末尾再 .filter(Boolean)
 * 于是「当日佩戴 0 分钟」这类告警的实际值行整行消失（Alice T428 走查第 10 项实测），
 * 技师只看得到阈值、看不到最该看到的 0。
 *
 * 判据改为「字段在场且是有限数」——与 admin 告警页的 != null 写法、
 * 患者端 utils/anomaly.ts 的 != null 写法同一口径，不再各留一套。
 * 字符串位（传感器、处理备注）仍按空串判，语义本就不同。
 */
import type { Alert } from '@bracesync/shared-types'
import { formatAlertValue, alertTypeLabel, isHiddenAlertType } from '@bracesync/shared-utils'
import { alertAdviceLines } from './alertAdvice'
import { patientDisplayValue } from './patientDisplay'

/** 0 是合法读数；只有 null / undefined / NaN 才算「这个字段没值」 */
export function hasAlertNumber(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

/**
 * T451：技师端告警「界面隐藏、数据不删」的行过滤（PRD §7D.6 历史数据处置拍 C，与 admin 同源）。
 * 判据真源只有 shared-utils 的 isHiddenAlertType 一处，页面与本页模板都不写类型字面量。
 * 只在已加载行上滤 ⇒ 后端返回值、分页契约、库里历史行全不动。
 */
export function filterVisibleAlertRows(rows: Alert[]): Alert[] {
  return rows.filter((row) => !isHiddenAlertType(row.type))
}

/** actualValue 与 thresholdValue 共用同一套判据与格式化 */
export function alertValueText(type: string, value: unknown): string {
  return hasAlertNumber(value) ? formatAlertValue(type, value) : ''
}

/** 详情弹窗正文（uni.showModal 只吃字符串，行构造放这里才可测） */
export function buildAlertDetailLines(a: Alert): string[] {
  const threshold = alertValueText(a.type, a.thresholdValue)
  const actual = alertValueText(a.type, a.actualValue)
  return [
    `类型: ${alertTypeLabel(a.type)}`,
    `患者: ${patientDisplayValue(a.patientName, a.patientId)}`,
    `设备: ${a.deviceId}`,
    a.sensorPoint ? `传感器: ${a.sensorPoint}` : '',
    threshold ? `阈值: ${threshold}` : '',
    actual ? `实际值: ${actual}` : '',
    `详情: ${a.detail}`,
    a.processNote ? `处理备注: ${a.processNote}` : '',
    // 三态映射照搬改前页面原句（'processing' 也落「已处理」），本卡不改行为，缺陷已随 T433 登记
    `状态: ${a.processStatus === 'pending' ? '待处理' : '已处理'}`,
    // 裁定①丙：固定模板，不是后端下发也不是推导；无模板的码值整块不落
    ...alertAdviceLines(a.type),
  ].filter(Boolean)
}
