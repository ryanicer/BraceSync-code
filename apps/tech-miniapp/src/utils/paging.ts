/**
 * T433 缺陷一：技师端列表「取数只发一页」的收口层。
 *
 * 后端分页信封是 { list, total, page, pageSize }（device-service handler/query.go:63-68、
 * alert-service handler/public.go:149-153），total 是谓词集上的真 COUNT(*)
 * （device-service repo/query.go:140、alert-service repo/query.go:157）。
 * 改前两个页面把「本页返回条数」当总数渲染（页头「共 20 条记录」，接口实回 31），
 * 且页面没有任何翻页控件 ⇒ 多出的行既看不到、页头数字还骗人。
 *
 * 这里不新增稿面没有的翻页控件（设计稿 docs/design/tech/records.html 无分页区），
 * 改成按 pageSize 逐页取满后再交回页面，使「共 N 条」与列表内容一致。
 * maxPages 是兜底闸门：staging 实测记录 31 / 告警 86 都远不到顶；
 * 真到顶（后端 total 异常或数据量激增）则返回 truncated，由页面显式标注，不静默截断。
 */
export interface PageEnvelope<T> {
  list?: T[] | null
  total?: number | null
}

export interface Aggregated<T> {
  /** 逐页取满后的全量行 */
  rows: T[]
  /** 接口回的真总数；后端未回 total 时退化为 rows.length */
  total: number
  /** 触到 maxPages 闸门仍未取满 */
  truncated: boolean
}

export const DEFAULT_MAX_PAGES = 20

export async function fetchAllPages<T>(
  fetchPage: (page: number, pageSize: number) => Promise<PageEnvelope<T>>,
  opts: { pageSize: number; maxPages?: number } = { pageSize: 20 },
): Promise<Aggregated<T>> {
  const pageSize = opts.pageSize > 0 ? opts.pageSize : 20
  const maxPages = opts.maxPages ?? DEFAULT_MAX_PAGES
  const rows: T[] = []
  let total = 0
  let page = 1

  for (; page <= maxPages; page++) {
    const res = await fetchPage(page, pageSize)
    const batch = res?.list ?? []
    if (typeof res?.total === 'number' && Number.isFinite(res.total)) total = res.total
    rows.push(...batch)
    // 本页没满 = 到底了；或已取满接口报的总数 —— 两个出口都要，
    // 单靠 total 判会在后端漏回 total 时死循环翻页。
    // total>0 这个前置是实测出来的：mock 与后端都可能压根不回 total，
    // 此时 total 仍是初值 0，「rows.length >= 0」恒真 ⇒ 第一页就退出，
    // 正好把本卡要修的「只发一页」又犯一遍。
    if (batch.length < pageSize) return { rows, total: total || rows.length, truncated: false }
    if (total > 0 && rows.length >= total) return { rows, total, truncated: false }
  }
  return { rows, total, truncated: rows.length < total }
}
