// 复查报告上传白名单（T130 增补单，Boss 2026-09-10 12:42 裁定）
// 与后端 services/file-service/internal/service/presigner.go
//   reviewReportAllowedExtensions / reviewReportExpectedMIMEs 保持同源。
// 后端魔数指纹校验为权威；此处仅做前端体验层快反馈（扩展名 + MIME 放宽）。

/** 允许的扩展名（含点，小写） */
export const REVIEW_REPORT_ALLOWED_EXT = [
  '.pdf', '.jpg', '.jpeg', '.png',
  '.doc', '.docx', '.xlsx', '.pptx', '.zip',
] as const

/** 允许的 MIME 类型（含 application/octet-stream 与空串，Office 文件浏览器常误报） */
export const REVIEW_REPORT_ALLOWED_MIME = [
  'application/pdf',
  'image/jpeg',
  'image/png',
  'application/msword',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  'application/zip',
  'application/x-zip-compressed',
  'application/octet-stream',
  '',
] as const

export interface ValidateResult {
  ok: boolean
  reason?: 'ext' | 'mime'
  message?: string
}

/** 前端校验：扩展名白名单 + MIME 放宽（后端魔数为权威兜底） */
export function validateReviewReportFile(file: { name: string; type: string }): ValidateResult {
  const ext = file.name.substring(file.name.lastIndexOf('.')).toLowerCase()
  if (!(REVIEW_REPORT_ALLOWED_EXT as readonly string[]).includes(ext)) {
    return {
      ok: false,
      reason: 'ext',
      message: `不支持的文件类型：${ext}，仅支持 ${REVIEW_REPORT_ALLOWED_EXT.join(' / ')}`,
    }
  }
  if (!(REVIEW_REPORT_ALLOWED_MIME as readonly string[]).includes(file.type)) {
    return {
      ok: false,
      reason: 'mime',
      message: `不支持的 MIME 类型：${file.type}`,
    }
  }
  return { ok: true }
}
