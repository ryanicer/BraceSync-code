// T401 防回潮：导出 CSV 的下载文件名必须取 Content-Disposition 里的完整名。
//
// 修前现场（Alice 第 34 轮业务验收 §七 D-1）：api/index.ts 的 dispositionFilename 用
// /filename="?([^";]+?)"?/ —— 惰性量词配可选引号，最短匹配只取到首字符「a」，
// a.download 被设成 "a"，浏览器按 MIME 补成 a.csv，用户拿到无意义文件名。
//
// 判据头的形状不靠猜：alert-service 的 reportFilename 恒发带引号形态，且把 patientId
// 收敛到 [A-Za-z0-9_-]，这两条由 services/alert-service/internal/handler/t300_report_test.go
// 逐字钉住（本文件用例 1 与用例 3 用的就是那两个契约串）。
import { describe, it, expect, vi, afterEach } from 'vitest'

const FALLBACK = 'abnormal-report.csv'

/** USE_MOCK 是模块级常量，与 test/export-401.spec.ts 同法：stub 环境变量后重新求值模块 */
async function loadDispositionFilename() {
  vi.stubEnv('VITE_USE_MOCK', 'false')
  vi.resetModules()
  const mod = await import('../src/api')
  return mod.dispositionFilename
}

afterEach(() => {
  vi.unstubAllEnvs()
})

describe('T401 导出文件名解析', () => {
  it('用例 1：alert-service 契约头取到完整文件名（含患者与日期范围）', async () => {
    const dispositionFilename = await loadDispositionFilename()
    expect(
      dispositionFilename('attachment; filename="abnormal-report-P001-2026-09-01_2026-09-03.csv"'),
    ).toBe('abnormal-report-P001-2026-09-01_2026-09-03.csv')
  })

  it('用例 2：现网实测形（P+年+12hex 患者号 + 起止日期）不被截断', async () => {
    const dispositionFilename = await loadDispositionFilename()
    expect(
      dispositionFilename(
        'attachment; filename="abnormal-report-P20260123456789ab-2026-09-20_2026-09-26.csv"',
      ),
    ).toBe('abnormal-report-P20260123456789ab-2026-09-20_2026-09-26.csv')
  })

  it('用例 3：patientId 被后端收敛成下划线后仍是完整名（引号逃逸样本）', async () => {
    const dispositionFilename = await loadDispositionFilename()
    expect(
      dispositionFilename('attachment; filename="abnormal-report-P001___X-Evil_1-2026-09-01_2026-09-03.csv"'),
    ).toBe('abnormal-report-P001___X-Evil_1-2026-09-01_2026-09-03.csv')
  })

  it('用例 4：头缺失或没有 filename 参数时回落到固定名，而不是空串', async () => {
    const dispositionFilename = await loadDispositionFilename()
    expect(dispositionFilename(null)).toBe(FALLBACK)
    expect(dispositionFilename('')).toBe(FALLBACK)
    expect(dispositionFilename('attachment')).toBe(FALLBACK)
  })

  it('用例 5：取到的名字里没有分号残留（旧写法若贪婪会连附件参数一起吞进来）', async () => {
    const dispositionFilename = await loadDispositionFilename()
    const name = dispositionFilename(
      'attachment; filename="abnormal-report-P001-2026-09-01_2026-09-03.csv"; x-vendor=tapd',
    )
    expect(name).toBe('abnormal-report-P001-2026-09-01_2026-09-03.csv')
    expect(name).not.toContain(';')
  })
})
