/**
 * T677：技师端登录失败的分级判据（纯层，vitest 可直接测）。
 *
 * 背景：改前 login/index.vue 的 catch 无条件弹「手机号或密码错误」，
 * 于是「网络失败 / 域名不可达」（wx.request fail，无信封码也无 HTTP 状态）
 * 也被报成凭证错误，误导排查方向。此处把分级判据独立出来，供页面复用且可单测。
 *
 * 分级：
 *  - network：传输层失败（无信封码、无 HTTP 状态），弹 modal 报「网络连接失败」，
 *    正文走共享文案层（带 NET 码位），与凭证错误彻底分开；
 *  - credential：服务端 401／10401（凭据错误 / 账号不存在，统一防枚举），文案逐字保持既有句；
 *  - server：其余服务端错误码，走共享错误文案层 userErrorCopy。
 *
 * 🔴 原始 errMsg（request:fail 原文）不给展示汇点：T465 门禁规定展示层不得出现
 *    logErrorText 技术原文（no-bare-message-display.test.ts R3）。它留在日志面
 *    （login/index.vue catch 里的 logger.error + logErrorText），供真机取证读取。
 */
import { userErrorCopy } from '@bracesync/shared-utils'

export type LoginErrorKind = 'network' | 'credential' | 'server'

export interface LoginErrorPrompt {
  kind: LoginErrorKind
  /** 展示标题：toast 的 title / modal 的 title */
  title: string
  /** modal 正文（仅 network 有长文本，其余为空串） */
  content: string
  /** true 用 uni.showModal（长文本可读可复制），false 用 uni.showToast */
  modal: boolean
}

export function classifyLoginError(error: unknown): LoginErrorPrompt {
  const code = (error as { code?: unknown } | null | undefined)?.code
  const httpStatus = (error as { httpStatus?: unknown } | null | undefined)?.httpStatus

  // 传输层失败：既无信封码也无 HTTP 状态（断网 / 域名不可达 / request fail）
  if (code == null && httpStatus == null) {
    return {
      kind: 'network',
      title: '网络连接失败',
      content: userErrorCopy(error, {
        scope: 'tech',
        fallback: '登录请求未能送达服务器，请检查网络后重试',
      }),
      modal: true,
    }
  }

  // 服务端凭证错误（401 / 10401）：防枚举统一文案
  if (code === 10401 || httpStatus === 401) {
    return {
      kind: 'credential',
      title: '手机号或密码错误',
      content: '',
      modal: false,
    }
  }

  // 其余服务端错误码
  return {
    kind: 'server',
    title: userErrorCopy(error, { scope: 'tech', fallback: '登录失败' }),
    content: '',
    modal: false,
  }
}
