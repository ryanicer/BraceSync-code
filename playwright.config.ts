import { defineConfig, devices } from '@playwright/test'

/**
 * BraceSync Playwright E2E 基座配置
 *
 * 测试目标：apps/patient-miniapp（uni-app H5 dev server，USE_MOCK 数据）
 * - 本机：Iris 的 dev server 跑在 http://localhost:5173（reuseExistingServer 复用）
 * - CI：由 webServer.command 拉起 dev server（需 apps/patient-miniapp 已入库，见 e2e.yml）
 *
 * 选择器策略：T016 业务代码不加 data-testid（红线），用例基于 class/文案定位；
 * 后续 Iris 补 data-testid 后可平滑迁移。
 */
export default defineConfig({
  testDir: './e2e/tests',
  // 技师端（T027）与运营后台（T029）各有独立 config，患者端基座不收集其用例
  testIgnore: ['**/tech-*.spec.ts', '**/admin-*.spec.ts'],
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 2 : undefined,
  timeout: 60_000,
  expect: {
    // 配网流程 mock 耗时约 10s（BLE 连接 1s + 5 步 × 1.2s + 0.8s），放宽断言超时
    timeout: 15_000,
  },
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:5173',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      // 患者端为移动端 H5：用 Android 设备模拟（chromium 内核，小程序 WebView 同为 chromium 系）
      name: 'chromium',
      use: { ...devices['Pixel 5'] },
    },
  ],
  webServer: {
    // 本机 5173 已在跑则直接复用；否则拉起 workspace dev server（uni/vite 默认端口 5173）
    command: 'npm run dev:patient',
    url: 'http://localhost:5173',
    reuseExistingServer: true,
    timeout: 180_000,
  },
})
