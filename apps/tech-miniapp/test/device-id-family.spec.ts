/**
 * T406 — 设备 ID 家族门禁（假串短尾号形态不许回流到实现 / 夹具 / 源码）
 *
 * 背景：`PRS-ML05-RC-` 前缀后只跟 3 位数字那族号（早期 mock 留下的短尾号），
 * 现网精确命中 0，形态却不加标注，技师照抄会绑不到设备（T362 删页面硬编码、T380 收口绑定页示例）。
 * T380 工程复验（U4）登记了修复范围外的 9 行残留，T406 按 PM 裁定延伸清理。
 *
 * 本用例钉三件事，对应卡面「产物可 grep、夹具可 grep、稿面与实现同源」里源码侧那两条：
 *  1. 全仓（排除依赖、构建产物、docs 任务记录）再没有出现「`PRS-ML05-RC-` + 非 11 位数字」的写法，
 *     除非它落在下面的正当引用清单里（注释里点名旧值、反向断言、模板前缀段等）；
 *  2. 患者端 mock / E2E 夹具 / e2e helpers 三处设备 ID 同读一个常量，不再各留一份字面量；
 *  3. mock 用「形态合法但属保留合成批次」的号段，既不冒充现网设备，也不与技师端示例串撞号。
 *
 * 判据全部读源码字符串，不 import 页面模块 —— 与 test/bind-scan.spec.ts 同一约定
 * （uni-app 条件编译链在 vitest 里读不动，import 会假绿）。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { DEVICE_ID_EXAMPLE, DEVICE_ID_SHAPE } from '../src/utils/bind-copy'

const REPO = fileURLToPath(new URL('../../../', import.meta.url))
const read = (rel: string) => fs.readFileSync(path.join(REPO, rel), 'utf8')

/**
 * 现网 5 台设备的批次（T406 只读 `GET /devices` 实测；`scripts/db/seed/seed.sql` 同一族）。
 * 分成日期段与序号拼出来写，是为了本门禁不自扫到自己（下面的全仓扫描会读源文本）。
 */
const LIVE_BATCH_DATE = '20260701'
const LIVE_BATCH = new RegExp(`^PRS-ML05-RC-${LIVE_BATCH_DATE}\\d{3}$`)

/**
 * 正当引用：出现在注释/反向断言/模板前缀里的非 11 位写法。
 * 每条按「文件 + 定位子串」认领，定位子串故意不含假串本身（否则本文件自己会被扫成残留）。
 * 清理掉一处就该同步删掉一条 —— 下面的陈旧条目用例会替我盯着。
 */
const LEGITIMATE: Array<{ file: string; on: string; why: string }> = [
  { file: 'apps/patient-miniapp/src/mock/device.ts', on: '是 T380 清理过的假串形态', why: '注释点名被替换掉的旧值' },
  { file: 'apps/tech-miniapp/src/api/install.ts', on: '是 3 位尾号假串', why: '注释点名被替换掉的旧值' },
  { file: 'apps/tech-miniapp/src/api/install.ts', on: "deviceId: `PRS-ML05-RC-", why: '模板前缀段（8 位日期 + 3 位序号分开拼），最终值由下面的合成用例校验' },
  { file: 'apps/tech-miniapp/src/api/alert.ts', on: "deviceId: `PRS-ML05-RC-", why: 'T433 告警 mock 沿用同一保留合成批次的模板前缀段' },
  { file: 'apps/tech-miniapp/src/pages/bind/index.vue', on: 'T362: 去掉 T089 的 mock 硬编码', why: 'T362 留下的历史说明注释' },
  { file: 'apps/tech-miniapp/src/utils/bind-copy.ts', on: '旧示例', why: '注释点名被替换掉的旧示例串' },
  { file: 'apps/tech-miniapp/test/bind-scan.spec.ts', on: 'T380 — 同页设备ID 示例口径', why: '文件头注释' },
  { file: 'apps/tech-miniapp/test/bind-scan.spec.ts', on: 'expect(DEVICE_ID_EXAMPLE).not.toBe', why: '反向断言：示例串不得是旧假串' },
  { file: 'apps/tech-miniapp/test/bind-scan.spec.ts', on: 'expect(DEVICE_ID_PLACEHOLDER.includes', why: '反向断言：占位文案不得含旧假串' },
  { file: 'apps/tech-miniapp/test/bind-scan.spec.ts', on: 'expect(template).not.toMatch', why: '反向断言：模板段不得再现假串族' },
  { file: 'e2e/tech-helpers.ts', on: '旧值', why: '注释点名被替换掉的旧夹具值' },
  { file: 'e2e/tests/tech-bind.spec.ts', on: '设备ID 输入框示例为真实形态', why: '用例标题点名旧假串' },
  { file: 'services/device-service/internal/model/model_test.go', on: 'dev_sim_02', why: '校验器放宽用例的入参表：刻意列入非规范形态的历史 ID，证明 ValidDeviceID 的宽松白名单仍在' },
]

const SKIP_DIRS = new Set(['node_modules', '.git', 'dist', 'build', 'coverage', 'test-results', 'playwright-report', 'unpackage', '.uni-app'])
const SCAN_EXT = new Set(['.ts', '.tsx', '.js', '.cjs', '.mjs', '.vue', '.go', '.sql', '.json', '.yaml', '.yml', '.html', '.sh', '.ps1'])

/** docs/ 是任务记录与验收证据，会原样引用当时的假串，属历史材料而非产物 */
function isSkippedDir(relDir: string) {
  return relDir === 'docs' || relDir.startsWith('docs' + path.sep)
}

