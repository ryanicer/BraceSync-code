/**
 * T465 防回潮门禁（源码级）：用户面不得直接展示错误对象的 message。
 *
 * 为什么是源码级而不是运行时：admin 的 ElMessage / 两端的 uni.showToast 都在页面 catch 块里，
 * 单测挂页面既取不到真机 toast，也无法覆盖全部页面（三端 148 个源文件）。这里按「展示汇点」
 * （ElMessage* / ElMessageBox* / ElNotification* / uni.showToast|showModal|showLoading 及其
 * title/content 选项键 / .vue 模板插值）逐行扫描，命中即红。
 *
 * 三条规则：
 *  R1 零容忍：展示汇点里读 error 型变量（e/err/error/ex/reason/writeErr/cause）的 .message —— 就是卡面
 *     点名的「裸展示 e.message」，一处都不许有。
 *  R2 例外台账：展示汇点里读其它对象的 .message（自研状态机 / 校验器的中文句）必须逐行登记在
 *     AUTHORED_COPY 里；新写法不登记就红，登记时得写明它为什么是代码自撰中文。
 *  R3 反向：展示汇点不得用 logErrorText（那是日志面出口，返回技术原文）。
 *
 * 未覆盖范围（如实登记）：先赋给 ref/变量再在模板渲染的间接展示、以及把 message 拼进请求体或
 * store 的写法，本门禁静态扫不到；那两类靠 error-copy.test.ts 的纯层判据与 e2e 用例兜。
 * 扫描面只有 apps 下三个包的 src（三端用户面）；本包 src/errorCopy.ts 自身读 message 是实现，不在禁列。
 * 清单出口：T465_SCAN=1 npx vitest run test/no-bare-message-display（交件用的逐行审计表）。
 */
