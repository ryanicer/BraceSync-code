/**
 * 患者端「康复建议」页纯层（T641）。
 *
 * 抽出来的原因同 utils/feelings.ts：本包 vitest 是 `environment: 'node'`（vitest.config.ts），
 * .vue 页面挂不起来，判据留在 SFC 里就测不到（同 T298/T443 的形状）。
 *
 * 🔴 隐私约束（设计稿 §八）：本页只出「角色 + 职称 + 时间 + 正文」。团队成员行不带姓名，
 * 服务端组 DTO 时就不 SELECT 姓名列（handler.go toCareTeamDTO），前端这里也不给回落空间。
 */
import type { Advice, CareTeamMember } from '@bracesync/shared-types'

/** 时间轴摘要一行的长度上限（rune，不按字节：正文是中文，按字节会把一行截成半个字） */
export const ADVICE_SUMMARY_MAX_RUNES = 60

/** 角色列（memberType 那一维）的两枚展示词 */
const ROLE_LABEL: Record<CareTeamMember['memberType'], string> = {
  doctor: '医护',
  technician: '技术支撑',
}

/** 稿面 advice.html:56：无职称可展示时画「——」，不留白、更不用姓名兜底 */
export const CARE_TEAM_TITLE_PLACEHOLDER = '——'

export function careTeamRoleLabel(memberType: CareTeamMember['memberType']): string {
  return ROLE_LABEL[memberType] ?? memberType
}

/**
 * 职称列。技师表没有 title 列（000001:53-65），后端按 R6 甲把固定标签「技术支撑」放进
 * title 槽位；而角色列同一行已经写了这个词 ⇒ 这里折成稿面的「——」，
 * 否则患者屏幕上那行长成「技术支撑 · 技术支撑」。
 */
export function careTeamTitleLabel(member: CareTeamMember): string {
  const title = (member?.title ?? '').trim()
  if (title === '' || title === careTeamRoleLabel(member?.memberType)) return CARE_TEAM_TITLE_PLACEHOLDER
  return title
}

/** 「2026-10-09T14:20:00Z」→ 本地「2026-10-09 22:20」；后端下发 UTC（toAdviceDTO 的 .UTC()），截串会早 8 小时 */
export function adviceTimeLabel(iso?: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

/** 稿面 advice.html:70 行头一行 = 「时间 · 职称」 */
export function adviceMetaLabel(item: Advice): string {
  return `${adviceTimeLabel(item?.createdAt)} · ${item?.title ?? ''}`
}

/** 摘要一行的两枚出参：折好的文本 + 需不需要「展开全文」这一档 */
export interface AdviceExcerpt {
  text: string
  expandable: boolean
}

export function adviceExcerpt(content?: string | null, maxRunes = ADVICE_SUMMARY_MAX_RUNES): AdviceExcerpt {
  const raw = content ?? ''
  const runes = Array.from(raw)
  if (runes.length <= maxRunes) return { text: raw, expandable: false }
  return { text: `${runes.slice(0, maxRunes).join('')}…`, expandable: true }
}

/** 撰写/编辑框的字数计数（与后端同一把 rune 尺，前端不用 .length，那按 UTF-16 码元数） */
export function adviceRuneCount(content?: string | null): number {
  return Array.from(content ?? '').length
}
