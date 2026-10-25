// T716 建设备「受压面积」必填校验 + 详情抽屉改面积（admin-web 侧）
//
// 判据按「会怎样坏」排：
//  1 解析器真值表：空/非数字/0/负/三位小数必须在发请求前拦下；合法值原样返回，不许把 " 0.64 " 当 NaN。
//  2 mock 与后端同形：建设备落 T715 默认 0.64；T508 整值覆盖拒绝非正数；幂等命中不覆盖既有面积。
//  3 真实模式线形守卫：PUT /devices/:id/contact-area、请求体键名 contactAreaCm2、设备号必须转义。
//  4 页面接线（源码级）：必填项 + parseContactArea 在 registerDeviceApi 之前调用、失败提示文案在页面上。
//     SFC 不在 vitest 里挂载（取舍同 test/device-secret-t728.spec.ts），浏览器形状由 e2e 负责。
import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { parseContactArea } from '../src/utils/deviceContactArea'
import { AREA_MISSING_HINT } from '@bracesync/shared-utils'
import * as deviceMock from '../src/mock/devices'

const appRoot = join(dirname(fileURLToPath(import.meta.url)), '..')
const pageSrc = readFileSync(join(appRoot, 'src/pages/devices/index.vue'), 'utf8')

describe('T716 parseContactArea 真值表', () => {
  it('空值与纯空白 ⇒ 请输入受压面积', () => {
    for (const raw of ['', '   ']) {
      expect(parseContactArea(raw)).toEqual({ ok: false, message: '请输入受压面积' })
    }
  })

  it('非数字/科学计数/逗号小数 ⇒ 请输入有效的受压面积', () => {
    for (const raw of ['abc', '0.6cm', '1,25', '1e2', '.64', '0.64.0']) {
      expect(parseContactArea(raw)).toEqual({ ok: false, message: '请输入有效的受压面积' })
    }
  })

  it('0 与负数 ⇒ 受压面积必须大于 0', () => {
    for (const raw of ['0', '0.00', '-1', '-0.01']) {
      expect(parseContactArea(raw)).toEqual({ ok: false, message: '受压面积必须大于 0' })
    }
  })

  it('超过两位小数 ⇒ 最多两位小数', () => {
    expect(parseContactArea('1.234')).toEqual({ ok: false, message: '最多两位小数' })
  })

  it('合法值（含首尾空白与一位小数）原样转 number', () => {
    expect(parseContactArea('0.64')).toEqual({ ok: true, value: 0.64 })
    expect(parseContactArea(' 2 ')).toEqual({ ok: true, value: 2 })
    expect(parseContactArea('12.5')).toEqual({ ok: true, value: 12.5 })
  })
})

describe('T716 mock 与后端语义同形', () => {
  it('新建设备默认面积 0.64（T715 服务端默认值）', () => {
    const dev = deviceMock.mockRegisterDevice({ deviceId: 'T716-DEFAULT' })
    expect(dev.contactAreaCm2).toBe(0.64)
  })

  it('T508 整值覆盖：写入后详情读到新值', () => {
    deviceMock.mockRegisterDevice({ deviceId: 'T716-SET' })
    deviceMock.mockSetDeviceContactArea('T716-SET', 1.25)
    expect(deviceMock.mockDeviceDetail('T716-SET').contactAreaCm2).toBe(1.25)
  })

  it('非正数与不存在的设备一律拒绝（与 device-service 校验同判据）', () => {
    expect(() => deviceMock.mockSetDeviceContactArea('DEV-A3F312', 0)).toThrow('大于 0')
    expect(() => deviceMock.mockSetDeviceContactArea('DEV-A3F312', -1)).toThrow('大于 0')
    expect(() => deviceMock.mockSetDeviceContactArea('T716-MISSING', 1)).toThrow('设备不存在')
  })

  it('幂等注册不覆盖既有面积', () => {
    deviceMock.mockRegisterDevice({ deviceId: 'T716-IDEMPOTENT' })
    deviceMock.mockSetDeviceContactArea('T716-IDEMPOTENT', 2.5)
    const again = deviceMock.mockRegisterDevice({ deviceId: 'T716-IDEMPOTENT' })
    expect(again.deviceSecret).toBeUndefined()
    expect(again.contactAreaCm2).toBe(2.5)
  })
})

// ===== 真实模式线形守卫（USE_MOCK=false）=====
vi.mock('../src/utils/request', () => {
  const request = vi.fn()
  return { request, USE_MOCK: false, API_BASE_URL: '' }
})
import { request } from '../src/utils/request'
import { setDeviceContactAreaApi } from '../src/api'

