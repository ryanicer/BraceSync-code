// T716：配网成功页的面积缺失分支（contact_area_cm2 === null）必须逐字渲染 PM 定稿句。
// SFC 不在这里挂载（uni-app 组件依赖运行时），源码级咬住「判定 → 传参 → 渲染常量」这条链，
// 词句本体由 shared-utils 单测与 e2e 共同兜底。
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { AREA_MISSING_HINT } from '@bracesync/shared-utils'

const appRoot = join(dirname(fileURLToPath(import.meta.url)), '../..')
const pageSrc = readFileSync(join(appRoot, 'src/pages/wifi-setup/index.vue'), 'utf8')
const successSrc = readFileSync(join(appRoot, 'src/components/wifi-setup/WifiSuccess.vue'), 'utf8')

describe('T716 wifi-setup 面积缺失提示', () => {
  it('PM 定稿句逐字（含管理后台去处）', () => {
    expect(AREA_MISSING_HINT).toBe('受压面积未配置，请联系运维人员在管理后台【设备管理 → 受压面积】完成配置')
  })

  it('null 分支只在明确为 null 时成立（undefined 不误报）', () => {
    expect(pageSrc).toContain('deviceStore.currentDevice?.contactAreaCm2 === null')
    expect(pageSrc).not.toContain('contactAreaCm2 == null')
  })

  it('判定结果传进成功页，模板渲染同一常量', () => {
    expect(pageSrc).toContain(':area-missing="areaMissing"')
    expect(successSrc).toContain("areaMissing?: boolean")
    expect(successSrc).toContain('v-if="areaMissing"')
    expect(successSrc).toContain('{{ AREA_MISSING_HINT }}')
  })
})
