// T728 设备验签密钥的回显 / 下载 / 重新生成（admin-web 侧）
//
// 这一卡前端写错代价不对称：把密钥开放给医护＝任何人可伪造设备上报；把一次性明文落进
// 列表/详情＝每次刷新都在屏幕上重复披露。判据按「会怎样坏」排：
//  1 角色闸门真值表：判据写反＝给非 admin 放出一排后端必 403 的按钮（T351 现场形态）。
//  2 与两处真源对拍：网关 rbac.go 的 adminOnlyPatterns（不登记即 T260 默认 403，登记错矩阵即放行错角色）；
//     device-service service.MaxSecretExportBatch（批量限额，前端这格漂了就成「本地放行、上线 400」）。
//  3 真实模式（USE_MOCK=false）线形守卫：URL 段、method、请求体键名、一次性字段的取用点、
//     以及「非 2xx 时一个文件都不落盘」——把 403 的错误信封存成 .env 是最难被发现的一类坏文件。
//  4 mock 层与后端语义同形：幂等命中不回显、两次生成不同值、重生成覆盖旧值且版本自增。
//  5 页面接线（源码级）：入口存在且被闸门包住、明文只有一个消费点、重生成 confirm 排在发枪之前。
//     本项目页面 SFC 在 vitest 里挂载成本高于收益（取舍同 test/patient-password-t500.spec.ts），
//     浏览器里的真实形状由 e2e 负责。
import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { canAccessDeviceSecret, MAX_SECRET_EXPORT_BATCH } from '../src/utils/deviceSecretAccess'
import * as deviceMock from '../src/mock/devices'

const appRoot = join(dirname(fileURLToPath(import.meta.url)), '..') // apps/admin-web
const repoRoot = join(appRoot, '..', '..')
const read = (rel: string) => readFileSync(join(repoRoot, rel), 'utf8')

// path 是网关矩阵里的登记串，goMethod 是 gin 路由那条的真方法词（http.MethodGet / MethodPost）：
// 两处任一侧漂形，前端拿到的就是 403 或 404，而对不齐只有这条对拍能发现。
const SECRET_ROUTES = [
  { goMethod: 'Get', path: '/api/v1/admin/devices/:deviceId/secret-file' },
  { goMethod: 'Post', path: '/api/v1/admin/device-secrets/export' },
  { goMethod: 'Post', path: '/api/v1/admin/devices/:deviceId/secret-regenerate' },
]

const rbacSrc = read('services/gateway/cmd/server/rbac.go')

/** 取某张矩阵声明块内的原文（从 `var X = []rbacPattern{` 到与之配对的 `}`）——读法同 T500 那条对拍 */
function matrixBlock(varName: string): string {
  const start = rbacSrc.indexOf(`var ${varName} = []rbacPattern{`)
  if (start < 0) throw new Error(`rbac.go 里找不到 ${varName}（改名了？同步这条对拍）`)
  const end = rbacSrc.indexOf('\n}', start)
  if (end < 0) throw new Error(`${varName} 的声明块没有闭合`)
  return rbacSrc.slice(start, end)
}

const ALL_MATRICES = [
  'adminOnlyPatterns',
  'techAdminOnlyPatterns',
  'provisionKeyPatterns',
  'doctorAdminOnlyPatterns',
  'staffOnlyPatterns',
  'abnormalReportPatterns',
  'publicPatterns',
]

describe('T728 密钥入口的角色闸门真值表', () => {
  it('只有 admin 放行', () => {
    expect(canAccessDeviceSecret('admin')).toBe(true)
  })

  it('医护／客服／后端原始角色串／无角色一律拦（判据写反在这里先红）', () => {
    for (const role of ['doctor', 'cs', 'technician', 'ROLE_ADMIN', 'ROLE_TECHNICIAN', '', null, undefined]) {
      expect(canAccessDeviceSecret(role as never), `这个角色本该拦住：${String(role)}`).toBe(false)
    }
  })
})

