import { defineConfig, devices } from '@playwright/test'

/**
 * BraceSync Playwright E2E 基座配置
 *
 * 测试目标：apps/patient-miniapp（uni-app H5 dev server，USE_MOCK 数据）
 * - 本机：默认端口 http://localhost:5173（E2E_PORT 可改），reuseExistingServer 复用已在跑的 server
 * - CI：由 webServer.command 拉起 dev server（需 apps/patient-miniapp 已入库，见 e2e.yml）
 *
 * 选择器策略：T016 业务代码不加 data-testid（红线），用例基于 class/文案定位；
 * 后续 Iris 补 data-testid 后可平滑迁移。
 */
const port = Number(process.env.E2E_PORT ?? 5173)
const baseUrl = `http://localhost:${port}`

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
    baseURL: process.env.E2E_BASE_URL || baseUrl,
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
    // 本机缺省端口已在跑则直接复用（reuseExistingServer）；否则拉起患者端 dev server
    // 端口走 E2E_PORT（缺省 5173，与技师端 5174 / 后台 5175 错开）。这里不用 `npm run dev:patient`：
    // 那层 npm workspace 会吞掉 --port（同 tech-playwright.config.ts 的实测结论），端口就锁不住
    command: `cd apps/patient-miniapp && npx uni --port ${port}`,
    url: baseUrl,
    reuseExistingServer: true,
    timeout: 180_000,
  },
})
