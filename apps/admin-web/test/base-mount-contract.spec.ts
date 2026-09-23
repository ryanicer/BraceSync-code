// T336 挂载点契约门禁（admin-web）
//
// 背景：nginx 把后台挂在 /admin/ 下，而 SPA 曾按根路径构建（createWebHistory() 无 base、
// vite 也无 base）。两边各说各话时，/admin/patients 这类深链在路由表里匹配不到，落 catch-all
// 被弹回首页 —— 表现为「子路由刷不出来、登录后回不到原页」（T279 实跑记录、T336 复现）。
// 根因不在 nginx（它的 try_files fallback 一直正常回 index.html），缺的是一道把
// 「前端构建 base」和「nginx 挂载点」钉在一起的检查：谁只改一边，这里立刻变红。
//
// 路径解析说明：这里用 fileURLToPath(import.meta.url) 而不是 new URL(rel, import.meta.url) ——
// 后者的字面量形式会被 Vite 的 asset-import-meta-url 插件在编译期改写成 http 资源地址，运行时拿不到盘路径。
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { ADMIN_BASE } from '../src/utils/mount'

const appRoot = join(dirname(fileURLToPath(import.meta.url)), '..') // apps/admin-web
const repoRoot = join(appRoot, '..', '..')

const readIn = (dir: string, rel: string): string => readFileSync(join(dir, rel), 'utf8')

const viteConfig = readIn(appRoot, 'vite.config.ts')
const routerSrc = readIn(appRoot, 'src/router/index.ts')
const requestSrc = readIn(appRoot, 'src/utils/request.ts')
const nginxConf = readIn(join(repoRoot, 'scripts/deploy'), 'nginx.conf')

describe('T336 挂载点契约：前端构建 base 与 nginx 挂载点必须同值', () => {
  it('挂载点常量为 /admin/，且带尾斜杠（BASE_URL 约定）', () => {
    expect(ADMIN_BASE).toBe('/admin/')
    expect(ADMIN_BASE.endsWith('/')).toBe(true)
  })

  it('vite.config 的 base 取挂载点常量（不许再漏配或写死根路径）', () => {
    expect(viteConfig).toMatch(/from '\.\/src\/utils\/mount'/)
    expect(viteConfig).toMatch(/base:\s*ADMIN_BASE/)
    expect(viteConfig).not.toMatch(/^\s*base:\s*['"]\/['"]/m)
  })

  it('router 的 history base 取构建期 BASE_URL', () => {
    expect(routerSrc).toMatch(/createWebHistory\(import\.meta\.env\.BASE_URL\)/)
    expect(routerSrc).not.toMatch(/createWebHistory\(\s*\)/)
  })

  it('401 整页跳转带挂载前缀（不写死根路径 /login）', () => {
    expect(requestSrc).toMatch(/window\.location\.href\s*=\s*`\$\{import\.meta\.env\.BASE_URL\}login`/)
    expect(requestSrc).not.toMatch(/window\.location\.href\s*=\s*'\/login'/)
  })

  it('API 请求仍是根绝对 /api/：挂载点不得污染接口前缀', () => {
    expect(requestSrc).toMatch(/export const API_BASE_URL = ''/)
    const apiDir = join(appRoot, 'src/api')
    const apiFiles = readdirSync(apiDir).filter((f) => f.endsWith('.ts'))
    expect(apiFiles.length).toBeGreaterThan(0)
    const urls = apiFiles
      .flatMap((f) => [...readIn(apiDir, f).matchAll(/\burl:\s*['"`]([^'"`]+)['"`]/g)])
      .map((m) => m[1])
    expect(urls.length).toBeGreaterThan(20) // 扫描范围非空，防正则静默失效
    const offContract = urls.filter((u) => !u.startsWith('/api/'))
    expect(offContract, `API url 必须以 /api/ 开头（带挂载前缀会 404）：${offContract.join(', ')}`).toEqual([])
  })

  it('nginx 把后台挂在 /admin/，深链 fallback 到该前缀下的 index.html', () => {
    const locations = [...nginxConf.matchAll(/location\s+(\/[^\s{]*)\s*\{/g)].map((m) => m[1])
    expect(locations.filter((l) => l === ADMIN_BASE).length, '至少一个 server 块挂载 /admin/').toBeGreaterThan(0)
    expect(nginxConf).toMatch(new RegExp(`try_files \\$uri \\$uri/ ${ADMIN_BASE}index\\.html;`))
    // 根路径只做跳转（T320：absolute_redirect off 才不丢 staging 的 :81 端口）
    expect(nginxConf).toMatch(/absolute_redirect off;/)
    expect(nginxConf).toMatch(/return 302 \/admin\/;/)
    // 反证：后台不能同时挂在根上——两处挂载本身就是下一次漂移
    expect(nginxConf).not.toMatch(/location \/ \{[^}]*try_files/s)
  })
})
