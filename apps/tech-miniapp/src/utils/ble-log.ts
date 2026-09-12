/**
 * BLE 全链路实时日志封装（T089-R5）
 *
 * 策略：
 *  - MP-WEIXIN：优先使用 wx.getRealtimeLogManager()（小程序后台实时日志）
 *  - H5 / 其他：降级为 console 输出
 *  - 所有调用 try-catch，日志失败绝不抛错、不影响主流程
 *  - console 双写（便于本地调试 + 后台留痕）
 *
 * 安全：本模块不感知业务数据，不记 WiFi 密码 / provision_key，
 * 由调用方负责脱敏后再传入。
 */

// #ifdef MP-WEIXIN
const realtimeLogger = wx.getRealtimeLogManager()
// #endif
// #ifndef MP-WEIXIN
const realtimeLogger: any = null
// #endif

const PREFIX = '[BLE]'

function safe(fn: () => void) {
  try {
    fn()
  } catch (e) {
    // 日志失败静默吞掉，绝不影响主流程
  }
}

export const bleLog = {
  info(msg: string, data?: any) {
    safe(() => {
      console.log(`${PREFIX} ${msg}`, data ?? '')
      realtimeLogger?.info(`${PREFIX} ${msg}`, data ?? '')
    })
  },
  warn(msg: string, data?: any) {
    safe(() => {
      console.warn(`${PREFIX} ${msg}`, data ?? '')
      realtimeLogger?.warn(`${PREFIX} ${msg}`, data ?? '')
    })
  },
  error(msg: string, data?: any) {
    safe(() => {
      console.error(`${PREFIX} ${msg}`, data ?? '')
      realtimeLogger?.error(`${PREFIX} ${msg}`, data ?? '')
    })
  },
}