import { defineConfig, devices } from '@playwright/test'

/**
 * 技师端 Playwright E2E 配置（T027 tech-miniapp）
 *
 * 测试目标：apps/tech-miniapp（uni-app H5 dev server，USE_MOCK 数据）
 * - 本机：dev server 默认跑在 http://localhost:5174（可用 E2E_PORT 改端口，多 worktree 并存时互不复用）
 * - CI：由 webServer.command 拉起 dev server
 */
const port = Number(process.env.E2E_PORT ?? 5174)
const baseUrl = `http://localhost:${port}`

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
    baseURL: process.env.E2E_LOCAL_BASE_URL || baseUrl,
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
    // 端口缺省 5174（避开患者端 5173）；E2E_PORT 可覆盖，多 worktree 并存时各占一个端口
    // 直接从 app 目录启动，确保 --port 传给 uni CLI（npm workspace 会吞掉 --port）
    command: `cd apps/tech-miniapp && npx uni --port ${port}`,
    url: baseUrl,
    cwd: '..',
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
  },
})
