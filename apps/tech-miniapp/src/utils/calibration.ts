/**
 * 空载校准语义（T675，Boss 2026-10-12 拍板「语义重构」）
 * ------------------------------------------------------------------
 * 旧语义（已废除）：范围门禁 rangePass —— 各通道 |偏移| ≤ 0.05N 才算通过。
 *   真机治具应力下空载 P01 约 1.5N 也属正常稳定偏置，被门禁判死后 UI 卡
 *   「在采集中」静默循环，是 T675 的缺陷根因。
 * 新语义（本模块）：
 *   1. 校准的目的 = 把任何稳定偏置记为偏移量扣掉，「偏移大」不是失败；
 *   2. 只保留点数完整性校验：5 帧 × 20 点全部收齐（且均为有限数）才算汇总；
 *   3. 故障哨兵只提示、不拦截：通道整轮恒 0（死通道）或读数饱和/明显超量程
 *      （>10N），界面出警示条「通道 X 读数异常，请检查传感器」，但允许继续安装。
 * 失败要说话：超时/点数不完整由页面侧落到失败面板，禁止静默重试。
 */

/** 校准需要的帧数（B513 以 1Hz 推送，取 5 帧） */
export const CALIBRATION_FRAME_COUNT = 5
/** 每帧点数（20 个传感器，P01..P20） */
export const CALIBRATION_POINT_COUNT = 20
/**
 * B513 订阅后帧到达超时（ms）：
 * 订阅成功后 10 秒零帧到达即判超时；中途断流（已收部分帧后 10 秒无新帧）同判失败。
 * 禁止 start/stop 静默循环——超时必须给技师可见原因与重试出口。
 */
export const CALIBRATION_FRAME_TIMEOUT_MS = 10_000
/**
 * 故障哨兵饱和阈值（N，只提示不拦截）：任一帧某通道 |读数| 超过该值视为
 * 读数饱和/明显超量程。10N 为 Boss 例示值（固件量程 60N 档），非通过/失败门禁，
 * 仅驱动警示条。
 */
export const CALIBRATION_SATURATED_N = 10

/** 故障哨兵原因：dead=整轮恒 0；saturated=饱和/超量程 */
export type SentinelReason = 'dead' | 'saturated'

export interface CalibrationSentinel {
  /** 通道号（1-based，P01..P20） */
  channel: number
  reason: SentinelReason
}

/**
 * 单帧点数完整性：必须恰好 20 个有限数。
 * NaN/Infinity、缺点/多点、非数组一律不收（缺点不静默补 0）。
 */
export function isValidCalibrationFrame(frame: unknown): frame is number[] {
  return (
    Array.isArray(frame) &&
    frame.length === CALIBRATION_POINT_COUNT &&
    frame.every((v) => typeof v === 'number' && Number.isFinite(v))
  )
}

/** 校准点数完整性：5 帧 × 每帧 20 点收齐才能算汇总 */
export function isCalibrationComplete(frames: readonly number[][]): boolean {
  return frames.length === CALIBRATION_FRAME_COUNT && frames.every((f) => isValidCalibrationFrame(f))
}

/**
 * 各通道零点偏移 = 该通道跨帧算术平均（N）。
 * 任何稳定偏置都如实记录、不设偏移量门禁；不完整时响亮失败（不许拿残缺帧算假偏移）。
 */
export function computeCalibrationOffsets(frames: readonly number[][]): number[] {
  if (!isCalibrationComplete(frames)) {
    throw new Error(
      `校准帧不完整：需 ${CALIBRATION_FRAME_COUNT} 帧 × ${CALIBRATION_POINT_COUNT} 点，实收 ${frames.length} 帧`,
    )
  }
  return Array.from({ length: CALIBRATION_POINT_COUNT }, (_, p) => {
    let sum = 0
    for (const f of frames) sum += f[p]
    return sum / frames.length
  })
}

/**
 * 故障哨兵检测（提示但放行）：
 * - dead：该通道整轮每帧恒为 0；
 * - saturated：该通道任一帧 |读数| > saturatedN。
 * 恒 0 与超量程互斥（0 不超量程），按事实归一档。
 */
export function detectCalibrationSentinels(
  frames: readonly number[][],
  saturatedN: number = CALIBRATION_SATURATED_N,
): CalibrationSentinel[] {
  const out: CalibrationSentinel[] = []
  for (let p = 0; p < CALIBRATION_POINT_COUNT; p++) {
    const values = frames.map((f) => f[p])
    if (values.length > 0 && values.every((v) => v === 0)) {
      out.push({ channel: p + 1, reason: 'dead' })
    } else if (values.some((v) => Math.abs(v) > saturatedN)) {
      out.push({ channel: p + 1, reason: 'saturated' })
    }
  }
  return out
}

export interface CalibrationOffsetSummary {
  /** 最大绝对偏移（N） */
  maxAbs: number
  /** 最大绝对偏移所在通道（1-based） */
  maxChannel: number
  /** 20 通道绝对偏移均值（N） */
  meanAbs: number
}

/** 偏移摘要（成功要说话：给技师一句可核对的摘要，配合 20 格矩阵） */
export function summarizeCalibrationOffsets(offsets: readonly number[]): CalibrationOffsetSummary {
  if (offsets.length !== CALIBRATION_POINT_COUNT) {
    throw new Error(`偏移数组应为 ${CALIBRATION_POINT_COUNT} 点，实收 ${offsets.length}`)
  }
  let maxAbs = 0
  let maxChannel = 1
  let sumAbs = 0
  offsets.forEach((v, i) => {
    const a = Math.abs(v)
    sumAbs += a
    if (a > maxAbs) {
      maxAbs = a
      maxChannel = i + 1
    }
  })
  return { maxAbs, maxChannel, meanAbs: sumAbs / offsets.length }
}

/**
 * 哨兵 → 警示条文案（逐字口径 = 卡面：「通道 X 读数异常，请检查传感器」）。
 * 多通道：「通道 01、05 读数异常，请检查传感器」；无哨兵返空串（调用方隐藏警示条）。
 */
export function calibrationSentinelText(sentinels: readonly CalibrationSentinel[]): string {
  if (!sentinels.length) return ''
  const channels = sentinels.map((s) => String(s.channel).padStart(2, '0')).join('、')
  return `通道 ${channels} 读数异常，请检查传感器`
}
