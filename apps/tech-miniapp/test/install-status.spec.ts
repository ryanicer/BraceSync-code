/**
 * T443 — 技师端实现侧差异收口判据
 * 依据：docs/tasks/peter/T417-对照清单-技师端.md 的 TC-1／TC-2／TI-9／TI-12／TR-11；
 * PRD 第 862 行（跳过配网按钮＋弹窗文案）、§8.2 第 1772 行（可达性为本地派生展示）。
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

describe('TR-11 — WiFi 词形按稿面收口（records.html:102,134,204）', () => {
  it('两态标签＝已连接／未配置；未知值不落「已连接」（DB CHECK 只放行两值，第三态归 T446）', () => {
    expect(WIFI_STATUS_LABEL).toEqual({ connected: '已连接', unconfigured: '未配置' })
    expect(wifiStatusLabel('connected')).toBe('已连接')
    expect(wifiStatusLabel('unconfigured')).toBe('未配置')
    expect(wifiStatusLabel('failed')).toBe('未配置')
    expect(wifiStatusLabel(null)).toBe('未配置')
    expect(wifiStatusLabel(undefined)).toBe('未配置')
  })

  it('四个页面 + 工具层均无「已联网／待配置」残留在渲染行里', () => {
    const files = [...PAGE_NAMES.map((n) => pageSrc(n)), fs.readFileSync(
      fileURLToPath(new URL('../src/utils/installStatus.ts', import.meta.url)),
      'utf8',
    )]
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
    expect(src).toContain("import { wifiStatusLabel } from '../../utils/installStatus'")
    expect(src).not.toMatch(/'已连接' : '未配置'/)
  })

  it('每页用到的出口都在本页 import 里（漏 import 时模板运行时才炸，构建不报）', () => {
    const expected: Record<(typeof PAGE_NAMES)[number], { fns: string[]; consts: string[] }> = {
      install: { fns: ['confirmSkipNetwork'], consts: ['WIFI_SKIPPED_LABEL'] },
      complete: {
        fns: [
          'baselineStatusLabel',
          'baselineStatusBadgeClass',
          'wifiRowLabel',
          'reachabilityLabel',
          'reachabilityBadgeClass',
        ],
        consts: [],
      },
      records: { fns: ['wifiStatusLabel'], consts: [] },
      'wifi-config': { fns: ['confirmSkipNetwork'], consts: [] },
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
      for (const key of expected[name].consts) {
        expect(clause, `${name} 的 import 缺 ${key}`).toContain(key)
        expect(src, `${name} 里没插值 ${key}`).toContain(`{{ ${key} }}`)
      }
    }
  })

  it('跳过态的显示值只有一处字面量（页面插值走常量，防改一处漏一处）', () => {
    const literal = '已跳过配网'
    const util = fs.readFileSync(
      fileURLToPath(new URL('../src/utils/installStatus.ts', import.meta.url)),
      'utf8',
    )
    expect(codeLines(util).some((l) => l.includes(`${literal}'`)), '常量定义侧应有该字面量').toBe(true)
    for (const name of PAGE_NAMES) {
      for (const line of codeLines(pageSrc(name))) {
        expect(line, `${name} 页内又写了一遍字面量`).not.toContain(literal)
      }
    }
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

describe('TC-2 — 跳过配网只打本地标记，不冒充已连接', () => {
  it('store：跳过态 networkSkipped=true 且 wifiStatus 仍是 unconfigured', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()
    store.setNetworkSkipped(true)

    expect(store.networkSkipped).toBe(true)
    expect(store.wifiStatus).toBe('unconfigured')
  })

  it('store：真配网成功会撤掉跳过标记（handleSuccess 的两笔写入顺序）', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()
    store.setNetworkSkipped(true)
    store.setWifiStatus('connected')
    store.setNetworkSkipped(false)

    expect(store.wifiStatus).toBe('connected')
    expect(store.networkSkipped).toBe(false)
  })

  it('store：resetInstall 把跳过标记清零（跨会话不带过去）', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()
    store.setNetworkSkipped(true)
    store.resetInstall()

    expect(store.networkSkipped).toBe(false)
    expect(store.wifiStatus).toBe('unconfigured')
  })

  it('显示值：跳过态优先，WiFi 行与可达性行不会同屏互斥', () => {
    expect(wifiRowLabel('unconfigured', true)).toBe(WIFI_SKIPPED_LABEL)
    expect(wifiRowLabel('connected', true)).toBe(WIFI_SKIPPED_LABEL)
    expect(reachabilityLabel(true, false)).toBe('已跳过')
    expect(reachabilityLabel(true, true)).toBe('已跳过')
    expect(reachabilityLabel(false, true)).toBe('已验证')
    expect(reachabilityLabel(false, false)).toBe('待验证')
    expect(reachabilityBadgeClass(true, true)).toBe('status-pending')
    expect(reachabilityBadgeClass(false, true)).toBe('status-ok')
    expect(reachabilityBadgeClass(false, false)).toBe('status-warn')
  })

  it('wifi-config 的跳过入口不再把 wifiStatus 写成 connected', () => {
    const body = pageFnBody('wifi-config', 'skipNetworkSetup')
    expect(body).toContain('confirmSkipNetwork()')
    expect(body).toContain('installStore.setNetworkSkipped(true)')
    expect(body).not.toContain('setWifiStatus(')
    // 反证：改前那一行就是 setWifiStatus('connected')
    expect(pageSrc('wifi-config')).toContain("installStore.setWifiStatus('connected')")
    const successBody = pageFnBody('wifi-config', 'handleSuccess')
    expect(successBody).toContain("setWifiStatus('connected')")
    expect(successBody).toContain('setNetworkSkipped(false)')
  })

  it('install 页跳过态仍有出口：skipped 分支带「完成安装」，不致流程走死', () => {
    const src = pageSrc('install')
    expect(src).toContain("const wifiStage = computed<'before' | 'skipped' | 'done'>")
    expect(src).toContain("v-else-if=\"wifiStage === 'skipped'\"")
    const atSkipped = src.indexOf("wifiStage === 'skipped'")
    const atDone = src.indexOf('v-else>', atSkipped)
    expect(atDone).toBeGreaterThan(atSkipped)
    const skippedBlock = src.slice(atSkipped, atDone)
    expect(skippedBlock).toContain('completeInstall')
    expect(skippedBlock).toContain('{{ WIFI_SKIPPED_LABEL }}')
    expectVisibleLine(skippedBlock, 'completeInstall')
  })

  it('complete 页 WiFi 行走 wifiRowLabel，跳过态可读', () => {
    const src = pageSrc('complete')
    expect(src).toContain('wifiRowLabel(summary.value.wifiStatus, installStore.networkSkipped)')
  })
})

describe('TI-12 — 跳过入口文案与二次确认（PRD 第 862 行逐字）', () => {
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
 * 稿面 install.html 校准完成屏原无此入口（Peter 在 :245,249,400-404 的注记里把这条挂裁），
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
