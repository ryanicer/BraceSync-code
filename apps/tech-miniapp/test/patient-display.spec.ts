/**
 * T443 裁定⑤（Boss 2026-09-28）— 技师端患者位显示改读姓名。
 * 依据：docs/tasks/winner/T446-字段级对读-结论与字段表.md §三 C-5。
 *
 * 页面模板在本包测不到（vitest.config.ts 是 environment: 'node'，无 VTU），
 * 故真源函数下沉到 utils/patientDisplay.ts 直接测，三处接线按源码断言
 * （同 test/install-status.spec.ts 的既有写法）。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'
import { patientDisplayValue, patientNameWithId } from '../src/utils/patientDisplay'

function srcOf(rel: string): string {
  return fs.readFileSync(fileURLToPath(new URL(rel, import.meta.url)), 'utf8')
}

/** 只取会渲染的行：注释里的旧词是解释为什么改，不算与稿面词并存 */
function codeLines(src: string): string[] {
  return src
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter(
      (l) =>
        l !== '' &&
        !l.startsWith('//') &&
        !l.startsWith('/*') &&
        !l.startsWith('*/') &&
        !l.startsWith('<!--'),
    )
}

describe('patientDisplayValue（纯层）', () => {
  it('有姓名显姓名', () => {
    expect(patientDisplayValue('张明远', 'P20260001')).toBe('张明远')
  })

  it('空串与 null／undefined 同等回落 ID —— alerts 族的空值是空串（COALESCE），InstallRecord 族是 null', () => {
    expect(patientDisplayValue('', 'P20260001')).toBe('P20260001')
    expect(patientDisplayValue(null, 'P20260001')).toBe('P20260001')
    expect(patientDisplayValue(undefined, 'P20260001')).toBe('P20260001')
  })

  it('两格都缺才显占位符，不显空白格', () => {
    expect(patientDisplayValue('', '')).toBe('--')
    expect(patientDisplayValue(null, undefined)).toBe('--')
  })

  it('反证：写成 ?? 会漏掉空串那一族（改前真实缺陷面）', () => {
    expect('' ?? 'P20260001').toBe('')
  })
})

describe('裁定⑤接线 — 三处患者位同读一个真源', () => {
  const CALL = 'patientDisplayValue('

  it('告警列表行的患者位不再挂裸 patientId', () => {
    const src = srcOf('../src/pages/alerts/index.vue')
    expectVisible(src, `{{ ${CALL}alert.patientName, alert.patientId) }}`)
    for (const line of codeLines(src)) {
      expect(line).not.toMatch(/meta-value">\{\{\s*alert\.patientId\s*\}\}/)
    }
  })

  it('告警详情弹窗正文的患者行同源', () => {
    const src = srcOf('../src/utils/alertDisplay.ts')
    expectVisible(src, '患者: ${' + CALL + 'a.patientName, a.patientId)}')
    for (const line of codeLines(src)) {
      expect(line).not.toMatch(/`患者: \$\{a\.patientId\}`/)
    }
  })

  it('完成页患者行按稿面「姓名 (ID)」同行，标签不再写「患者 ID」', () => {
    const src = srcOf('../src/pages/complete/index.vue')
    expectVisible(src, `patientNameWithId(installStore.patient?.name, summary.value.patientId)`)
    for (const line of codeLines(src)) {
      expect(line).not.toMatch(/>\s*患者 ID\s*</)
    }
    // 反证：改回上一行形态时标签断言对它有牙
    const mutated = src.replace(
      '<text class="summary-label">患者</text>',
      '<text class="summary-label">患者 ID</text>',
    )
    let hit = false
    for (const line of codeLines(mutated)) {
      if (/>\s*患者 ID\s*</.test(line)) hit = true
    }
    expect(hit, '反证失效：标签已被改到断言看不见的形状').toBe(true)
  })

  it('两形态各按各自稿面：告警位只显姓名（alerts.html:153），完成页姓名＋ID 同行（complete.html:70-71）', () => {
    expect(patientDisplayValue('张明远', 'P20260001')).toBe('张明远')
    expect(patientNameWithId('张明远', 'P20260001')).toBe('张明远 (P20260001)')
    expect(patientNameWithId('', 'P20260001')).toBe('P20260001')
    expect(patientNameWithId('张明远', '')).toBe('张明远')
    expect(patientNameWithId(null, null)).toBe('--')
    // 完成页不许用只显姓名的那个（会与稿面丢 ID）
    expect(srcOf('../src/pages/complete/index.vue')).not.toContain('patientDisplayValue(')
  })

  it('三处都 import 了真源（漏 import 时模板运行时才炸，本包构建不报）', () => {
    const expected: Array<[string, string]> = [
      ['../src/pages/alerts/index.vue', 'patientDisplayValue'],
      ['../src/pages/complete/index.vue', 'patientNameWithId'],
      ['../src/utils/alertDisplay.ts', 'patientDisplayValue'],
    ]
    for (const [rel, sym] of expected) {
      const src = srcOf(rel)
      const lines = codeLines(src).filter((l) => l.startsWith('import') && l.includes(sym))
      expect(lines.length, `${rel} 的 import 缺 ${sym}`).toBeGreaterThan(0)
      expect(lines[0], `${rel} 的 ${sym} import 不指向 patientDisplay`).toContain('patientDisplay')
      expect(src.includes(`${sym}(`), `${rel} 里没用到 ${sym}`).toBe(true)
    }
  })

  it('显示真源只有一处字面量：页面／工具层都不再自写三元回落', () => {
    const util = srcOf('../src/utils/patientDisplay.ts')
    expect(codeLines(util).some((l) => l.includes("return name || id || '--'"))).toBe(true)
    for (const rel of ['../src/pages/alerts/index.vue', '../src/pages/complete/index.vue', '../src/utils/alertDisplay.ts']) {
      for (const line of codeLines(srcOf(rel))) {
        expect(line).not.toMatch(/patientName \? .*patientId/)
        expect(line).not.toMatch(/patientName \?\? /)
      }
    }
  })

  it('mock 种子带姓名，且刻意留一行无姓名 —— 否则回落分支在门禁里从不执行', () => {
    const src = srcOf('../src/api/alert.ts')
    expectVisible(src, 'patientName: names[i % 6],')
    const namesLine = codeLines(src).find((l) => l.startsWith('const names ='))
    expect(namesLine, 'mock 姓名列被改动到断言看不见的形状').toBeDefined()
    expect(namesLine).toContain('null')
  })
})

/** 反证用：确认 codeLines 真能看见渲染行，否则「无并存」是空过滤器造出来的假绿 */
function expectVisible(src: string, needle: string): void {
  expect(codeLines(src).some((l) => l.includes(needle)), `codeLines 里应能看到 ${needle}`).toBe(true)
}