const requestMock = request as unknown as Mock

function lastRequest(): { url: string; method?: string; data?: Record<string, unknown> } {
  const calls = requestMock.mock.calls as unknown as [{ url: string; method?: string; data?: Record<string, unknown> }][]
  return calls[calls.length - 1][0]
}

describe('T716 真实模式守卫', () => {
  beforeEach(() => {
    requestMock.mockReset()
    requestMock.mockResolvedValue(null)
    localStorage.setItem('admin_token', 'TOK716')
  })
  afterEach(() => {
    localStorage.removeItem('admin_token')
  })

  it('PUT /devices/:id/contact-area，请求体键名 contactAreaCm2（写错后端 400/字段读不到）', async () => {
    await setDeviceContactAreaApi('DEV-A3F312', 0.64)
    expect(lastRequest()).toEqual({
      url: '/api/v1/devices/DEV-A3F312/contact-area',
      method: 'PUT',
      data: { contactAreaCm2: 0.64 },
    })
  })

  it('设备号里的保留字符必须转义（不许拼出第二条路由）', async () => {
    await setDeviceContactAreaApi('DEV/../A', 1)
    expect(lastRequest().url).toBe('/api/v1/devices/DEV%2F..%2FA/contact-area')
  })
})

describe('T716 页面接线（源码级）', () => {
  it('注册弹窗有受压面积必填项，且校验先于发请求', () => {
    expect(pageSrc).toContain('label="受压面积" required')
    expect(pageSrc.indexOf('parseContactArea(registerForm.value.area)')).toBeGreaterThan(-1)
    expect(pageSrc.indexOf('parseContactArea(registerForm.value.area)')).toBeLessThan(
      pageSrc.indexOf('await registerDeviceApi('),
    )
  })

  it('面积写入失败有明确提示，幂等命中不覆盖并告知面积未变更', () => {
    expect(pageSrc).toContain('设备已注册，受压面积未保存，请重试')
    expect(pageSrc).toContain('该设备号已注册，验签密钥未变更，面积未变更')
    // 面积写入必须挂在新建分支 if (res.deviceSecret) 内，否则幂等重注册会把既有面积静默改掉
    const putAt = pageSrc.indexOf('await setDeviceContactAreaApi(deviceId, area.value)')
    const guardAt = pageSrc.lastIndexOf('if (res.deviceSecret) {', putAt)
    expect(guardAt).toBeGreaterThan(-1)
  })

  it('详情抽屉有受压面积行、修改入口与完整配置路径', () => {
    expect(pageSrc).toContain('label="受压面积"')
    expect(pageSrc).toContain('label="配置入口"')
    expect(pageSrc).toContain('设备管理 → 受压面积')
    expect(pageSrc).toContain('openAreaDialog')
    expect(pageSrc).toContain('submitArea')
  })
})

// ===== T716 两端互指闭环（PM 定稿句 ↔ admin 标签/路径）=====
describe('T716 两端互指自证', () => {
  const repoRoot = join(appRoot, '../..')
  const patientSuccessSrc = readFileSync(
    join(repoRoot, 'apps/patient-miniapp/src/components/wifi-setup/WifiSuccess.vue'),
    'utf8',
  )
  const patientWifiPageSrc = readFileSync(
    join(repoRoot, 'apps/patient-miniapp/src/pages/wifi-setup/index.vue'),
    'utf8',
  )

  it('患者端提示逐字含后台去处「设备管理 → 受压面积」', () => {
    expect(AREA_MISSING_HINT).toBe('受压面积未配置，请联系运维人员在管理后台【设备管理 → 受压面积】完成配置')
    expect(AREA_MISSING_HINT).toContain('管理后台【设备管理 → 受压面积】')
  })

  it('患者端 wifi-setup 有 contact_area 为 null 的分支，且渲染同一常量', () => {
    expect(patientWifiPageSrc).toContain('deviceStore.currentDevice?.contactAreaCm2 === null')
    expect(patientWifiPageSrc).toContain(':area-missing="areaMissing"')
    expect(patientSuccessSrc).toContain('v-if="areaMissing"')
    expect(patientSuccessSrc).toContain('{{ AREA_MISSING_HINT }}')
  })

  it('admin-web 标签同名「受压面积」，详情抽屉显示完整路径', () => {
    expect(pageSrc).toContain('label="受压面积"')
    expect(pageSrc).toContain('设备管理 → 受压面积')
  })
})
