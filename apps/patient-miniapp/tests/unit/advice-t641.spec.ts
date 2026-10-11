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
  adviceMetaLabel, adviceRuneCount, adviceDateLabel,
  careTeamAvatar, careTeamTitleLabel,
} from '../../src/utils/advice'

const here = dirname(fileURLToPath(import.meta.url))
const srcRoot = join(here, '..', '..', 'src')
const pageFile = readFileSync(join(srcRoot, 'pages/advice/index.vue'), 'utf8')
const apiFile = readFileSync(join(srcRoot, 'api/advice.ts'), 'utf8')
const profileFile = readFileSync(join(srcRoot, 'pages/profile/index.vue'), 'utf8')
const pagesJson = JSON.parse(readFileSync(join(srcRoot, 'pages.json'), 'utf8'))

describe('A 纯层：团队区头像 + 职称（T659 按稿 advice.html:45-63 改形）', () => {
  it('角色只由装饰头像 emoji 承担：doctor/technician 两枚封闭值', () => {
    expect(careTeamAvatar('doctor')).toBe('👨‍⚕️')
    expect(careTeamAvatar('technician')).toBe('👨‍🔧')
  })

  it('职称列原样直通后端 DTO：前端不做同词折损、不画破折号', () => {
    expect(careTeamTitleLabel({ memberType: 'doctor', title: '主治医师' })).toBe('主治医师')
    expect(careTeamTitleLabel({ memberType: 'doctor', title: '护士' })).toBe('护士')
    // T659 前的回归点：技师固定词不得再被折成「——」（Boss 真机截图实证）
    expect(careTeamTitleLabel({ memberType: 'technician', title: '技术支撑' })).toBe('技术支撑')
    // 空串 trim 后仍为空也不前端造词（回落 title→department→医护团队 在后端 DTO 已组好）
    expect(careTeamTitleLabel({ memberType: 'doctor', title: '   ' })).toBe('')
  })
})

describe('A 纯层：时间轴行的时间（T659 稿面只到日期）与撰写字数', () => {
  it('UTC 串按本地时区呈现到日，不是把 UTC 串直接截成前 10 位（那会早 8 小时跨日）', () => {
    const iso = '2026-10-09T14:20:00Z'
    const offMin = -new Date(iso).getTimezoneOffset()
    const got = adviceDateLabel(iso)
    expect(got).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    // UTC 14:20 在东八区是次日 10-10，截串会停在 10-09
    if (offMin === -480) expect(got).toBe('2026-10-10')
  })

  it('无时刻 / 非法时刻不编造时间', () => {
    expect(adviceDateLabel(null)).toBe('')
    expect(adviceDateLabel(undefined)).toBe('')
    expect(adviceDateLabel('not-a-time')).toBe('not-a-time')
  })

  it('行头 = 「日期 · 职称」，不带时分，职称用服务端回落结果，不拼姓名', () => {
    const meta = adviceMetaLabel({
      adviceId: '1', patientId: 'PAT-001', title: '主治医师',
      content: 'x', createdAt: '2026-10-09T00:00:00Z', updatedAt: '2026-10-09T00:00:00Z', editable: false,
    })
    expect(meta).toMatch(/^\d{4}-\d{2}-\d{2} · 主治医师$/)
    expect(meta).not.toMatch(/\d{2}:\d{2}/)
  })

  it('撰写字数仍按 rune（不按 UTF-16 码元、更不按字节）', () => {
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

  it('T659 按 advice.html：区块②标题=「康复建议」，无旧的「医护建议」区块标题', () => {
    const titles = [...pageFile.matchAll(/class="section-title">([^<]+)</g)].map((m) => m[1])
    expect(titles).toContain('康复建议')
    expect(titles).not.toContain('医护建议')
  })

  it('T659 团队区=横排头像 strip：class 锚点在场，旧的角色文字胶囊与纵向行零残留', () => {
    expect(pageFile).toContain('class="card team-strip"')
    expect(pageFile).toContain('careTeamAvatar(')
    expect(pageFile).toContain('team-avatar-doctor')
    expect(pageFile).toContain('team-avatar-tech')
    expect(pageFile, '角色文字胶囊已随稿退场').not.toContain('team-role')
    expect(pageFile, '纵向团队行已随稿退场').not.toContain('team-row')
    // 破折号占位只查模板渲染区（注释里的中文破折号是语义用法不算）
    const tpl = pageFile.slice(pageFile.indexOf('<template>'), pageFile.indexOf('</template>'))
    expect(tpl, '破折号占位已随稿退场').not.toContain('——')
  })

  it('T659 摘要两行截断走 CSS line-clamp（非 JS 字符切），展开控件恒显（稿面四条 seed 同形）', () => {
    expect(pageFile).toContain('-webkit-line-clamp: 2')
    // toggle 不再挂 v-if 条件
    const toggleBlock = pageFile.slice(pageFile.indexOf('class="advice-toggle"') - 200, pageFile.indexOf('class="advice-toggle"') + 80)
    expect(toggleBlock).not.toContain('v-if')
    // 摘要直接渲染原文，不经 rune 截断函数
    expect(pageFile).toMatch(/class="advice-summary">\{\{\s*item\.content\s*\}\}/)
    expect(pageFile).not.toContain('adviceExcerpt')
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
