import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { userErrorCopy } from '@bracesync/shared-utils'
import { CODE_OLD_PASSWORD_WRONG } from '../src/utils/authError'

/**
 * T486 技师自助改密：请求层 + 拦截器豁免
 *
 * 这里跑的是 src/utils/request.ts 真身（不是 mock 掉的 request），
 * 因为本卡的缺陷恰好长在拦截器那一段：后端「旧密码填错」回 HTTP 401 + 信封 10001
 * （与登录口同形防枚举），而 T326 的失效判定看的是 HTTP 401 ——
 * 不豁免的话技师填错一次口令就被清 token 踢回登录页，页面那句提示永远渲染不到。
 *
 * 因此正反两向都要有牙齿：
 *   豁免只在「这条 URL + 10001」这一格生效；
 *   同一 URL 上的真失效（网关 401 信封 / 服务层 10401）仍必须踢。
 */

// request.ts 的 USE_MOCK / API_BASE_URL 是 vite define 注入的编译期常量，
// node 环境下必须在 import 之前挂到 globalThis 上（模块求值时读一次）。
;(globalThis as Record<string, unknown>).__USE_MOCK__ = false
;(globalThis as Record<string, unknown>).__API_BASE_URL__ = 'https://gateway.invalid'

// logger.ts 里 realtimeLogger 用 uni 条件编译声明了两次（#ifdef MP-WEIXIN / #ifndef），
// node 环境没有 uni 插件剥分支 ⇒ esbuild 直接判「symbol already declared」。
// 桩掉它才能把 request.ts 真身跑起来（本 spec 判的是拦截器行为，与日志无关）。
vi.mock('../src/utils/logger', () => ({
  logger: { info: () => {}, warn: () => {}, error: () => {} },
  maskPhone: (phone: string) => phone,
}))

const CHANGE_URL = '/api/v1/tech/change-password'
const OTHER_URL = '/api/v1/install-records'
const TOKEN_KEY = 'bracesync_tech_token'

type Envelope = { statusCode: number; code?: number; message?: string; data?: unknown }

let reply: Envelope = { statusCode: 200, code: 0 }
let removedKeys: string[] = []
let toasts: string[] = []
let relaunched: string[] = []
let sent: { url: string; method?: string; data?: unknown; header?: Record<string, string> }[] = []

function envelopeOf(e: Envelope) {
  return { code: e.code, message: e.message ?? '', data: e.data ?? null }
}

const uniStub = {
  getStorageSync: (key: string) => (key === TOKEN_KEY ? 'tech-jwt-token' : ''),
  setStorageSync: () => {},
  removeStorageSync: (key: string) => { removedKeys.push(key) },
  showToast: (opts: { title: string }) => { toasts.push(opts.title) },
  reLaunch: (opts: { url: string }) => { relaunched.push(opts.url) },
  request: (opts: {
    url: string
    method?: string
    data?: unknown
    header?: Record<string, string>
    success?: (res: { statusCode: number; data: unknown }) => void
    fail?: (err: { errMsg: string }) => void
  }) => {
    sent.push(opts)
    opts.success?.({ statusCode: reply.statusCode, data: envelopeOf(reply) })
  },
}

let request: (options: { url: string; method?: 'GET' | 'POST' | 'PUT' | 'DELETE'; data?: Record<string, unknown> }) => Promise<unknown>
let changeTechPassword: (oldPassword: string, newPassword: string) => Promise<void>

beforeEach(async () => {
  reply = { statusCode: 200, code: 0, data: { techId: 'TECH0001' } }
  removedKeys = []
  toasts = []
  relaunched = []
  sent = []
  ;(globalThis as Record<string, unknown>).uni = uniStub
  const req = await import('../src/utils/request')
  const api = await import('../src/api/password')
  request = req.request
  changeTechPassword = api.changeTechPassword
})

afterEach(() => {
  delete (globalThis as Record<string, unknown>).uni
})

