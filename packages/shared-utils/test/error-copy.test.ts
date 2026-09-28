/**
 * T465 映射表与兜底收口的纯层用例。
 *
 * 覆盖：已知码出中文、trace 优先、三端覆盖表、未知码/网络层必带码位、
 * 英文原文永不出现在用户面、代码自撰中文（标记位与进程内提示）如何存活、
 * 以及码表自身的不变量。
 */
import { describe, it, expect } from 'vitest'
import {
  ADMIN_ERROR_CODE_COPY, ERROR_CODE_COPY, PATIENT_ERROR_CODE_COPY, TECH_ERROR_CODE_COPY,
  attachErrorMeta, errorCodeCopy, logErrorText, markUserCopy,
  userErrorCode, userErrorCodeToken, userErrorCopy, userErrorHttpStatus,
} from '../src/errorCopy'

/** 后端在真实链路里会给的英文原文（取自 staging 实测串的形状） */
const ENGLISH_BACKEND = 'collectIntervalSeconds must be between 60 and 3600'
const ENGLISH_GATEWAY = 'invalid token: token expired'
const ENGLISH_TRANSPORT = 'request:fail timeout'

function backendError(message: string, code?: number, httpStatus?: number): Error {
  return attachErrorMeta(new Error(message), { code, httpStatus })
}

describe('userErrorCopy 已知码', () => {
  it('按码出中文，不看 message 文本', () => {
    const err = backendError(ENGLISH_BACKEND, 20400, 400)
    expect(userErrorCopy(err, { scope: 'admin', fallback: '保存失败' })).toBe('设备信息有误，请核对后重试')
  })

  it('trace.errorCode（T464）优先于信封 code，两者同值时结果一致', () => {
    const withTrace = new Error(ENGLISH_BACKEND) as Error & { trace?: { errorCode?: number } }
    withTrace.trace = { errorCode: 50404 }
    withTrace.code = 90001
    expect(userErrorCode(withTrace)).toBe(50404)
    expect(userErrorCopy(withTrace, { fallback: '推送失败' })).toBe('消息记录不存在，请刷新后重试')
  })

  it('信封 code 是字符串形态也认', () => {
    const err = new Error(ENGLISH_BACKEND)
    ;(err as Error & { code?: unknown }).code = '40404'
    expect(userErrorCodeToken(err)).toBe(40404)
    expect(userErrorCopy(err)).toBe('告警记录不存在，请刷新后重试')
  })
})

describe('userErrorCopy 三端覆盖表', () => {
  it('10401 在患者端用 PRD 句、admin 用防枚举句、技师端用手机号句', () => {
    const err = backendError(ENGLISH_GATEWAY, 10401, 401)
    expect(userErrorCopy(err, { scope: 'patient' })).toBe('授权信息已失效，请重新登录')
    expect(userErrorCopy(err, { scope: 'admin' })).toBe('用户名或密码错误')
    expect(userErrorCopy(err, { scope: 'tech' })).toBe('手机号或密码错误，请重试')
    // 不传 scope 走基础表
    expect(userErrorCopy(err)).toBe('登录信息已失效，请重新登录')
  })

  it('覆盖表的键必须都在基础表里（否则不传 scope 会退化成未知码兜底）', () => {
    for (const table of [PATIENT_ERROR_CODE_COPY, ADMIN_ERROR_CODE_COPY, TECH_ERROR_CODE_COPY]) {
      for (const code of Object.keys(table)) {
        expect(ERROR_CODE_COPY[code], `基础表缺码 ${code}`).toBeTruthy()
      }
    }
  })
})

describe('userErrorCopy 未知码与网络层兜底（卡面第 2 项：务必带错误码）', () => {
  it('未知码 + 页面兜底句 ⇒ 句子保留、码追进括号', () => {
    const err = backendError(ENGLISH_BACKEND, 42424, 400)
    expect(userErrorCopy(err, { fallback: '加载失败' })).toBe('加载失败（错误码 42424）')
  })

  it('未知码 + 无兜底句 ⇒ Boss 整句', () => {
    const err = backendError(ENGLISH_GATEWAY, 42424, 401)
    expect(userErrorCopy(err)).toBe('操作失败（错误码 42424），请截图反馈')
  })

  it('只有 HTTP 状态、没信封码 ⇒ 码位 HTTP<status>', () => {
    const err = backendError('', undefined, 500)
    expect(userErrorCodeToken(err)).toBe('HTTP500')
    expect(userErrorCopy(err, { fallback: '保存失败' })).toBe('保存失败（错误码 HTTP500）')
  })

  it('传输层失败（无码无状态）⇒ 码位 NET，且绝不回落到 request:fail 原文', () => {
    const err = new Error(ENGLISH_TRANSPORT)
    expect(userErrorCopy(err, { fallback: '配网失败' })).toBe('配网失败（错误码 NET）')
    expect(userErrorCopy(err)).toBe('操作失败（错误码 NET），请截图反馈')
  })

  it('message 为空也出中文', () => {
    expect(userErrorCopy(new Error(''), { fallback: '提交失败' })).toBe('提交失败（错误码 NET）')
  })

  it('非 Error 的 reject 值（字符串、null、对象）不抛异常且必带码位', () => {
    for (const val of ['boom', null, undefined, { detail: 'x' }]) {
      const out = userErrorCopy(val, { fallback: '加载失败' })
      expect(out).toContain('加载失败（错误码 NET）')
    }
  })
})