describe('T728 闸门与网关真源对拍', () => {
  it('三条端点都登记在 adminOnlyPatterns，且方法词与后端路由一致', () => {
    const block = matrixBlock('adminOnlyPatterns')
    for (const r of SECRET_ROUTES) {
      expect(block, `${r.path} 未登记进 adminOnlyPatterns（T260 起未登记即默认 403）`).toContain(`"${r.path}"`)
      const line = block.split('\n').find((l) => l.includes(`"${r.path}"`)) ?? ''
      expect(line, `${r.path} 的方法词漂了：${line.trim()}`).toContain(`http.Method${r.goMethod}`)
    }
  })

  it('且没有同时出现在其它任何矩阵（多一处登记＝多放一类角色，而能取密钥＝能伪造设备上报）', () => {
    for (const name of ALL_MATRICES.filter((n) => n !== 'adminOnlyPatterns')) {
      // 反证前置：先证这块矩阵被读出了内容，否则下面的排除式断言是「读到空串」的假绿
      expect(matrixBlock(name), `${name} 被切成空块，排除式断言没有判别力`).toMatch(/rbacOf\(/)
      for (const r of SECRET_ROUTES) {
        expect(matrixBlock(name), `${r.path} 被重复登记进 ${name}`).not.toContain(`"${r.path}"`)
      }
    }
  })

  it('批量限额与 device-service 同源（改了后端没改前端＝本地放行、上线 400）', () => {
    const goSrc = read('services/device-service/internal/service/secret_t728.go')
    const m = /const MaxSecretExportBatch = (\d+)/.exec(goSrc)
    expect(m, '后端 MaxSecretExportBatch 的形状变了，同步这一格').not.toBeNull()
    expect(MAX_SECRET_EXPORT_BATCH).toBe(Number(m?.[1]))
  })
})

// ===== 真实模式线形守卫（USE_MOCK=false）=====
// vi.mock 会被提升到文件顶部，factory 里造的 mock 与下面 import 到的是同一实例（同 contract-drift-gate.spec.ts）。
vi.mock('../src/utils/request', () => {
  const request = vi.fn()
  return { request, USE_MOCK: false, API_BASE_URL: '' }
})
import { request } from '../src/utils/request'
import { dispositionFilename, registerDeviceApi, regenerateDeviceSecretApi, downloadDeviceSecretFileApi, exportDeviceSecretsApi } from '../src/api'

const requestMock = request as unknown as Mock

/** request 只收单个 options 对象 ⇒ 最近一次调用的入参 */
function lastRequest(): { url: string; method?: string; data?: Record<string, unknown> } {
  const calls = requestMock.mock.calls as unknown as [{ url: string; method?: string; data?: Record<string, unknown> }][]
  return calls[calls.length - 1][0]
}

/** 一次附件下载：返回 {fetchMock, savedName}（落盘文件名从 createObjectURL 那一步的 <a download> 上取） */
function attachmentResponse(res: { ok: boolean; status?: number; headers?: Record<string, string>; json?: unknown }) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok: res.ok,
    status: res.status ?? 200,
    headers: { get: (k: string) => res.headers?.[k] ?? null },
    blob: async () => new Blob(['DEVICE_ID=x\n'], { type: 'text/plain' }),
    json: async () => res.json,
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('T728 真实模式守卫', () => {
  beforeEach(() => {
    requestMock.mockReset()
    localStorage.setItem('admin_token', 'TOK728')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.removeItem('admin_token')
  })

  it('注册：POST /devices 后原样透传 deviceSecret（键名错一个字母就是不弹回显窗）', async () => {
    requestMock.mockResolvedValue({ deviceId: 'DEV-T728', model: 'PRS-ML05-RC', deviceSecret: 'a'.repeat(64) })
    const res = await registerDeviceApi({ deviceId: 'DEV-T728' })
    expect(lastRequest()).toMatchObject({ url: '/api/v1/devices', method: 'POST' })
    expect(res.deviceSecret).toBe('a'.repeat(64))
  })

  it('注册：幂等命中既有行时该键缺席，前端不得替后端补一个假值', async () => {
    requestMock.mockResolvedValue({ deviceId: 'DEV-T728', model: 'PRS-ML05-RC' })
    expect((await registerDeviceApi({ deviceId: 'DEV-T728' })).deviceSecret).toBeUndefined()
  })

  it('重生成 ⇒ POST /admin/devices/:id/secret-regenerate，明文与版本号只从本响应取', async () => {
    requestMock.mockResolvedValue({ deviceId: 'DEV-T728', secret: 'b'.repeat(64), secretVersion: 2 })
    const res = await regenerateDeviceSecretApi('DEV-T728')
    expect(lastRequest()).toEqual({
      url: '/api/v1/admin/devices/DEV-T728/secret-regenerate',
      method: 'POST',
      data: {},
    })
    expect(res.secret).toBe('b'.repeat(64))
    expect(res.secretVersion).toBe(2)
  })

  it('设备号里的保留字符必须转义（不许拼出第二条路由）', async () => {
    requestMock.mockResolvedValue({ deviceId: 'x', secret: 'y', secretVersion: 1 })
    await regenerateDeviceSecretApi('DEV/../A')
    expect(lastRequest().url).toBe('/api/v1/admin/devices/DEV%2F..%2FA/secret-regenerate')
  })

  it('单台下载 ⇒ GET 带 token，文件名取后端 Content-Disposition 的 <设备号>.env', async () => {
    const createUrl = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:t728')
    const fetchMock = attachmentResponse({
      ok: true,
      headers: { 'Content-Disposition': 'attachment; filename="DEV-T728.env"' },
    })
    await downloadDeviceSecretFileApi('DEV-T728')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/admin/devices/DEV-T728/secret-file')
    expect((fetchMock.mock.calls[0][1] as RequestInit).headers).toMatchObject({ Authorization: 'Bearer TOK728' })
    expect(createUrl).toHaveBeenCalledTimes(1)
    createUrl.mockRestore()
  })

  it('单台下载失败（后端回信封而非文件）⇒ 透出后端文案且一个文件都不落盘', async () => {
    const createUrl = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:t728')
    attachmentResponse({
      ok: false,
      status: 403,
      json: { code: 20403, message: '仅管理员可下载设备验签密钥', data: null },
    })
    await expect(downloadDeviceSecretFileApi('DEV-T728')).rejects.toThrow('仅管理员可下载设备验签密钥')
    expect(createUrl).not.toHaveBeenCalled()
    createUrl.mockRestore()
  })

  it('批量下载 ⇒ POST /admin/device-secrets/export，body 只有 deviceIds 这一枚勾选白名单，落 zip', async () => {
    const createUrl = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:t728')
    const fetchMock = attachmentResponse({
      ok: true,
      headers: { 'Content-Disposition': 'attachment; filename="device-secrets-20261020T120000Z.zip"' },
    })
    await exportDeviceSecretsApi(['DEV-A', 'DEV-B'])
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/admin/device-secrets/export')
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ deviceIds: ['DEV-A', 'DEV-B'] })
    expect(createUrl).toHaveBeenCalledTimes(1)
    createUrl.mockRestore()
  })

  it('落盘名：头里没有 filename 时回落各自的默认名，而不是 CSV 那个旧默认（共用函数被改坏在这里显形）', () => {
    expect(dispositionFilename('attachment; filename="DEV-T728.env"', 'fallback.env')).toBe('DEV-T728.env')
    expect(dispositionFilename(null, 'device-secrets.zip')).toBe('device-secrets.zip')
    expect(dispositionFilename('attachment')).toBe('abnormal-report.csv')
  })
})

