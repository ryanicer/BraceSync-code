// T427 挂载点契约（tech-miniapp H5 走查站）
// 与 apps/patient-miniapp/tests/unit/h5-mount-contract.spec.ts 同判据，只差站点常量。
// 两个 app 各留一份而不抽公共用例：契约要红在「改的那个 app」上，公共夹具会让报错指不到站点。
import { describe, it, expect } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const SITE = 'tech-h5'
const MOUNT = `/${SITE}/`
const HTML_ROOT = `/usr/share/nginx/html/${SITE}`
const ESC = MOUNT.replace(/[/]/g, '\\/')

const here = dirname(fileURLToPath(import.meta.url))
let repoRoot = here
while (repoRoot !== dirname(repoRoot) && !existsSync(join(repoRoot, 'scripts/deploy/nginx.conf'))) {
  repoRoot = dirname(repoRoot)
}
const readRepo = (rel: string) => readFileSync(join(repoRoot, rel), 'utf8')

const manifest = JSON.parse(readFileSync(join(here, '..', 'src/manifest.json'), 'utf8'))
const nginxConf = readRepo('scripts/deploy/nginx.conf')
const compose = readRepo('scripts/deploy/docker-compose.yml')
const deploySh = readRepo('scripts/deploy/deploy-staging.sh')

describe(`T427 挂载点契约：H5 构建 base 与 nginx/compose/部署脚本四处同值（${MOUNT}）`, () => {
  it(`manifest 的 h5.router.base 为 ${MOUNT} 且带尾斜杠`, () => {
    expect(manifest.h5.router.base).toBe(MOUNT)
    expect(manifest.h5.router.mode).toBe('hash')
  })

  it('nginx 两个 server 块（80 与 443）都挂该前缀，且 alias 与 fallback 指向同一挂载点', () => {
    const locRe = new RegExp(`location\\s+${ESC}\\s*\\{`, 'g')
    expect([...nginxConf.matchAll(locRe)].length, '80/443 两处 server 块都要挂该前缀').toBeGreaterThanOrEqual(2)
    expect(nginxConf).toContain(`alias ${HTML_ROOT}/;`)
    expect(nginxConf).toContain(`try_files $uri $uri/ ${MOUNT}index.html;`)
    expect(nginxConf).not.toMatch(new RegExp(`location\\s+${ESC}[^{]*\\{[^}]*\\broot\\s+/usr/share/nginx/html/admin`))
  })

  it('docker-compose 把宿主机产物目录只读挂进 alias 指向的路径', () => {
    expect(compose).toContain(`./apps/${SITE}/dist:${HTML_ROOT}:ro`)
  })

  it('部署脚本真的构建本站点并落到 compose 引用的目录', () => {
    expect(deploySh).toContain(`build_h5_site tech-miniapp ${SITE}`)
    expect(deploySh).toContain(`$STAGING_DIR/apps/$site/dist`)
  })

  it('H5 站点与 API 反代落在同一个 server 块（网关无 CORS，页面只能同源调接口）', () => {
    const servers = nginxConf.split(/\n\s*server\s*\{/)
    const host = servers.filter((s) => s.includes(`location ${MOUNT}`))
    expect(host.length, '至少有 80/443 一处 server 块挂 H5').toBeGreaterThan(0)
    for (const s of host) {
      expect(s).toMatch(/location \/api\/\s*\{[^}]*proxy_pass\s+http:\/\/gateway/)
    }
    const envStaging = readFileSync(join(here, '..', '.env.staging'), 'utf8')
    const api = envStaging.match(/^VITE_API_BASE_URL=(.*)$/m)
    expect(api, '.env.staging 必须显式给出 VITE_API_BASE_URL').not.toBeNull()
    expect(api![1].trim()).toBe('http://hbksd.com.cn:81')
  })
})
