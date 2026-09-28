/**
 * T443／T459 — 技师端安装流程展示与写侧判据
 * 依据：docs/tasks/peter/T417-对照清单-技师端.md 的 TC-1／TC-2／TI-9／TI-12／TR-11；
 * docs/tasks/winner/T447-WiFi状态唯一词形表.md 第一节（四档词形）；
 * PRD 第 876 行（跳过配网按钮＋弹窗文案）、§7C.6（-4 提示句）、§8.2 第 1786 行（可达性为本地派生展示）。
 * 以上 PRD／稿面行号由 T452 按 docs main 重取（原写 862／1772，随 #651、#661 并稿位移）；
 * 复核时按引号内文案定位，不要只按行号。
 *
 * 页面模板在本包测不到（vitest.config.ts 是 environment: 'node'，无 VTU），
 * 故判定与文案全下沉到 utils/installStatus.ts（可直接测），页面接线按源码断言
 * （同 test/provision-seq.spec.ts、test/b512-resubscribe.spec.ts 的既有写法）。
 */
import { describe, it, expect, vi, afterEach } from 'vitest'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('../src/utils/request', () => {
  const request = vi.fn()
  return { request, USE_MOCK: false }
})

import { useInstallStore } from '../src/stores/install'
import {
  WIFI_STATUS_LABEL,
  WIFI_SKIPPED_LABEL,
  SKIP_NETWORK_CONFIRM,
  wifiStatusLabel,
  wifiRowLabel,
  wifiBadgeTone,
  provisionFailureWifiStatus,
  baselineStatusLabel,
  baselineStatusBadgeClass,
  reachabilityLabel,
  reachabilityBadgeClass,
  confirmSkipNetwork,
} from '../src/utils/installStatus'

const PAGE_NAMES = ['install', 'complete', 'records', 'wifi-config'] as const

function pageSrc(name: (typeof PAGE_NAMES)[number]): string {
  return fs.readFileSync(
    fileURLToPath(new URL(`../src/pages/${name}/index.vue`, import.meta.url)),
    'utf8',
  )
}

/** 展示层真源本体：词表门禁要证明字面量活在这里、不在页面里 */
function utilSrc(): string {
  return fs.readFileSync(
    fileURLToPath(new URL('../src/utils/installStatus.ts', import.meta.url)),
    'utf8',
  )
}

/**
 * 只取会渲染／会执行的行。
 * 注释里的旧词（如「不再冒充『已联网』」）是解释为什么改，不算与稿面词并存。
 */
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

/** 取 .vue <script> 里某个函数的源码体（到下一个顶层 function 为止） */
function pageFnBody(name: (typeof PAGE_NAMES)[number], fn: string): string {
  const src = pageSrc(name)
  const start = src.search(new RegExp(`\\n(?:async )?function ${fn}\\(`))
  expect(start, `页面 ${name} 里找不到 function ${fn}`).toBeGreaterThan(-1)
  const rest = src.slice(start + 1)
  const next = rest.search(/\n(?:export )?(?:async )?function /)
  return next === -1 ? rest : rest.slice(0, next)
}

/** 反证用：确认 codeLines 真能看见渲染行，否则「无并存」是空过滤器造出来的假绿 */
function expectVisibleLine(src: string, needle: string): void {
  expect(codeLines(src).some((l) => l.includes(needle)), `codeLines 里应能看到 ${needle}`).toBe(true)
}

