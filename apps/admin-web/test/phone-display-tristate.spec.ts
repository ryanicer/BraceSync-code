// T410 手机号三态展示的跨页口径（依据：PRD_V3 §9.2「空值展示为占位破折号，不得渲染成残串」+ 技师侧同口径）
//
// 缺陷原貌：三态修复（T361 / code PR 200）只落在医护账号页，技师管理页与团队管理的成员表
// 仍是裸 prop="phoneMasked" ⇒ phoneState=absent（库内 NULL）时渲染空白格，同一个号码在三张表里三种长相。
// 这里把三张表钉成同一个真源 phoneDisplay()，并逐格断言「各呈一形」：
//   absent → '—' / masked → 脱敏号 / unreadable → 后端占位符 '***'
// 🔴 断言一律按表头取列下标读「那一格」，不断整行文本 —— 同一行里「所属团队」「角色」等列也会落横杠，
// 整行断言会把手机列的回退掩掉（T377 变异实测出的同一条门禁缺口）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import TechniciansPage from '../src/pages/technicians/index.vue'
import TeamsPage from '../src/pages/teams/index.vue'
import { PHONE_DASH, PHONE_PLACEHOLDER, phoneDisplay } from '../src/utils/phoneField'
import { __resetOrgForTest, mockCreateTechnician } from '../src/mock/org'

async function flushAll() {
  // mock 层带 delay；el-table 的 mode 切换有过渡，推进多轮确保落定
  for (let i = 0; i < 8; i++) {
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
  }
}

function mountPage(page: typeof TechniciansPage): VueWrapper {
  return mount(page, { global: { plugins: [ElementPlus] } })
}

/** 按表头文案取列下标，再取该行那一格的文本（禁整行 text()，理由见文件头） */
function cellText(wrapper: VueWrapper, header: string, rowName: string): string {
  const ths = wrapper.findAll('th')
  const col = ths.findIndex((th) => th.text().trim() === header)
  if (col < 0) throw new Error(`表头里没有「${header}」列，实有=${JSON.stringify(ths.map((t) => t.text().trim()))}`)
  const row = wrapper.findAll('tbody tr').find((r) => r.text().includes(rowName))
  if (!row) throw new Error(`列表里没有「${rowName}」那一行`)
  const tds = row.findAll('td')
  if (tds.length <= col) throw new Error(`第 ${col} 列在该行没有对应单元格，该行实有 ${tds.length} 格`)
  return tds[col].text().trim()
}

/** 进入团队管理「成员管理」模式：点 TEAM-101（骨科一组）那行的「成员」按钮 */
async function openMemberPanel(wrapper: VueWrapper) {
  const row = wrapper.findAll('tbody tr').find((r) => r.text().includes('骨科一组'))
  if (!row) throw new Error('团队列表里没有「骨科一组」那一行')
  const btn = row.findAll('button').find((b) => b.text().trim() === '成员')
  if (!btn) throw new Error('该行没有「成员」按钮')
  await btn.trigger('click')
  await flushAll()
}

describe('phoneDisplay 纯函数（三态各呈一形）', () => {
  it('absent / null / undefined 一律落横杠', () => {
    expect(phoneDisplay('')).toBe(PHONE_DASH)
    expect(phoneDisplay(null)).toBe(PHONE_DASH)
    expect(phoneDisplay(undefined)).toBe(PHONE_DASH)
  })

  it('masked 原样展示脱敏号，unreadable 原样展示占位符（不是空值，不许被洗成横杠）', () => {
    expect(phoneDisplay('138****5678')).toBe('138****5678')
    expect(phoneDisplay(PHONE_PLACEHOLDER)).toBe(PHONE_PLACEHOLDER)
  })

  it('三态两两不同形', () => {
    expect(new Set([phoneDisplay(''), phoneDisplay('138****5678'), phoneDisplay(PHONE_PLACEHOLDER)]).size).toBe(3)
  })
})

describe('T410 三张表手机号列同源', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    __resetOrgForTest()
    // 播种一行「确实没有手机号」的技师（absent ⇒ phoneMasked 空串），挂在骨科一组，
    // 于是技师管理页与团队成员表同时可观测；afterEach 整体回滚，不动基线行集与统计卡计数。
    mockCreateTechnician({ name: 'T410无号技师', phone: '', teamId: 'TEAM-101' })
  })

  afterEach(() => {
    __resetOrgForTest()
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('技师管理页：absent 落横杠、masked 落脱敏号，不留空白格', async () => {
    const wrapper = mountPage(TechniciansPage)
    await flushAll()
    expect(cellText(wrapper, '手机号', 'T410无号技师')).toBe(PHONE_DASH)
    expect(cellText(wrapper, '手机号', '周师傅')).toBe('138****5678')
    wrapper.unmount()
  })

  it('团队成员表：absent 落横杠', async () => {
    const wrapper = mountPage(TeamsPage)
    await flushAll()
    await openMemberPanel(wrapper)
    expect(cellText(wrapper, '手机号', 'T410无号技师')).toBe(PHONE_DASH)
    wrapper.unmount()
  })

  it('团队成员表：unreadable 仍是 ***（与医护页同形，回退不得吞掉占位符）', async () => {
    const wrapper = mountPage(TeamsPage)
    await flushAll()
    await openMemberPanel(wrapper)
    const cell = cellText(wrapper, '手机号', '王护士') // DOC-102 phoneState=unreadable
    expect(cell).toBe(PHONE_PLACEHOLDER)
    expect(cell).not.toBe(PHONE_DASH)
    expect(cellText(wrapper, '手机号', '张主任')).toBe('138****1101')
    wrapper.unmount()
  })
})
