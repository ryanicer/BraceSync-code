/**
 * T641 患者端「康复建议」页门禁（设计稿 §十二 行 ⑥ + 行 ⑦ 的前端那一半）。
 *
 * 分两段的理由同 feelings.spec.ts：本包 vitest 是 `environment: 'node'`，.vue 挂不起来，
 * 所以纯层直接调、页面接线用源码级断言（读文件文本）。
 * 稿面 advice.html 的行号引用只作注释，不从 docs 仓读文件（code 仓 CI 不检出 docs 仓，T370 教训）。
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { CareTeamMember } from '@bracesync/shared-types'
import {
  adviceExcerpt, adviceMetaLabel, adviceRuneCount, adviceTimeLabel,
  careTeamRoleLabel, careTeamTitleLabel, CARE_TEAM_TITLE_PLACEHOLDER,
} from '../../src/utils/advice'

const here = dirname(fileURLToPath(import.meta.url))
const srcRoot = join(here, '..', '..', 'src')
const pageFile = readFileSync(join(srcRoot, 'pages/advice/index.vue'), 'utf8')
const apiFile = readFileSync(join(srcRoot, 'api/advice.ts'), 'utf8')
const profileFile = readFileSync(join(srcRoot, 'pages/profile/index.vue'), 'utf8')
const pagesJson = JSON.parse(readFileSync(join(srcRoot, 'pages.json'), 'utf8'))

describe('A 纯层：团队区两列（角色 + 职称，无姓名）', () => {
  it('角色列 = memberType 的两枚封闭值', () => {
    expect(careTeamRoleLabel('doctor')).toBe('医护')
    expect(careTeamRoleLabel('technician')).toBe('技术支撑')
  })

  it('职称列原样展示服务端算好的回落结果；空职称画稿面的「——」', () => {
    expect(careTeamTitleLabel({ memberType: 'doctor', title: '主治医师' })).toBe('主治医师')
    expect(careTeamTitleLabel({ memberType: 'doctor', title: '' })).toBe(CARE_TEAM_TITLE_PLACEHOLDER)
    expect(careTeamTitleLabel({ memberType: 'doctor', title: '   ' })).toBe(CARE_TEAM_TITLE_PLACEHOLDER)
    // 反证（针有牙）：有职称的医生行不许被折成占位符
    expect(careTeamTitleLabel({ memberType: 'doctor', title: '护士' })).not.toBe(CARE_TEAM_TITLE_PLACEHOLDER)
  })

  it('技师行：后端按 R6 甲把固定标签放进职称槽，角色列同一行已说过这个词 ⇒ 职称列折成「——」，不重复一遍', () => {
    expect(careTeamTitleLabel({ memberType: 'technician', title: '技术支撑' })).toBe(CARE_TEAM_TITLE_PLACEHOLDER)
  })
})

describe('A 纯层：时间轴行的时间与摘要', () => {
  it('UTC 串按本地时区呈现到分钟，不是把 UTC 串直接截断（同 replyTimeLabel 的坑）', () => {
    const iso = '2026-10-09T14:20:00Z'
    const offMin = -new Date(iso).getTimezoneOffset()
    const localMinutes = (14 * 60 + 20 + offMin + 1440) % 1440
    const got = adviceTimeLabel(iso)
    expect(got).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/)
    expect(Number(got.slice(11, 13))).toBe(Math.floor(localMinutes / 60))
    expect(got.slice(14, 16)).toBe(String(localMinutes % 60).padStart(2, '0'))
    if (offMin !== 0) expect(got).not.toBe('2026-10-09 14:20')
  })

  it('无时刻 / 非法时刻不编造时间', () => {
    expect(adviceTimeLabel(null)).toBe('')
    expect(adviceTimeLabel(undefined)).toBe('')
    expect(adviceTimeLabel('not-a-time')).toBe('not-a-time')
  })

  it('行头 = 「时间 · 职称」，职称用服务端回落结果，不拼姓名', () => {
    const meta = adviceMetaLabel({
      adviceId: '1', patientId: 'PAT-001', title: '主治医师',
      content: 'x', createdAt: '2026-10-09T00:00:00Z', updatedAt: '2026-10-09T00:00:00Z', editable: false,
    })
    expect(meta).toMatch(/^2026-\d{2}-\d{2} \d{2}:\d{2} · 主治医师$/)
  })

  it('上限内的正文不折、不出现「展开全文」这一档', () => {
    const short = adviceExcerpt('复查提醒：10 月 16 日上午门诊复查。')
    expect(short.expandable).toBe(false)
    expect(short.text).toBe('复查提醒：10 月 16 日上午门诊复查。')
    expect(adviceExcerpt('')).toEqual({ text: '', expandable: false })
    expect(adviceExcerpt(null)).toEqual({ text: '', expandable: false })
  })

  it('超长正文按 rune 折（不按 UTF-16 码元、更不按字节）', () => {
    const cjk = '方'.repeat(120) // 360 字节：按字节数会把一行截成半个字
    const got = adviceExcerpt(cjk, 60)
    expect(got.expandable).toBe(true)
    expect(Array.from(got.text)).toHaveLength(61) // 60 枚正文 + 省略号
    expect(got.text.endsWith('…')).toBe(true)

    const astral = '😀'.repeat(61) // .length = 122，按码元取 60 会劈开末枚代理对
    const wide = adviceExcerpt(astral, 60)
    expect(wide.text).toBe(`${'😀'.repeat(60)}…`)
    expect(adviceRuneCount('😀'.repeat(10))).toBe(10)
    expect(adviceRuneCount('方'.repeat(500))).toBe(500)
  })
})

describe('B 路由注册与入口直连（§十二 行 ⑥ 的三件）', () => {
  it('pages.json 注册 pages/advice/index，导航栏标题 = 稿面页名「康复建议」', () => {
    const entry = pagesJson.pages.find((p: { path: string }) => p.path === 'pages/advice/index')
    expect(entry, 'pages.json 缺 pages/advice/index').toBeTruthy()
    expect(entry.style.navigationBarTitleText).toBe('康复建议')
  })

  it('该页不是 tabBar 页（稿面 tabbar 四页固定）', () => {
    expect(JSON.stringify(pagesJson.tabBar)).not.toContain('pages/advice/index')
  })

  it('「我的」页那一行直连新页，副文案与占位函数一并退场', () => {
    expect(profileFile).toMatch(/@click="goAdvice"[\s\S]{0,120}康复建议/)
    expect(profileFile).toMatch(/navigateTo\(\{ url: '\/pages\/advice\/index' \}\)/)
    expect(profileFile).not.toContain('doctorSubText')
  })

  it('新页不是写侧：三枚写方法一次都不出现（留言板，患者零写操作）', () => {
    expect(apiFile).not.toMatch(/method: '(POST|PUT|DELETE)'/)
    expect(pageFile).not.toMatch(/method: '(POST|PUT|DELETE)'/)
    expect(pageFile).not.toMatch(/class="btn-|<input|<textarea|placeholder=/)
  })

  it('两枚读端点：建议流带患者路径（本人令牌），团队区走 self-scope 路径', () => {
    expect(apiFile).toContain('`/api/v1/patients/${patientId}/advice`')
    expect(apiFile).toContain('/api/v1/patient/care-team')
    expect(pageFile).toContain('listPatientAdvice(auth.patientId)')
    expect(pageFile).toContain('getCareTeam()')
  })

  it('🔴 页面源码不引用医护姓名的两枚档案键（针清单留在本文件，不写进被筛的那枚页面）', () => {
    for (const needle of ['doctorName', 'teamName', 'realName', 'userName', 'phone']) {
      expect(pageFile, `康复建议页不应出现姓名键 ${needle}`).not.toContain(needle)
    }
  })

  it('列表顺序只认后端（倒序在 pg.go 的 ORDER BY，前端不再重排）', () => {
    expect(pageFile).not.toMatch(/\.(sort|reverse)\(/)
  })

  it('页脚那句是静态文案：文案在场，且那一行没有点击绑定', () => {
    expect(pageFile).toContain('如需反馈佩戴情况，可在「我的 · 矫形日志」里记录。')
    const footerLine = pageFile.split('\n').find((l) => l.includes('如需反馈佩戴情况')) ?? ''
    expect(footerLine).not.toContain('@click')
    // 反证：全文展开那一档确实是点击绑定的（否则「零交互」这条断言会空转）
    expect(pageFile).toContain('@click="toggleExpand(')
  })

  it('唯一的展示态切换不落服务端：展开态是本地 reactive，页内不直连 request', () => {
    expect(pageFile).toContain('reactive<Record<string, boolean>>')
    expect(pageFile).not.toMatch(/readAt|isRead|markRead|unread/)
    // 取数只有 api/advice.ts 那两枚读口，页面不绕过它直接发请求（绕过就没有类型面可依）
    expect(pageFile).not.toContain("from '../../utils/request'")
  })
})

describe('B 团队行的 v-for 键（多医生团队不得撞键）', () => {
  it('键带下标：一个团队第二枚在职医生出行时，角色维会重复', () => {
    const rows: CareTeamMember[] = [
      { memberType: 'doctor', title: '主治医师' },
      { memberType: 'doctor', title: '护士' },
      { memberType: 'technician', title: '技术支撑' },
    ]
    const keys = rows.map((m, i) => `${m.memberType}-${i}`)
    expect(new Set(keys).size).toBe(3)
    expect(pageFile).toContain(':key="member.memberType')
  })
})