// ===== mock 层语义：本地开发路径也得跑到「一次性」这件事 =====
describe('T728 mock 层与后端语义同形', () => {
  it('首次注册回 64 位 hex；同一设备号再注册（幂等）不回显', () => {
    const first = deviceMock.mockRegisterDevice({ deviceId: 'DEV-MOCK-T728' })
    expect(first.deviceSecret).toMatch(/^[0-9a-f]{64}$/)
    expect(deviceMock.mockRegisterDevice({ deviceId: 'DEV-MOCK-T728' }).deviceSecret).toBeUndefined()
  })

  it('两台设备不得拿到同一个密钥', () => {
    const a = deviceMock.mockRegisterDevice({ deviceId: 'DEV-MOCK-A1' }).deviceSecret
    const b = deviceMock.mockRegisterDevice({ deviceId: 'DEV-MOCK-B2' }).deviceSecret
    expect(a).toMatch(/^[0-9a-f]{64}$/)
    expect(b).toMatch(/^[0-9a-f]{64}$/)
    expect(a).not.toBe(b)
  })

  it('.env 文件体两行 + LF 行尾（逐字照 service.SecretEnvBody：CRLF 会让固件侧 sha256 对不上）', () => {
    const secret = deviceMock.mockRegisterDevice({ deviceId: 'DEV-MOCK-ENV' }).deviceSecret
    expect(deviceMock.mockDeviceSecretEnv('DEV-MOCK-ENV')).toBe(`DEVICE_ID=DEV-MOCK-ENV\nDEVICE_SECRET=${secret}\n`)
  })

  it('重生成：新值覆盖旧值、版本自增，旧明文此后取不到（= 旧 secret 即时失效）', () => {
    deviceMock.mockRegisterDevice({ deviceId: 'DEV-MOCK-REGEN' })
    const stale = deviceMock.mockDeviceSecretEnv('DEV-MOCK-REGEN')
    const r1 = deviceMock.mockRegenerateDeviceSecret('DEV-MOCK-REGEN')
    expect(r1.secretVersion).toBe(2)
    expect(r1.secret).toMatch(/^[0-9a-f]{64}$/)
    const after = deviceMock.mockDeviceSecretEnv('DEV-MOCK-REGEN')
    expect(after).toBe(`DEVICE_ID=DEV-MOCK-REGEN\nDEVICE_SECRET=${r1.secret}\n`)
    expect(after).not.toBe(stale)
    const r2 = deviceMock.mockRegenerateDeviceSecret('DEV-MOCK-REGEN')
    expect(r2.secretVersion).toBe(3)
    expect(r2.secret).not.toBe(r1.secret)
  })

  it('seed 那批设备没有可导出的密钥（镜像 T573 现网密文不成立那一支），批量逐台降级', () => {
    expect(() => deviceMock.mockDeviceSecretEnv('DEV-A3F312')).toThrow(/重新生成/)
    deviceMock.mockRegisterDevice({ deviceId: 'DEV-MOCK-OK1' })
    const { envs, skipped } = deviceMock.mockDeviceSecretEnvs(['DEV-MOCK-OK1', 'DEV-A3F312'])
    expect(envs).toHaveLength(1)
    expect(skipped).toEqual(['DEV-A3F312'])
  })
})