describe('TR-11／T459 — WiFi 四态词形按 T447 唯一词形表收口（records.html:95,127,197＋complete.html:81）', () => {
  it('四档标签＝已连接／未配置／连接失败／已跳过配网；大小写敏感；未知值仍回落「未配置」', () => {
    expect(WIFI_STATUS_LABEL).toEqual({
      connected: '已连接',
      unconfigured: '未配置',
      failed: '连接失败',
      skipped: '已跳过配网',
    })
    expect(wifiStatusLabel('connected')).toBe('已连接')
    expect(wifiStatusLabel('unconfigured')).toBe('未配置')
    // 改前这条把「库里存 failed 时显示未配置」当预期钉住（那时 DB CHECK 只两值）；
    // T447 迁移 000031 扩四值＋T459 按词形表补齐后，failed 显示连接失败，故本条极性翻转。
    expect(wifiStatusLabel('failed')).toBe('连接失败')
    expect(wifiStatusLabel('skipped')).toBe('已跳过配网')
    expect(wifiStatusLabel('CONNECTED')).toBe('未配置')
    expect(wifiStatusLabel('bogus')).toBe('未配置')
    expect(wifiStatusLabel(null)).toBe('未配置')
    expect(wifiStatusLabel(undefined)).toBe('未配置')
  })

  it('四个页面 + 工具层均无「已联网／待配置」残留在渲染行里', () => {
    const files = [...PAGE_NAMES.map((n) => pageSrc(n)), utilSrc()]
    // 正对照：过滤规则没把模板行滤空
    expectVisibleLine(pageSrc('records'), 'wifiStatusLabel(rec.wifiStatus)')
    for (const src of files) {
      for (const line of codeLines(src)) {
        expect(line).not.toMatch(/已联网/)
        expect(line).not.toMatch(/待配置/)
      }
    }
  })

  it('记录页徽章改接 wifiStatusLabel，不再页内自写一套三元词', () => {
    const src = pageSrc('records')
    expect(src).toContain("import { wifiStatusLabel, wifiBadgeTone } from '../../utils/installStatus'")
    expect(src).not.toMatch(/'已连接' : '未配置'/)
    expect(src).not.toMatch(/rec\.wifiStatus === 'connected' \? 'wifi-ok'/)
  })

  it('徽章三色按稿面 records.html：已连接绿／连接失败红／未配置灰；skipped 稿面未画，归灰（遗留已登记）', () => {
    expect(wifiBadgeTone('connected')).toBe('ok')
    expect(wifiBadgeTone('failed')).toBe('fail')
    expect(wifiBadgeTone('unconfigured')).toBe('pending')
    expect(wifiBadgeTone('skipped')).toBe('pending')
    expect(wifiBadgeTone(null)).toBe('pending')
    // 记录页三色的类名映射在本页，且三档都有对应样式，否则红档渲染成无样式
    const src = pageSrc('records')
    expect(src).toContain('WIFI_TONE_CLASS[wifiBadgeTone(status)]')
    for (const cls of ['wifi-ok', 'wifi-fail', 'wifi-pending']) {
      expect(src, `记录页缺 ${cls} 样式`).toContain(`.${cls} {`)
    }
  })

  it('C2 裁定（PM 2026-09-28）：只有 -4 落库 failed，-1／-2／-3 维持不改，失败原因走 toast 文案', () => {
    expect(provisionFailureWifiStatus(-4)).toBe('failed')
    expect(provisionFailureWifiStatus(-1)).toBe(null)
    expect(provisionFailureWifiStatus(-2)).toBe(null)
    expect(provisionFailureWifiStatus(-3)).toBe(null)
    // 非失败码不得顺手扳值（成功 9 走 handleSuccess 那条路）
    expect(provisionFailureWifiStatus(9)).toBe(null)
    expect(provisionFailureWifiStatus(0)).toBe(null)
  })

  it('每页用到的出口都在本页 import 里（漏 import 时模板运行时才炸，构建不报）', () => {
    const expected: Record<
      (typeof PAGE_NAMES)[number],
      { fns: string[]; consts: { key: string; usedAs: string }[] }
    > = {
      install: {
        fns: ['confirmSkipNetwork', 'wifiStatusLabel'],
        consts: [{ key: 'WIFI_FAILED_NOTE', usedAs: '? WIFI_FAILED_NOTE' }],
      },
      complete: {
        fns: [
          'baselineStatusLabel',
          'baselineStatusBadgeClass',
          'wifiRowLabel',
          'wifiBadgeTone',
          'reachabilityLabel',
          'reachabilityBadgeClass',
        ],
        consts: [],
      },
      records: { fns: ['wifiStatusLabel', 'wifiBadgeTone'], consts: [] },
      'wifi-config': {
        fns: ['confirmSkipNetwork', 'provisionFailureWifiStatus'],
        // -4 提示句取单一真源：页内查表引常量，不重抄文案
        consts: [{ key: 'WIFI_FAILED_NOTE', usedAs: '[-4]: WIFI_FAILED_NOTE' }],
      },
    }
    for (const name of PAGE_NAMES) {
      const src = pageSrc(name)
      const atImport = src.indexOf("from '../../utils/installStatus'")
      expect(atImport, `${name} 未 import installStatus`).toBeGreaterThan(-1)
      const clause = src.slice(src.lastIndexOf('import', atImport), atImport)
      for (const fn of expected[name].fns) {
        // 用到的符号必须同时在函数体／模板里被调用，否则是在测一条没人用的 import
        expect(clause, `${name} 的 import 缺 ${fn}`).toContain(fn)
        expect(src, `${name} 里没用到 ${fn}`).toContain(`${fn}(`)
      }
      for (const { key, usedAs } of expected[name].consts) {
        expect(clause, `${name} 的 import 缺 ${key}`).toContain(key)
        expect(src, `${name} 里没用到 ${key}（用法应为 ${usedAs}）`).toContain(usedAs)
        expectVisibleLine(src, usedAs)
      }
    }
  })

  /**
   * T459 后 skipped 进了查表，同一词形会在常量与表里各出现一次，
   * 所以门禁口径从「只有一处字面量」改成「字面量只在工具层、页面零字面量」。
   * 「已连接／未配置」两词有一处豁免：记录页 WiFi 筛选 chip 按 C1 裁定（PM 拍「丙」）本轮保持两档不动，
   * 那是筛选维度的选项文本、不是记录值的显示词。豁免要正向数出来，不能拿排除式扫成空过滤器假绿。
   */
  it('四档词形只活在工具层查表里，页面写死一律判红（记录页 chip 两处豁免除外）', () => {
    const words = ['已连接', '未配置', '连接失败', '已跳过配网']
    const strict = ['连接失败', '已跳过配网']
    const util = utilSrc()
    for (const w of words) {
      expect(codeLines(util).some((l) => l.includes(`'${w}'`)), `词表缺 ${w}`).toBe(true)
    }
    const mk = (list: string[]) => ({
      quoted: new RegExp("['\"`](" + list.join('|') + ")['\"`]"),
      baked: new RegExp('>\\s*(' + list.join('|') + ')\\s*<'),
    })
    const hard = mk(strict)
    const soft = mk(words)
    for (const name of PAGE_NAMES) {
      for (const line of codeLines(pageSrc(name))) {
        expect(line, `${name} 页内又写了一遍档名字面量`).not.toMatch(hard.quoted)
        expect(line, `${name} 页内把档名画死在模板里`).not.toMatch(hard.baked)
        // 全词表的命中只允许是筛选 chip；别处写死同样判红
        if (soft.quoted.test(line) || soft.baked.test(line)) {
          expect(line, `${name} 有非 chip 的档名写死行`).toContain('seg-btn')
        }
      }
    }
    // 正向取证：豁免实打实只有记录页那两行
    const exempt = PAGE_NAMES.flatMap((n) =>
      codeLines(pageSrc(n)).filter((l) => soft.quoted.test(l) || soft.baked.test(l)),
    )
    expect(exempt).toHaveLength(2)
    expect(exempt.every((l) => l.includes('seg-btn'))).toBe(true)
    // 反证：把 install 页的查表插值换回写死文本，硬门禁必须抓到
    const mutated = pageSrc('install').replace(
      '<text>{{ wifiStageLabel }}</text>',
      '<text>连接失败</text>',
    )
    expect(codeLines(mutated).some((l) => hard.baked.test(l)), '反证失效：模板形状已变到扫描看不见').toBe(true)
    // 负对照：稿面错误文案「网络连接失败（DHCP）」不是档名，不许被抓到
    expect(hard.quoted.test("'网络连接失败（DHCP），请检查路由器'")).toBe(false)
  })
})