describe('T486 拦截器豁免：改密端的「旧密码填错」不当作会话失效', () => {
  it('401 + 10001 打在这条 URL 上：不清 token、不回登录页，错误原样上抛', async () => {
    reply = { statusCode: 401, code: CODE_OLD_PASSWORD_WRONG, message: 'invalid old password' }

    await expect(request({ url: CHANGE_URL, method: 'POST', data: { oldPassword: 'a1', newPassword: 'b2' } })).rejects
      .toThrowError(/invalid old password/)

    expect(removedKeys).toEqual([])
    expect(relaunched).toEqual([])
    expect(toasts).toEqual([])
  })

  it('反证一：同一句 401 + 10001 换到别的 URL 仍然踢出（豁免不是把 401 整体放行）', async () => {
    reply = { statusCode: 401, code: CODE_OLD_PASSWORD_WRONG, message: 'invalid old password' }

    await expect(request({ url: OTHER_URL })).rejects.toThrow()

    expect(removedKeys).toEqual([TOKEN_KEY])
    expect(relaunched).toEqual(['/pages/login/index'])
  })

  it('反证二：这条 URL 上的真失效照样踢出——网关信封 code=401', async () => {
    reply = { statusCode: 401, code: 401, message: 'unauthorized: invalid or expired token' }

    await expect(request({ url: CHANGE_URL, method: 'POST' })).rejects.toThrow()

    expect(removedKeys).toEqual([TOKEN_KEY])
    expect(relaunched).toEqual(['/pages/login/index'])
    expect(toasts).toEqual(['登录已过期，请重新登录'])
  })

  it('反证三：这条 URL 上的真失效照样踢出——服务层信封 10401', async () => {
    reply = { statusCode: 200, code: 10401, message: 'unauthorized' }

    await expect(request({ url: CHANGE_URL, method: 'POST' })).rejects.toThrow()

    expect(removedKeys).toEqual([TOKEN_KEY])
    expect(relaunched).toEqual(['/pages/login/index'])
  })

  it('成功响应（code 0）不触发任何踢出动作', async () => {
    await expect(request({ url: CHANGE_URL, method: 'POST', data: { oldPassword: 'ab1def', newPassword: 'cd1efg' } }))
      .resolves.toEqual({ techId: 'TECH0001' })

    expect(removedKeys).toEqual([])
    expect(relaunched).toEqual([])
  })
})

describe('T486 api 层出入参与文案', () => {
  it('上行只有 oldPassword / newPassword 两个键，走 tech JWT', async () => {
    await changeTechPassword('ab1def', 'cd1efg')

    expect(sent).toHaveLength(1)
    const call = sent[0]
    expect(call.url).toBe('https://gateway.invalid' + CHANGE_URL)
    expect(call.method).toBe('POST')
    expect(Object.keys(call.data as Record<string, unknown>).sort()).toEqual(['newPassword', 'oldPassword'])
    expect(call.header?.Authorization).toBe('Bearer tech-jwt-token')
  })

  it('10001 打成改密页专用句，展示出口不再给出「登录状态已失效」', async () => {
    reply = { statusCode: 401, code: CODE_OLD_PASSWORD_WRONG }

    const err = await changeTechPassword('wrong-old', 'cd1efg').catch((e) => e)
    expect(userErrorCopy(err, { scope: 'tech', fallback: '修改失败' })).toBe('原密码不正确，请重新输入')
    // 对照：同一格若没被打 userCopy，命中的是共享基础表那一行（患者端语义），
    // 会把「填错一次口令」渲染成「去重登」。
    expect(userErrorCopy({ code: CODE_OLD_PASSWORD_WRONG }, { scope: 'tech' })).toBe('登录状态已失效，请重新登录')
  })

  it('非 10001 的错误不被 api 层改写（原样上抛，交给展示层查表）', async () => {
    reply = { statusCode: 400, code: 10400, message: 'new password must be 6-16 ...' }

    const err = await changeTechPassword('ab1def', 'weak').catch((e) => e)
    expect(err.userCopy).toBeUndefined()
    expect(userErrorCopy(err, { scope: 'tech', fallback: '修改失败' })).toBe('提交的信息有误，请检查后重试')
  })
})

describe('T486 前后端码位对拍（防「前端认的码」与后端常量漂移）', () => {
  const modelGo = fileURLToPath(new URL(
    '../../../services/user-service/internal/model/model.go',
    import.meta.url,
  ))

  it('CODE_OLD_PASSWORD_WRONG 与 Go 侧 CodeInvalidCredentials 同值', () => {
    const src = readFileSync(modelGo, 'utf8')
    const line = src.split(/\r?\n/).find((l) => /^\s*CodeInvalidCredentials\s*=\s*\d+/.test(l))
    expect(line, 'Go 侧找不到 CodeInvalidCredentials 常量行').toBeDefined()
    expect(Number(line?.match(/\d+/)?.[0])).toBe(CODE_OLD_PASSWORD_WRONG)
  })
})
