import { defineConfig } from 'vitest/config'

// T094a: 镜像 apps/tech-miniapp/vitest.config.ts
// 纯 TS 状态逻辑测试，不依赖 DOM/uni，使用 node 环境。
export default defineConfig({
  test: {
    globals: true,
    environment: 'node',
  },
})
