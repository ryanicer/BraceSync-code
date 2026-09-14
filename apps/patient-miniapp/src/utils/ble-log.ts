/**
 * BLE 调试日志（T094b / T192）
 *
 * 走 logger 双写：console（开发者工具）+ wx.getRealtimeLogManager()（真机可远程反查）。
 * 此前这里是 `if (!isDev()) return` + 仅 console，导致生产包里整条 BLE 链路零日志。
 * 安全：调用方负责脱敏，勿传密码 / provision_key。
 */

import { logger } from './logger'

type BleLogLevel = 'info' | 'warn' | 'error'

function brief(ctx: unknown): string {
  if (ctx instanceof Error) return ctx.message || ctx.name
  if (typeof ctx === 'string') return ctx
  try {
    return JSON.stringify(ctx)
  } catch {
    return String(ctx)
  }
}

export function bleLog(level: BleLogLevel, msg: string, ctx?: Record<string, unknown>) {
  const text = ctx === undefined ? `[BLE] ${msg}` : `[BLE] ${msg} ${brief(ctx)}`
  if (level === 'error') logger.error(text)
  else if (level === 'warn') logger.warn(text)
  else logger.info(text)
}
