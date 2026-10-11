/**
 * T614 · 链 B / 链 C 的「H5 源」解析（两条链共用一份，不在用例里各写一遍）
 *
 * 为什么要有这一份：e2e-real 的 admin 腿跟着入口变量 E2E_STAGING_URL 走，而技师端/患者端
 * 两条 H5 腿此前各自在文件里写死 `process.env.E2E_xxx_H5_URL ?? 'http://hbksd.com.cn:81'`。
 * 于是「把一轮回归指向另一套环境」只改 E2E_STAGING_URL 是不够的 —— H5 腿会静默留在 staging。
 * 2026-10-07 真打 TST 的复跑原文就是这么红的：链 C 在 TST 建完患者后，把「设登录口令」那一发
 * （拼的就是该链的 H5 源）发到了 http://hbksd.com.cn:81，回 401 —— 红的是环境错位，不是服务故障。
 *
 * 三条判据，一条都不放宽：
 *   1) 形状：仍要求 http + 显式端口（沿用两条链原话，摘掉显式端口会判红）。
 *   2) 生产红线：仍拒绝 api.hbksd.com.cn（49.235.137.217 已转 TST，2026-10-11 Boss 口径）。
 *   3) 新增·跨环境点名：E2E_STAGING_URL 指向的不是 staging、也不是回环隧道，而该链的 H5 变量没设
 *      ⇒ 抛红并点名该设哪一颗。回环（localhost/127.0.0.1）不点名：那是 runbook 里既有的
 *      「ssh -L 隧道 + 产物写死的 staging 域名源」姿势，H5 腿走默认值才是对的。
 *
 * 只解析地址，不碰任何断言；口令类变量不经这里（仍由用例自己的凭据门管）。
 */

/** staging 的 H5 走查站源 = 产物构建期写死的 API 基址（apps/tech-miniapp/.env.staging、apps/patient-miniapp/.env.staging） */
export const STAGING_H5_ORIGIN = 'http://hbksd.com.cn:81'

/** 同一套 staging 的两种入口写法，只用来判「是不是同一环境」，不做地址替换 */
const STAGING_HOSTS = new Set(['hbksd.com.cn', '106.52.39.208'])

function hostOf(origin: string): string {
  return new URL(origin).hostname
}

function isStagingOrigin(origin: string): boolean {
  return STAGING_HOSTS.has(hostOf(origin))
}

function isLoopbackOrigin(origin: string): boolean {
  const h = hostOf(origin)
  return h === 'localhost' || h === '127.0.0.1' || h === '::1'
}

/**
 * 取本链的 H5 源。
 *
 * @param varName 该链的注入变量名（E2E_TECH_H5_URL / E2E_PATIENT_H5_URL）
 * @param chainLabel 判红文案里的链名（沿用两条链原有措辞）
 */
export function resolveH5Origin(varName: string, chainLabel: string): string {
  // 空串按「未设」处理：CI 的 `${{ vars.X }}` 在变量没配时展开成空串，`?? 默认值` 兜不住它，
  // 会把整条链崩成「实得 <空>」这种看不出缺哪一颗的红。
  const injected = (process.env[varName] ?? '').trim()
  const origin = (injected || STAGING_H5_ORIGIN).replace(/\/+$/, '')

  if (!/^http:\/\/[^/]+:\d+$/.test(origin)) {
    throw new Error(`${chainLabel}源应是 http + 显式端口的 staging 入口，实得 ${origin}`)
  }
  if (/api\.hbksd\.com\.cn/.test(origin)) { // 49.235.137.217 自 2026-10-11 起为 TST（Boss 口径），移出生产针
    throw new Error(`${chainLabel}命中生产入口，红线拒绝：${origin}`)
  }

  const target = (process.env.E2E_STAGING_URL ?? '').trim()
  if (!injected && target && !isStagingOrigin(target) && !isLoopbackOrigin(target)) {
    throw new Error(
      `${chainLabel}本轮目标与 H5 源不在同一套环境：E2E_STAGING_URL=${target}，` +
        `而 ${varName} 未设置 ⇒ H5 腿会留在 staging 默认源 ${STAGING_H5_ORIGIN}。` +
        `请把 ${varName} 设为该环境实际投放的 H5 源（要与产物构建期写死的 API 基址同源，` +
        `否则应用自己的请求跨源被浏览器拦掉）。`,
    )
  }

  console.log(
    `[t614][H5源] ${chainLabel} | ${varName}=${injected ? '已注入' : '未设，走 staging 默认'} | 源=${origin} | 目标=${target || '<未设>'}`,
  )
  return origin
}
