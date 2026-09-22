import { defineConfig, devices } from '@playwright/test'

/**
 * 运营后台 Playwright E2E 配置（T029 admin-web）
 *
 * 测试目标：apps/admin-web（Vue3 + Element Plus 标准 Vite Web，USE_MOCK=true 数据）
 * - 本机：dev server 默认跑在 http://localhost:5175（可用 E2E_PORT 改端口，多 worktree 并存时互不复用）
 * - CI：由 webServer.command 拉起 dev server
 *
 * 与患者端（playwright.config.ts）/技师端（tech-playwright.config.ts）隔离：
 * 独立 testMatch（admin-*.spec.ts）+ 独立端口 + 桌面设备（Element Plus 为桌面后台）
 */
// T301/G12: 端口从环境变量取，多 worktree 并存时各跑各的端口，避免复用到别人的构建
const port = Number(process.env.E2E_PORT ?? 5175)
const baseUrl = `http://localhost:${port}`

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
    // mock 用例只打本地 dev server；staging 地址的唯一变量是 E2E_STAGING_URL（见 e2e-real/）
    baseURL: process.env.E2E_LOCAL_BASE_URL || baseUrl,
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
    // 端口缺省 5175（避开患者端 5173 / 技师端 5174），E2E_PORT 可覆盖 —— 多 worktree 并存时各占一个端口
    // --strictPort 禁止端口被占时自动回退（回退会让用例连到别的端的构建上）
    // 直接从 workspace 目录启动 vite，CLI --port 覆盖 vite.config.ts 内的默认端口
    command: `cd apps/admin-web && npx vite --port ${port} --strictPort`,
    url: baseUrl,
    cwd: '..',
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
  },
})