describe('TC-1 — 完成页基线格改读 installStore.baselineSaved', () => {
  it('已保存／未保存与 status-ok／status-warn 成对', () => {
    expect(baselineStatusLabel(true)).toBe('已保存')
    expect(baselineStatusLabel(false)).toBe('未保存')
    expect(baselineStatusBadgeClass(true)).toBe('status-ok')
    expect(baselineStatusBadgeClass(false)).toBe('status-warn')
  })

  it('完成页渲染行不再硬编码「已保存」，改为同源标志', () => {
    const src = pageSrc('complete')
    expectVisibleLine(src, 'baselineStatusLabel(installStore.baselineSaved)')
    for (const line of codeLines(src)) {
      expect(line).not.toMatch(/>\s*已保存\s*</)
    }
    // 反证：改前那版是 <text>已保存</text>，上面的断言对它有牙
    const mutated = src.replace('>{{ baselineLabel }}<', '>已保存<')
    let hit = false
    for (const line of codeLines(mutated)) {
      if (/>\s*已保存\s*</.test(line)) hit = true
    }
    expect(hit, '反证失效：模板已被改动到断言看不见的形状').toBe(true)
  })
})

describe('TC-2 — 跳过配网写第四档 skipped，不冒充已连接', () => {
  it('store：跳过态两笔成对（wifiStatus=skipped 且 networkSkipped=true），resetInstall 一起清零', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()
    store.setWifiStatus('skipped')
    store.setNetworkSkipped(true)

    expect(store.wifiStatus).toBe('skipped')
    expect(store.networkSkipped).toBe(true)

    store.resetInstall()
    expect(store.networkSkipped).toBe(false)
    expect(store.wifiStatus).toBe('unconfigured')
  })

  it('store：真配网成功会撤掉跳过标记（两个跳过入口走完后重配的两笔写入顺序）', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()
    store.setWifiStatus('skipped')
    store.setNetworkSkipped(true)
    store.setWifiStatus('connected')
    store.setNetworkSkipped(false)

    expect(store.wifiStatus).toBe('connected')
    expect(store.networkSkipped).toBe(false)
  })

  it('store：C2 裁定后可达的失败档只有 failed，写侧由 -4 分支扳值', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()
    store.setWifiStatus(provisionFailureWifiStatus(-4)!)
    expect(store.wifiStatus).toBe('failed')
    // -1／-2／-3 不扳值：映射成 null 后写入分支不进，列维持默认
    expect(provisionFailureWifiStatus(-1)).toBe(null)
    expect(store.wifiStatus).toBe('failed')
    store.resetInstall()
    expect(store.wifiStatus).toBe('unconfigured')
  })

  it('显示值：跳过态优先，WiFi 行与可达性行不会同屏互斥', () => {
    expect(wifiRowLabel('unconfigured', true)).toBe(WIFI_SKIPPED_LABEL)
    expect(wifiRowLabel('connected', true)).toBe(WIFI_SKIPPED_LABEL)
    // T459：skipped 进了查表，未跳过时按库值取词（skipped 行不再靠页内三元词兜）
    expect(wifiRowLabel('failed', false)).toBe('连接失败')
    expect(wifiRowLabel('skipped', false)).toBe(WIFI_SKIPPED_LABEL)
    expect(reachabilityLabel(true, false)).toBe('已跳过')
    expect(reachabilityLabel(true, true)).toBe('已跳过')
    expect(reachabilityLabel(false, true)).toBe('已验证')
    expect(reachabilityLabel(false, false)).toBe('待验证')
    expect(reachabilityBadgeClass(true, true)).toBe('status-pending')
    expect(reachabilityBadgeClass(false, true)).toBe('status-ok')
    expect(reachabilityBadgeClass(false, false)).toBe('status-warn')
  })

  it('wifi-config 的跳过入口：改成写 skipped，但仍不许写 connected', () => {
    const body = pageFnBody('wifi-config', 'skipNetworkSetup')
    expect(body).toContain('confirmSkipNetwork()')
    expect(body).toContain("installStore.setWifiStatus('skipped')")
    expect(body).toContain('installStore.setNetworkSkipped(true)')
    // 改前极性：那时只打本地标记、断言是 not.toContain('setWifiStatus(')；
    // T459 写侧补齐后 skipped 必须真的进 wifiStatus（PUT 送的就是它），故本条翻成「要写 skipped」。
    expect(body).not.toContain("setWifiStatus('connected')")
    const successBody = pageFnBody('wifi-config', 'handleSuccess')
    expect(successBody).toContain("setWifiStatus('connected')")
    expect(successBody).toContain('setNetworkSkipped(false)')
  })

  it('wifi-config 的失败处理：只有 -4 经纯函数扳 failed，-1／-2／-3 不进写侧', () => {
    const body = pageFnBody('wifi-config', 'handleError')
    expect(body).toContain('provisionFailureWifiStatus(code)')
    expect(body).toContain('if (nextStatus) installStore.setWifiStatus(nextStatus)')
    // 反证：无条件写 failed 会把密码错（-1）也落库，与 C2 裁定相反
    const mutated = body.replace('if (nextStatus) installStore.setWifiStatus(nextStatus)', "installStore.setWifiStatus('failed')")
    expect(mutated).not.toContain('if (nextStatus)')
    expect(mutated).toContain("setWifiStatus('failed')")
    // 三条非 -4 的稿面文案留在页内 toast/errorMessage，不落库
    // （-4 那格取单一真源常量，由上面「每页用到的出口」那条门禁点名）
    const src = pageSrc('wifi-config')
    for (const copy of ['密码错误，请检查 WiFi 密码', '未找到 WiFi 网络，请检查 SSID', '网络连接失败（DHCP），请检查路由器']) {
      expectVisibleLine(src, copy)
    }
  })

  it('install 页跳过入口与 wifi-config 同源：两笔成对，写完才留在本页阶段三显示', () => {
    const body = pageFnBody('install', 'onSkipNetwork')
    expect(body).toContain('confirmSkipNetwork()')
    expect(body).toContain("installStore.setWifiStatus('skipped')")
    expect(body).toContain('installStore.setNetworkSkipped(true)')
    expect(body).not.toContain("setWifiStatus('connected')")
  })

  it('install 页：跳过／失败两态各有色档，且都留「完成安装」出口，不致流程走死', () => {
    const src = pageSrc('install')
    expect(src).toContain("const wifiStage = computed<'before' | 'skipped' | 'failed' | 'done'>")
    const atUnion = src.indexOf("wifiStage === 'skipped' || wifiStage === 'failed'")
    expect(atUnion, 'skipped／failed 未并成同一显示块').toBeGreaterThan(-1)
    const atDone = src.indexOf('v-else>', atUnion)
    expect(atDone).toBeGreaterThan(atUnion)
    const block = src.slice(atUnion, atDone)
    expect(block).toContain('completeInstall')
    // 词形走查表插值，两态共用一个 label computed；色档按 failed 分叉
    expect(block).toContain('{{ wifiStageLabel }}')
    expect(block).toContain("wifiStage === 'failed' ? 'badge-danger' : 'badge-warning'")
    expect(src).toContain('.badge-danger {')
    expectVisibleLine(block, 'completeInstall')
    // 反证：failed 从互斥并集里被摘掉 ⇒ 失败态会掉回 v-else（配网成功块），本页再无完成出口
    const union = "wifiStage === 'skipped' || wifiStage === 'failed'"
    expect(src).toContain(union)
    const mutated = src.replace(union, "wifiStage === 'skipped'")
    expect(mutated).not.toContain(union)
    expect(mutated).toContain("<template v-else-if=\"wifiStage === 'skipped'\">")
  })

  it('complete 页 WiFi 行走 wifiRowLabel＋三色 tone，跳过态优先', () => {
    const src = pageSrc('complete')
    expect(src).toContain('wifiRowLabel(summary.value.wifiStatus, installStore.networkSkipped)')
    expect(src).toContain("wifiBadgeTone(installStore.networkSkipped ? 'skipped' : summary.value.wifiStatus)")
    expect(src).toContain("tone === 'fail' ? 'status-fail'")
    expect(src).toContain('.status-fail {')
  })
})

