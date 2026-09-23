// T360：职称下拉词表。
//
// 设计稿 docs/design/admin/医护账号.html:284 写的是「预置 4 项、可扩展」，:306/:308 两处下拉
// （筛选与编辑）都读同一个 titles 数组 —— 即「同源」是稿面结构，不是额外要求。
// 但实现只把「预置 4 项」当词表，而库里既有值不止这 4 个（staging seed D0002 = 副主任医师，
// mock 档案 DOC-002/DOC-005 同样是它）⇒ 筛不到那一行，且编辑态重选只能落到 4 项之一，
// 一次普通保存就把真实职称静默改写。故词表取「预置 ∪ 当前列表出现过的职称」。
//
// 顺序：预置在前（稿面口径），库内新值按首次出现序追加 —— 不排字典序，避免下拉每次刷新换序。
import { MEDICAL_TITLES } from '../mock/medicalAccounts'

export function titleOptions(rows: readonly { title?: string | null }[]): string[] {
  const out = [...MEDICAL_TITLES]
  const seen = new Set(out)
  for (const row of rows) {
    const title = row.title?.trim()
    if (!title || seen.has(title)) continue
    seen.add(title)
    out.push(title)
  }
  return out
}
