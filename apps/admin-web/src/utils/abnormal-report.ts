// 异常报告页（T372）纯展示层：把 GET /admin/abnormal-reports 的四视图换算成 KPI、图表与文字汇总。
// 抽成纯模块的原因：SFC 里的换算 vitest 测不到（同 T298/T322 的教训），且这些口径要逐条钉住。
import { alertTypeLabel } from '@bracesync/shared-utils'
import type { AbnormalReport, AbnormalReportCount } from '../mock/alerts'

export interface ReportRange {
  /** 北京日历日 YYYY-MM-DD，含端点（与后端 reportQuery 同口径） */
  start: string
  end: string
}

const DAY_MS = 86400000

export function dayText(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** 稿面 :310「生成时间 2026-07-21 16:04:12」的秒级时间戳格式 */
export function stampText(d = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${dayText(d)} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/** 稿面快捷范围「近 N 天」= 含今天的 N 个日历日 */
export function lastNDays(n: number, today = new Date()): ReportRange {
  return { start: dayText(new Date(today.getTime() - (n - 1) * DAY_MS)), end: dayText(today) }
}

/** 稿面快捷范围「本月至今」 */
export function monthToDate(today = new Date()): ReportRange {
  return { start: dayText(new Date(today.getFullYear(), today.getMonth(), 1)), end: dayText(today) }
}

function dayIndex(date: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date)
  if (!m) return null
  return Math.floor(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])) / DAY_MS)
}

/** 含端点的日历日跨度；非法区间回 0，调用方按「不计算日均」处理 */
export function rangeDays(range: ReportRange): number {
  const a = dayIndex(range.start)
  const b = dayIndex(range.end)
  if (a === null || b === null || b < a) return 0
  return b - a + 1
}

function countOf(list: AbnormalReportCount[], key: string): number {
  return list.find((x) => x.key === key)?.count ?? 0
}

function round1(v: number): number {
  return Math.round(v * 10) / 10
}

export interface ReportKpi {
  total: number
  /** 稿面「日均 4.1 次」= 总次数 / 区间日历天数（含端点） */
  dailyAvg: number
  /** 稿面 KPI 第 2 卡：压力偏高次数与占比 */
  pressureHigh: number
  pressureHighShare: number
  /** 稿面 KPI 第 3 卡：设备离线次数（wear_interrupt，Boss 2026-09-20 裁定同义） */
  deviceOffline: number
  /** 稿面「未处理条数」：processed 之外的三态都算未处理（后端 processed_by 只在置为 processed 时写） */
  unprocessed: number
  processedRate: number
}

export function kpiFromReport(r: AbnormalReport, range: ReportRange): ReportKpi {
  const days = rangeDays(range)
  const processed = countOf(r.byStatus, 'processed')
  const share = (n: number) => (r.total > 0 ? round1((n / r.total) * 100) : 0)
  return {
    total: r.total,
    dailyAvg: days > 0 ? round1(r.total / days) : 0,
    pressureHigh: countOf(r.byType, 'pressure_high'),
    pressureHighShare: share(countOf(r.byType, 'pressure_high')),
    deviceOffline: countOf(r.byType, 'wear_interrupt'),
    unprocessed: r.byStatus.filter((x) => x.key !== 'processed').reduce((s, x) => s + x.count, 0),
    processedRate: share(processed),
  }
}

/**
 * 文字汇总固定 6 段模板里的 4 段（稿面 `:354`）。
 * ③ 集中点位、④ 佩戴依从性 未产出 —— 现读端点没有采集点维度与时长维度，
 * 拼出来就是编数据（派发单「不伪造」红线），缺口已在卡内登记。
 */
export function summaryLines(r: AbnormalReport, range: ReportRange): string[] {
  const kpi = kpiFromReport(r, range)
  const lines: string[] = []
  lines.push(`① 总体：区间内共产生异常 ${kpi.total} 次，日均 ${kpi.dailyAvg} 次。`)
  const detail = r.byType.map((x) => `${alertTypeLabel(x.key)} ${x.count} 次（${kpi.total > 0 ? round1((x.count / kpi.total) * 100) : 0}%）`).join('、')
  lines.push(`② 构成：${detail || '区间内无异常记录'}`)
  lines.push(`⑤ 处理情况：已处理 ${kpi.total - kpi.unprocessed} 次，未处理 ${kpi.unprocessed} 次；处理率 ${kpi.processedRate}%。`)
  lines.push('⑥ 提示：本报告为区间数据汇总，不构成诊疗结论。')
  return lines
}
