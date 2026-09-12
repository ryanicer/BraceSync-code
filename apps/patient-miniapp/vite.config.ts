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
// - 构建（uni build，mode=production）：按显式 TARGET（staging|prod）选择目标源文件。
//   缺省 = staging（安全默认，绝不静默落到生产）；TARGET 缺失但构建时打印自证。
//   .env.local 始终最高优先（官方出包脚本 / 临时注入用）。
// - development：默认沿用患者端原硬编码值（API_BASE_URL=生产、USE_MOCK=false），
//   保证「不配置任何 env 时行为与现状完全一致」；.env.local 仍可覆盖
// 改自 apps/tech-miniapp/vite.config.ts：仅把开发默认回退值由「mock/空地址」改为患者端
// 原始硬编码值（技师端页面走 mock 函数，患者端页面直接调 request()，故患者端默认必须非 mock）。
//
// 注意：这里读取 process.env.TARGET 是刻意的「显式目标」开关（非 VITE_* 值，系统环境里
// 不会存在），不会像普通 VITE_* 那样被系统环境变量意外覆盖入库值。
export default defineConfig(({ mode }) => {
  const isProd = mode === 'production'

  // 显式构建目标：TARGET ∈ staging | prod，缺省 staging（安全默认，绝不静默生产）。
  // 生产永远是显式动作（TARGET=prod 或官方出包脚本 build-miniapp.mjs … prod）。
  const rawTarget = (process.env.TARGET || 'staging').trim().toLowerCase()
  const TARGET_OK = rawTarget === 'staging' || rawTarget === 'prod'
  const chosen = TARGET_OK ? rawTarget : 'staging'
  if (!TARGET_OK) console.warn(`[build] 未知 TARGET="${rawTarget}"，缺省按 staging 处理`)

  const envTargetFile = chosen === 'prod' ? '.env.production' : '.env.staging'
  const envTarget = isProd ? readEnvFile(envTargetFile) : {}
  const envLocal = readEnvFile('.env.local')

  // 优先级：.env.local > 目标源文件（仅构建模式）> 患者端原有硬编码默认值
  const apiBaseUrl = envLocal.VITE_API_BASE_URL ?? envTarget.VITE_API_BASE_URL ?? 'https://api.hbksd.com.cn'
  const useMock = (envLocal.VITE_USE_MOCK ?? envTarget.VITE_USE_MOCK ?? 'false') !== 'false'

  // 构建期打印本次解析结果，自证 target（产物注入校验见 scripts/wechat/smoke-*.js）
  if (isProd) {
    console.log(`[build] target=${chosen} file=${envTargetFile} API_BASE_URL=${apiBaseUrl} USE_MOCK=${useMock}`)
    if (chosen === 'prod') console.warn('[build] ⚠ 生产包：直连生产后端，仅正式上线用')
    else if (!process.env.TARGET) console.log('[build] 未显式指定 TARGET，缺省=staging（安全默认）')
  }

  return {
    plugins: [uni()],
    define: {
      __API_BASE_URL__: JSON.stringify(apiBaseUrl),
      __USE_MOCK__: JSON.stringify(useMock),
    },
  }
})