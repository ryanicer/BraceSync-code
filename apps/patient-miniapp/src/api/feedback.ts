/**
 * 配网失败反馈（T192 / 设计稿 07-contact + PRD §7A.9「联系技师的真实路由」）
 *
 * POST /api/v1/feedbacks  { patientId, type, content, status }
 *
 * ⚠️ 后端缺口（T192 上报 §D1，待 Winner 补）：user-service 目前只注册了
 *   GET  /api/v1/feedbacks（admin 列表）与 POST /api/v1/feedbacks/:id/process（回复），
 *   没有创建端点（services/user-service/internal/handler/handler.go:191-192）。
 *   本函数按真实契约实现，端点落地前调用会失败，由页面如实提示、不伪造成功。
 */

import { request } from '../utils/request'

export const WIFI_SETUP_FEEDBACK_TYPE = 'wifi_setup_failure'

export interface WifiFailureFeedbackPayload {
  patientId: string
  /** 设计稿：失败类型 + 设备名 + 发生时间 */
  content: string
}

/** 提交配网失败反馈；成功返回反馈 id */
export async function submitWifiFailureFeedback(
  payload: WifiFailureFeedbackPayload
): Promise<{ feedbackId: string }> {
  return request<{ feedbackId: string }>({
    url: '/api/v1/feedbacks',
    method: 'POST',
    data: { ...payload, type: WIFI_SETUP_FEEDBACK_TYPE, status: 'pending' },
  })
}

/** 拼设计稿要求的 content：问题类型 + 设备名(BSYNC-XXX) + 时间 */
export function buildFailureContent(issueType: string, deviceName: string, at: Date): string {
  return `${issueType}；设备 ${deviceName || '未知设备'}；发生时间 ${formatFeedbackTime(at)}`
}

/** 07-contact 页展示与反馈内容统一用同一时间格式（设计稿：2026-09-06 14:32） */
export function formatFeedbackTime(at: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${at.getFullYear()}-${p(at.getMonth() + 1)}-${p(at.getDate())} ${p(at.getHours())}:${p(at.getMinutes())}`
}
