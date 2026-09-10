// 复查报告上传白名单校验单测（T130 增补单）
// 覆盖：白名单扩展名放行、非白名单扩展名拒绝、MIME 放宽（octet-stream/空串）、MIME 不匹配拒绝。
import { describe, it, expect } from 'vitest'
import {
  validateReviewReportFile,
  checkReviewReportFileSize,
  REVIEW_REPORT_ALLOWED_EXT,
  REVIEW_REPORT_ALLOWED_MIME,
  REVIEW_REPORT_MAX_BYTES,
} from '../src/utils/review-report-whitelist'

function mockFile(name: string, type: string) {
  return { name, type }
}

describe('validateReviewReportFile 白名单校验', () => {
  it('白名单扩展名 + 对应 MIME → 放行', () => {
    const cases: [string, string][] = [
      ['report.pdf', 'application/pdf'],
      ['photo.jpg', 'image/jpeg'],
      ['photo.jpeg', 'image/jpeg'],
      ['image.png', 'image/png'],
      ['doc.doc', 'application/msword'],
      ['doc.docx', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'],
      ['sheet.xlsx', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'],
      ['slide.pptx', 'application/vnd.openxmlformats-officedocument.presentationml.presentation'],
      ['archive.zip', 'application/zip'],
    ]
    for (const [name, type] of cases) {
      const r = validateReviewReportFile(mockFile(name, type))
      expect(r.ok, `${name} / ${type} should pass`).toBe(true)
    }
  })

  it('Office 文件 MIME 误报 octet-stream / 空串 → 放行（后端魔数兜底）', () => {
    const cases: [string, string][] = [
      ['doc.docx', 'application/octet-stream'],
      ['sheet.xlsx', 'application/octet-stream'],
      ['slide.pptx', 'application/octet-stream'],
      ['archive.zip', 'application/octet-stream'],
      ['doc.doc', ''],
      ['report.pdf', 'application/octet-stream'],
    ]
    for (const [name, type] of cases) {
      const r = validateReviewReportFile(mockFile(name, type))
      expect(r.ok, `${name} / ${type} should pass`).toBe(true)
    }
  })

  it('非白名单扩展名 → 拒绝（扩展名维度）', () => {
    const rejected = ['mal.xls', 'bad.ppt', 'doc.wps', 'sheet.et', 'slide.dps', 'run.exe', 'script.bat', 'evil.js', 'tpl.dot']
    for (const name of rejected) {
      const r = validateReviewReportFile(mockFile(name, 'application/octet-stream'))
      expect(r.ok, `${name} should be rejected`).toBe(false)
      expect(r.reason).toBe('ext')
    }
  })

  it('白名单扩展名但 MIME 非白名单且非 octet-stream/空串 → 拒绝', () => {
    const r = validateReviewReportFile(mockFile('report.pdf', 'text/html'))
    expect(r.ok).toBe(false)
    expect(r.reason).toBe('mime')
  })

  it('白名单常量与后端同源：扩展名覆盖 Boss 定稿 9 类', () => {
    // Boss 2026-09-10 12:42 定稿：pdf/jpg/png/doc/docx/xlsx/pptx/zip（jpeg 为 jpg 别名）
    expect(REVIEW_REPORT_ALLOWED_EXT).toEqual(
      expect.arrayContaining(['.pdf', '.jpg', '.jpeg', '.png', '.doc', '.docx', '.xlsx', '.pptx', '.zip']),
    )
    expect(REVIEW_REPORT_ALLOWED_EXT).toHaveLength(9)
  })

  it('白名单常量不含被拦截类型', () => {
    const blocked = ['.xls', '.ppt', '.wps', '.et', '.dps', '.exe', '.bat', '.js', '.dot']
    for (const ext of blocked) {
      expect(REVIEW_REPORT_ALLOWED_EXT).not.toContain(ext)
    }
  })
})

// T135（R4-b 定稿值 20MB，前端预校验；后端 upload-complete 权威强制）
describe('checkReviewReportFileSize 大小校验', () => {
  it('恰 20MB → 放行', () => {
    const r = checkReviewReportFileSize({ size: REVIEW_REPORT_MAX_BYTES })
    expect(r.ok).toBe(true)
  })

  it('超过 20MB → 拒绝，提示「文件大小超过 20MB，请压缩后重试」', () => {
    const r = checkReviewReportFileSize({ size: REVIEW_REPORT_MAX_BYTES + 1 })
    expect(r.ok).toBe(false)
    expect(r.reason).toBe('size')
    expect(r.message).toContain('文件大小超过 20MB')
  })

  it('20MB 常量与后端同源（20<<20）', () => {
    expect(REVIEW_REPORT_MAX_BYTES).toBe(20 * 1024 * 1024)
  })
})
