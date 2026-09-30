import { expect } from '@playwright/test'

/*
 * T502 登录耗时判据（mock 线 e2e/ 与真实线 e2e-real/ 共用）
 *
 * 来源缺口 = T497 定性结论：登录用例只等「最后跳到了」，慢到 15s 照样绿。
 * 本文件把耗时改成显式判据：实测毫秒数进测试输出（报告可反查），超预算直接红。
 * 阈值在此定义一次，两条线共用，调阈值不改用例。
 */

/** 预算默认 10s；E2E_LOGIN_BUDGET_MS 可在不改码前提下调阈值（排障 / 环境慢时复核用） */
const DEFAULT_LOGIN_BUDGET_MS = 10_000

const parseBudget = (raw: string | undefined): number => {
  const n = Number(raw)
  return Number.isFinite(n) && n > 0 ? n : DEFAULT_LOGIN_BUDGET_MS
}

export const LOGIN_BUDGET_MS = parseBudget(process.env.E2E_LOGIN_BUDGET_MS)

/**
 * 跑一次登录动作，返回实测毫秒。
 * 失败分支也要把已耗时打出来再抛：慢失败的数值正是 T497 要的东西，
 * 只留堆栈就看不出「是被预算卡住」还是「被 waitForURL 超时卡住」。
 */
export async function timedLogin(label: string, action: () => Promise<unknown>): Promise<number> {
  const startedAt = Date.now()
  try {
    await action()
  } catch (err) {
    console.log(
      `[login-timing] ${label}: 登录动作在第 ${Date.now() - startedAt} ms 处抛错（预算 ${LOGIN_BUDGET_MS} ms）`,
    )
    throw err
  }
  const ms = Date.now() - startedAt
  console.log(`[login-timing] ${label}: 实测 ${ms} ms（预算 ${LOGIN_BUDGET_MS} ms）`)
  return ms
}

/** 耗时判据：不低于预算即红，失败信息自带实测值、阈值与覆盖入口 */
export function expectLoginWithinBudget(label: string, ms: number): void {
  expect(
    ms,
    `${label} 登录耗时 ${ms} ms，未低于预算 ${LOGIN_BUDGET_MS} ms（阈值可用环境变量 E2E_LOGIN_BUDGET_MS 调整）`,
  ).toBeLessThan(LOGIN_BUDGET_MS)
}
