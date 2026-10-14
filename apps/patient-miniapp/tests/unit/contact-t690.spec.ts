/**
 * T690 患者端统一咨询入口（§7A.13 · PRD V3.48）契约测试
 *
 * 把文案红线与结构裁定从人工走查变成可执行断言：
 *   A. 禁「工单」措辞与「微信客服回复您」承诺（Boss 二轮裁定）
 *   B. 留言类型三选词表恒定（§8.2 Feedback；wifi_setup_failure 不进用户可选项）
 *   C. 「我的」页一级菜单「联系我们」直连 contact；设置抽屉（绑定微信/退出登录）整组撤销
 *   D. 两处在线客服按钮均带 session-from（MP 侧；身份透传 §7A.13 通道一）
 */
import { describe, it, expect, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
// request → logger 含 uni 条件编译（// #ifdef），node 环境 esbuild 不剥离 ⇒ mock 掉
// （与 wear-target-t579.spec.ts 同法），feedback 常量本身不依赖 request 实现
vi.mock('../../src/utils/request', () => ({ request: vi.fn() }))
import { FEEDBACK_TYPES, WIFI_SETUP_FEEDBACK_TYPE } from '../../src/api/feedback'
import { CONTACT, FAILURE_COMMON } from '../../src/utils/wifi-copy'

const srcRoot = join(dirname(fileURLToPath(import.meta.url)), '../../src')
const readSrc = (p: string) => readFileSync(join(srcRoot, p), 'utf8')

const CONTACT_PAGE = readSrc('pages/contact/index.vue')
const PROFILE_PAGE = readSrc('pages/profile/index.vue')
const WIFI_CONTACT = readSrc('components/wifi-setup/WifiContact.vue')
const PAGES_JSON = JSON.parse(readSrc('pages.json'))

// 红线约束的是患者可见文案，注释里引用红线词本身不算违例 ⇒ 断言前剥掉注释
const stripComments = (s: string) =>
  s
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/^\s*\/\/.*$/gm, '')

describe('T690 A — 文案红线（§7A.13 V3.48）', () => {
  it('统一咨询入口页零「工单」措辞', () => {
    expect(stripComments(CONTACT_PAGE)).not.toContain('工单')
  })

  it('统一咨询入口页不承诺「微信客服回复您」类应用内回复通道', () => {
    const visible = stripComments(CONTACT_PAGE)
    expect(visible).not.toContain('微信客服回复您')
    expect(visible).not.toContain('客服将通过')
    expect(visible).not.toContain('回复您')
  })

  it('配网变体文案同样不踩两条红线', () => {
    const all = JSON.stringify(CONTACT) + FAILURE_COMMON.contactBtn
    expect(all).not.toContain('工单')
    expect(all).not.toContain('回复您')
  })

  it('通道二描述使用定稿句（稍后回复 + 引流在线客服）', () => {
    expect(CONTACT_PAGE).toContain('稍后回复')
    expect(CONTACT_PAGE).toContain('提交后运营人员会尽快查看处理')
    expect(CONTACT_PAGE).toContain('请直接使用上方「在线客服」')
  })
})

describe('T690 B — 留言类型词表（§8.2 Feedback V3.48）', () => {
  it('三选词表恒定且 wifi_setup_failure 不在其中', () => {
    expect([...FEEDBACK_TYPES]).toEqual(['佩戴咨询', '设备问题', '功能建议'])
    expect(WIFI_SETUP_FEEDBACK_TYPE).toBe('wifi_setup_failure')
    expect(FEEDBACK_TYPES).not.toContain(WIFI_SETUP_FEEDBACK_TYPE as never)
  })

  it('配网变体页的补充留言跳统一入口（不另立业务）', () => {
    const wifiPage = readSrc('pages/wifi-setup/index.vue')
    expect(wifiPage).toContain('/pages/contact/index')
  })
})

describe('T690 C — 「我的」页入口与设置抽屉撤销（§7A.5 V3.48 联动注）', () => {
  it('pages.json 注册 contact 路由，导航栏标题「联系我们」', () => {
    const entry = PAGES_JSON.pages.find((p: { path: string }) => p.path === 'pages/contact/index')
    expect(entry, 'pages.json 缺 pages/contact/index').toBeTruthy()
    expect(entry.style.navigationBarTitleText).toBe('联系我们')
  })

  it('一级菜单「联系我们」+ 副文案「在线客服 · 留言反馈」，直连 contact 页', () => {
    expect(PROFILE_PAGE).toContain('联系我们')
    expect(PROFILE_PAGE).toContain('在线客服 · 留言反馈')
    expect(PROFILE_PAGE).toContain("url: '/pages/contact/index'")
  })

  it('设置抽屉整组撤销：openSettingsSheet / bindWechat / logout 与三项文案均不得复活', () => {
    // 恢复点注记（HTML/行注释）允许引用被撤元素句——断言只看可执行代码面
    const code = PROFILE_PAGE.replace(/<!--[\s\S]*?-->/g, '')
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .replace(/^\s*\/\/.*$/gm, '')
    for (const gone of ['openSettingsSheet', 'settingsSheetVisible', 'bindWechat', 'logout', '退出登录', '绑定微信']) {
      expect(code, `profile 代码面不应再含 ${gone}`).not.toContain(gone)
    }
    // 恢复点注记必须留痕（原元素句/恢复点指路）
    expect(PROFILE_PAGE).toContain('恢复点')
  })

  it('原「设置」菜单行（⚙️ 设置）已被「联系我们」行取代', () => {
    expect(PROFILE_PAGE).not.toContain('>设置<')
    expect(PROFILE_PAGE).toContain('💬')
  })
})

describe('T690 D — session-from 身份透传（§7A.13 通道一）', () => {
  it('contact 页 MP 分支在线客服按钮带 session-from', () => {
    const mpBlock = CONTACT_PAGE.split('#ifdef MP-WEIXIN')[1]?.split('#endif')[0] ?? ''
    expect(mpBlock).toContain('open-type="contact"')
    expect(mpBlock).toContain(':session-from=')
  })

  it('配网变体在线客服按钮带 session-from（同一身份透传口径）', () => {
    const mpBlock = WIFI_CONTACT.split('#ifdef MP-WEIXIN')[1]?.split('#endif')[0] ?? ''
    expect(mpBlock).toContain('open-type="contact"')
    expect(mpBlock).toContain(':session-from=')
  })
})
