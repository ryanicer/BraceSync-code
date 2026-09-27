// T427 挂载点契约（patient-miniapp H5 走查站）
//
// 背景：H5 包挂在 /patient-h5/ 下，涉及四处各写一次的字符串 —— manifest 的 h5.router.base、
// nginx.conf 的 location 与 alias、docker-compose.yml 的挂载卷、deploy-staging.sh 的构建落点。
// 谁只改一边，表现是「入口 200 但资源 404 的白屏页」（同 T336 在 admin-web 上踩过的形状），
// 而这条只在浏览器里看得见，构建与部署都不会报错。本用例把四处钉成同一个值。
//
// 路径解析用 fileURLToPath 而非 new URL(rel, import.meta.url)：后者字面量形式会被 Vite 的
// asset-import-meta-url 插件在编译期改写成 http 资源地址，运行时拿不到盘路径（同 admin-web 那条教训）。
import { describe, it, expect } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const SITE = 'patient-h5'
const MOUNT = `/${SITE}/`
const HTML_ROOT = `/usr/share/nginx/html/${SITE}`

const here = dirname(fileURLToPath(import.meta.url))
let repoRoot = here
while (repoRoot !== dirname(repoRoot) && !existsSync(join(repoRoot, 'scripts/deploy/nginx.conf'))) {
  repoRoot = dirname(repoRoot)
}
const readRepo = (rel: string) => readFileSync(join(repoRoot, rel), 'utf8')

const manifest = JSON.parse(readFileSync(join(here, '..', '..', 'src/manifest.json'), 'utf8'))
const nginxConf = readRepo('scripts/deploy/nginx.conf')
const compose = readRepo('scripts/deploy/docker-compose.yml')
const deploySh = readRepo('scripts/deploy/deploy-staging.sh')

describe(`T427 挂载点契约：H5 构建 base 与 nginx/compose/部署脚本四处同值（${MOUNT}）`, () => {
  it(`manifest 的 h5.router.base 为 ${MOUNT} 且带尾斜杠`, () => {
    expect(manifest.h5.router.base).toBe(MOUNT)
    // hash 路由：深链在 # 之后，nginx 只需回入口 HTML，不依赖 history fallback
    expect(manifest.h5.router.mode).toBe('hash')
  })

  it('nginx 两个 server 块（80 与 443）都挂该前缀，且 alias 与 fallback 指向同一挂载点', () => {
    const locRe = new RegExp(`location\\s+${MOUNT.replace(/[/]/g, '\\/')}\\s*\\{`, 'g')
    expect([...nginxConf.matchAll(locRe)].length, '80/443 两处 server 块都要挂该前缀').toBeGreaterThanOrEqual(2)
    expect(nginxConf).toContain(`alias ${HTML_ROOT}/;`)
    expect(nginxConf).toContain(`try_files $uri $uri/ ${MOUNT}index.html;`)
    // 反证：不许把 H5 挂到 admin 的 root 下（/assets/ 那条 location 会把它的资源抢走）
    expect(nginxConf).not.toMatch(new RegExp(`location\\s+${MOUNT.replace(/[/]/g, '\\/')}[^{]*\\{[^}]*\\broot\\s+/usr/share/nginx/html/admin`))
  })

  it('docker-compose 把宿主机产物目录只读挂进 alias 指向的路径', () => {
    expect(compose).toContain(`./apps/${SITE}/dist:${HTML_ROOT}:ro`)
  })

  it('部署脚本真的构建本站点并落到 compose 引用的目录', () => {
    expect(deploySh).toContain(`build_h5_site patient-miniapp ${SITE}`)
    expect(deploySh).toContain(`$STAGING_DIR/apps/$site/dist`)
  })

  it('H5 站点与 API 反代落在同一个 server 块（网关无 CORS，页面只能同源调接口）', () => {
    const servers = nginxConf.split(/\n\s*server\s*\{/)
    const host = servers.filter((s) => s.includes(`location ${MOUNT}`))
    expect(host.length, '至少有 80/443 一处 server 块挂 H5').toBeGreaterThan(0)
    for (const s of host) {
      expect(s).toMatch(/location \/api\/\s*\{[^}]*proxy_pass\s+http:\/\/gateway/)
    }
    // 产物里注入的是绝对地址（.env.staging），所以走查入口必须按同源方式打开
    const envStaging = readFileSync(join(here, '..', '..', '.env.staging'), 'utf8')
    const api = envStaging.match(/^VITE_API_BASE_URL=(.*)$/m)
    expect(api, '.env.staging 必须显式给出 VITE_API_BASE_URL').not.toBeNull()
    expect(api![1].trim()).toBe('http://hbksd.com.cn:81')
  })
})
