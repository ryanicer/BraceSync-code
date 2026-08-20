import { defineConfig, devices } from '@playwright/test'

/**
 * 技师端 Playwright E2E 配置（T027 tech-miniapp）
 *
 * 测试目标：apps/tech-miniapp（uni-app H5 dev server，USE_MOCK 数据）
 * - 本机：dev server 跑在 http://localhost:5174（--port 5174，避免与患者端 5173 冲突）
 * - CI：由 webServer.command 拉起 dev server
 */
export default defineConfig({
  testDir: './tests',
  testMatch: '**/tech-*.spec.ts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 2 : undefined,
  timeout: 60_000,
  expect: {
    // WiFi 配网 mock 耗时约 8s（BLE 连接 1s + 5 步 × 1.2s + 0.8s），放宽断言超时
    timeout: 15_000,
  },
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:5174',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      // 技师端同为移动端 H5：用 Android 设备模拟（与患者端一致）
      name: 'chromium',
      use: { ...devices['Pixel 5'] },
    },
  ],
  webServer: {
    // --port 5174 避免与患者端 dev server（5173）冲突
    // 直接从 workspace 目录启动，确保 --port 传递给 uni CLI（npm workspace 会吞掉 --port）
    command: 'cd apps/tech-miniapp && npx uni --port 5174',
    url: 'http://localhost:5174',
    cwd: '..',
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
  },
})
