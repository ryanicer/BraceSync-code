/**
 * 扫码结果归一（T362）
 *
 * 抽成纯层的原因：页面 SFC 里的 `uni.*` 调用在 vitest 下不可导入（本项目既有约定），
 * 把「成功 / 取消 / 无内容 / 失败」四类判定从页面里剥出来，才能被单测逐个钉住。
 *
 * 🔴 关键约束：真机（微信小程序）才有相机；`@dcloudio/uni-h5` 里 `scanCode` 是
 * `createUnsupportedAsyncApi`（dist/uni-h5.es.js:24516），在 H5 下必然 reject。
 * 该链路必须落到 `failed`，**不允许**回落到"已填入"的假成功 —— 这正是本卡要删掉的
 * mock 硬编码行为的形状。
 */

export interface ScanResponseLike {
  result?: string
}

export type ScanImpl = (opts: { scanType: string[] }) => Promise<ScanResponseLike>

export type ScanOutcome =
  | { kind: 'ok'; value: string }
  | { kind: 'cancelled' }
  | { kind: 'empty' }
  | { kind: 'failed'; message: string }

/** uni 失败回调给的是 `{ errMsg: 'scanCode:fail cancel' }` 这类对象，不是 Error */
function errMsgOf(err: unknown): string {
  if (err && typeof err === 'object' && 'errMsg' in err) {
    const raw = (err as { errMsg?: unknown }).errMsg
    if (typeof raw === 'string' && raw) return raw
  }
  if (err instanceof Error) return err.message
  return typeof err === 'string' ? err : ''
}

/**
 * 调一次二维码扫描并归一结果。`scan` 由调用方注入（页面传 `uni.scanCode`），
 * 使四种链路都能在单测里确定性地覆盖。
 */
export async function readQrCode(scan: ScanImpl): Promise<ScanOutcome> {
  let res: ScanResponseLike | undefined
  try {
    res = await scan({ scanType: ['qrCode'] })
  } catch (err) {
    const msg = errMsgOf(err)
    // 用户主动取消不是失败：提示与日志都要分开，否则真机上看不出是设备问题还是人为中止
    if (msg.includes('cancel')) return { kind: 'cancelled' }
    return { kind: 'failed', message: msg }
  }
  const value = String(res?.result ?? '').trim()
  if (!value) return { kind: 'empty' }
  return { kind: 'ok', value }
}
