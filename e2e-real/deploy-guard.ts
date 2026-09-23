import { test, type Page } from '@playwright/test'

/**
 * T324 部署守卫（e2e-real 门禁与 staging 部署顺序解耦）
 *
 * 要解决的问题：e2e-real 打的是【已部署在 staging 上的前端包】。PR 里新写/新改的行为断言，
 * 在本 PR 合并并由 Andy 部署之前，staging 上必然还是旧构建 —— 于是「门禁要绿就得先部署，
 * 要部署就得先合并，要合并就得先门禁绿」，形成死锁（T322 实测 3 failed / 29 passed 就是这么来的）。
 *
 * 这里给的不是 continue-on-error，也不是 `|| true`（那正是 T304 要修的旧病）：
 *   · 判据本身一条不放宽；
 *   · 只在【staging 当前包确实不含该标记】时，把这一条显式标成 post-deploy 跳过，
 *     并把「跳过原因 + 归属卡号」写进 Playwright 报告的 annotations，可反查；
 *   · 一旦 staging 部署上带该标记的构建，同一条断言自动转成真跑，此后任何回归照常判红；
 *   · 部署后回归（定时 / 手动，CI 里 E2E_POST_DEPLOY_STRICT=1）不允许再有 post-deploy 跳过：
 *     那个时点 staging 本该已经追平，仍缺标记就是「合并了却从没在真环境验过」⇒ 直接判红。
 *
 * 用例侧的写法（一行）：
 *   await requireDeployedBuild(page, { marker: 'T315-doctor-accounts', probe: (p) => ... })
 * marker 必须以 T 卡号开头（helper 会抛），保证每一条跳过都能追到一张卡上。
 */

export const POST_DEPLOY_ANNOTATION = 'post-deploy'

/** 本轮 run 内缓存探测结果：同一条用例集合里同一个标记只探一次 */
const probeCache = new Map<string, boolean>()

/** 部署后回归（定时 / 手动）由 CI 置 1；PR 门禁阶段不置 */
export function strictDeployedMode(): boolean {
  return process.env.E2E_POST_DEPLOY_STRICT === '1'
}

/**
 * 断言「staging 已部署的构建带有该标记」，未带则按 post-deploy 处理。
 *
 * probe 收到的是用例当前的 page（beforeEach 已登录并停在目标页），返回 true 表示标记已在
 * 已部署包里出现。probe 内不要写业务断言，只做存在性探测（探测失败/超时算 false 太宽，
 * 这里让异常照常抛出——页面结构坏了本该红，不该被当成「未部署」）。
 */
export async function requireDeployedBuild(
  page: Page,
  opts: { marker: string; probe: (page: Page) => Promise<boolean>; why?: string },
): Promise<void> {
  const { marker, probe, why } = opts
  if (!/^T\d{3}-./.test(marker)) {
    throw new Error(`requireDeployedBuild: marker「${marker}」须以卡号开头（形如 T315-doctor-accounts），否则跳过无法追溯`)
  }

  let present = probeCache.get(marker)
  if (present === undefined) {
    try {
      present = await probe(page)
    } catch (e) {
      // 探测只该问「在不在」，不该抛（抛了这里就无法区分「旧包缺元素」与「页面真坏了」）。
      // 但作者写错成业务断言时也得看得懂，故原样带上现场信息再抛——判红是对的，别降级成跳过。
      const msg = e instanceof Error ? e.message : String(e)
      throw new Error(
        `requireDeployedBuild(${marker}) 探测抛错，未当作「未部署」处理：probe 只能做存在性探测` +
        `（用 .count() / .isVisible().catch(() => false)，不要在旧包上 await 一个不存在的元素）。原始错误：${msg}`,
      )
    }
    probeCache.set(marker, present)
  }
  if (present) return

  const desc = `${marker}：staging 当前部署的构建还没有这条行为${why ? `（${why}）` : ''}`
  test.info().annotations.push({ type: POST_DEPLOY_ANNOTATION, description: desc })

  if (strictDeployedMode()) {
    // 部署后回归阶段：staging 本该已追平，缺标记 = 合并过的行为从未在真环境验过 ⇒ 判红
    throw new Error(
      `部署守卫判红（${POST_DEPLOY_ANNOTATION}）：${desc}。本阶段 staging 应已部署 main 最新构建，` +
      `请核对 staging 部署是否落在该卡合并之后（部署脚本：scripts/deploy/deploy-staging.sh）`,
    )
  }
  console.log(`[post-deploy] skip ${marker} —— ${desc}`)
  test.skip(true, `${POST_DEPLOY_ANNOTATION}：${desc} ⇒ 本条对旧包无意义，显式跳过；部署后自动转真跑`)
}
