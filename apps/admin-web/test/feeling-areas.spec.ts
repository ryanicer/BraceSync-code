import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { FEELING_AREAS, LEGACY_AREA_LABELS, areaLabel } from '../src/utils/feelingAreas'

/**
 * T370「不适部位」词表门禁（展示层收口 + 跨端防漂移）
 *
 * 为什么要这么多条同源断言：这套词能分叉至今，是因为它同时存在于几处而全仓只有 1 处展示位点 ——
 * 患者端设计稿 8 区（docs/design/patient/feelings.html:64-72）、后端写侧白名单
 * （handler.go 的 feelingAreaLabels，PM T188 裁定 Q4 存中文原词）、seed/mock 的历史英文码、
 * 前端译名表。没有一条把两端字面量摆在一起比的断言，改任何一处都不会判红。
 * T368 的权限矩阵是同一个形状的事故。
 *
 * 现网实测（staging 只读探针 2026-09-24，原始回包见 docs 仓 T370 证据包）：
 * feeling_logs 7 行，discomfort_areas 取值 lumbar 3 / thoracic 3 / 空 2，中文区名 0 条
 * —— 写侧只收 8 区中文，而患者端录入端点尚无 App 在调（T188 患者端是只读版）。
 *
 * 稿面区名不从 docs 文件里读：docs 仓不随 code 仓 CI 检出，读了会在 CI 里必然抛错。
 * 基数与顺序由下面那条硬字面量断言钉住（PRD §7A.7 是同一份口径），改设计稿时由人同步这里。
 */

const appRoot = join(dirname(fileURLToPath(import.meta.url)), '..') // apps/admin-web
const repoRoot = join(appRoot, '..', '..')
const read = (rel: string) => readFileSync(join(repoRoot, rel), 'utf8')

/** 从后端源码抽写侧白名单字面量 */
function backendAreaLiterals(): string[] {
  const src = read('services/user-service/internal/handler/handler.go')
  const block = src.match(/var feelingAreaLabels = map\[string\]bool\{([\s\S]*?)\n\}/)
  if (!block) {
    throw new Error('后端源码里找不到 feelingAreaLabels 声明：词表被改名或挪走了，本门禁要跟着改，不能静默放行')
  }
  return [...block[1].matchAll(/"([^"]+)"\s*:\s*true/g)].map((m) => m[1])
}

/** seed.sql 感受日志块里真正写进 discomfort_areas 的取值（只从 ARRAY[...]::varchar[] 抽，避开 notes 自由文本） */
function seedAreaValues(): string[] {
  const seed = read('scripts/db/seed/seed.sql')
  const block = seed.match(/(INSERT INTO feeling_logs[\s\S]*?)ON CONFLICT/)
  if (!block) throw new Error('seed.sql 里找不到 feeling_logs 的 INSERT 块，造数口径变了，本门禁要跟着改')
  return [...block[1].matchAll(/ARRAY\[([^\]]*)\]::varchar\[\]/g)]
    .flatMap((m) => [...m[1].matchAll(/'([^']*)'/g)].map((x) => x[1]))
    .filter(Boolean)
}

describe('FEELING_AREAS 与写侧白名单同源（T370 跨端漂移门禁）', () => {
  it('前端现行区名集合 = 后端 feelingAreaLabels 字面量集合', () => {
    expect([...FEELING_AREAS].sort()).toEqual(backendAreaLiterals().sort())
  })

  it('基数 8 且逐字等于设计稿区名（稿面改名必须同步这里）', () => {
    expect([...FEELING_AREAS]).toEqual(['右肩', '左肩', '胸椎', '右侧腰', '左侧腰', '骶骨', '右髂嵴', '左髂嵴'])
  })
})

describe('areaLabel：历史英文码向 8 区口径归并，归并不了的绝不伪造', () => {
  it('8 区原词一字不动地透出（写侧数据零改写）', () => {
    for (const zone of FEELING_AREAS) expect(areaLabel(zone)).toBe(zone)
  })

  it('4 个历史码都有译名，不把裸英文码交给医护', () => {
    expect(Object.keys(LEGACY_AREA_LABELS).sort()).toEqual(['lumbar', 'neck', 'pelvis', 'thoracic'])
    for (const code of Object.keys(LEGACY_AREA_LABELS)) {
      expect(areaLabel(code), `历史码 ${code} 原样透出了`).not.toBe(code)
    }
  })

  it('thoracic 归并到 8 区的「胸椎」——唯一 1 对 1 可归并的历史码', () => {
    expect(areaLabel('thoracic')).toBe('胸椎')
    expect([...FEELING_AREAS]).toContain(areaLabel('thoracic'))
  })

  it('lumbar / pelvis / neck 的译名不得落进 8 区：历史码不带侧别、也无颈区，落进去就是伪造', () => {
    for (const code of ['lumbar', 'pelvis', 'neck'] as const) {
      expect([...FEELING_AREAS], `${code} 被并进了现行区名，等于伪造侧别或伪造一个不存在的区`).not.toContain(
        LEGACY_AREA_LABELS[code],
      )
    }
  })

  it('译名不再产出 PRD 作废四区里的「胸段 / 腰段」（防回潮到修前形态）', () => {
    const labels = Object.values(LEGACY_AREA_LABELS)
    expect(labels).not.toContain('胸段')
    expect(labels).not.toContain('腰段')
    expect(areaLabel('thoracic')).not.toBe('胸段')
    expect(areaLabel('lumbar')).not.toBe('腰段')
  })

  it('未命中值与空串原样透出（不吞真实数据、不判红）', () => {
    expect(areaLabel('腋下')).toBe('腋下') // 稿面样例里出现过的第三套词，见 T370 卡第六节第 2 项
    expect(areaLabel('')).toBe('')
    expect(areaLabel('unknown_code')).toBe('unknown_code')
  })
})

describe('造数不许引入第五套词', () => {
  it('seed 的 discomfort_areas 取值都在「8 区 ∪ 历史码」内', () => {
    const allowed = new Set<string>([...FEELING_AREAS, ...Object.keys(LEGACY_AREA_LABELS)])
    const values = seedAreaValues()
    expect(values.length, '没抽到部位数组字面量，取数正则是空的').toBeGreaterThan(0)
    for (const v of values) expect(allowed, `seed 里出现词表外取值「${v}」`).toContain(v)
  })

  it('admin-web mock 的部位取值同样受限（mock 是历史行替身，也不许造第五套词）', () => {
    const allowed = new Set<string>([...FEELING_AREAS, ...Object.keys(LEGACY_AREA_LABELS)])
    const src = read('apps/admin-web/src/mock/orthosis.ts')
    const values = [...src.matchAll(/discomfortAreas: \[([^\]]*)\]/g)]
      .flatMap((m) => [...m[1].matchAll(/'([^']*)'/g)].map((x) => x[1]))
    expect(values.length).toBeGreaterThan(0)
    for (const v of values) expect(allowed, `mock 里出现词表外取值「${v}」`).toContain(v)
  })
})
