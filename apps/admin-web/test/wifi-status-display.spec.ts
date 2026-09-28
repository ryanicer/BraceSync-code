// T450 第四件：管理端安装记录页 WiFi 显示按 T447 四值词形表对齐。
// 依据：docs/tasks/winner/T447-WiFi状态唯一词形表.md §一（四档标准词）与 §五-5（admin 稿面映射只有两键）。
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import {
  WIFI_STATUS_VALUES,
  wifiStatusLabel,
  wifiStatusTagType,
  type WifiStatus,
} from '../src/utils/wifiStatus'

// 路径解析用 fileURLToPath(import.meta.url)：new URL(rel, import.meta.url) 的字面量形式会被
// Vite 编译期改写成 http 资源地址，运行时拿不到盘路径（同 medical-titles.spec.ts / base-mount-contract.spec.ts）。
const read = (rel: string): string =>
  readFileSync(join(dirname(fileURLToPath(import.meta.url)), rel), 'utf8')

const sharedTypesSrc = read('../../../packages/shared-types/src/index.ts')
const pageSrc = read('../src/pages/install-records/index.vue')

// Record<WifiStatus, string> 让「少一档」在 vue-tsc 阶段就判红，「多一档」由下面 lengths 那条运行时兜住。
const EXPECTED_LABEL: Record<WifiStatus, string> = {
  connected: '已连接',
  failed: '连接失败',
  unconfigured: '未配置',
  skipped: '已跳过配网',
}
const EXPECTED_TAG: Record<WifiStatus, 'success' | 'danger' | 'info'> = {
  connected: 'success',
  failed: 'danger',
  unconfigured: 'info',
  skipped: 'info',
}

describe('WiFi 四档标准词（纯层，逐字对齐 T447 词形表）', () => {
  it('四档各一个词、各一个徽标颜色，字形与 §一 一致', () => {
    const values = Object.keys(EXPECTED_LABEL) as WifiStatus[]
    expect(WIFI_STATUS_VALUES).toHaveLength(values.length)
    for (const value of values) {
      expect(WIFI_STATUS_VALUES).toContain(value)
      expect(wifiStatusLabel(value)).toBe(EXPECTED_LABEL[value])
      expect(wifiStatusTagType(value)).toBe(EXPECTED_TAG[value])
    }
  })

  it('词表里不存在「未连接」（改前四值塌两词就是它）', () => {
    for (const value of WIFI_STATUS_VALUES) expect(wifiStatusLabel(value)).not.toBe('未连接')
  })

  // 与 services/device-service/internal/model/wifi_status_t447_test.go 的同源判据同一思路（那边比
  // Go 常量／迁移 CHECK／shared-types 联合类型，这里比 shared-types 联合类型／显示词表）：
  // 值集再扩一档而显示层没跟上时，本条先判红，不必等 e2e 跑到那一档的 mock 行。
  it('词表的键集与 shared-types 的 wifiStatus 联合类型同集', () => {
    const decl = sharedTypesSrc.match(/wifiStatus:\s*([^;\n]+);/)
    expect(decl, 'shared-types 里没匹配到 wifiStatus 联合类型声明').not.toBeNull()
    const values = (decl as RegExpMatchArray)[1]
      .split('|')
      .map((s) => s.trim().replace(/['"]/g, ''))
      .filter(Boolean)
    expect(values.length, '联合类型只解析出一项，解析本身不可信').toBeGreaterThan(1)
    expect([...values].sort()).toEqual([...WIFI_STATUS_VALUES].sort())
  })
})

describe('安装记录页两处显示同源（源码契约）', () => {
  it('列表列与详情抽屉都走查表函数，颜色也走同一个 util', () => {
    expect(pageSrc).toContain('wifiStatusLabel(row.wifiStatus)')
    expect(pageSrc).toContain('wifiStatusLabel(detail.wifiStatus)')
    expect(pageSrc).toContain('wifiStatusTagType(row.wifiStatus)')
    expect(pageSrc).toContain("from '../../utils/wifiStatus'")
  })

  it('整页不出现「未连接」，也不再留 wifiStatus 的相等判据', () => {
    expect(pageSrc).not.toContain('未连接')
    expect(pageSrc).not.toMatch(/wifiStatus\s*===\s*'connected'/)
  })
})