describe('代码自撰中文如何存活', () => {
  it('userCopy 标记位优先级最高：逐字返回、不补码、不被覆盖表抢走', () => {
    // 技师端会话失效同样是 401 + 10401，若不标记会被覆盖表改成「手机号或密码错误」
    const err = markUserCopy(backendError('登录已过期，请重新登录', 10401, 401), '登录已过期，请重新登录')
    expect(userErrorCopy(err, { scope: 'tech', fallback: '加载失败' })).toBe('登录已过期，请重新登录')
  })

  it('无码无状态的进程内中文提示（mock/服务层）当主语，但仍补 NET 满足带码要求', () => {
    expect(userErrorCopy(new Error('团队名称已存在'), { scope: 'admin', fallback: '保存失败' }))
      .toBe('团队名称已存在（错误码 NET）')
    expect(userErrorCopy(new Error('网络错误，请稍后重试'), { scope: 'admin', fallback: '登录失败' }))
      .toBe('网络错误，请稍后重试（错误码 NET）')
  })

  it('有码位信号时不用进程内中文当主语（页面自己的句子更具体）', () => {
    const err = backendError('服务器异常，请稍后重试', undefined, 503)
    expect(userErrorCopy(err, { scope: 'patient', fallback: '加载实时数据失败' }))
      .toBe('加载实时数据失败（错误码 HTTP503）')
  })
})

describe('永不把技术原文渲染给用户', () => {
  const englishSamples = [ENGLISH_BACKEND, ENGLISH_GATEWAY, ENGLISH_TRANSPORT, 'not found', 'user disabled']
  for (const text of englishSamples) {
    it(`英文原文 ${JSON.stringify(text)} 不出现在任何形态的返回值里`, () => {
      for (const err of [
        new Error(text),
        backendError(text, undefined, 400),
        markUserCopy(new Error(text), ''),
      ]) {
        const out = userErrorCopy(err, { scope: 'admin', fallback: '操作失败' })
        expect(out).not.toContain(text)
        expect(out).toMatch(/[一-龥]/)
      }
    })
  }
})

describe('attachErrorMeta', () => {
  it('只加字段，不改 message（技术原文留给日志面）', () => {
    const err = attachErrorMeta(new Error(ENGLISH_BACKEND), { code: 10400, httpStatus: 400 })
    expect(err.message).toBe(ENGLISH_BACKEND)
    expect(userErrorCode(err)).toBe(10400)
    expect(userErrorHttpStatus(err)).toBe(400)
  })

  it('NaN／undefined 不写进字段（否则会污染码位）', () => {
    const err = attachErrorMeta(new Error('x'), { code: Number.NaN, httpStatus: undefined })
    expect((err as Error & { code?: unknown }).code).toBeUndefined()
    expect(userErrorCode(err)).toBeNull()
  })

  it('带 requestId 时按 T464 形状落成 trace{errorCode, requestId}', () => {
    const err = attachErrorMeta(new Error('x'), { code: 30403, requestId: 'a1b2c3d4e5f60718' })
    expect((err as Error & { trace?: unknown }).trace).toEqual({ errorCode: 30403, requestId: 'a1b2c3d4e5f60718' })
  })
})

describe('logErrorText（日志面出口）', () => {
  it('Error 取原文，string 原样，null 与对象不抛', () => {
    expect(logErrorText(new Error(ENGLISH_BACKEND))).toBe(ENGLISH_BACKEND)
    expect(logErrorText('boom')).toBe('boom')
    expect(logErrorText(null)).toBe('null')
    expect(logErrorText(undefined)).toBe('undefined')
    expect(logErrorText({ errMsg: 'request:fail' })).toBe('{"errMsg":"request:fail"}')
  })

  it('循环引用不抛（JSON.stringify 会抛）', () => {
    const cyclic: Record<string, unknown> = { name: 'x' }
    cyclic.self = cyclic
    expect(() => logErrorText(cyclic)).not.toThrow()
    expect(logErrorText(cyclic)).toBe('[object Object]')
  })
})

describe('码表不变量（表内容与展示层形状）', () => {
  it('每条文案都是中文，且不含英文技术句残留', () => {
    for (const [name, table] of Object.entries({
      base: ERROR_CODE_COPY, patient: PATIENT_ERROR_CODE_COPY,
      admin: ADMIN_ERROR_CODE_COPY, tech: TECH_ERROR_CODE_COPY,
    })) {
      for (const [code, copy] of Object.entries(table)) {
        expect(/^[1-9][0-9]{4}$/.test(code), `${name} 键形异常 ${code}（应为域号+HTTP 三位）`).toBe(true)
        expect(copy, `${name} ${code} 文案为空`).toBeTruthy()
        expect(copy, `${name} ${code} 缺中文`).toMatch(/[一-龥]/)
        expect(copy, `${name} ${code} 含等号或箭头形状`).not.toMatch(/=>|undefined/)
      }
    }
  })

  it('码位数与域号一致：基础表每个键都能被 errorCodeCopy 命中', () => {
    for (const code of Object.keys(ERROR_CODE_COPY).map(Number)) {
      expect(errorCodeCopy(code), `码 ${code} 查不到`).toBe(ERROR_CODE_COPY[code])
    }
  })
})
