/**
 * 患者端矫形日志（feelings）纯层（T505）。
 *
 * 为什么单独抽出来：patient-miniapp 的 vitest 是 `environment: 'node'`（vitest.config.ts），
 * .vue 页面在本包内挂不起来，判据留在 SFC 里就测不到（同 T298 anomaly 页的教训）。
 *
 * 词表与档位码值一律读 @bracesync/shared-utils（FEELING_AREAS / FEELING_LEVELS /
 * feelingLevelLabel / areaLabel / FEELING_NOTES_MAX_LEN），不在本包另立一套 ——
 * 跨语言同源门禁在 apps/admin-web/test/feeling-areas.spec.ts（与后端 handler.go 逐字比对）。
 */
import { areaLabel, feelingLevelLabel } from '@bracesync/shared-utils'
import type { FeelingLog } from '@bracesync/shared-types'

/** 稿面 feelings.html:64 日期后带周几（「2026-07-12 周日」），序按 JS getDay()（0=周日） */
const WEEKDAY_CN = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']

/**
 * 「2026-07-12」→「2026-07-12 周日」。
 * 逐段拆给本地构造函数，不用 `new Date('2026-07-12')`：那串按 UTC 零点解析，
 * 负时区会把周几退一天。非法输入原样返回（不伪造日期）。
 */
export function logDateLabel(logDate: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(logDate ?? '')
  if (!m) return logDate ?? ''
  const day = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])).getDay()
  return `${logDate} ${WEEKDAY_CN[day]}`
}

/** 设备本地日期（YYYY-MM-DD），录入默认值 = 稿面「日期」默认今日 */
export function todayLocalDate(now: Date = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${now.getFullYear()}-${p(now.getMonth() + 1)}-${p(now.getDate())}`
}

/** 不适部位展示文案：历史英文码经 areaLabel 译名后按顿号连接（稿面弹窗 viewZones 一行） */
export function areasText(areas?: string[] | null): string {
  if (!Array.isArray(areas) || areas.length === 0) return ''
  return areas.map(areaLabel).join('、')
}

/** 档位展示词（贴合 / 不适）；历史行 comfort_level 为 NULL 时返回空串，由调用方隐藏标签 */
export function feelingLabel(feeling?: FeelingLog['feeling'] | null): string {
  return feelingLevelLabel(feeling)
}

/** 档位标签配色：稿面 app-log-tag-ok（贴合）/ app-log-tag-warn（不适） */
export function feelingTagClass(feeling?: FeelingLog['feeling'] | null): string {
  return feeling === 'discomfort' ? 'log-tag-warn' : 'log-tag-ok'
}

/**
 * 医生回复时刻 → 本地「YYYY-MM-DD HH:mm」。
 * 后端下发的是 `.UTC()` 后的 RFC3339（结尾 Z，handler.go toFeelingDTO），
 * 直接截字符串会显示成 UTC，比患者实际收到回复的时间早 8 小时。
 */
export function replyTimeLabel(iso?: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

/** 部位 chip 点选切换（稿面 toggleZone）：不重复追加、保持点选顺序 */
export function toggleArea(areas: string[], area: string): string[] {
  return areas.includes(area) ? areas.filter((a) => a !== area) : [...areas, area]
}

export interface FeelingDraft {
  logDate: string
  feeling: string
  areas: string[]
  notes: string
}

/**
 * POST /api/v1/patients/:patientId/feeling-logs 请求体。
 * 后端字段名见 model.CreateFeelingLogRequest（feeling / discomfortAreas / notes / logDate），
 * 同日重复提交按 Q3 裁定覆盖当日行 ⇒ 「编辑」与「新增」共用这一个写通道。
 */
export function buildFeelingPayload(draft: FeelingDraft) {
  return {
    feeling: draft.feeling,
    discomfortAreas: draft.areas,
    notes: draft.notes.trim(),
    logDate: draft.logDate,
  }
}
