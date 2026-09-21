// 复查报告域 mock 数据（T130 复查记录 + T135 复查模板；对齐 shared-types ReviewRecord / ReviewTemplate）
//
// T307 建卡缘由：这两页的 mock 分支此前恒返回空数组，且 uploadFileDirect 在 mock 下会对
// mock-cos 域名发真 PUT。本文件把「列表两态 + 上传链路 + 写入后回填」做成可控数据源。
import type { CreateReviewRecordRequest, CreateReviewTemplateRequest, ReviewRecord, ReviewTemplate } from '@bracesync/shared-types'

/**
 * 列表两态开关。页面本身没有「清空数据」入口，且红线要求零产品页面改动，
 * 故由 URL query 声明：?mock_review=empty 为空态，缺省（或 =data）为有数据态。
 * 只影响 mock 分支的返回值，真实模式（VITE_USE_MOCK=false）不读这里。
 */
export type MockReviewState = 'data' | 'empty'

export function mockReviewState(): MockReviewState {
  const search = typeof window === 'undefined' ? '' : window.location.search
  return new URLSearchParams(search).get('mock_review') === 'empty' ? 'empty' : 'data'
}

// ========== 上传文件登记表（presign 建档 ⇒ 直传置为已传 ⇒ 写记录/模板时按 fileId 回填元数据） ==========

export interface MockUploadedFile {
  fileId: string
  fileName: string
  contentType: string
  size: number
  /** presign 建档后置 pending，直传完成后置 uploaded；未 uploaded 的文件不允许挂到记录上 */
  status: 'pending' | 'uploaded'
  uploadedAt: string
}

const FILES = new Map<string, MockUploadedFile>()

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

let fileSeq = 0

/** presign 侧自增编号，避免同一毫秒内两次申请撞 id */
export function mockNextFileId(): string {
  fileSeq += 1
  return `FILE-M${String(fileSeq).padStart(4, '0')}`
}

export function mockRegisterPendingFile(params: { fileId: string; fileName: string; contentType: string }): void {
  FILES.set(params.fileId, {
    fileId: params.fileId,
    fileName: params.fileName,
    contentType: params.contentType,
    size: 0,
    status: 'pending',
    uploadedAt: new Date().toISOString(),
  })
}

/** mock 直传：解析 presign 给出的 mock URL（最后一段即 fileId），分段延时模拟传输耗时 */
export async function mockDirectUpload(uploadUrl: string, file: File): Promise<void> {
  const fileId = uploadUrl.split('/').pop() ?? ''
  for (let i = 0; i < 3; i++) await sleep(80)
  const f = FILES.get(fileId)
  if (f) {
    f.status = 'uploaded'
    f.size = file.size
    f.uploadedAt = new Date().toISOString()
    return
  }
  FILES.set(fileId, {
    fileId,
    fileName: file.name,
    contentType: file.type,
    size: file.size,
    status: 'uploaded',
    uploadedAt: new Date().toISOString(),
  })
}

export function mockUploadedFile(fileId: string): MockUploadedFile | null {
  return FILES.get(fileId) ?? null
}

export function mockFileDownloadUrl(fileId: string): string {
  return `https://mock-cos.example.com/download/${fileId}`
}

// ========== 复查记录 ==========

// 夹具约束：每条记录的 reviewDate 与该患者的其它日期字段互不重复，
// E2E 用「按日期筛行」定位，日期撞车会一行命中两条（T307 首跑即被这条判据拦下）。

const RECORDS: ReviewRecord[] = [
  {
    reviewId: 'RV-0001', patientId: 'PT-001', reviewDate: '2026-06-12', reviewType: 'initial',
    findings: 'Cobb 28°，胸腰段为主弯，予以每日 22h 佩戴方案。', nextReviewDate: '2026-09-10',
    doctorId: 'DOC-001', reportFileId: 'FILE-R0001', reportFileName: '林小雨-初诊报告.pdf',
    reportContentType: 'application/pdf', reportSize: 245760,
    reportUploadedAt: '2026-06-12T10:05:00+08:00', reportDownloadUrl: mockFileDownloadUrl('FILE-R0001'),
    createdAt: '2026-06-12T10:05:00+08:00', updatedAt: '2026-06-12T10:05:00+08:00',
  },
  {
    reviewId: 'RV-0002', patientId: 'PT-001', reviewDate: '2026-09-15', reviewType: 'follow-up',
    findings: '胸腰段压力点较前缓解，皮肤耐受良好。', nextReviewDate: '2026-12-15',
    doctorId: 'DOC-001', reportFileId: null, reportFileName: null,
    reportContentType: null, reportSize: null, reportUploadedAt: null, reportDownloadUrl: null,
    createdAt: '2026-09-15T15:20:00+08:00', updatedAt: '2026-09-15T15:20:00+08:00',
  },
  {
    reviewId: 'RV-0003', patientId: 'PT-002', reviewDate: '2026-08-28', reviewType: 'follow-up',
    findings: 'Cobb 35°，腰段加压垫增厚 2mm 后复评。', nextReviewDate: null,
    doctorId: 'DOC-001', reportFileId: 'FILE-R0002', reportFileName: '陈子航-复诊记录.docx',
    reportContentType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', reportSize: 88320,
    reportUploadedAt: '2026-08-28T09:12:00+08:00', reportDownloadUrl: mockFileDownloadUrl('FILE-R0002'),
    createdAt: '2026-08-28T09:12:00+08:00', updatedAt: '2026-08-28T09:12:00+08:00',
  },
]

