/**
 * 实时日志封装（T208 + 通用）
 *  - MP-WEIXIN：使用 wx.getRealtimeLogManager()（真机可通过 backend --app tech 查）
 *  - H5 / 其他：降级为 console 输出
 *  - 所有调用 try-catch，日志失败绝不抛错
 */

// #ifdef MP-WEIXIN
const realtimeLogger = typeof wx !== 'undefined' && wx.getRealtimeLogManager
  ? wx.getRealtimeLogManager()
  : null
// #endif
// #ifndef MP-WEIXIN
const realtimeLogger: any = null
// #endif

function safe(fn: () => void) {
  try { fn() } catch { /* 静默吞掉，不影响主流程 */ }
}

/** 手机号脱敏：139****0001 */
export function maskPhone(phone: string): string {
  if (!phone) return ''
  if (phone.length <= 7) return phone
  return phone.slice(0, 3) + '****' + phone.slice(-4)
}

export const logger = {
  info(...args: any[]) {
    const msg = args.join(' ')
    safe(() => {
      console.log(msg)
      realtimeLogger?.info(msg)
    })
  },
  warn(...args: any[]) {
    const msg = args.join(' ')
    safe(() => {
      console.warn(msg)
      realtimeLogger?.warn(msg)
    })
  },
  error(...args: any[]) {
    const msg = args.join(' ')
    safe(() => {
      console.error(msg)
      realtimeLogger?.error(msg)
    })
  },
}
