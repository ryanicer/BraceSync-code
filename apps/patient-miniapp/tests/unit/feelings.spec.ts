/**
 * T505 患者端矫形日志（feelings）页门禁。
 *
 * 为什么分两段写：本包 vitest 是 `environment: 'node'`（vitest.config.ts），.vue 挂不起来
 * （同 T298/T443 的形状），所以判据分两类——
 *   A. 纯层 utils/feelings.ts 直接调；
 *   B. 页面接线（词表来自共享包、路由已注册、入口已直连、写通道字段名、作废口径不回潮）
 *      用源码级断言（读文件文本），改坏了会判红而不是靠人眼review。
 * 稿面 feelings.html 的行号引用不从 docs 仓读：code 仓 CI 不检出 docs 仓，读了必然抛错（T370 教训）。
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { FEELING_AREAS, FEELING_LEVELS, FEELING_NOTES_MAX_LEN } from '@bracesync/shared-utils'
import {
  areasText, buildFeelingPayload, feelingLabel, feelingTagClass,
  logDateLabel, replyTimeLabel, todayLocalDate, toggleArea,
} from '../../src/utils/feelings'

const here = dirname(fileURLToPath(import.meta.url))
const srcRoot = join(here, '..', '..', 'src')
const pageFile = readFileSync(join(srcRoot, 'pages/feelings/index.vue'), 'utf8')
const profileFile = readFileSync(join(srcRoot, 'pages/profile/index.vue'), 'utf8')
const pagesJson = JSON.parse(readFileSync(join(srcRoot, 'pages.json'), 'utf8'))

describe('A 纯层：日期与周几（稿面 feelings.html:64「2026-07-12 周日」画法）', () => {
  it('日期后追加周几，且周几按本地日历算（不用 new Date("YYYY-MM-DD")，那串按 UTC 零点解析）', () => {
    expect(logDateLabel('2026-07-12')).toBe('2026-07-12 周日')
    expect(logDateLabel('2026-07-11')).toBe('2026-07-11 周六')
    expect(logDateLabel('2026-07-13')).toBe('2026-07-13 周一')
  })

  it('非 YYYY-MM-DD 的输入原样返回，不伪造日期', () => {
    expect(logDateLabel('')).toBe('')
    expect(logDateLabel('2026-7-1')).toBe('2026-7-1')
    expect(logDateLabel(undefined as unknown as string)).toBe('')
  })

  it('录入默认日期 = 设备本地今日（YYYY-MM-DD，零补齐）', () => {
    expect(todayLocalDate(new Date(2026, 6, 5))).toBe('2026-07-05')
    expect(todayLocalDate(new Date(2026, 11, 31))).toBe('2026-12-31')
  })
})

describe('A 纯层：档位与部位展示文案', () => {
  it('两档读共享词表，NULL 档位返回空串（由调用方隐藏标签，不冒充「贴合」）', () => {
    expect(feelingLabel('fitted')).toBe('贴合')
    expect(feelingLabel('discomfort')).toBe('不适')
    expect(feelingLabel(null)).toBe('')
    expect(feelingLabel(undefined)).toBe('')
  })

  it('档位配色按稿面两档画法：不适=warn，其余=ok', () => {
    expect(feelingTagClass('discomfort')).toBe('log-tag-warn')
    expect(feelingTagClass('fitted')).toBe('log-tag-ok')
  })

  it('历史英文码经译名后按顿号连接，空/缺失返回空串', () => {
    expect(areasText(['右侧腰', '胸椎'])).toBe('右侧腰、胸椎')
    expect(areasText(['lumbar'])).toBe('腰部')
    expect(areasText([])).toBe('')
    expect(areasText(null)).toBe('')
  })
})

describe('A 纯层：医生回复时刻（后端下发 .UTC() 的 RFC3339）', () => {
  it('按本地时区呈现到分钟，不是把 UTC 串直接截断', () => {
    const iso = '2026-09-24T18:00:00Z'
    const offMin = -new Date(iso).getTimezoneOffset()
    const localMinutes = (18 * 60 + offMin + 1440) % 1440
    const got = replyTimeLabel(iso)
    expect(Number(got.slice(11, 13))).toBe(Math.floor(localMinutes / 60))
    expect(got.slice(14, 16)).toBe('00')
    expect(got).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/)
    // 独立推导：正时区（如 UTC+8）下截串画法会把日期显示成前一天 18:00，这里必须不是它
    if (offMin > 0) expect(got).not.toBe('2026-09-24 18:00')
  })

  it('无回复 / 非法时刻不编造时间', () => {
    expect(replyTimeLabel(null)).toBe('')
    expect(replyTimeLabel(undefined)).toBe('')
    expect(replyTimeLabel('not-a-time')).toBe('not-a-time')
  })
})

describe('A 纯层：部位点选与写通道请求体', () => {
  it('点选不重复追加、再点取消，且保持稿面点选顺序', () => {
    expect(toggleArea([], '右肩')).toEqual(['右肩'])
    expect(toggleArea(['右肩'], '胸椎')).toEqual(['右肩', '胸椎'])
    expect(toggleArea(['右肩', '胸椎'], '右肩')).toEqual(['胸椎'])
  })

  it('请求体字段名与后端 CreateFeelingLogRequest 对齐，notes 去首尾空白', () => {
    expect(buildFeelingPayload({
      logDate: '2026-07-12',
      feeling: 'discomfort',
      areas: ['右侧腰'],
      notes: '  束带调松后缓解  ',
    })).toEqual({
      feeling: 'discomfort',
      discomfortAreas: ['右侧腰'],
      notes: '束带调松后缓解',
      logDate: '2026-07-12',
    })
  })
})

describe('B 页面接线：词表单一真源（不得在患者包自造一套）', () => {
  it('页面从 @bracesync/shared-utils 读 8 区 / 两档 / 200 字上限', () => {
    expect(pageFile).toContain("import { FEELING_AREAS, FEELING_LEVELS, FEELING_NOTES_MAX_LEN } from '@bracesync/shared-utils'")
    expect(pageFile).toMatch(/v-for="zone in FEELING_AREAS"/)
    expect(pageFile).toMatch(/v-for="level in FEELING_LEVELS"/)
  })

  it('8 区区名只出现在共享包，页面里一个都不写（写侧白名单同源，改区名只改一处）', () => {
    for (const zone of FEELING_AREAS) {
      expect(pageFile.includes(zone), `页面不应内联区名「${zone}」`).toBe(false)
    }
    expect(FEELING_AREAS).toHaveLength(8)
  })

  it('详细描述上限绑后端同源常量（写死数字会在后端改口径时假绿）', () => {
    expect(pageFile).toMatch(/:maxlength="FEELING_NOTES_MAX_LEN"/)
    expect(FEELING_NOTES_MAX_LEN).toBe(200)
  })
})

describe('B 页面接线：作废口径不回潮（PRD V3.22 方案 A）', () => {
  it('三档码值 good / mild / pain 不得作为字面量回到本页', () => {
    expect(FEELING_LEVELS).toEqual(['fitted', 'discomfort'])
    expect(pageFile).not.toMatch(/["'](good|mild|pain)["']/)
  })

  it('三档展示词与「支具贴合度」控件不回页面', () => {
    expect(pageFile).not.toContain('舒适')
    expect(pageFile).not.toContain('轻微不适')
    expect(pageFile).not.toContain('明显疼痛')
    expect(pageFile).not.toMatch(/fitLevel|fit_level/)
  })

  it('回复气泡不含医生姓名（feeling_logs 无该列，Boss D4 不批 schema 变更）', () => {
    expect(pageFile).not.toMatch(/doctorName|repliedBy|doctor\.name/)
  })

  it('患者页不接医生回复写端点（该端点是 doctorAdminOnly，本页只读）', () => {
    expect(pageFile).not.toContain('/reply')
  })
})

describe('B 路由注册与入口直连（T505 三件的第二、三件）', () => {
  it('pages.json 注册 pages/feelings/index，导航栏标题 = 稿面页名「矫形日志」', () => {
    const entry = pagesJson.pages.find((p: { path: string }) => p.path === 'pages/feelings/index')
    expect(entry, 'pages.json 缺 pages/feelings/index').toBeTruthy()
    expect(entry.style.navigationBarTitleText).toBe('矫形日志')
  })

  it('该页不是 tabBar 页（稿面 tabbar 四页固定，加第五个 tab 属改导航口径）', () => {
    expect(JSON.stringify(pagesJson.tabBar)).not.toContain('pages/feelings/index')
  })

  it('「我的」页入口从占位（即将开放 toast）改为直连新页', () => {
    expect(profileFile).toMatch(/@click="goFeelings"[\s\S]{0,120}矫形日志/)
    expect(profileFile).toMatch(/navigateTo\(\{ url: '\/pages\/feelings\/index' \}\)/)
    // 反证：矫形日志那一行不能再绑占位 handler（占位仍服务「我的医生」，函数本身保留）
    const feelingsItem = profileFile.match(/@click="(\w+)"[^>]*>\s*<text class="menu-ic">📝<\/text>[\s\S]{0,60}矫形日志/)
    expect(feelingsItem?.[1]).toBe('goFeelings')
    expect(profileFile).toContain('function comingSoon()')
  })
})

describe('B 写通道契约（后端 handler.go:292-294 已上线，非纸面能力）', () => {
  it('读写同一患者路径，写用 POST（wx.request 不支持 PATCH；同日由后端 upsert 覆盖）', () => {
    expect(pageFile).toContain('`/api/v1/patients/${auth.patientId}/feeling-logs`')
    expect(pageFile).toMatch(/method: 'POST'/)
    expect(pageFile).not.toMatch(/method: '(PUT|PATCH|DELETE)'/)
  })
})
