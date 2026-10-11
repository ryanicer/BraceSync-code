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

/** 角色的两枚装饰头像 emoji（稿面 advice.html:45-57：角色由头像+底色承担，无文字标签） */
const ROLE_AVATAR: Record<CareTeamMember['memberType'], string> = {
  doctor: '👨‍⚕️',
  technician: '👨‍🔧',
}

/** T659：团成员头像 emoji 按 memberType 映射（稿面 👩‍⚕️「护士」是多名医生的 seed 示意，
 *  memberType 只有 doctor/technician 两值、无性别维度，医生统一一枚）。 */
export function careTeamAvatar(memberType: CareTeamMember['memberType']): string {
  return ROLE_AVATAR[memberType] ?? ROLE_AVATAR.doctor
}

/**
 * 职称列直接取后端 DTO 的 title：技师恒为固定词「技术支撑」（toCareTeamDTO R6 甲），
 * 医生走 title → department →「医护团队」回落链（adviceDisplayTitle）——空值不留白在
 * 服务端组 DTO 时已保证，前端不做同词折损（T659 前那版把「技术支撑」折成「——」与稿不符）。
 */
export function careTeamTitleLabel(member: CareTeamMember): string {
  return (member?.title ?? '').trim()
}

/** 「2026-10-09T14:20:00Z」→ 本地「2026-10-09」；后端下发 UTC（toAdviceDTO 的 .UTC()），截串会早 8 小时。
 *  T659：稿面 advice.html 时间轴 meta 只到日期（不带时分）。 */
export function adviceDateLabel(iso?: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** 稿面 advice.html:72 行头一行 = 「日期 · 职称」 */
export function adviceMetaLabel(item: Advice): string {
  return `${adviceDateLabel(item?.createdAt)} · ${item?.title ?? ''}`
}

/** 撰写/编辑框的字数计数（与后端同一把 rune 尺，前端不用 .length，那按 UTF-16 码元数） */
export function adviceRuneCount(content?: string | null): number {
  return Array.from(content ?? '').length
}
