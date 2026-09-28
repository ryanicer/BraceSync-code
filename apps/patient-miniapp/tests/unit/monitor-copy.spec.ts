/**
 * T444 患者端实现侧差异收口 —— 纯层判据 + 源码级接线门禁。
 *
 * 来源清单 docs/tasks/peter/T417-对照清单-患者端.md 的 M-1 / M-2 / P-2 / L-7。
 * 抽成纯层（monitor-copy.ts / profile-format.ts）的用理由同 T443：patient-miniapp 的
 * vitest 是 environment:'node' 且不挂 VTU，模板里的三元拼接在本包测不到，判据只能落在这里；
 * 再用源码级门禁（读 .vue/.json 原文）证明页面确实引了这些函数/常量，不是「纯层绿、页面没接」。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import {
  HEATMAP_DETAIL_PLACEHOLDER,
  heatmapDetailLine,
  trendSectionTitle,
} from '../../src/utils/monitor-copy'
import { CONTACT_GROUP_LABEL } from '../../src/utils/profile-format'

const here = dirname(fileURLToPath(import.meta.url))
let pkgRoot = here
while (pkgRoot !== dirname(pkgRoot) && !existsSync(join(pkgRoot, 'src/pages.json'))) {
  pkgRoot = dirname(pkgRoot)
}
const readSrc = (rel: string) => readFileSync(join(pkgRoot, rel), 'utf8')

describe('M-1 heatmapDetailLine —— 未点选出稿面默认句，点选后才出数值行', () => {
  it('无点位（null/undefined/空串）一律回稿面 monitor.html:102 默认句', () => {
    expect(HEATMAP_DETAIL_PLACEHOLDER).toBe('点击网格查看详情')
    expect(heatmapDetailLine(null, 42.18, 60)).toBe(HEATMAP_DETAIL_PLACEHOLDER)
    expect(heatmapDetailLine(undefined, 42.18, 60)).toBe(HEATMAP_DETAIL_PLACEHOLDER)
    expect(heatmapDetailLine('', 42.18, 60)).toBe(HEATMAP_DETAIL_PLACEHOLDER)
  })

  it('有点位时为「点位 · 数值N · 阈值上限 上限N」', () => {
    expect(heatmapDetailLine('P12', 42.18, 60)).toBe('P12 · 42.18N · 阈值上限 60N')
  })

  it('点位有值但压力为负/NaN 时按 formatPressureValue 口径（归零 / --），不复现 0.00 残缺', () => {
    expect(heatmapDetailLine('P03', -5, 60)).toBe('P03 · 0.00N · 阈值上限 60N')
    expect(heatmapDetailLine('P03', null, 60)).toBe('P03 · --N · 阈值上限 60N')
  })
})

describe('M-2 trendSectionTitle —— 空点位不得留前置分隔符「 · 今日压力趋势」', () => {
  it('有点位带前缀', () => {
    expect(trendSectionTitle('P12', '今日')).toBe('P12 · 今日压力趋势')
    expect(trendSectionTitle('P01', '本周')).toBe('P01 · 本周压力趋势')
  })

  it('无点位（首帧未落/失败/无数据）丢掉「点位 ·」段', () => {
    // 修前现场（H5 实测）渲染成「· 今日压力趋势」——分隔符前置、点位前缀空
    expect(trendSectionTitle(undefined, '今日')).toBe('今日压力趋势')
    expect(trendSectionTitle(null, '本月')).toBe('本月压力趋势')
    expect(trendSectionTitle('', '今日')).not.toMatch(/^\s*·/)
  })
})

describe('M-1/M-2 源码级接线：页面确实引上述函数，而非只在纯层绿', () => {
  const heatmap = readSrc('src/components/PressureHeatmap.vue')
  const monitor = readSrc('src/pages/monitor/index.vue')

  it('PressureHeatmap 引 heatmapDetailLine 并用 selectedByUser 区分点选前后', () => {
    expect(heatmap).toMatch(/import \{ heatmapDetailLine \} from '\.\.\/utils\/monitor-copy'/)
    expect(heatmap).toContain('selectedByUser')
    expect(heatmap).toMatch(/if \(!props\.selectedByUser\) return heatmapDetailLine\(null/)
    // 模板不再直接拼点位/数值（改走 detailLine）
    expect(heatmap).toContain('{{ detailLine }}')
    expect(heatmap).not.toContain('activePoint?.pointId }} · {{ formatPressureValue')
  })

  it('monitor 页引 trendSectionTitle 且传 selected-by-user、标题走 trendTitle', () => {
    expect(monitor).toMatch(/import \{ trendSectionTitle \} from '\.\.\/\.\.\/utils\/monitor-copy'/)
    expect(monitor).toContain(':selected-by-user="userTappedPoint"')
    expect(monitor).toContain('{{ trendTitle }}')
    // 修前那种空点位会留前置分隔符的写法必须消失
    expect(monitor).not.toContain("{{ activePoint ? activePoint.pointId : '' }} · ")
    expect(monitor).toContain('userTappedPoint.value = true')
    expect(monitor).toContain('userTappedPoint.value = false')
  })
})

describe('P-2 profile 编辑弹层 —— 组标题跟稿面「联系方式」，去掉重字组标题与整句标题', () => {
  const profile = readSrc('src/pages/profile/index.vue')

  it('常量逐字取稿面 profile.html:133', () => {
    expect(CONTACT_GROUP_LABEL).toBe('联系方式')
  })

  it('弹层用 CONTACT_GROUP_LABEL 当组标题，引自 profile-format', () => {
    expect(profile).toMatch(/import \{[^}]*CONTACT_GROUP_LABEL[^}]*\} from '\.\.\/\.\.\/utils\/profile-format'/)
    expect(profile).toContain('<text class="form-section-label">{{ CONTACT_GROUP_LABEL }}</text>')
  })

  it('修前的两处缺陷写法必须消失：整句当组标题、组标题与字段标签都写「紧急联系人」', () => {
    expect(profile).not.toContain('<text class="form-section-label">紧急联系人</text>')
    expect(profile).not.toContain('<text class="form-section-label">手机号由微信授权提供')
    // 字段标签「紧急联系人」仍在（作为 form-label），只是不再当组标题
    expect(profile).toContain('<text class="form-label">紧急联系人</text>')
  })

  it('弹层内 form-section-label 只剩一处（联系方式），不再有两处重字/串位', () => {
    const labels = profile.match(/class="form-section-label"/g) ?? []
    expect(labels.length).toBe(1)
  })
})

describe('L-7 绑定冲突形态：实现侧只核导航标题与页内标题一致性（不做整页↔弹窗改造）', () => {
  const pages = JSON.parse(readSrc('src/pages.json'))
  const titleOf = (path: string) =>
    pages.pages.find((p: { path: string }) => p.path === path)?.style?.navigationBarTitleText

  it('bind / no-match / conflict 三页的 pages.json 导航标题与页内 nav-title 逐页一致', () => {
    const pairs: Array<[string, string]> = [
      ['pages/login/bind', 'bind.vue'],
      ['pages/login/no-match', 'no-match.vue'],
      ['pages/login/conflict', 'conflict.vue'],
    ]
    for (const [path, file] of pairs) {
      const navTitle = titleOf(path)
      const page = readSrc(`src/pages/login/${file}`)
      const inner = page.match(/class="nav-title">([^<]+)</)
      expect(navTitle, `${path} 应有 navigationBarTitleText`).toBeTruthy()
      expect(inner, `${file} 应有页内 nav-title`).not.toBeNull()
      expect(inner![1]).toBe(navTitle)
    }
  })
})

describe('命名口径：本批三个改动文件不含品牌名串（全线统一「矫智通」归 T442 同批）', () => {
  const files = [
    'src/pages/monitor/index.vue',
    'src/components/PressureHeatmap.vue',
    'src/pages/profile/index.vue',
    'src/utils/monitor-copy.ts',
    'src/utils/profile-format.ts',
  ]
  it('改动文件内既无旧名「矫治通」也无误入的新品牌串（本批刻意不带品牌名）', () => {
    for (const f of files) {
      const src = readSrc(f)
      expect(src, `${f} 不应出现旧品牌名`).not.toContain('矫治通')
      expect(src, `${f} 本批不新增品牌名`).not.toContain('矫智通')
    }
  })
})
