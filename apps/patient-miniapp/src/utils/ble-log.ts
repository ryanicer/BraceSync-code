/**
 * BLE 调试日志（T094b）
 * 仅在开发/调试时输出，生产环境静默。
 * 不引入 console 之外的依赖。
 */

type BleLogLevel = 'info' | 'warn' | 'error'

function isDev(): boolean {
  return process.env.NODE_ENV !== 'production'
}

function format(msg: string, ctx?: Record<string, unknown>): string {
  const ts = new Date().toISOString().slice(11, 19)
  const ctxStr = ctx ? ' ' + JSON.stringify(ctx) : ''
  return `[BLE ${ts}] ${msg}${ctxStr}`
}

export function bleLog(level: BleLogLevel, msg: string, ctx?: Record<string, unknown>) {
  if (!isDev()) return
  const text = format(msg, ctx)
  if (level === 'error') console.error(text)
  else if (level === 'warn') console.warn(text)
  else console.log(text)
}
