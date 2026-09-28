/**
 * T465 卡面 ④「映射表落 docs 供三端共用」的对账门禁：
 * docs 镜像（packages/shared-utils/docs/error-copy.md）必须与代码里的映射表逐行对平。
 *
 * 为什么在本仓而不是 docs 仓：code CI 不检出 docs 仓（T370 已实测），跨仓判据在 CI 里必抛错。
 * 所以这里锁住「代码真源 ↔ 仓内镜像」，docs 仓那份由同一内容拷贝；两仓是否同字用
 * scripts/ci/check-error-copy-doc-sync.mjs 在本机对平（原文输出进证据包）。
 */
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, it, expect } from 'vitest'
import {
  ADMIN_ERROR_CODE_COPY,
  ERROR_CODE_COPY,
  PATIENT_ERROR_CODE_COPY,
  TECH_ERROR_CODE_COPY,
  userErrorCopy,
} from '../src/errorCopy'

const DOC = fileURLToPath(new URL('../docs/error-copy.md', import.meta.url))
const md = readFileSync(DOC, 'utf8')

/** 基础表行：| 10401 | 文案 | 语义来源 | */
const BASE_ROWS = md
  .split(/\r?\n/)
  .filter((l) => /^\|\s*[1-9][0-9]{4}\s*\|/.test(l))
  .map((l) => {
    const cells = l.split('|').slice(1, -1).map((c) => c.trim())
    return { code: Number(cells[0]), copy: cells[1], cols: cells.length }
  })

/** 覆盖表行：| patient | 10401 | 文案 | 依据 | */
const OVERRIDE_ROWS = md
  .split(/\r?\n/)
  .filter((l) => /^\|\s*(patient|admin|tech)\s*\|/.test(l))
  .map((l) => {
    const cells = l.split('|').slice(1, -1).map((c) => c.trim())
    return { scope: cells[0], code: Number(cells[1]), copy: cells[2], cols: cells.length }
  })

const SCOPE_TABLES: Record<string, Record<number, string>> = {
  admin: ADMIN_ERROR_CODE_COPY,
  patient: PATIENT_ERROR_CODE_COPY,
  tech: TECH_ERROR_CODE_COPY,
}

describe('T465 映射表的 docs 镜像与代码真源对平', () => {
  it('镜像读到了内容（读取失败时不许靠空数组假绿）', () => {
    expect(BASE_ROWS.length).toBeGreaterThan(30)
    expect(OVERRIDE_ROWS.length).toBe(10)
    // 每行都得是「码 + 文案 + 依据」三列，列数掉了说明表格被改坏，后面的对平就没意义
    for (const r of BASE_ROWS) expect(r.cols, `${r.code}`).toBe(3)
    for (const r of OVERRIDE_ROWS) expect(r.cols, `${r.scope} ${r.code}`).toBe(4)
  })

  it('基础表：码集合与文案逐行相等（镜像缺行／多行／抄错都判红）', () => {
    const tableCodes = Object.keys(ERROR_CODE_COPY).map(Number).sort((a, b) => a - b)
    const docCodes = BASE_ROWS.map((r) => r.code).sort((a, b) => a - b)
    expect(docCodes).toEqual(tableCodes)
    for (const r of BASE_ROWS) {
      expect(r.copy, `码 ${r.code} 的文案与代码不一致`).toBe(ERROR_CODE_COPY[r.code])
    }
  })

  it('覆盖表：三端覆盖码集合与文案逐行相等', () => {
    for (const [scope, table] of Object.entries(SCOPE_TABLES)) {
      const rows = OVERRIDE_ROWS.filter((r) => r.scope === scope)
      expect(rows.map((r) => r.code).sort((a, b) => a - b), scope).toEqual(
        Object.keys(table).map(Number).sort((a, b) => a - b)
      )
      for (const r of rows) expect(r.copy, `${scope} ${r.code}`).toBe(table[r.code])
    }
  })

  it('患者端覆盖表覆盖到 PRD §7A.1.1 点名的码（防镜像只写一半）', () => {
    const patientRows = OVERRIDE_ROWS.filter((r) => r.scope === 'patient').map((r) => r.code)
    for (const code of [10001, 10401, 10502, 10601, 10602, 10603, 10604, 10605]) {
      expect(patientRows, `patient ${code}`).toContain(code)
    }
  })

  it('镜像里的兜底形状与实现一致（这两句写错，三端就照着错的下抄）', () => {
    // 无兜底句分支：整句形状写死
    const whole = docCopyOfRow('该页没给兜底句')
    expect(userErrorCopy(Object.assign(new Error('boom'), { code: 42424 }))).toBe(
      whole.replace('<token>', '42424')
    )
    // 有兜底句分支：形状为「原句（错误码 X）」
    const shaped = docCopyOfRow('该页有自研中文兜底句')
    const err = Object.assign(new Error('collectIntervalSeconds must be >= 30'), {
      code: 42424,
      httpStatus: 400,
    })
    expect(userErrorCopy(err, { scope: 'admin', fallback: '加载失败' })).toBe(
      shaped.replace('<页面原有中文>', '加载失败').replace('<token>', '42424')
    )
    // 传输层失败（无码无 HTTP 状态）走同一形状，token 退化成 NET
    expect(userErrorCopy(new Error('request:fail timeout'), { fallback: '加载失败' })).toBe(
      shaped.replace('<页面原有中文>', '加载失败').replace('<token>', 'NET')
    )
  })
})

/** 取「三、未知码与网络层兜底」表里某一行的文案列（去掉 markdown 反引号） */
function docCopyOfRow(lead: string): string {
  const line = md.split(/\r?\n/).find((l) => l.includes(lead))
  expect(line, `镜像里缺兜底形状行：${lead}`).toBeTruthy()
  const cell = (line as string).split('|').slice(2, 3)[0].trim()
  return cell.replace(/`/g, '')
}
