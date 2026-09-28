/**
 * 患者端实时监测页的展示句构造（T444）。
 *
 * 依据：docs/tasks/peter/T417-对照清单-患者端.md 的 M-1／M-2；稿面
 * `docs/design/patient/monitor.html:102`（未点选前的详情行默认句）、
 * `:108`＋`:201-206`＋`:221`（趋势区标题「{点位} · {档}压力趋势」）。
 *
 * 抽成纯函数的理由同 T443：patient-miniapp 的 vitest 是 `environment: 'node'`
 * 且不挂 VTU，模板里的三元拼接在本包测不到，判据只能落在这里。
 */
import { formatPressureValue } from './format'

/** M-1：稿面 monitor.html:102 的默认句，逐字取用，不在实现里另拟 */
export const HEATMAP_DETAIL_PLACEHOLDER = '点击网格查看详情'

/**
 * M-1：详情行文本。用户点选前（或无可解析点位时）出稿面默认句，
 * 点选后才是「点位 · 压力值N · 阈值上限 N」。
 */
export function heatmapDetailLine(
  pointId: string | undefined | null,
  pressureValue: number | undefined | null,
  elevatedMax: number,
): string {
  if (!pointId) return HEATMAP_DETAIL_PLACEHOLDER
  return `${pointId} · ${formatPressureValue(pressureValue)}N · 阈值上限 ${elevatedMax}N`
}

/**
 * M-2：趋势区标题。稿面恒带点位前缀，但 `activePoint` 为空时（首帧未落、
 * 加载失败、无数据）实现会渲染出前导分隔符残留「 · 今日压力趋势」；
 * 无点位时丢掉「点位 ·」这一段，只留稿面后半句。
 */
export function trendSectionTitle(pointId: string | undefined | null, segLabel: string): string {
  return pointId ? `${pointId} · ${segLabel}压力趋势` : `${segLabel}压力趋势`
}
