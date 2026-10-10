/**
 * 患者端「康复建议」读口（T641，前端半侧 ← user-service handler.go 的 listAdvice / getCareTeam）
 *
 * 两枚都是只读：本功能是留言板（Boss 13:12 拍板 1），患者端零写操作——
 * 无回复 / 无点赞 / 无已读回执 / 无输入控件，写三腿（POST/PUT/DELETE）在网关是 doctorAdminOnly。
 *
 * 隐私（设计稿 §八）：两枚出参的字段名集合里不得出现 name / doctorName / teamName /
 * username / phone*，断言钉在服务端用例（advice_t641_test.go TestT641_Privacy_*）。
 */

import { request } from '../utils/request'
import type { Advice, CareTeamMember } from '@bracesync/shared-types'

/**
 * GET /api/v1/patients/:patientId/advice —— 某患者的建议时间轴，倒序（后端 pg.go:1079
 * ORDER BY created_at DESC, advice_id DESC，前端不再重排）。
 * editable 由服务端按调用人算，患者令牌恒 false。
 */
export async function listPatientAdvice(patientId: string): Promise<Advice[]> {
  return request<Advice[]>({ url: `/api/v1/patients/${patientId}/advice`, method: 'GET' })
}

/**
 * GET /api/v1/patient/care-team —— 「你的医护团队」两列（角色 + 职称，无姓名）。
 * self-scope：路径不带患者 ID，查询对象由网关从 JWT sub 注入的 X-User-Id 决定
 * （口径同 api/profile.ts 的 getPatientProfile），因此结构上请求不到别人的团队。
 */
export async function getCareTeam(): Promise<CareTeamMember[]> {
  return request<CareTeamMember[]>({ url: '/api/v1/patient/care-team', method: 'GET' })
}