/** PT-003 及以后刻意无记录：不靠 URL 参数也能验「某患者列表为空」。 */

export function mockReviewRecords(patientId: string): ReviewRecord[] {
  if (mockReviewState() === 'empty') return []
  return RECORDS.filter((r) => r.patientId === patientId).map((r) => ({ ...r }))
}

let recordSeq = RECORDS.length

export function mockCreateReviewRecord(input: CreateReviewRecordRequest): ReviewRecord {
  recordSeq += 1
  const now = new Date().toISOString()
  const file = input.reportFileId ? FILES.get(input.reportFileId) : undefined
  const hasFile = !!file && file.status === 'uploaded'
  const row: ReviewRecord = {
    reviewId: `RV-9${String(recordSeq).padStart(3, '0')}`,
    patientId: input.patientId,
    reviewDate: input.reviewDate,
    reviewType: input.reviewType,
    findings: input.findings ?? null,
    nextReviewDate: input.nextReviewDate ?? null,
    doctorId: input.doctorId ?? null,
    reportFileId: input.reportFileId ?? null,
    reportFileName: hasFile ? file!.fileName : null,
    reportContentType: hasFile ? file!.contentType : null,
    reportSize: hasFile ? file!.size : null,
    reportUploadedAt: hasFile ? file!.uploadedAt : null,
    reportDownloadUrl: hasFile ? mockFileDownloadUrl(file!.fileId) : null,
    createdAt: now,
    updatedAt: now,
  }
  RECORDS.push(row)
  return { ...row }
}

// ========== 复查报告模板 ==========

const TEMPLATES: ReviewTemplate[] = [
  {
    templateId: 'TPL-0001', groupId: 'GRP-0001', name: '脊柱侧弯复查报告模板（成人）', version: 3,
    fileId: 'FILE-T0001', status: 'active', uploadedBy: 'ADMIN', uploadedAt: '2026-09-01',
    updatedAt: '2026-09-01T11:00:00+08:00',
    fileName: '脊柱侧弯复查报告模板-v3.docx',
    contentType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    fileSize: 51200, downloadUrl: mockFileDownloadUrl('FILE-T0001'),
  },
  {
    templateId: 'TPL-0002', groupId: 'GRP-0002', name: '儿童矫形复查记录表', version: 1,
    fileId: 'FILE-T0002', status: 'active', uploadedBy: 'ADMIN', uploadedAt: '2026-08-19',
    updatedAt: '2026-08-19T16:30:00+08:00',
    fileName: null, contentType: null, fileSize: null, downloadUrl: null,
  },
]

/** 列表口径对齐契约：只回 active 版本（旧版 retired 后不出现在列表里） */
export function mockReviewTemplates(): ReviewTemplate[] {
  if (mockReviewState() === 'empty') return []
  return TEMPLATES.filter((t) => t.status === 'active').map((t) => ({ ...t }))
}

let templateSeq = TEMPLATES.length

export function mockCreateReviewTemplate(input: CreateReviewTemplateRequest): ReviewTemplate {
  templateSeq += 1
  const file = FILES.get(input.fileId)
  const row: ReviewTemplate = {
    templateId: `TPL-9${String(templateSeq).padStart(3, '0')}`,
    groupId: `GRP-9${String(templateSeq).padStart(3, '0')}`,
    name: input.name,
    version: 1,
    fileId: input.fileId,
    status: 'active',
    uploadedBy: 'ADMIN',
    uploadedAt: new Date().toISOString().slice(0, 10),
    updatedAt: new Date().toISOString(),
    fileName: file?.fileName ?? null,
    contentType: file?.contentType ?? null,
    fileSize: file?.size ?? null,
    downloadUrl: file ? mockFileDownloadUrl(file.fileId) : null,
  }
  TEMPLATES.push(row)
  return { ...row }
}

/** 版本替换：同组 active 旧版置 retired，新版 version 递增并进列表 */
export function mockReplaceReviewTemplate(groupId: string, fileId: string): ReviewTemplate {
  templateSeq += 1
  const current = TEMPLATES.filter((t) => t.groupId === groupId && t.status === 'active')
  const nextVersion = current.length ? Math.max(...current.map((t) => t.version)) + 1 : 1
  for (const t of current) {
    t.status = 'retired'
    t.updatedAt = new Date().toISOString()
  }
  const file = FILES.get(fileId)
  const row: ReviewTemplate = {
    templateId: `TPL-9${String(templateSeq).padStart(3, '0')}`,
    groupId,
    name: current[0]?.name ?? '未命名模板',
    version: nextVersion,
    fileId,
    status: 'active',
    uploadedBy: 'ADMIN',
    uploadedAt: new Date().toISOString().slice(0, 10),
    updatedAt: new Date().toISOString(),
    fileName: file?.fileName ?? null,
    contentType: file?.contentType ?? null,
    fileSize: file?.size ?? null,
    downloadUrl: file ? mockFileDownloadUrl(file.fileId) : null,
  }
  TEMPLATES.push(row)
  return { ...row }
}
