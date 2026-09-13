/**
 * 患者端通用实时日志封装（T177）
 *
 * 策略：
 *  - MP-WEIXIN：优先使用 wx.getRealtimeLogManager()（小程序后台实时日志）
 *  - H5 / 其他：降级为 console 输出
 *  - 所有调用 try-catch，日志失败绝不抛错、不影响主流程
 *  - console 双写（便于本地调试 + 后台留痕）
 *
 * 接口对齐技师端 ble-log.ts，便于 T179 抽公共。
 * 安全：调用方负责脱敏（勿传密码 / token / 敏感数据）。
 */

// #ifdef MP-WEIXIN
const realtimeLogger = wx.getRealtimeLogManager()
// #endif
// #ifndef MP-WEIXIN
const realtimeLogger: any = null
// #endif

function safe(fn: () => void) {
  try {
    fn()
  } catch {
    // 静默吞掉，绝不影响主流程
  }
}

export const logger = {
  info(msg: string, data?: any) {
    safe(() => {
      console.log(msg, data ?? '')
      realtimeLogger?.info(msg, data ?? '')
    })
  },
  warn(msg: string, data?: any) {
    safe(() => {
      console.warn(msg, data ?? '')
      realtimeLogger?.warn(msg, data ?? '')
    })
  },
  error(msg: string, data?: any) {
    safe(() => {
      console.error(msg, data ?? '')
      realtimeLogger?.error(msg, data ?? '')
    })
  },
}
