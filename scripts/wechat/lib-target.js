// lib-target.js — 冒烟脚本「构建目标自证」共享工具（CommonJS）。
//
// 冒烟脚本连的是已经构建好的产物（apps/<app>/dist/build/mp-weixin）。为杜绝
// 「以为 staging、实际 production」，这里从**产物本身**解析实际打进去的后端地址，
// 并据此判定 target（staging | prod | unknown），再由调用方的 smoke 脚本决定
// 是继续、告警还是拒绝运行。
//
// 两个已知后端地址（与官方出包脚本 / .env.staging / .env.production 一致）：
//   - staging → http://hbksd.com.cn:81
//   - prod    → https://api.hbksd.com.cn
//
// USE_MOCK 不直接从产物取（编译后被内联为布尔字面量、难以可靠正则），而是按
// vite 同款优先序（.env.local > 目标源文件，缺省 staging）从源码 env 解析，与
// 构建期打印口径一致，仅用于自证展示。

const fs = require('fs')
const path = require('path')

const PROD_URL = 'https://api.hbksd.com.cn'
const STAGING_URL = 'http://hbksd.com.cn:81'

const SKIP_EXT = new Set([
  '.png', '.jpg', '.jpeg', '.gif', '.webp', '.ico', '.bmp',
  '.woff', '.woff2', '.ttf', '.eot', '.otf', '.mp4', '.zip', '.pdf',
])

// 遍历 dir 下非二进制文本文件，统计两段地址在产物里的命中次数。
// 跳过单文件过大（>3MB）与跳过扩展名列表，避免把二进制/压缩包读进内存。
function walk(dir, out = []) {
  let names
  try { names = fs.readdirSync(dir) } catch { return out }
  for (const n of names) {
    const p = path.join(dir, n)
    let st
    try { st = fs.statSync(p) } catch { continue }
    if (st.isDirectory()) walk(p, out)
    else if (st.size <= 3 * 1024 * 1024 && !SKIP_EXT.has(path.extname(p).toLowerCase())) out.push(p)
  }
  return out
}

// 解析产物实际打到的后端 target。
// 返回 { target: 'staging' | 'prod' | 'unknown', prodHits, stagingHits, urlFound }
function resolveBuiltTarget(distDir) {
  const files = walk(distDir)
  let prodHits = 0
  let stagingHits = 0
  for (const f of files) {
    let txt
    try { txt = fs.readFileSync(f, 'utf8') } catch { continue }
    if (txt.includes(PROD_URL)) prodHits++
    if (txt.includes(STAGING_URL)) stagingHits++
  }
  let target = 'unknown'
  if (prodHits > 0 && stagingHits === 0) target = 'prod'
  else if (stagingHits > 0 && prodHits === 0) target = 'staging'
  else if (prodHits === 0 && stagingHits === 0) target = 'unknown'
  // prodHits>0 && stagingHits>0：两个地址都命中 → 视作可疑（unknown 走最严路径）
  return { target, prodHits, stagingHits, urlFound: prodHits > 0 || stagingHits > 0 }
}

// 解析单个 .env 文件（KEY=VALUE，忽略注释/空行/引号），与 vite.config.ts 同款。
function parseEnv(content) {
  const result = {}
  if (!content) return result
  for (const line of content.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    const idx = trimmed.indexOf('=')
    if (idx > 0) {
      let key = trimmed.slice(0, idx).trim()
      let value = trimmed.slice(idx + 1).trim()
      if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
        value = value.slice(1, -1)
      }
      if (key) result[key] = value
    }
  }
  return result
}

function readEnvFile(filepath) {
  try { return parseEnv(fs.readFileSync(filepath, 'utf8')) } catch { return {} }
}

// 按 vite 同款优先序解析 VITE_USE_MOCK：.env.local > 目标源文件（缺省 staging）。
// appDir：apps/<app> 目录绝对路径。chosen：'staging' | 'prod'（默认由 TARGET 解析）。
function resolveMockFlag(appDir, chosen) {
  const envLocal = readEnvFile(path.join(appDir, '.env.local'))
  const targetFile = chosen === 'prod' ? '.env.production' : '.env.staging'
  const envTarget = readEnvFile(path.join(appDir, targetFile))
  const raw = envLocal.VITE_USE_MOCK ?? envTarget.VITE_USE_MOCK ?? (chosen === 'prod' ? 'false' : 'false')
  return raw !== 'false' ? 'true' : 'false'
}

// 输出首行自证信息（含义：产物实际打到的地址 + target）。
// 供 smoke 脚本在 run() 最前面调用；返回 resolveBuiltTarget 的结果。
function printTargetReport(appDir, distDir) {
  const { target, prodHits, stagingHits, urlFound } = resolveBuiltTarget(distDir)
  const mock = resolveMockFlag(appDir, target === 'prod' ? 'prod' : 'staging')
  console.log('==================================================')
  console.log(`[TARGET] API_BASE_URL=${urlFound ? (prodHits ? PROD_URL : STAGING_URL) : '(产物中未命中已知后端地址)'}`)
  console.log(`[TARGET] USE_MOCK=${mock}`)
  console.log(`[TARGET] 产物命中 staging=${stagingHits} 处 / prod=${prodHits} 处 → target=${target}`)
  console.log('==================================================')
  return resolveBuiltTarget(distDir)
}

module.exports = { PROD_URL, STAGING_URL, resolveBuiltTarget, resolveMockFlag, printTargetReport }