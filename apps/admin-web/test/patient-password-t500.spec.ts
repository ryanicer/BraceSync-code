// T500 患者设登录口令（admin-web 人工入口）
//
// 四条关注点，逐条对应「这个入口写错了会怎样」：
//  1 角色闸门真值表：判据写反＝给非 admin 放出一个后端必 403 的按钮（T351 的现场形态）。
//  2 闸门与网关真源对拍：判据的依据是 rbac.go 把这条端点收在 adminOnlyPatterns，
//    后端一旦把它挪到别的矩阵（或删掉），这条先红——防止「前端闸门还在、后端已放行更多角色」
//    或反过来「后端收窄、前端还挂着入口」两种静默分叉。
//  3 mock 层与 T477 handler 同形：未知患者要抛（后端 404 且判定序在写之前），口令形态照
//    服务端发号器（前缀 + 8 位随机 + 后缀），每次调用都得不同——一次性凭据不许长得像常量。
//  4 页面接线（源码级）：本项目的页面 SFC 在 vitest 里挂载成本高于收益（要拖 QR 生成与整张表格），
//    故按仓内既有写法只钉接线事实：入口存在且被闸门包住、口令只出现在一次性弹窗里、
//    取消那一腿不发请求。浏览器里的真实形状由 e2e/tests/admin-patient-writes.spec.ts 负责。
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { canSetPatientPassword } from '../src/utils/patientPasswordAccess'
import { mockSetPatientPassword } from '../src/mock/patients'

const appRoot = join(dirname(fileURLToPath(import.meta.url)), '..') // apps/admin-web
const repoRoot = join(appRoot, '..', '..')
const read = (rel: string) => readFileSync(join(repoRoot, rel), 'utf8')

const PASSWORD_ROUTE = '"/api/v1/admin/patients/:patientId/password"'
const rbacSrc = read('services/gateway/cmd/server/rbac.go')

/** 取某张矩阵声明块内的原文（从 `var X = []rbacPattern{` 到与之配对的 `}`） */
function matrixBlock(varName: string): string {
  const start = rbacSrc.indexOf(`var ${varName} = []rbacPattern{`)
  if (start < 0) throw new Error(`rbac.go 里找不到 ${varName}（改名了？同步这条对拍）`)
  const end = rbacSrc.indexOf('\n}', start)
  if (end < 0) throw new Error(`${varName} 的声明块没有闭合`)
  return rbacSrc.slice(start, end)
}

const ALL_MATRICES = [
  'adminOnlyPatterns',
  'techAdminOnlyPatterns',
  'provisionKeyPatterns',
  'doctorAdminOnlyPatterns',
  'staffOnlyPatterns',
  'abnormalReportPatterns',
  'publicPatterns',
]

describe('T500 设密入口的角色闸门真值表', () => {
  it('只有 admin 放行', () => {
    expect(canSetPatientPassword('admin')).toBe(true)
  })

  it('医护／客服／未知角色／无角色一律拦（判据写反在这里先红）', () => {
    for (const role of ['doctor', 'cs', 'technician', 'ROLE_ADMIN', '', null, undefined]) {
      expect(canSetPatientPassword(role as never), `这个角色本该拦住：${String(role)}`).toBe(false)
    }
  })
})

describe('T500 闸门与网关真源对拍', () => {
  it('端点确实登记在 adminOnlyPatterns 里', () => {
    expect(matrixBlock('adminOnlyPatterns')).toContain(PASSWORD_ROUTE)
  })

  it('且没有同时出现在其它任何矩阵（判据只能是「仅 admin」，多一处登记就是多放一类角色）', () => {
    for (const name of ALL_MATRICES.filter((n) => n !== 'adminOnlyPatterns')) {
      // 反证前置：先证这块矩阵被读出了内容（含 rbacOf 条目），否则 not.toContain 是「读到空串」的假绿
      expect(matrixBlock(name), `${name} 被切成空块，下面的排除式断言没有判别力`).toMatch(/rbacOf\(/)
      expect(matrixBlock(name), `端点被重复登记进 ${name}`).not.toContain(PASSWORD_ROUTE)
    }
  })
})

describe('T500 mock 层与 T477 handler 同形', () => {
  it('已知患者：返回 {patientId, password}，口令形态照服务端发号器', () => {
    const res = mockSetPatientPassword('PT-001')
    expect(res.patientId).toBe('PT-001')
    // Br + 8 位随机 + #7，镜像 handler.go genDoctorPassword（患者侧复用的就是它）
    expect(res.password).toMatch(/^Br[0-9a-z]{8}#7$/)
  })

  it('一次性凭据：两次调用不得给出同一个口令', () => {
    const first = mockSetPatientPassword('PT-002').password
    const second = mockSetPatientPassword('PT-002').password
    expect(first).not.toBe(second)
  })

  it('未知患者抛错而不是静默成功（后端判定序：存在性在写之前，404 且不写库）', () => {
    expect(() => mockSetPatientPassword('PT-999')).toThrow(/不存在/)
  })
})

describe('T500 页面接线（源码级）', () => {
  const page = read('apps/admin-web/src/pages/patients/index.vue')

  it('抽屉里有「设登录口令」入口，且被闸门包住', () => {
    const btn = page.match(/<el-button[^>]*v-if="canSetPatientPassword\(auth\.role\)"[^>]*>设登录口令<\/el-button>/)
    expect(btn, '入口没渲染出来或没被 canSetPatientPassword 包住').toBeTruthy()
  })

  it('口令只从一次性弹窗出去，页面里没有第二个消费点', () => {
    expect(page.match(/setPatientPasswordApi\(/g)).toHaveLength(1)
    expect(page).toContain('初始密码：${password}')
    expect(page).toContain('仅此一次展示，关闭后不可再看')
  })

  it('取消确认框那一腿不发请求（confirm 在 await setPatientPasswordApi 之前）', () => {
    const confirmAt = page.indexOf("ElMessageBox.confirm(\n      `将为患者")
    const callAt = page.indexOf('await setPatientPasswordApi(')
    expect(confirmAt).toBeGreaterThan(0)
    expect(callAt).toBeGreaterThan(confirmAt)
    // 中间必须先是「取消即 return」，否则取消也会发枪
    expect(page.slice(confirmAt, callAt)).toContain('return // 取消或关掉弹层都不发请求')
  })

  it('读侧没有把口令写进列表或详情字段（Patient 类型不含 password）', () => {
    expect(page).not.toMatch(/row\.password|detail\.password|password_hash/)
  })
})
