/**
 * T677：日志文本序列化（纯层，vitest 可直接测）。
 *
 * logger.ts 的 realtimeLogger 用 #ifdef 条件编译声明了两次，无法在 vitest 直接导入，
 * 故把「参数 → 文本」的序列化逻辑独立到此，供 logger 复用且可单测。
 *
 * 改前 logger.info/warn/error 用 `args.join(' ')` 拼装，对象被隐式 String()
 * 成 `[object Object]`，真机实时日志里 errMsg/url 等关键信息全丢。
 */

/** 单参数序列化：对象 → JSON 字符串；Error → stack；其余按原语义 */
export function toLogText(a: unknown): string {
  if (typeof a === 'string') return a
  if (a instanceof Error) return a.stack || `${a.name}: ${a.message}`
  if (typeof a === 'undefined') return 'undefined'
  try {
    const s = JSON.stringify(a)
    return s === undefined ? String(a) : s
  } catch {
    // 循环引用等序列化失败场景，退化为 String()
    return String(a)
  }
}

/** 拼装日志文本：所有参数先序列化，杜绝 [object Object] */
export function formatLogArgs(args: unknown[]): string {
  return args.map(toLogText).join(' ')
}
