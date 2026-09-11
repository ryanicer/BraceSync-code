import { defineConfig } from 'vite'
import uni from '@dcloudio/vite-plugin-uni'
import { readFileSync, existsSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

// 配置文件所在目录（apps/patient-miniapp/），不依赖 process.cwd()
const __dirname = dirname(fileURLToPath(import.meta.url))

// 解析 .env 文件内容（KEY=VALUE 格式，支持 # 注释和引号）
function parseEnv(content: string): Record<string, string> {
  const result: Record<string, string> = {}
  for (const line of content.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    const idx = trimmed.indexOf('=')
    if (idx > 0) {
      const key = trimmed.slice(0, idx).trim()
      let value = trimmed.slice(idx + 1).trim()
      // 去除首尾引号
      if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
        value = value.slice(1, -1)
      }
      result[key] = value
    }
  }
  return result
}

// 读取指定 .env 文件，不存在返回空对象
function readEnvFile(filename: string): Record<string, string> {
  const filepath = resolve(__dirname, filename)
  if (!existsSync(filepath)) return {}
  return parseEnv(readFileSync(filepath, 'utf-8'))
}

// 注意：@dcloudio/vite-plugin-uni@5020320260806002 会把 import.meta.env 整体替换为 {}，
// 导致 VITE_* 变量在 mp-weixin 产物中全部丢失。
// 此处直接读取 .env 文件，不使用 loadEnv/process.env，避免系统环境变量覆盖入库值。
//
// 模式策略：
// - production：读取 .env.production（入库默认值），.env.local 可覆盖
// - development：默认沿用患者端原硬编码值（API_BASE_URL=生产、USE_MOCK=false），
//   保证「不配置任何 env 时行为与现状完全一致」；.env.local 仍可覆盖
// 改自 apps/tech-miniapp/vite.config.ts：仅把开发默认回退值由「mock/空地址」改为患者端
// 原始硬编码值（技师端页面走 mock 函数，患者端页面直接调 request()，故患者端默认必须非 mock）。
export default defineConfig(({ mode }) => {
  const isProd = mode === 'production'
  const envProduction = isProd ? readEnvFile('.env.production') : {}
  const envLocal = readEnvFile('.env.local')

  // 优先级：.env.local > .env.production（仅生产模式）> 患者端原有硬编码默认值
  const apiBaseUrl = envLocal.VITE_API_BASE_URL ?? envProduction.VITE_API_BASE_URL ?? 'https://api.hbksd.com.cn'
  const useMock = (envLocal.VITE_USE_MOCK ?? envProduction.VITE_USE_MOCK ?? 'false') !== 'false'

  return {
    plugins: [uni()],
    define: {
      __API_BASE_URL__: JSON.stringify(apiBaseUrl),
      __USE_MOCK__: JSON.stringify(useMock),
    },
  }
})