// ===== 页面接线（源码级）=====
describe('T728 设备页接线', () => {
  const page = read('apps/admin-web/src/pages/devices/index.vue')

  it('三个入口都存在且都被闸门包住，闸门源自 canAccessDeviceSecret(auth.role)', () => {
    expect(page).toContain('canAccessDeviceSecret(auth.role)')
    for (const label of ['下载密钥', '重新生成', '批量下载密钥']) {
      const re = new RegExp(`<el-button[^>]*v-if="canSecret"[^>]*>[^<]*${label}`)
      expect(re.test(page), `入口「${label}」没渲染出来或没被 canSecret 包住`).toBe(true)
    }
    // 多选列同样只对 admin 出现（非 admin 不该看到一列勾不动的复选框）
    expect(page).toMatch(/<el-table-column v-if="canSecret" type="selection"/)
  })

  it('详情抽屉里也有这一对入口（卡面验收 2 的落点写的是「设备详情 → 下载 secret 文件」）', () => {
    const drawer = page.slice(page.indexOf('<el-drawer'))
    expect(drawer).toContain('v-if="canSecret && detail"')
    expect(drawer).toContain('downloadSecret(detail.deviceId)')
    expect(drawer).toContain('askRegenerateSecret(detail.deviceId)')
  })

  it('明文只有一个消费点（一次性弹窗），列表与详情不投影密钥', () => {
    expect(page.match(/\{\{ secretPanel\.secret \}\}/g)).toHaveLength(1)
    expect(page).not.toMatch(/row\.deviceSecret|detail\.deviceSecret|row\.secret/)
    expect(page).toContain('验签密钥仅此次展示，关闭后本页不可再看')
  })

  it('幂等命中那一发不开回显窗（否则等于把一个没落库的串摆上去让人烧录）', () => {
    const branch = page.indexOf('if (res.deviceSecret) {')
    expect(branch).toBeGreaterThan(0)
    const body = page.slice(branch, branch + 400)
    expect(body).toContain('showOneTimeSecret(')
    expect(body).toContain('密钥未变更')
  })

  it('重生成的确认排在发枪之前，且确认文案写明旧密钥即刻失效', () => {
    const confirmAt = page.indexOf('await ElMessageBox.confirm(')
    const callAt = page.indexOf('await regenerateDeviceSecretApi(')
    expect(confirmAt).toBeGreaterThan(0)
    expect(callAt).toBeGreaterThan(confirmAt)
    const between = page.slice(confirmAt, callAt)
    expect(between).toContain('return // 取消或关掉弹层都不发枪')
    expect(between).toContain('旧密钥即刻失效')
  })

  it('批量超限与空勾选都在本地挡住，且用的是同源常量而不是字面量 50', () => {
    expect(page).toContain('ids.length > MAX_SECRET_EXPORT_BATCH')
    expect(page).not.toMatch(/ids\.length > 50\b/)
    const exportAt = page.indexOf('await exportDeviceSecretsApi(ids)')
    expect(exportAt).toBeGreaterThan(0)
    expect(page.slice(page.indexOf('async function exportSelectedSecrets'), exportAt)).toContain('ids.length === 0')
  })

  it('下载走服务端端点而不是在浏览器里拼文件（每次取用都得留痕）', () => {
    expect(page).toContain('await downloadDeviceSecretFileApi(deviceId)')
    expect(page).not.toContain('new Blob(')
  })
})
