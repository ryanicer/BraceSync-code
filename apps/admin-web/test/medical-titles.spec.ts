// T360 职称词表纯层（依据：设计稿 docs/design/admin/医护账号.html :284「预置 4 项、可扩展」
// + :306/:308 两处下拉读同一个数组；库内实际值见 code 仓 seed 与 mock/org.ts 的「副主任医师」）。
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { MEDICAL_TITLES } from '../src/mock/medicalAccounts'
import { mockDoctors } from '../src/mock/org'
import { titleOptions } from '../src/utils/medicalTitles'

describe('titleOptions 职称下拉词表（T360）', () => {
  it('预置 4 项在前且顺序不变，库内既有值按首次出现序追加', () => {
    const out = titleOptions([
      { title: '副主任医师' },
      { title: '主治医师' },
      { title: '康复治疗师' },
    ])
    expect(out).toEqual([...MEDICAL_TITLES, '副主任医师', '康复治疗师'])
    expect(out.slice(0, MEDICAL_TITLES.length)).toEqual(MEDICAL_TITLES)
  })

  it('去重、忽略空串与 null，非空值两端空白被裁掉', () => {
    const out = titleOptions([
      { title: '主治医师' },
      { title: ' 护士 ' },
      { title: '' },
      { title: null },
      { title: undefined },
    ])
    expect(out).toEqual(MEDICAL_TITLES)
  })

  it('每行结果独立：调用方拿到的数组被改动不污染下一次调用', () => {
    const first = titleOptions([])
    first.push('污染值')
    expect(titleOptions([])).toEqual(MEDICAL_TITLES)
  })

  // 反证方向：本卡缺陷就是「词表只有预置 4 项、库内值越界」。mock 档案与 seed 同形
  // （DOC-002 陈小芳 / DOC-005 赵敏 = 副主任医师），所以这条在纯层就能钉住「筛得到」的前提。
  it('mock 档案里的越界职称必须进词表（否则筛选与编辑都读不到它）', () => {
    const titles = [...new Set(mockDoctors().map((d) => d.title))]
    const out = titleOptions(mockDoctors())
    for (const t of titles) expect(out).toContain(t)
    expect(out).toContain('副主任医师')
  })
})

// 防回潮（卡面「只改一处不算完」的机器判据）：设计稿 :306/:308 本来就是「两处下拉读同一个
// titles 数组」的结构。谁把其中一处退回逐项枚举 MEDICAL_TITLES，DOM 用例只能覆盖到被改的那一处，
// 另一处照样静默改写职称 —— 所以直接对页面源码立约：全篇不出现预置常量，两处 v-for 同引 titleChoices。
// 路径解析用 fileURLToPath(import.meta.url)：new URL(rel, import.meta.url) 的字面量形式会被
// Vite 的 asset-import-meta-url 插件编译期改写成 http 资源地址，运行时拿不到盘路径（同 base-mount-contract.spec.ts）。
describe('医护账号页 两处职称下拉同源（源码契约）', () => {
  const pageSrc = readFileSync(
    join(dirname(fileURLToPath(import.meta.url)), '../src/pages/doctors/index.vue'),
    'utf8',
  )

  it('页面不再引用预置常量，词表只能经 titleOptions 取并集', () => {
    expect(pageSrc).not.toContain('MEDICAL_TITLES')
    expect(pageSrc).toMatch(/from '\.\.\/\.\.\/utils\/medicalTitles'/)
  })

  it('筛选与编辑两处 v-for 同读 titleChoices', () => {
    const hits = pageSrc.match(/v-for="t in titleChoices"/g) ?? []
    expect(hits).toHaveLength(2)
  })
})
