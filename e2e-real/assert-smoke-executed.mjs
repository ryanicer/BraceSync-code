// T549 防假绿断言：real-backend-smoke job 的「用例真的执行了」腿。
//
// 读 playwright json reporter 落盘的报告 stats（config 里 outputFile 取 configDir 相对路径，
// 与 playwright.real.config.ts 的 test-results/result.json 同一姿势），要求：
//   · skipped === 0    —— 「1 skipped」的旧恒绿形状（缺凭证静默 skip）直接翻红
//   · passed+flaky >= 1 —— job success 但 0 执行（如 testMatch 打偏）也翻红
// json reporter 只在整跑结束后落盘，所以本脚本只在「playwright 报 success」之后被调用；
// 文件不存在即报错判红，而不是静默放行。任何 stats 键缺失都抛 —— 键形变了要人来对表，
// 不要让 undefined 穿成 undefined===0 的假红或假绿。
import { existsSync, readFileSync } from 'node:fs'

// outputFile 的相对路径解析口径（configDir vs cwd）在文档里没写死，两把都试并打印
// 真正读到的那一条 —— 路径没对准的尺子比没有尺子更坏（它会拿旧一轮的报告报绿）。
const candidates = [
  new URL('./test-results/smoke-result.json', import.meta.url),
  new URL('../test-results/smoke-result.json', import.meta.url),
]
const picked = candidates.find((u) => existsSync(u))
if (!picked) {
  console.error(`[T549] 两把候选路径都没有报告：${candidates.map(String).join(' , ')}`)
  console.error('[T549] 缺报告按未执行处理，判红。')
  process.exit(1)
}
let report
try {
  report = JSON.parse(readFileSync(picked, 'utf8'))
} catch (err) {
  console.error(`[T549] 读不到 json 报告（${err.message}）。缺报告按未执行处理，判红。`)
  process.exit(1)
}
console.log(`[T549] 报告取自：${picked.href}`)
const stats = report?.stats
for (const key of ['expected', 'skipped', 'unexpected', 'flaky']) {
  if (typeof stats?.[key] !== 'number') {
    console.error(`[T549] json 报告缺 stats.${key} —— 键形与预期不符，判红（别拿 undefined 当 0）。`)
    process.exit(1)
  }
}
const line = `[T549] smoke-result stats: passed(expected)=${stats.expected} skipped=${stats.skipped} unexpected=${stats.unexpected} flaky=${stats.flaky}`
const executed = stats.expected + stats.flaky
if (stats.skipped > 0 || executed < 1) {
  console.error(line)
  console.error(`[T549] 断言失败：真实执行数 ${executed}、skip ${stats.skipped} —— 冒烟没有真的打过 staging，按 T549 口径报红而不是 success。`)
  process.exit(1)
}
console.log(`${line} => 断言通过（真实执行 ${executed} 条、0 skip）`)
