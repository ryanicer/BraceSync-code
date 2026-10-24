// T716：受压面积输入口径。字段必填、大于 0、最多两位小数（cm²）。
// 纯函数便于单测；页面与 mock 共用同一判据，避免两处文案漂移。
export type ContactAreaParseResult =
  | { ok: true; value: number }
  | { ok: false; message: string }

export function parseContactArea(raw: string): ContactAreaParseResult {
  const s = raw.trim()
  if (!s) return { ok: false, message: '请输入受压面积' }
  if (!/^-?\d+(\.\d+)?$/.test(s)) return { ok: false, message: '请输入有效的受压面积' }
  const value = Number(s)
  if (!(value > 0)) return { ok: false, message: '受压面积必须大于 0' }
  if (/^-?\d+\.\d{3,}$/.test(s)) return { ok: false, message: '最多两位小数' }
  return { ok: true, value }
}
