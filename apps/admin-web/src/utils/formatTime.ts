// T597：RFC3339 时间串 → 东八区显示文本（六处页面共用的唯一出口）。
//
// 缺陷背景：六处页面直接对外部 ISO 串做字符串切片（slice(11, 16) 取时分），
// 永远显示 UTC 时分，比北京时间早 8 小时；北京 00:00-07:59 的记录日期还会
// 切成前一天。同应用内 monitor / frameFreshness / workbenchData 三处本就按
// 东八区渲染 —— 实现不统一，本文件收口。
//
// 口径与 utils/workbenchData.ts 的 cstDate 同族：Intl.DateTimeFormat 显式
// timeZone，不依赖运行环境时区（浏览器时区配错也照出北京时间）。
// 🔴 hourCycle 必须 h23：Intl 的 h24 会让 UTC 16:00 后的北京 00:00 显示成 24:00。

const CST_FMT = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hourCycle: 'h23',
})

/** 东八区各部件；输入非法（非串/解析失败）返回 null */
function cstParts(iso: string | null | undefined): Record<'year' | 'month' | 'day' | 'hour' | 'minute', string> | null {
  if (typeof iso !== 'string' || iso.length === 0) return null
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  const wanted = ['year', 'month', 'day', 'hour', 'minute'] as const
  const out = {} as Record<(typeof wanted)[number], string>
  for (const p of CST_FMT.formatToParts(d)) {
    if ((wanted as readonly string[]).includes(p.type)) {
      out[p.type as (typeof wanted)[number]] = p.value
    }
  }
  if (!out.year || !out.month || !out.day || !out.hour || !out.minute) return null
  return out
}

/** YYYY-MM-DD HH:mm（东八区）；非法输入回 '-'（对齐各页原守卫的展示占位） */
export function formatCstDateTime(iso: string | null | undefined): string {
  const p = cstParts(iso)
  if (!p) return '-'
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}`
}

/** MM-DD HH:mm（东八区，稿面不带年份的列表列）；非法输入回 '-' */
export function formatCstMonthDayTime(iso: string | null | undefined): string {
  const p = cstParts(iso)
  if (!p) return '-'
  return `${p.month}-${p.day} ${p.hour}:${p.minute}`
}
