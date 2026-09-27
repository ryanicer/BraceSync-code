// T433 缺陷一（格二「取数只发一页」）：翻页收口层的纯层用例。
//
// 为什么单测写在这里而不是页面里：apps/tech-miniapp/vitest.config.ts 是
// environment: 'node'（无 happy-dom / 无 VTU 挂载），SFC 模板在本包测不到 ——
// 这正是这两格此前「本地零覆盖」的直接原因，故判据全部下沉到 utils 层。
import { describe, it, expect, vi } from 'vitest'
import { fetchAllPages, DEFAULT_MAX_PAGES } from '../src/utils/paging'

function envelope<T>(list: T[], total: number) {
  return { list, total }
}

describe('fetchAllPages（T433 技师端列表取满）', () => {
  it('多页：逐页取满并按接口 total 收口，不返回本页条数', async () => {
    const pages = [envelope(['a', 'b'], 5), envelope(['c', 'd'], 5), envelope(['e'], 5)]
    const fetchPage = vi.fn(async (page: number) => pages[page - 1])

    const agg = await fetchAllPages(fetchPage, { pageSize: 2 })

    expect(agg.rows).toEqual(['a', 'b', 'c', 'd', 'e'])
    expect(agg.total).toBe(5)
    expect(agg.truncated).toBe(false)
    expect(fetchPage).toHaveBeenCalledTimes(3)
    expect(fetchPage.mock.calls.map((c) => c[0])).toEqual([1, 2, 3])
  })

  it('现网形态：86 条 / 单页 50 ⇒ 两页取满，页头读到 86 而不是 50', async () => {
    const fetchPage = vi.fn(async (page: number) =>
      page === 1 ? envelope(Array.from({ length: 50 }, (_, i) => i), 86)
        : envelope(Array.from({ length: 36 }, (_, i) => 50 + i), 86))

    const agg = await fetchAllPages(fetchPage, { pageSize: 50 })

    expect(agg.rows).toHaveLength(86)
    expect(agg.total).toBe(86)
    expect(fetchPage).toHaveBeenCalledTimes(2)
  })

  it('末页恰好取满：按 total 出口收口，不多发一页空请求', async () => {
    const fetchPage = vi.fn(async () => envelope(['a', 'b'], 4))

    const agg = await fetchAllPages(fetchPage, { pageSize: 2 })

    expect(agg.rows).toHaveLength(4)
    expect(agg.truncated).toBe(false)
    expect(fetchPage).toHaveBeenCalledTimes(2)
  })

  it('后端未回 total：靠「本页没满」出口收口，不死循环翻页', async () => {
    const fetchPage = vi.fn(async (page: number) =>
      page === 1 ? { list: ['a', 'b'] } : { list: ['c'] })

    const agg = await fetchAllPages(fetchPage, { pageSize: 2 })

    expect(agg.rows).toEqual(['a', 'b', 'c'])
    expect(agg.total).toBe(3)
    expect(fetchPage).toHaveBeenCalledTimes(2)
  })

  it('触顶闸门：truncated 为真且已取行完整回传（不静默丢）', async () => {
    const fetchPage = vi.fn(async () => envelope(['a', 'b'], 999))

    const agg = await fetchAllPages(fetchPage, { pageSize: 2, maxPages: 3 })

    expect(agg.rows).toHaveLength(6)
    expect(agg.total).toBe(999)
    expect(agg.truncated).toBe(true)
    expect(fetchPage).toHaveBeenCalledTimes(3)
  })

  it('缺省闸门生效：不传 maxPages 时用 DEFAULT_MAX_PAGES', async () => {
    const fetchPage = vi.fn(async () => envelope(['a', 'b'], 9999))

    const agg = await fetchAllPages(fetchPage, { pageSize: 2 })

    expect(fetchPage).toHaveBeenCalledTimes(DEFAULT_MAX_PAGES)
    expect(agg.truncated).toBe(true)
  })

  it('单页装得下（31 条 / pageSize 40）：只发一页', async () => {
    const fetchPage = vi.fn(async () => envelope(Array.from({ length: 31 }, (_, i) => i), 31))

    const agg = await fetchAllPages(fetchPage, { pageSize: 40 })

    expect(agg.total).toBe(31)
    expect(fetchPage).toHaveBeenCalledTimes(1)
  })
})