describe('TI-12 — 跳过入口文案与二次确认（PRD 第 876 行逐字）', () => {
  const uniBackup = (globalThis as { uni?: unknown }).uni

  afterEach(() => {
    if (uniBackup === undefined) delete (globalThis as { uni?: unknown }).uni
    else (globalThis as { uni?: unknown }).uni = uniBackup
  })

  it('弹窗标题与正文按 PRD 原文，不在实现里自拟', () => {
    expect(SKIP_NETWORK_CONFIRM.title).toBe('跳过配网')
    expect(SKIP_NETWORK_CONFIRM.content).toBe('跳过配网后设备将无法自动上传数据，确定跳过？')
  })

  it('confirmSkipNetwork 把原文传给 showModal，并按 confirm／fail 返回', async () => {
    const calls: { title: string; content: string }[] = []
    ;(globalThis as { uni?: unknown }).uni = {
      showModal: (opt: { title: string; content: string; success?: (r: { confirm: boolean }) => void; fail?: () => void }) => {
        calls.push({ title: opt.title, content: opt.content })
        opt.success?.({ confirm: true })
      },
    }
    await expect(confirmSkipNetwork()).resolves.toBe(true)
    expect(calls).toEqual([
      { title: '跳过配网', content: '跳过配网后设备将无法自动上传数据，确定跳过？' },
    ])

    ;(globalThis as { uni?: unknown }).uni = {
      showModal: (opt: { success?: (r: { confirm: boolean }) => void }) => opt.success?.({ confirm: false }),
    }
    await expect(confirmSkipNetwork()).resolves.toBe(false)

    // 弹窗失败按「没跳过」处理，不能顺水推舟置成已跳过
    ;(globalThis as { uni?: unknown }).uni = {
      showModal: (opt: { fail?: () => void }) => opt.fail?.(),
    }
    await expect(confirmSkipNetwork()).resolves.toBe(false)
  })

  it('两个跳过入口（install 阶段三＋wifi-config）都先过确认；wifi-config 旧「先完成安装」按钮已改名', () => {
    expect(pageFnBody('install', 'onSkipNetwork')).toContain('confirmSkipNetwork()')
    expect(pageFnBody('wifi-config', 'skipNetworkSetup')).toContain('confirmSkipNetwork()')
    const wifi = pageSrc('wifi-config')
    expectVisibleLine(wifi, '<text>跳过配网</text>')
    for (const src of [wifi, pageSrc('install')]) {
      for (const line of codeLines(src)) expect(line).not.toMatch(/先完成安装/)
    }
  })
})

