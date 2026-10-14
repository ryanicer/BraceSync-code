// 患者沟通域 mock 数据（对齐 api-contracts.ts getFeedbacks/processFeedback，PRD §7D.7 客服子角色）
import type { Feedback } from '@bracesync/shared-types'

// T691 r134：mock 以 admin 视角装配，故带 phoneMasked（真实后端仅对 ROLE_ADMIN 回传该字段，
// 非 admin 恒 undefined）。明文另经 reveal 端点按需解密（见 mockRevealPatientPhone）。
const FEEDBACKS: Feedback[] = [
  {
    feedbackId: 'FB-001', patientId: 'PT-001', type: '佩戴不适',
    content: '左肩位置佩戴 2 小时后有压痛感，希望医生帮忙看看支具是否需要调整。',
    submitTime: '2026-08-11T09:20:00+08:00', handler: null, replyContent: null, replyTime: null, status: 'pending',
    phoneMasked: '138****6688',
  },
  {
    feedbackId: 'FB-002', patientId: 'PT-002', type: '设备问题',
    content: '设备昨晚没有自动连上 WiFi，重新配网后恢复，请确认数据有没有丢失。',
    submitTime: '2026-08-10T21:05:00+08:00', handler: '客服小美', replyContent: '已核查，昨夜数据完整补传成功，无丢失。', replyTime: '2026-08-11T08:40:00+08:00', status: 'replied',
    phoneMasked: '139****2233',
  },
  {
    feedbackId: 'FB-003', patientId: 'PT-003', type: '使用咨询',
    content: '体育课能不能暂时摘掉支具？摘掉后需要补戴多久？',
    submitTime: '2026-08-10T16:30:00+08:00', handler: '陈小芳', replyContent: '体育课可摘除，当日补戴至目标时长即可，建议课间也佩戴。', replyTime: '2026-08-10T18:10:00+08:00', status: 'resolved',
    phoneMasked: '137****5566',
  },
  {
    feedbackId: 'FB-004', patientId: 'PT-004', type: '佩戴不适',
    content: '腰部魔术贴磨损严重，粘不牢，申请更换配件。',
    submitTime: '2026-08-11T11:45:00+08:00', handler: null, replyContent: null, replyTime: null, status: 'pending',
    phoneMasked: '136****7788',
  },
]

// T691 r134：mock 明文表（reveal 端点按需解密的等价物）——真实明文不进列表接口，只在 reveal 时返回。
const PHONE_PLAIN: Record<string, string> = {
  'PT-001': '138 6688 6688', 'PT-002': '139 1234 2233',
  'PT-003': '137 5555 5566', 'PT-004': '136 7777 7788',
}

export function mockFeedbacks(params: { keyword?: string }): Feedback[] {
  let list = FEEDBACKS.map((f) => ({ ...f }))
  if (params.keyword) {
    const kw = params.keyword.toLowerCase()
    list = list.filter((f) => f.content.toLowerCase().includes(kw) || f.patientId.toLowerCase().includes(kw))
  }
  return list
}

export function mockFeedbackPatientName(patientId: string): string {
  const names: Record<string, string> = {
    'PT-001': '林小雨', 'PT-002': '陈子航', 'PT-003': '王梓萌',
    'PT-004': '刘俊熙', 'PT-005': '赵欣然', 'PT-006': '孙浩然',
  }
  return names[patientId] ?? patientId
}

export function mockProcessFeedback(
  feedbackId: string,
  payload: { replyContent?: string; markResolved?: boolean },
): void {
  const fb = FEEDBACKS.find((f) => f.feedbackId === feedbackId)
  if (!fb) return
  const defaultHandler = '运营客服'
  if (payload.markResolved) {
    // T374：仅标记已处理 —— 只推进状态与处理人，已存处理备注原样保留
    fb.status = 'resolved'
    fb.handler = fb.handler ?? defaultHandler
    return
  }
  const note = payload.replyContent?.trim()
  if (!note) return
  fb.replyContent = note
  fb.replyTime = new Date().toISOString()
  fb.status = fb.status === 'resolved' ? 'resolved' : 'replied'
  fb.handler = fb.handler ?? defaultHandler
}

// T691 r134：按需解密查看手机号明文（reveal 端点等价物）。真实后端每次调用落 sensitive_read 审计。
export function mockRevealPatientPhone(patientId: string): { patientId: string; phone: string } {
  return { patientId, phone: PHONE_PLAIN[patientId] ?? '' }
}
