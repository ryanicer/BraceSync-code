import { defineConfig, devices } from '@playwright/test'

/**
 * ⚠️ 运营后台「真实模式」E2E Playwright 配置（T053，全量回归）
 *
 * 与 e2e-real/playwright.config.ts（T144 contract-smoke 契约门禁）严格隔离：
 *  - 调用方式：--config=./e2e-real/playwright.real.config.ts（区别于 nightly 用的 playwright.config.ts）
 *  - testDir: ./tests —— 只跑 e2e-real/tests/ 下的真实用例，绝不扩到根目录，避免卷进契约门禁
 *  - 测试目标：staging 已部署的 admin-web（VITE_USE_MOCK=false）
 *  - baseURL：只读 E2E_STAGING_URL（全仓唯一的 staging 入口变量，来源见 .github/workflows/e2e.yml 顶部注释）
 *      · CI 打 staging 公开入口 http://106.52.39.208:81
 *      · 本机走隧道：ssh -N -L 2080:127.0.0.1:81 ubuntu@106.52.39.208
 *        再 E2E_STAGING_URL=http://localhost:2080 npx playwright test --config=...
 *      · 各路由由 realRoutes.xxx 提供 /admin/ 前缀，Nginx strip 后 router 见根路径
 *  - 无 webServer 字段：不启动本地 vite，直连 staging 真实服务
 *  - 串行执行（workers=1）：避免真实接口数据竞争
 *  - 范围：运营后台 Web 5 模块（登录/Dashboard/告警/监控/患者/团队/沟通）
 *  - 范围外：技师端/患者端小程序（排 T054）
 */

// CI 下缺 E2E_STAGING_URL 就直接抛：宁可 job 立刻红，也不要真实模式悄悄打到 localhost:2080 上报「通过」
const baseUrl = process.env.E2E_STAGING_URL ?? ''
if (!baseUrl && process.env.CI) {
  throw new Error('e2e-real 真实模式缺 E2E_STAGING_URL（CI 不允许回落本地地址）')
}

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
  reporter: process.env.CI
    ? [['github'], ['list'], ['html', { open: 'never' }], ['json', { outputFile: 'test-results/result.json' }]]
    : [['list'], ['html', { open: 'never' }], ['json', { outputFile: 'test-results/result.json' }]],
  use: {
    baseURL: baseUrl || 'http://localhost:2080',
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
})