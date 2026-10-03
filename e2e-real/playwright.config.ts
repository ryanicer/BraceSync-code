import { defineConfig, devices } from '@playwright/test'

// ⚠️ 这里不能在模块加载期校验 E2E_STAGING_URL：ci-e2e-real-static.yml 第 54 行在 CI 里跑
// `--list`（该 workflow 不注入 E2E_STAGING_URL），配置一抛就是「静态门禁红」而不是「缺变量红」。
// 同一判据搬到用例运行时（contract-smoke.spec.ts 的 stagingTarget()）：真跑缺变量照样判红，
// 只是红在用例上、并写明缺哪个变量。

/**
 * T144 交付② P1：真实后端冒烟（contract-smoke）
 *
 * 目标：登录真实后端（staging）→ 请求目标列表接口 → 断言「返回非空 且 页面渲染 ≥1 条」，
 * 防止 T143 那类「后端 200 有返回、页面却空」问题在真实环境复现。
 *
 * 规范：
 * - 独立目录 e2e-real/，与 mock E2E（e2e/）完全隔离
 * - 不启动本地 webServer：直连 staging（baseURL）
 * - 不进 PR 门禁，仅 nightly / workflow_dispatch 触发
 * - 凭证：ADMIN_USERNAME / ADMIN_PASSWORD，或走 real-helpers 的 E2E_REAL_PASSWORD 凭据门。
 *   T549 起 CI 里缺凭证判红而不是 skip；json 报告（CI）供 job 的「用例真的执行了」断言读 stats。
 */
export default defineConfig({
  testDir: './',
  testMatch: 'contract-smoke.spec.ts',
  timeout: 120_000,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI
    ? [
        ['github'],
        ['html', { open: 'never' }],
        // T549 防假绿：e2e.yml real-backend-smoke job 读这份 json 的 stats，
        // 要求 executed>=1 且 skipped==0 —— 「0 执行也报绿」这条路被封死。
        ['json', { outputFile: 'test-results/smoke-result.json' }],
      ]
    : [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.E2E_STAGING_URL || 'http://localhost:2080',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      // admin-web 为桌面后台，用桌面 Chrome 视口
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})