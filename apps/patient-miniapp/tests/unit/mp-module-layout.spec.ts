/**
 * T192 — 小程序模块落位守卫
 *
 * 真机报错「module 'pages/wifi-setup/copy.js' is not defined」：
 * 被 components/ 引用的共享模块若落在 pages/<页>/ 里，微信运行时按页面作用域注册模块，
 * 跨目录 require 取不到；`uni build` 与 H5 e2e 都发现不了。
 * 这里把"组件不得伸进 pages/ 取模块"变成可执行断言（全仓扫描，不只配网）。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const SRC = fileURLToPath(new URL('../../src', import.meta.url))

function sourceFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const full = path.join(dir, e.name)
    return e.isDirectory() ? sourceFiles(full) : /\.(ts|vue)$/.test(e.name) ? [full] : []
  })
}

/** 抓 `from './x'`、`import './x'`（无 from 的副作用导入）与 `require('./x')` */
function importedSpecifiers(file: string): string[] {
  const src = fs.readFileSync(file, 'utf8')
  const re =
    /(?:^|\s)(?:import|export)\s+[^'"]*?from\s*['"](\.[^'"]+)['"]|(?:^|\s)import\s*['"](\.[^'"]+)['"]|require\(\s*['"](\.[^'"]+)['"]\s*\)/gm
  return [...src.matchAll(re)].map((m) => m[1] ?? m[2] ?? m[3])
}

describe('T192 — 共享模块不得放在 pages/ 下被组件引用', () => {
  const components = sourceFiles(path.join(SRC, 'components'))
  const violations = components
    .filter((f) => importedSpecifiers(f).some((spec) => spec.includes('pages/')))
    .map((f) => path.relative(SRC, f).replace(/\\/g, '/'))

  it('src/components 下无任何文件 import 或 require pages/…', () => {
    expect(components.length).toBeGreaterThanOrEqual(10) // 扫描确实覆盖到组件目录（防假绿）
    expect(violations).toEqual([])
  })
})