describe('TI-9 — 基线提交成功侧有反馈（稿面 install.html 校准成功路径的 showToast「基线已提交云端」）', () => {
  it('saveBaseline 成功后 toast「基线已提交云端」，位置在置位之后、catch 之前', () => {
    const body = pageFnBody('install', 'finalizeCalibration')
    const atSaved = body.indexOf('installStore.setBaselineSaved(bs.baselineId)')
    const atToast = body.indexOf("uni.showToast({ title: '基线已提交云端'")
    const atCatch = body.indexOf('} catch (e) {')
    expect(atSaved).toBeGreaterThan(-1)
    expect(atToast).toBeGreaterThan(atSaved)
    expect(atCatch).toBeGreaterThan(atToast)
  })
})

/**
 * 裁定⑥（Boss 2026-09-28，经 PM 评论转述）：允许现场重新采集一次。
 * 稿面 install.html 校准完成屏原无此入口（Peter 的挂裁注记按 docs main 61edaf98 现位于 install.html
 * :243-250／:254-262／:271-274／:444-459，随并稿还会继续位移 ⇒ 复核按引号内文案定位，别只按行号）；
 * 现按裁定落地，但必须与规矩 A（PRD §7C.4：校准是一次性权威动作、无复校通道）不冲突：
 * 云端已有权威基线（保存成功或 20409）时入口必须收起。
 */
