/**
 * T513 患者端实时监测页双单位（N / kPa）—— 纯层判据 + 源码级接线门禁。
 *
 * 为什么不 mount：本包 vitest 是 `environment: 'node'` 且不挂 VTU（同 T443/T444 的抽层理由），
 * 模板里的三元拼接在本包测不到 ⇒ 判据落纯层，再用源码级门禁证明页面/组件真的引了这些函数，
 * 不是「纯层绿、页面没接」。真机（微信小程序）腿本包无法覆盖，交件里按「未验」登记。
 *
 * 依据：PRD §7A.2.1 四.2（默认档 N、现状读数逐字不变）／四.9②（随档切换面：hero 主数值、
 * 单元格数字、选中点详情行）／四.9③（配色与判档恒按 N）／四.9④（不可换算 fail-closed）；
 * 五.3（禁退化 0、禁沿用上一帧、禁前端补默认面积）；派发单 §三 选型 A（kPa 取同快照下发值，
 * 前端不得重复实现换算）；裁定 e（档位记忆 = 本地持久化，不动 patient_preferences）。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { AREA_MISSING_HINT, unitNumberText, kpaNumberText } from '@bracesync/shared-utils'
import { HEATMAP_DETAIL_PLACEHOLDER, heatmapDetailLine } from '../../src/utils/monitor-copy'

const here = dirname(fileURLToPath(import.meta.url))
let pkgRoot = here
while (pkgRoot !== dirname(pkgRoot) && !existsSync(join(pkgRoot, 'src/pages.json'))) {
  pkgRoot = dirname(pkgRoot)
}
const readSrc = (rel: string) => readFileSync(join(pkgRoot, rel), 'utf8')

const page = readSrc('src/pages/monitor/index.vue')
const grid = readSrc('src/components/SensorGrid.vue')
const heatmap = readSrc('src/components/PressureHeatmap.vue')
const unitPref = readSrc('src/utils/unit-pref.ts')
const copy = readSrc('src/utils/monitor-copy.ts')

describe('M-detail 双档：详情行随档改「怎么写」，不改判档', () => {
  it('N 档逐字保持 T444 现状形态（PRD 四.2：默认档读数不变）', () => {
    expect(heatmapDetailLine('P12', 42.18, 60)).toBe('P12 · 42.18N · 阈值上限 60N')
    expect(heatmapDetailLine('P12', 42.18, 60, { unit: 'N' })).toBe('P12 · 42.18N · 阈值上限 60N')
    expect(heatmapDetailLine('P12', 42.18, 60, { unit: 'N', pressureKpa: 66, elevatedMaxKpa: 94 })).toBe(
      'P12 · 42.18N · 阈值上限 60N',
    )
  })

  it('kPa 档：数值段与阈值段同时换成后端下发值，字母挂 kPa', () => {
    expect(
      heatmapDetailLine('P12', 42.18, 60, { unit: 'kPa', pressureKpa: 66, elevatedMaxKpa: 94 }),
    ).toBe('P12 · 66kPa · 阈值上限 94kPa')
  })

  it('kPa 档不可换算（派生值 null）：数值段 --kPa、阈值段 --（稿面 monitor.html:312 不挂字母）', () => {
    expect(heatmapDetailLine('P12', 42.18, 60, { unit: 'kPa', pressureKpa: null, elevatedMaxKpa: null })).toBe(
      'P12 · --kPa · 阈值上限 --',
    )
  })

  it('反证：kPa 档缺派生值时不得兜底回 N 文本（否则两档读出同一个数）', () => {
    const t = heatmapDetailLine('P12', 42.18, 60, { unit: 'kPa' })
    expect(t).not.toContain('42.18N')
    expect(t).not.toContain('60N')
    expect(t).toContain('--')
  })

  it('0 kPa 是真值不是空位（禁退化成 --；后端 0 N 换算后仍 0）', () => {
    expect(heatmapDetailLine('P12', 0, 60, { unit: 'kPa', pressureKpa: 0, elevatedMaxKpa: 94 })).toBe(
      'P12 · 0kPa · 阈值上限 94kPa',
    )
    expect(kpaNumberText(0)).toBe('0')
  })

  it('未点选时仍是稿面默认句，档位不改这一句', () => {
    for (const u of ['N', 'kPa'] as const) {
      expect(heatmapDetailLine(null, null, 60, { unit: u })).toBe(HEATMAP_DETAIL_PLACEHOLDER)
    }
    expect(HEATMAP_DETAIL_PLACEHOLDER).toBe('点击网格查看详情')
  })
})

describe('单元格与 hero：数字走 shared-utils 单函数，患者端不挂字母', () => {
  it('纯层等价：N 档逐字不动、kPa 档只读派生值、不可换算出 --', () => {
    expect(unitNumberText('N', '42.18', 66)).toBe('42.18')
    expect(unitNumberText('kPa', '42.18', 66)).toBe('66')
    expect(unitNumberText('kPa', '42.18', null)).toBe('--')
    expect(unitNumberText('kPa', '--', null)).toBe('--')
  })

  it('SensorGrid 的 cellText 引 unitNumberText 并透传格子里的 kpa（不在组件内换算）', () => {
    expect(grid).toMatch(/import \{ unitNumberText.*\} from '@bracesync\/shared-utils'/)
    expect(grid).toMatch(
      /unitNumberText\(props\.unit, formatPressureValue\(cell\?\.value\), cell\?\.kpa\)/,
    )
    expect(grid).toMatch(/kpa\?: number \| null/)
  })

  it('hero 数值走 heroValue→unitNumberText，单位字母由 {{ unit }} 提供（不再硬写 N）', () => {
    expect(page).toMatch(/<text class="hero-unit">\{\{ unit \}\}<\/text>/)
    expect(page).not.toMatch(/<text class="hero-unit">N<\/text>/)
    expect(page).toMatch(
      /return unitNumberText\(unit\.value, formatPressureValue\(pt\.pressureValue\), kpaByPoint\.value\[pt\.pointId\] \?\? null\)/,
    )
    // 无点位时按档回 --（两档同形，不出现「kPa 档漏字母」的半句话）
    expect(page).toMatch(/if \(!pt\) return unitNumberText\(unit\.value, '--', null\)/)
  })

  it('副文案随配置派生、只在 N 档出现（T601：不再写死 20-60N，配置注入值出现在文案里）', () => {
    expect(page).toMatch(/v-show="heroRangeHintVisible\(unit\) && heroRange"/)
    expect(page).toMatch(/import \{[^}]*heroRangeHintVisible[^}]*heroRangeText[^}]*\} from '@bracesync\/shared-utils'/)
    // 渲染的是插值而不是字面量；页面源码不得再出现旧写死值
    expect(page).toContain('<text class="meta-text">{{ heroRange }}</text>')
    expect(page).not.toContain('20-60N 正常范围')
    // 派生链：文案来自 heroRangeText，两个边界取自同快照下发的配置字段
    expect(page).toMatch(/heroRangeText\(pressureRangeLow\.value, pressureRangeHigh\.value\)/)
    expect(page).toMatch(/snap\?\.pressureLowN/)
    expect(page).toMatch(/snap\?\.pressureHighN/)
  })
})

describe('裁定 e：档位记忆 = 本地持久化（uni storage），不发请求', () => {
  const KEY = 'bracesync_monitor_unit'
  let store: Record<string, string>
  let storageGet: ReturnType<typeof vi.fn>
  let storageSet: ReturnType<typeof vi.fn>

  beforeEach(() => {
    store = {}
    storageGet = vi.fn((k: string) => store[k] ?? '')
    storageSet = vi.fn((k: string, v: string) => {
      store[k] = v
    })
    ;(globalThis as Record<string, unknown>).uni = {
      getStorageSync: storageGet,
      setStorageSync: storageSet,
    }
  })

  afterEach(() => {
    delete (globalThis as Record<string, unknown>).uni
  })

  async function fresh() {
    vi.resetModules()
    return import('../../src/utils/unit-pref')
  }

  it('存储值 → 起版档：kPa 认、脏值与空值回落 N', async () => {
    const { readStoredUnit } = await fresh()
    store[KEY] = 'kPa'
    expect(readStoredUnit()).toBe('kPa')
    store[KEY] = 'KG'
    expect(readStoredUnit()).toBe('N')
    store[KEY] = ''
    expect(readStoredUnit()).toBe('N')
  })

  it('persistUnit 写同一个键；读侧立刻能拿回来（切档后重进页恢复的依据）', async () => {
    const { persistUnit, readStoredUnit } = await fresh()
    persistUnit('kPa')
    expect(storageSet).toHaveBeenCalledWith(KEY, 'kPa')
    expect(readStoredUnit()).toBe('kPa')
  })

  it('存储整体不可用（uni 缺失 / 抛错）时不炸，按默认档 N 起版', async () => {
    const { readStoredUnit, persistUnit } = await fresh()
    ;(globalThis as Record<string, unknown>).uni = {
      getStorageSync: () => {
        throw new Error('storage disabled')
      },
      setStorageSync: () => {
        throw new Error('storage disabled')
      },
    }
    expect(readStoredUnit()).toBe('N')
    expect(() => persistUnit('kPa')).not.toThrow()
  })

  it('反证：uni 完全不存在时也不抛（真机首帧前读取的兜底路径）', async () => {
    delete (globalThis as Record<string, unknown>).uni
    const { readStoredUnit } = await fresh()
    expect(readStoredUnit()).toBe('N')
  })

  it('持久化层只碰本地存储：无 await、无网络原语、不引 api 层', () => {
    expect(unitPref).toMatch(/uni\.setStorageSync/)
    expect(unitPref).toMatch(/uni\.getStorageSync/)
    expect(unitPref).toContain(KEY)
    expect(unitPref).not.toMatch(/\bawait\b|fetch\(|XMLHttpRequest|request\(|\.post\(|\.put\(/)
    expect(unitPref).not.toMatch(/from ['"][^'"]*api['"]/)
  })
})

describe('页面接线：二档分段、取数只读快照下发字段', () => {
  it('标题行右侧渲染 N / kPa 二档，按钮序与词表同序，激活态跟 unit', () => {
    expect(page).toMatch(/<view class="segmented unit-seg">/)
    expect(page).toMatch(/v-for="u in PRESSURE_UNITS"/)
    expect(page).toMatch(/\{ 'seg-active': unit === u \}/)
    expect(page).toMatch(/@click="switchUnit\(u\)"/)
    expect(page).toMatch(/import \{[^}]*PRESSURE_UNITS[^}]*\} from '@bracesync\/shared-utils'/)
  })

  it('起版档取本地记忆（readStoredUnit），不是硬编码 N 也不是读 URL 参数', () => {
    expect(page).toMatch(/const unit = ref<PressureUnit>\(readStoredUnit\(\)\)/)
    expect(page).toMatch(/import \{ readStoredUnit, persistUnit \} from '\.\.\/\.\.\/utils\/unit-pref'/)
    expect(page).not.toMatch(/uni\.getStorageSync/) // 走 unit-pref 单点，页面不自己摸存储
    expect(page).not.toMatch(/options\.unit|getCurrentOpenerInfo/) // 稿面 ?unit= 只是演示钩子
  })

  it('页面自身把档位透传给 PressureHeatmap（绑定形态，不许塌成静态字面量）', () => {
    // 变异 C 实测出来的缺口：上一版只在 PressureHeatmap.vue 里查 :unit="unit"（那是组件往下
    // 传 SensorGrid 的那一跳），把页面里的 :unit="unit" 改成 unit="N" 时 30 条全绿 ——
    // 页面→组件这一跳没门禁，「切了档格子不跟着换」这种缺陷本地判不出红。
    const tag = page.match(/<PressureHeatmap[\s\S]*?\/>/)?.[0]
    expect(tag, '页面应渲染 PressureHeatmap 标签').toBeTruthy()
    expect(tag).toMatch(/:unit="unit"/)
    expect(tag).not.toMatch(/\bunit="(N|kPa)"/)
  })

  it('kPa 来自同一快照响应（pressureHeatmap[].pressureKpa 与 heatmapMaxKpa）', () => {
    expect(page).toMatch(/for \(const hp of snap\?\.pressureHeatmap \?\? \[\]\) byPoint\[hp\.pointId\] = hp\.pressureKpa \?\? null/)
    expect(page).toMatch(/heatmapMaxKpa\.value = snap\?\.heatmapMaxKpa \?\? null/)
    expect(page).toMatch(/:kpa-by-point="kpaByPoint"/)
    expect(page).toMatch(/:heatmap-max-kpa="heatmapMaxKpa"/)
  })

  it('五.3 禁沿用上一帧：取数失败分支把两个派生值清空', () => {
    // 整函数体来取（页面里有多条 catch，锚在别的 catch 上会读到无关的兜底分支）
    const loadData = page.match(/async function loadData\(\) \{[\s\S]*?\n\}\n/)?.[0]
    expect(loadData, '页面应有 loadData 函数体').toBeTruthy()
    const catchBody = loadData!.match(/catch \(e: unknown\) \{[\s\S]*?\n  \} finally/)?.[0]
    expect(catchBody, 'loadData 应有 catch 分支').toBeTruthy()
    expect(catchBody).toContain('kpaByPoint.value = {}')
    expect(catchBody).toContain('heatmapMaxKpa.value = null')
  })

  it('切档函数体只改显示档 + 写本地记忆（不重新取数、不写服务端）', () => {
    const body = page.match(/function switchUnit\([\s\S]*?\n\}/)?.[0]
    expect(body, '页面应有 switchUnit').toBeTruthy()
    expect(body).toContain('persistUnit(u)')
    expect(body).not.toMatch(/loadData|request|await|refresh/)
  })

  it('PressureHeatmap 把档位与两份派生值透传给详情行与提示行（组件不自算）', () => {
    expect(heatmap).toMatch(/:unit="unit"/)
    expect(heatmap).toMatch(/kpa: props\.kpaByPoint\[p\.pointId\] \?\? null/)
    expect(heatmap).toMatch(/const showAreaWarn = computed\(\(\) => areaHintVisible\(props\.unit, props\.heatmapMaxKpa\)\)/)
    expect(heatmap).toMatch(/v-if="showAreaWarn" class="heatmap-area-warn"/)
    expect(heatmap).toMatch(/\{\{ AREA_MISSING_HINT \}\}/)
    expect(heatmap).toMatch(/import \{ AREA_MISSING_HINT, areaHintVisible/)
  })

  it('提示行文案逐字取判据本体（不在组件里另拟一句）', async () => {
    const { pickStartUnit } = await import('@bracesync/shared-utils')
    expect(pickStartUnit('kPa')).toBe('kPa')
    expect(AREA_MISSING_HINT).toBe('未配置面积，暂无法换算')
    expect(heatmap).not.toMatch(/面积未填写|无法换算，请先/)
  })
})

describe('前端零重复换算门禁（派发单 §三 选型 A：换算真源只有一份，在后端）', () => {
  const files: Array<[string, string]> = [
    ['pages/monitor/index.vue', page],
    ['components/SensorGrid.vue', grid],
    ['components/PressureHeatmap.vue', heatmap],
    ['utils/monitor-copy.ts', copy],
    ['utils/unit-pref.ts', unitPref],
  ]

  it.each(files)('%s 里既无换算公式也不读面积做除法', (_name, src) => {
    expect(src).not.toMatch(/\*[^\S\n]*10\b/)
    // [^\S\n]：除法两侧不许有空白之外的换行——JSDoc 的 `*/` 紧跟下一行字段名会被 \s 跨行连成假命中
    expect(src).not.toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
    // 面积字段只允许出现在「声明但不读」的契约描述里（页面 interface 与注释）
    expect(src).not.toMatch(/\.\s*contactAreaCm2\b/)
    expect(src).not.toMatch(/\bprops\.areaCm2\b|\bcell\.areaCm2\b/)
  })

  it('反面自检：三条「前端自己换算」的写法必须各自被上面判到', () => {
    const bad = [
      'const kpa = (p.pressureValue / contactAreaCm2) * 10',
      'const kpa = n/areaCm2*10',
      'const kpa = snap.contactAreaCm2 * 10',
    ]
    for (const b of bad) {
      expect(b, b).toMatch(/\*[^\S\n]*10\b/)
    }
    expect(bad[0]).toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
    expect(bad[1]).toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
    expect(bad[2]).toMatch(/\.\s*contactAreaCm2\b/)
  })

  it('页面出现的 contactAreaCm2 只有契约声明那一行（只透传字段、不参与显示）', () => {
    const hits = page.match(/contactAreaCm2/g) ?? []
    expect(hits).toHaveLength(1)
    expect(page).toMatch(/contactAreaCm2\?: number \| null/)
  })

  it('换算公式在全包显示层只有一处例外：真实模式 e2e 夹具的服务端替身', () => {
    const fixture = readSrc('tests/e2e/fixtures/patient.ts')
    expect(fixture).toMatch(/function serverKpa/)
    expect(fixture).toMatch(/\/ areaCm2/)
    expect(fixture).toMatch(/服务端替身/)
    // 夹具不是产品代码：src/ 下不许出现同样的除法
    expect([page, grid, heatmap, copy, unitPref].join('\n')).not.toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
  })
})
