/**
 * 反馈（feedbacks）提交 API —— 统一咨询入口两处落库共用一条契约：
 *
 *   POST /api/v1/feedbacks  { patientId, type, content, status }
 *
 *   - T690（§7A.13 通道二）：客服留言，类型三选 = 佩戴咨询 / 设备问题 / 功能建议；
 *   - T192（§7A.9 配网失败自动上报）：type = wifi_setup_failure（词表 §8.2 V3.48 注，
 *     普通留言、不单列概念），内容 = 失败类型 + 设备名 + 发生时间。
 *
 * 后端 createFeedback 已落地（handler.go，content ≤500 / type ≤32 / status 白名单），
 * 患者只能为本人提交（assertAdminOrSelf）。
 */

import { request } from '../utils/request'

export const WIFI_SETUP_FEEDBACK_TYPE = 'wifi_setup_failure'

/** §7A.13 通道二类型三选（沿用 feedbacks 现有词表；wifi_setup_failure 不进用户可选项） */
export const FEEDBACK_TYPES = ['佩戴咨询', '设备问题', '功能建议'] as const
export type FeedbackType = (typeof FEEDBACK_TYPES)[number]

export interface FeedbackPayload {
  patientId: string
  type: string
  content: string
}

async function createFeedback(payload: FeedbackPayload): Promise<{ feedbackId: string }> {
  return request<{ feedbackId: string }>({
    url: '/api/v1/feedbacks',
    method: 'POST',
    data: { ...payload, status: 'pending' },
  })
}

/** T690 客服留言（内容由页面限 300 字，后端上限 500）；成功返回反馈 id */
export async function submitFeedback(payload: FeedbackPayload): Promise<{ feedbackId: string }> {
  return createFeedback(payload)
}

export interface WifiFailureFeedbackPayload {
  patientId: string
  /** 设计稿：失败类型 + 设备名 + 发生时间 */
  content: string
}

/** 提交配网失败反馈；成功返回反馈 id */
export async function submitWifiFailureFeedback(
  payload: WifiFailureFeedbackPayload
): Promise<{ feedbackId: string }> {
  return createFeedback({ ...payload, type: WIFI_SETUP_FEEDBACK_TYPE })
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