describe('T443 裁定⑥ — 重新采集入口只在不破坏规矩 A 的前提下出现', () => {
  const src = pageSrc('install')

  it('入口只挂在采集中态（稿面 T449·TI-6），校准完成态不留入口', () => {
    const atCollecting = src.indexOf('v-else-if="calibrating && !calibrated"')
    const atDone = src.indexOf('<!-- 校准完成：零点偏移矩阵')
    const atBtn = src.indexOf('recollectCalibration"><text>重新采集')
    expect(atCollecting).toBeGreaterThan(-1)
    expect(atDone).toBeGreaterThan(atCollecting)
    expect(atBtn, '「重新采集」不在采集中态块内').toBeGreaterThan(atCollecting)
    expect(atBtn, '「重新采集」漏进校准完成态，与稿面 T449·TI-6 相反').toBeLessThan(atDone)
    const line = src.slice(src.lastIndexOf('\n', atBtn) + 1, src.indexOf('\n', atBtn))
    expect(line).toContain('v-if="!baselineLocked"')
    // 反证：闸门被删掉后，上面的行内判据必须判红
    const mutated = line.replace('v-if="!baselineLocked" ', '')
    expect(mutated).not.toContain('v-if="!baselineLocked"')
    expect(mutated).toContain('recollectCalibration')
  })

  it('闸门真源＝store.baselineSaved 或 20409 冲突，两者任一成立即收起入口', () => {
    const atLock = src.indexOf('const baselineLocked = computed(')
    expect(atLock).toBeGreaterThan(-1)
    const line = src.slice(atLock, src.indexOf('\n', atLock))
    expect(line).toContain('installStore.baselineSaved')
    expect(line).toContain('baselineConflict.value')
    // 「保存成功」这一支由 store 承载，不能只测 409
    expect(src).toContain('installStore.setBaselineSaved(bs.baselineId)')
  })

  it('20409 分支置位 baselineConflict（库里已有权威基线 ⇒ 不再给重采入口）', () => {
    const body = pageFnBody('install', 'finalizeCalibration')
    const at409 = body.indexOf('bizCode === 20409')
    const atSet = body.indexOf('baselineConflict.value = true')
    expect(at409).toBeGreaterThan(-1)
    expect(atSet).toBeGreaterThan(at409)
    // 置位在 catch 内、非 409 分支之前：普通保存失败仍要留入口（那才是本裁定要救的场景）
    const atElse = body.indexOf('} else {', at409)
    expect(atSet).toBeLessThan(atElse)
  })

  it('recollectCalibration 复位采集态并重启采集，不落基线', () => {
    const body = pageFnBody('install', 'recollectCalibration')
    expect(body).toContain('calibrated.value = false')
    expect(body).toContain('collectedPointCount.value = 0')
    expect(body).toContain('offsetValues.value = Array(20).fill(0)')
    expect(body).toContain('await startCalibration()')
    // 本函数只做前端状态复位，不碰写通道
    expect(body).not.toContain('saveBaseline(')
    expect(body).not.toContain('setBaselineSaved(')
  })

  it('采集中止重采要先收计时器与实时流（不收会叠第二个 interval，finalize 触发两次）', () => {
    const body = pageFnBody('install', 'recollectCalibration')
    const atClear = body.indexOf('clearInterval(collectTimer)')
    const atStop = body.indexOf('await stopRealtimePressure(')
    const atStart = body.indexOf('await startCalibration()')
    expect(atClear).toBeGreaterThan(-1)
    expect(atStop).toBeGreaterThan(atClear)
    expect(atStart).toBeGreaterThan(atStop)
  })

  it('蓝牙断链时先给提示、不静默起采（重采依赖实时取数）', () => {
    const body = pageFnBody('install', 'recollectCalibration')
    const atGuard = body.indexOf('if (!installStore.bleConnected)')
    const atToast = body.indexOf("uni.showToast({ title: '蓝牙未就绪，请先重新连接'")
    const atStart = body.indexOf('await startCalibration()')
    expect(atGuard).toBeGreaterThan(-1)
    expect(atToast).toBeGreaterThan(atGuard)
    expect(atStart).toBeGreaterThan(atToast)
    // 提示分支必须 return，否则守卫等于没加
    expect(body.slice(atToast, atStart)).toContain('return')
  })

  it('TW-8「立即返回」实现侧本轮未动（裁定只落在采集态，四步流程不变）', () => {
    for (const name of PAGE_NAMES) {
      for (const line of codeLines(pageSrc(name))) {
        expect(line, `${name} 页出现了未裁的「立即返回」`).not.toContain('立即返回')
      }
    }
  })
})
