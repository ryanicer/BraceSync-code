/**
 * 矫形日志（feelings）词表单一真源。
 *
 * T505 上收：这套词原先只住在 `apps/admin-web/src/utils/feelingAreas.ts`，而患者端 feelings 页
 * 是第二个消费端（同一列 `feeling_logs.discomfort_areas` 的两端读数），患者包反向依赖 admin-web
 * 不可行 ⇒ 移到共享包。跨语言对账门禁见 `apps/admin-web/test/feeling-areas.spec.ts`
 * （与后端写侧白名单 `feelingAreaLabels` / `feelingLevels` / `feelingNotesMaxLen` 逐字比对）。
 *
 * 为什么会有两套词：同一列有两个互不相通的来源 ——
 *   写侧只收设计稿 8 区中文原词（PM T188 裁定 Q4「直接存中文原词、不做码值转换」，
 *   实现 services/user-service/internal/handler/handler.go 的 feelingAreaLabels，非 8 区一律 400）；
 *   seed / 历史行存的是英文码 thoracic / lumbar（staging 现网 2026-09-24 T370 实测：中文区名 0 条，
 *   discomfort_areas 只有英文码与空数组两类 —— 因为患者端录入页直到 T505 才存在）。
 * 修前 admin 侧把英文码译成「颈部 / 胸段 / 腰段 / 骨盆」，而这四个词正是 PRD §7A.7 与 §8.2
 * 明文作废的旧四区口径；等患者端开始录入，同一列会同时出现「胸段」与「胸椎」指同一个区，
 * 医护会按两个区读。所以译名向现行 8 区归并。
 *
 * 译名取向（不能归并的绝不伪造）：
 *   thoracic 译胸椎 —— 8 区里唯一对应，历史行与新行显示同一个词，分叉消失；
 *   lumbar 译腰部   —— 8 区分「右侧腰 / 左侧腰」，历史码不带侧别，挑任一侧就是伪造数据；
 *   pelvis 译骨盆   —— 8 区分「骶骨 / 右髂嵴 / 左髂嵴」，同理；
 *   neck 译颈部     —— 8 区无颈区（有左右肩），只能沿用自描述词。
 * 未命中（含空串）原样透出：将来设计稿加区名不需要改代码就能显示，也不会吞掉真实数据。
 */

/** 现行取值集合：设计稿 docs/design/patient/feelings.html:104-111 的 8 区，顺序即稿面点选顺序 */
export const FEELING_AREAS = ['右肩', '左肩', '胸椎', '右侧腰', '左侧腰', '骶骨', '右髂嵴', '左髂嵴'] as const

/** 历史英文码到展示词的译名（数据本身不回填，卡面红线；只收口读数） */
export const LEGACY_AREA_LABELS: Readonly<Record<string, string>> = {
  thoracic: '胸椎',
  lumbar: '腰部',
  pelvis: '骨盆',
  neck: '颈部',
}

/** 「不适部位」单个取值的显示文案（admin 矫形日志表格列、患者端日志卡片与录入回显共用） */
export function areaLabel(area: string): string {
  return LEGACY_AREA_LABELS[area] ?? area
}

/**
 * 佩戴感受两档码值（Boss 2026-09-23 裁定【方案 A】，PRD V3.22 收口）。
 * 🔴 三档 good / mild / pain 从未进入代码，星级评分（comfort_score）继续作废、患者端不采集，
 * 后端 feelingLevels 白名单之外的取值一律 400 —— 本常量与它同源，改这里必同时改后端。
 */
export const FEELING_LEVELS = ['fitted', 'discomfort'] as const

export type FeelingLevel = (typeof FEELING_LEVELS)[number]

/** 两档展示词（稿面 feelings.html:97-98 「贴合 / 不适」） */
export const FEELING_LEVEL_LABELS: Readonly<Record<string, string>> = {
  fitted: '贴合',
  discomfort: '不适',
}

/** 档位显示文案；空值（历史行 comfort_level 为 NULL）交回调用方决定占位词 */
export function feelingLevelLabel(level?: string | null): string {
  if (!level) return ''
  return FEELING_LEVEL_LABELS[level] ?? level
}

/** 「详细描述」字数上限，与后端 `const feelingNotesMaxLen`（handler.go）同源 */
export const FEELING_NOTES_MAX_LEN = 200
