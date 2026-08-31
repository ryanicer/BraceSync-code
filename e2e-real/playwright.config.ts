import { defineConfig, devices } from '@playwright/test'

/**
 * ⚠️ 运营后台「真实模式」E2E Playwright 配置（T053）
 *
 * 与 e2e/admin-playwright.config.ts（mock 模式）完全隔离：
 *  - 测试目标：staging 已部署的 admin-web（VITE_USE_MOCK=false）
 *  - baseURL：默认 http://localhost:2080（SSH 隧道到 staging 81 端口，不含 /admin；各路由由 realRoutes.xxx 提供 /admin/ 前缀），可通过 E2E_BASE_URL env 覆盖
 *  - 无 webServer 字段：不启动本地 vite，直连 staging 真实服务
 *  - 串行执行（workers=1）：避免真实接口数据竞争
 *  - testDir: ./tests（与 e2e/tests/ 完全隔离）
 *  - 范围：运营后台 Web 5 模块（登录/Dashboard/告警/监控/患者/团队/沟通）
 *  - 范围外：技师端/患者端小程序（排 T054）
 */
export default defineConfig({
  testDir: './tests',
  testMatch: '**/*.spec.ts',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  timeout: 120_000,
  expect: {
    timeout: 15_000,
  },
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'playwright-report' }],
    ['json', { outputFile: 'test-results/result.json' }],
  ],
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:2080',
    trace: 'retain-on-failure',
    screenshot: 'on',
    actionTimeout: 20_000,
    navigationTimeout: 30_000,
  },
  projects: [
    {
      // admin-web 为桌面端后台（Element Plus），使用桌面 Chrome 视口
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // 无 webServer：复用 staging 已部署服务，禁止启动本地 vite
})