function collectShortFormHits() {
  const hits: Array<{ file: string; line: number; text: string }> = []
  const walk = (dir: string) => {
    for (const ent of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, ent.name)
      const rel = path.relative(REPO, full)
      if (ent.isDirectory()) {
        if (SKIP_DIRS.has(ent.name) || isSkippedDir(rel)) continue
        walk(full)
        continue
      }
      if (!SCAN_EXT.has(path.extname(ent.name))) continue
      const lines = fs.readFileSync(full, 'utf8').split(/\r?\n/)
      lines.forEach((line, idx) => {
        for (const m of line.matchAll(/PRS-ML05-RC-(\d+)/g)) {
          if (m[1].length !== 11) hits.push({ file: rel.split(path.sep).join('/'), line: idx + 1, text: line.trim() })
        }
      })
    }
  }
  walk(REPO)
  return hits
}

describe('设备 ID 假串族（T406 范围一 U4）', () => {
  it('全仓非 11 位形态只剩正当引用，且每条引用都还在', () => {
    const hits = collectShortFormHits()
    const unexplained = hits.filter((h) => !LEGITIMATE.some((l) => l.file === h.file && h.text.includes(l.on)))
    expect(unexplained.map((h) => `${h.file}:${h.line} ${h.text}`)).toEqual([])

    const stale = LEGITIMATE.filter((l) => !hits.some((h) => h.file === l.file && h.text.includes(l.on)))
    expect(stale.map((l) => `${l.file} :: ${l.on}`)).toEqual([])
  })

  it('患者端三处设备 ID 同读一个常量，不再各留字面量', () => {
    const deviceSrc = read('apps/patient-miniapp/src/mock/device.ts')
    const monitorSrc = read('apps/patient-miniapp/src/mock/monitor.ts')
    const fixtureSrc = read('apps/patient-miniapp/tests/e2e/fixtures/patient.ts')
    const helpersSrc = read('e2e/helpers.ts')

    expect(deviceSrc).toMatch(/export const MOCK_DEVICE_ID = '(PRS-ML05-RC-\d{11})'/)
    expect(monitorSrc).toMatch(/import \{ MOCK_DEVICE_ID \} from '\.\/device'/)
    expect(monitorSrc).toMatch(/deviceId: MOCK_DEVICE_ID,/)
    expect(fixtureSrc).toMatch(/import \{ MOCK_DEVICE_ID \} from '\.\.\/\.\.\/\.\.\/src\/mock\/device'/)
    expect(fixtureSrc).toMatch(/export const E2E_DEVICE_ID = MOCK_DEVICE_ID/)
    expect(helpersSrc).toMatch(/E2E_DEVICE_ID,\s*\} from '\.\.\/apps\/patient-miniapp\/tests\/e2e\/fixtures\/patient'/)
    expect(helpersSrc).toMatch(/export const HOTSPOT_NAME = E2E_DEVICE_ID/)
  })

  it('技师端夹具与绑定页示例同源（示例换号时夹具跟着换）', () => {
    const helpersSrc = read('e2e/tech-helpers.ts')
    expect(helpersSrc).toMatch(/import \{ DEVICE_ID_EXAMPLE \} from '\.\.\/apps\/tech-miniapp\/src\/utils\/bind-copy'/)
    expect(helpersSrc).toMatch(/export const MOCK_DEVICE_ID = DEVICE_ID_EXAMPLE/)
  })
})

describe('mock 号段取值（不冒充现网设备）', () => {
  it('患者端 mock 用保留合成批次，形态合法且不与现网 20260701 族撞号', () => {
    const src = read('apps/patient-miniapp/src/mock/device.ts')
    const id = src.match(/export const MOCK_DEVICE_ID = '(PRS-ML05-RC-\d+)'/)?.[1]
    expect(id).toBeDefined()
    expect(id).toMatch(DEVICE_ID_SHAPE)
    expect(id).not.toMatch(LIVE_BATCH)
    expect(id).not.toBe(DEVICE_ID_EXAMPLE)
  })

  it('技师端安装记录 seed 逐条形态合法，且首条与 e2e 断言值是同一串', () => {
    const src = read('apps/tech-miniapp/src/api/install.ts')
    const dateSeg = src.match(/deviceId: `PRS-ML05-RC-(\d{8})\$\{String\(i \+ 1\)\.padStart\(3, '0'\)\}`/)?.[1]
    expect(dateSeg).toBe('19700101')
    // 条数不写死：T433 把 mock 集从 6 扩到 26 正是为了让翻页那条腿在本地被执行，
    // 这里若钉 6 就等于让门禁替旧基数背书。
    const total = Number(src.match(/const MOCK_INSTALL_TOTAL = (\d+)/)?.[1])
    expect(total).toBeGreaterThan(20)
    const ids = Array.from({ length: total }, (_, i) => `PRS-ML05-RC-${dateSeg}${String(i + 1).padStart(3, '0')}`)
    for (const id of ids) {
      expect(id).toMatch(DEVICE_ID_SHAPE)
      expect(id).toHaveLength(23)
      expect(id).not.toMatch(LIVE_BATCH)
    }
    const specSrc = read('e2e/tests/tech-records.spec.ts')
    const asserted = specSrc.match(/\.record-device'\)\.first\(\)\)\.toHaveText\('(PRS-ML05-RC-\d{11})'\)/)?.[1]
    expect(asserted).toBe(ids[0])
  })

  it('网关密钥用例改用的 ID 也是真实形态', () => {
    const src = read('services/gateway/cmd/server/secret_provider_impl_test.go')
    const ids = [...src.matchAll(/"PRS-ML05-RC-(\d{11})"/g)].map((m) => `PRS-ML05-RC-${m[1]}`)
    expect(ids).toHaveLength(2)
    for (const id of ids) expect(id).toMatch(DEVICE_ID_SHAPE)
  })
})
