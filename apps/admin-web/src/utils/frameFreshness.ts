/**
 * 实时监控帧新鲜度判定（T322「假实时」收口）
 *
 * 判据只用契约里已有的两样东西，不新增字段：
 * - 无帧 ⇔ `pressureRecords` 为空。后端三条无帧路径（未绑定设备 / 有设备零上报 / Redis 回退整帧零点）
 *   都会把 `pressureHeatmap` 换成 seed 兜底（model.go:454 注释「未上报患者走 seed 兜底」），
 *   所以「热力图有没有数」不能当判据，只有 records 长度能当。
 * - 帧龄：取 records 内最大可解析 `timestamp`（缺则退 `uploadTime`）。后端只回最新 1 帧
 *   （api-contracts.ts getPatientRealtime 注释），按最大值取是为了不依赖数组顺序。
 *
 * TTL 2 小时与后端 status 的 online 判定同口径（契约注释「online(≤2h 有上报)」）。
 * 后端不下发 TTL，前端只能写同值常量；要改口径得后端补字段，已在 T322 卡内报备，不擅改契约。
 *
 * 取不到可解析时刻时按「已过期」处理而不是「正常」：宁可保守标陈旧，也不把无凭据的数据当实时。
 */

export const FRAME_TTL_MS = 2 * 60 * 60 * 1000

export type FrameState = 'fresh' | 'expired' | 'none'

/** 快照里跟帧时刻相关的字段（只声明用到的部分，便于对 mock / 真实响应两侧复用） */
export interface FrameLike {
  timestamp?: string
  uploadTime?: string
}

export interface FrameFreshness {
  state: FrameState
  /** 末次帧采集时刻（epoch ms）；无帧或时刻不可解析时为 null */
  collectedAt: number | null
  /** 帧龄；collectedAt 为 null 时无意义（0） */
  ageMs: number
}

export const FRAME_TAG_TEXT: Record<FrameState, string> = {
  fresh: '实时同步中',
  expired: '数据已过期',
  none: '无实时数据',
}

function parseTime(value: string | undefined): number | null {
  if (!value) return null
  const t = new Date(value).getTime()
  return Number.isNaN(t) ? null : t
}

/** 帧内最新可用时刻：优先采集时刻 timestamp，缺失再退回上行时刻 uploadTime */
export function latestFrameTime(records: FrameLike[] | null | undefined): number | null {
  let best: number | null = null
  for (const r of records ?? []) {
    const t = parseTime(r?.timestamp) ?? parseTime(r?.uploadTime)
    if (t !== null && (best === null || t > best)) best = t
  }
  return best
}

export function frameFreshness(
  records: FrameLike[] | null | undefined,
  now: number = Date.now(),
): FrameFreshness {
  if (!records || records.length === 0) return { state: 'none', collectedAt: null, ageMs: 0 }
  const at = latestFrameTime(records)
  if (at === null) return { state: 'expired', collectedAt: null, ageMs: 0 }
  // 设备/服务端时钟超前：帧龄按 0 计，不做「未来数据」展示，也不因此判过期
  const ageMs = Math.max(0, now - at)
  return { state: ageMs > FRAME_TTL_MS ? 'expired' : 'fresh', collectedAt: at, ageMs }
}

const pad = (n: number) => String(n).padStart(2, '0')

/** 本地时刻 HH:MM:SS；无时刻时返回空串，由调用方显示占位符 */
export function formatClock(ms: number | null): string {
  if (ms === null) return ''
  const d = new Date(ms)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/** 帧龄中文描述：秒 → 分 → 小时，逐级取整，避免「距今 44700s」这种要心算的读数 */
export function formatFrameAge(ageMs: number): string {
  const s = Math.round(ageMs / 1000)
  if (s < 60) return `距今 ${s}s`
  if (s < 3600) return `距今 ${Math.floor(s / 60)} 分`
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return m > 0 ? `距今 ${h} 小时 ${m} 分` : `距今 ${h} 小时`
}
