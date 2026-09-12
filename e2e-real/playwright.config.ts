import { defineConfig, devices } from '@playwright/test'

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
 * - 凭证：ADMIN_USERNAME / ADMIN_PASSWORD（找 Andy，可复用 T105 只读账号思路）；缺失则用例自动 skip
 */
export default defineConfig({
  testDir: './',
  testMatch: 'contract-smoke.spec.ts',
  timeout: 120_000,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.STAGING_URL || 'https://staging.bracesync.example.com',
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