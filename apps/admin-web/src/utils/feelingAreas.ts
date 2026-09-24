// T370：不适部位词表（展示层收口）。
//
// 为什么会有两套词：同一列 `feeling_logs.discomfort_areas` 有两个互不相通的来源 ——
//   写侧只收设计稿 8 区中文原词（PM T188 裁定 Q4「直接存中文原词、不做码值转换」，
//   实现 services/user-service/internal/handler/handler.go 的 feelingAreaLabels，非 8 区一律 400）；
//   seed / 历史行存的是英文码 thoracic / lumbar（staging 现网 2026-09-24 实测：7 行里
//   lumbar 3、thoracic 3、空 2，中文区名 0 条 —— 因为写入口端点还没有任何 App 在调，
//   患者端 T188 是只读版）。
// 修前这里把英文码译成「颈部 / 胸段 / 腰段 / 骨盆」，而这四个词正是 PRD §7A.7（:381）与
// §8.2（:1729）明文作废的旧四区口径；等患者端开始录入，同一列会同时出现「胸段」与「胸椎」
// 指同一个区，医护会按两个区读。所以译名向现行 8 区归并。
//
// 译名取向（不能归并的绝不伪造）：
//   thoracic 译胸椎 —— 8 区里唯一对应，历史行与新行显示同一个词，分叉消失；
//   lumbar 译腰部   —— 8 区分「右侧腰 / 左侧腰」，历史码不带侧别，挑任一侧就是伪造数据；
//   pelvis 译骨盆   —— 8 区分「骶骨 / 右髂嵴 / 左髂嵴」，同理；
//   neck 译颈部     —— 8 区无颈区（有左右肩），只能沿用自描述词。
// 未命中（含空串）原样透出：将来设计稿加区名不需要改代码就能显示，也不会吞掉真实数据。

/** 现行取值集合：设计稿 docs/design/patient/feelings.html:64-72 人体图 8 区，顺序即稿面点选顺序 */
export const FEELING_AREAS = ['右肩', '左肩', '胸椎', '右侧腰', '左侧腰', '骶骨', '右髂嵴', '左髂嵴'] as const

/** 历史英文码到展示词的译名（数据本身不回填，卡面红线；只收口读数） */
export const LEGACY_AREA_LABELS: Readonly<Record<string, string>> = {
  thoracic: '胸椎',
  lumbar: '腰部',
  pelvis: '骨盆',
  neck: '颈部',
}

/** 「不适部位」单元格单个取值的显示文案（全仓唯一位点：orthosis-log 工作台「佩戴感受」Tab 的表格列） */
export function areaLabel(area: string): string {
  return LEGACY_AREA_LABELS[area] ?? area
}