import { readFileSync, readdirSync, statSync, mkdirSync, writeFileSync, rmSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { relative, join } from 'node:path'
import { describe, it, expect, afterAll } from 'vitest'

const REPO_ROOT = fileURLToPath(new URL('../../..', import.meta.url))
const SCAN_ROOTS = ['apps/admin-web/src', 'apps/patient-miniapp/src', 'apps/tech-miniapp/src']
const EXTS = new Set(['.ts', '.vue', '.js'])

/** R2 例外台账：文件 + 整行原文（行号会漂，按内容认） */
const AUTHORED_COPY: { file: string; line: string; why: string }[] = [
  {
    file: 'apps/admin-web/src/pages/review-records/index.vue',
    line: `ElMessage.error(extResult.message || '不支持的文件类型')`,
    why: 'extResult 来自 utils/review-report-whitelist.ts 的自研校验器，message 是代码写死的中文',
  },
  {
    file: 'apps/admin-web/src/pages/review-records/index.vue',
    line: `ElMessage.error(sizeResult.message || '文件大小超过 20MB')`,
    why: '同上，sizeResult 是校验器结果不是错误对象',
  },
  {
    file: 'apps/admin-web/src/pages/review-templates/index.vue',
    line: `ElMessage.error(extResult.message || '不支持的文件类型')`,
    why: '同 review-records，共用白名单校验器',
  },
  {
    file: 'apps/admin-web/src/pages/review-templates/index.vue',
    line: `ElMessage.error(sizeResult.message || '文件大小超过 20MB')`,
    why: '同 review-records，共用白名单校验器',
  },
  {
    file: 'apps/admin-web/src/pages/alerts/flow/designer/FlowDesigner.vue',
    line: '`结构校验有 ${result.warnings.length} 条提示（不阻断保存）：${result.warnings[0].message}`,',
    why: 'validateGraph 的 V 系规则文案（本仓自撰中文），不是后端返回',
  },
  {
    file: 'apps/admin-web/src/pages/alerts/flow/designer/NodePropsPanel.vue',
    line: `<em>{{ it.code.replace(/^V\\d+_/, '') }}</em>{{ it.message }}`,
    why: '同上，画布右栏问题清单渲染自研校验文案',
  },
  ...[
    { file: 'apps/patient-miniapp/src/pages/login/index.vue', line: `uni.showToast({ title: result.message || '登录失败，请重试', icon: 'none' })` },
    { file: 'apps/patient-miniapp/src/pages/login/bind.vue', line: `uni.showToast({ title: result.message || '操作已过期，请重新绑定', icon: 'none' })` },
    { file: 'apps/patient-miniapp/src/pages/login/bind.vue', line: `uni.showToast({ title: result.message || '绑定失败，请重试', icon: 'none' })` },
    { file: 'apps/patient-miniapp/src/pages/login/conflict.vue', line: `uni.showToast({ title: result.message || '操作已过期，请重新绑定', icon: 'none' })` },
    { file: 'apps/patient-miniapp/src/pages/login/conflict.vue', line: `uni.showToast({ title: result.message || '绑定失败，请重试', icon: 'none' })` },
    { file: 'apps/patient-miniapp/src/pages/login/no-match.vue', line: `uni.showToast({ title: result.message || '操作已过期，请重新绑定', icon: 'none' })` },
    { file: 'apps/patient-miniapp/src/pages/login/no-match.vue', line: `uni.showToast({ title: result.message || '绑定失败，请重试', icon: 'none' })` },
  ].map((x) => ({ ...x, why: 'result 是 utils/auth-state.ts 状态机结果，message 按 PRD §7A.1.1 逐码写死中文' })),
]

const SINK = /(ElMessage\.[a-z]+\(|ElMessageBox\.[a-z]+\(|ElNotification|uni\.show(?:Toast|Modal|Loading)\(|\btitle:\s|\bcontent:\s|\{\{[^}]*\}\})/
/** 卡面点名的「裸展示 e.message」：错误型变量名直接点 .message 进展示汇点 */
const ERROR_RECEIVER = /\b(?:e|err|error|errors|ex|reason|cause|writeErr|firstErr)\.message\b/
const MESSAGE_READ = /\.message\b/

function sourceFiles(): string[] {
  const out: string[] = []
  const walk = (dir: string) => {
    for (const name of readdirSync(dir)) {
      if (name === 'node_modules' || name.startsWith('.')) continue
      const p = join(dir, name)
      if (statSync(p).isDirectory()) walk(p)
      else if (EXTS.has(name.slice(name.lastIndexOf('.')))) out.push(p)
    }
  }
  for (const root of SCAN_ROOTS) walk(join(REPO_ROOT, root))
  return out
}

interface Hit {
  file: string
  lineNo: number
  text: string
  rule: 'R1' | 'R2' | 'R3'
}

/**
 * 仓内相对路径。反证探针写在 .t465-gate-probe/ 下，这里剥掉前缀，
 * 让探针的「文件身份」与它冒充的现网路径一致 —— 否则「正对照：已登记行不红」
 * 会因为路径多一层前缀而永远命中不了台账，红的是门禁自己的匹配而不是代码。
 */
function repoRel(abs: string): string {
  return relative(REPO_ROOT, abs).replace(/\\/g, '/').replace(/^\.t465-gate-probe\//, '')
}

function collectHits(files: string[]): Hit[] {
  const hits: Hit[] = []
  for (const abs of files) {
    const rel = repoRel(abs)
    const lines = readFileSync(abs, 'utf8').split(/\r?\n/)
    lines.forEach((raw, i) => {
      const text = raw.trim()
      if (!SINK.test(text)) return
      const entry: Hit = { file: rel, lineNo: i + 1, text, rule: 'R2' }
      if (MESSAGE_READ.test(text) && ERROR_RECEIVER.test(text)) entry.rule = 'R1'
      else if (/logErrorText\s*\(/.test(text)) entry.rule = 'R3'
      else if (!MESSAGE_READ.test(text)) return
      else if (AUTHORED_COPY.some((a) => a.file === rel && a.line === text)) return
      hits.push(entry)
    })
  }
  return hits
}

/**
 * 审计清单出口：T465_SCAN=1 npx vitest run test/no-bare-message-display
 * 用同一个 walker 把三端所有 .message 行按「展示汇点 / 非展示」列全，
 * 交件与验收读的就是这份 —— 判定只有一处真源（本文件），清单不另起一套正则。
 */
if (process.env.T465_SCAN) {
  const rows: string[] = []
  let sinkCount = 0
  for (const abs of sourceFiles()) {
    const rel = repoRel(abs)
    readFileSync(abs, 'utf8')
      .split(/\r?\n/)
      .forEach((raw, i) => {
        const text = raw.trim()
        if (!MESSAGE_READ.test(text)) return
        const isSink = SINK.test(text)
        if (isSink) sinkCount++
        rows.push(`${isSink ? 'SINK  ' : 'NONSINK'} ${rel}:${i + 1} ${text}`)
      })
  }
  console.log(`T465_SCAN files=${sourceFiles().length} messageLines=${rows.length} sinkLines=${sinkCount}`)
  console.log(rows.join('\n'))
}

describe('T465 展示层不得裸读 .message', () => {
  const files = sourceFiles()

  it('扫描面非空（路径写错时不能靠 0 命中假绿）', () => {
    expect(files.length).toBeGreaterThan(120)
    const covered = new Set(files.map(repoRel))
    for (const probe of [
      'apps/admin-web/src/utils/request.ts',
      'apps/patient-miniapp/src/pages/login/index.vue',
      'apps/tech-miniapp/src/utils/request.ts',
    ]) {
      expect(covered.has(probe), `扫描面缺 ${probe}`).toBe(true)
    }
  })

  it('现网源码 0 命中（R1 裸 error.message / R2 未登记 / R3 展示用 logErrorText）', () => {
    const hits = collectHits(files)
    expect(
      hits.map((h) => `${h.rule} ${h.file}:${h.lineNo} ${h.text}`),
      '展示汇点上出现未收口的 .message 读取',
    ).toEqual([])
  })

  it('反证：R1 注入裸 e.message 会被判红', () => {
    const injected = `ElMessage.error(error ? error.message : '加载失败')`
    expect(SINK.test(injected) && ERROR_RECEIVER.test(injected)).toBe(true)
    expect(collectHits([syntheticFile('apps/admin-web/src/pages/__gate_probe__.vue', [
      'import { ElMessage } from "element-plus"',
      injected,
    ])])).toHaveLength(1)
  })

  it('反证：R2 未登记的自研中文句也会被登记面卡住', () => {
    const line = `uni.showToast({ title: result.message || '绑定失败，请重试', icon: 'none' })`
    expect(collectHits([syntheticFile('apps/patient-miniapp/src/pages/login/unknown.vue', [line])])).toHaveLength(1)
  })

  it('反证：R3 把日志面出口接到展示汇点会被判红', () => {
    const line = `uni.showToast({ title: logErrorText(err), icon: 'none' })`
    const hits = collectHits([syntheticFile('apps/tech-miniapp/src/pages/__probe__.vue', [line])])
    expect(hits).toHaveLength(1)
    expect(hits[0].rule).toBe('R3')
  })

  it('正对照：已登记的自研中文句与已收口写法都不红', () => {
    const clean = [
      `uni.showToast({ title: userErrorCopy(e, { scope: 'tech', fallback: '绑定失败' }), icon: 'none' })`,
      `uni.showToast({ title: result.message || '操作已过期，请重新绑定', icon: 'none' })`,
      `bleLog.error('scanBLE 异常', logErrorText(e))`,
    ].join('\n')
    expect(collectHits([syntheticFile('apps/patient-miniapp/src/pages/login/bind.vue', clean.split('\n'))]))
      .toHaveLength(0)
  })

  it('例外台账不许留空条目（登记面自身要能被删干净）', () => {
    const rel = new Set(sourceFiles().map((f) => relative(REPO_ROOT, f).replace(/\\/g, '/')))
    for (const a of AUTHORED_COPY) {
      expect(rel.has(a.file), `台账指向不存在的文件：${a.file}`).toBe(true)
      expect(a.why.length).toBeGreaterThan(4)
    }
    // 台账必须逐条仍然成立：任何一条对应的行在源码里找不到 ⇒ 台账过期，得删（否则豁免面只会变大）
    const allText = files.map((f) => readFileSync(f, 'utf8')).join('\n')
    for (const a of AUTHORED_COPY) {
      expect(allText.includes(a.line), `台账行已在源码消失：${a.file} :: ${a.line}`).toBe(true)
    }
  })
})

/** 反证用的临时探针根目录（在扫描面之外，路径以 . 开头，collectHits 只按显式入参读它） */
const PROBE_ROOT = join(REPO_ROOT, '.t465-gate-probe')

/** 把内存里的行写成临时文件，喂给同一个 collectHits（反证必须走真扫描函数，不是复制判定逻辑） */
function syntheticFile(relPath: string, lines: string[]): string {
  const abs = join(PROBE_ROOT, relPath)
  mkdirSync(join(abs, '..'), { recursive: true })
  writeFileSync(abs, lines.join('\n'), 'utf8')
  return abs
}

afterAll(() => {
  rmSync(PROBE_ROOT, { recursive: true, force: true })
})
