import { defineConfig, devices } from '@playwright/test'

/**
 * 运营后台 Playwright E2E 配置（T029 admin-web）
 *
 * 测试目标：apps/admin-web（Vue3 + Element Plus 标准 Vite Web，USE_MOCK=true 数据）
 * - 本机：dev server 跑在 http://localhost:5175（--port 5175，避免与患者端 5173 / 技师端 5174 冲突）
 * - CI：由 webServer.command 拉起 dev server
 *
 * 与患者端（playwright.config.ts）/技师端（tech-playwright.config.ts）隔离：
 * 独立 testMatch（admin-*.spec.ts）+ 独立端口 + 桌面设备（Element Plus 为桌面后台）
 */
export default defineConfig({
  testDir: './tests',
  testMatch: '**/admin-*.spec.ts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 2 : undefined,
  timeout: 60_000,
  expect: {
    timeout: 10_000,
  },
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:5175',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      // admin-web 是桌面端后台：用桌面 Chrome 视口（区别于患者端/技师端的移动 H5）
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    // --port 5175 避免与患者端（5173）/技师端（5174）dev server 冲突；
    // --strictPort 禁止端口占用时自动回退（否则会抢技师端 5174 造成 tech 用例误连）
    // 直接从 workspace 目录启动 vite，CLI --port 覆盖 vite.config.ts 内的 5174
    command: 'cd apps/admin-web && npx vite --port 5175 --strictPort',
    url: 'http://localhost:5175',
    cwd: '..',
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
  },
})
