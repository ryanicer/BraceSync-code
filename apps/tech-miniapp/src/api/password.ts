import { request, USE_MOCK } from '../utils/request'
import { logger } from '../utils/logger'
import { markUserCopy } from '@bracesync/shared-utils'
import { CODE_OLD_PASSWORD_WRONG, TECH_CHANGE_PASSWORD_URL } from '../utils/authError'

/**
 * T486 技师自助改密通道（POST /api/v1/tech/change-password，身份取自 JWT）
 *
 * 请求体只有 oldPassword / newPassword 两个键：没有 techId，前端无从指定「改谁的口令」，
 * 后端按 X-User-Id（网关按 JWT claims.sub 注入）定位那一行。
 *
 * 10001 那个码与「这条 URL 不触发 T326 踢出」的豁免判据同源，都取自 utils/authError，
 * 两处各写一份字面量迟早分叉 —— 分叉的结果是填错口令被踢回登录页，或失效会话不被清。
 */

/** 这一格的用户面文案：改密页专用，见下方 markUserCopy 的说明 */
export const OLD_PASSWORD_WRONG_COPY = '原密码不正确，请重新输入'

export async function changeTechPassword(oldPassword: string, newPassword: string): Promise<void> {
  if (USE_MOCK) {
    // mock 模式没有技师账号可校验，登录页那条 mock 分支同样直接放行；口令只记长度
    logger.info('[T486]', 'changeTechPassword (mock)', {
      oldLen: oldPassword.length,
      newLen: newPassword.length,
    })
    await new Promise((r) => setTimeout(r, 200))
    return
  }

  try {
    await request<{ techId: string }>({
      url: TECH_CHANGE_PASSWORD_URL,
      method: 'POST',
      data: { oldPassword, newPassword },
    })
  } catch (err) {
    // 10001 在共享码表里记作「登录状态已失效，请重新登录」（那是患者端 wx 登录的语义）。
    // 这条端点上它只有一个成因：旧口令填错。不覆盖就会把「填错一次」渲染成「去重登」，
    // 而 markUserCopy 的句子优先级最高，展示层仍走 userErrorCopy 唯一出口。
    if ((err as { code?: number } | null)?.code === CODE_OLD_PASSWORD_WRONG) {
      throw markUserCopy(err instanceof Error ? err : new Error('invalid old password'), OLD_PASSWORD_WRONG_COPY)
    }
    throw err
  }
}
