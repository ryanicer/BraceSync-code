/**
 * T465：错误码到中文文案的统一映射（三端共用）
 *
 * 背景：三端原先把 `e.message` 裸展示给用户（admin ElMessage / 两端 uni.showToast），
 * 而 `err.message` 的真实内容是后端英文串或传输层英文串（如 `request:fail`、
 * `collectIntervalSeconds must be...`），患者和运营都读不懂，还会透出「账号存在但被禁用」。
 *
 * 口径：
 *  - 有码 ⇒ 按码出中文文案（本表），**不看 message 文本、不看 HTTP status**。
 *  - 未知码 ⇒ 保留该页原有中文兜底句并把码追进括号：「加载失败（错误码 40404）」；
 *    该页没给兜底句时用 Boss 定的整句「操作失败（错误码 X），请截图反馈」。两种形状都必带码。
 *  - 无信封码只有 HTTP 状态 ⇒ 码位显示 HTTP<status>；两者都没有 ⇒ NET（传输层）。
 *  - 代码里刻意写好的中文提示（会话失效、登录凭据错误）走 userCopy 标记位，优先级最高，
 *    不会被表覆盖成通用句；它是作者写的常量，不是后端原文，所以不算「裸展示 e.message」。
 *  - mock 层/服务层自撰的中文业务提示（无码、无 HTTP 状态）仍当主语用，只补码位 NET，
 *    否则 mock 演示与进程内校验句会被通用句吞掉（「团队名称已存在」这类）。
 *
 * 🔴 刻意不改 `err.message` 本身：技术原文只进日志面（本包 logErrorText），
 *    展示层收口只发生在渲染前那一次。传输层改写会把三处依赖透传原文的断言判红。
 *
 * 码位权威叙述：docs/architecture/tech-architecture.md:248（域号 + HTTP 三位）。
 * T464（Winner）落地后响应体带顶层 trace{errorCode, requestId}，errorCode 与信封 code 同值，
 * 故取码顺序为 trace.errorCode 优先、信封 code 回退，本表不必改键形。
 */

/** 用户面错误提示的作用域：三端对同一个码可以有不同的既定文案 */
export type ErrorCopyScope = 'admin' | 'patient' | 'tech'

/**
 * 基础映射表：后端在用的信封码（origin/main 150b7e4 穷举，已剔 12345＝测试常量
 * 与 40029＝微信上游 errcode，两者都不是我们的码）。
 * 语义逐条取自 services 各服务的 model.go 与 file-service 的 error_response.go 常量注释。
 * （写法上刻意不嵌套注释块通配符，esbuild 会把注释内的双星当语法。）
 */
export const ERROR_CODE_COPY: Record<number, string> = {
  // 用户域 1xxxx（user-service/internal/model/model.go:19-34）
  10001: '登录状态已失效，请重新登录',
  10400: '提交的信息有误，请检查后重试',
  10401: '登录信息已失效，请重新登录',
  10403: '没有该操作的权限，请联系管理员',
  10404: '要操作的数据不存在，请刷新后重试',
  10409: '当前状态与该操作冲突，请刷新后重试',
  10502: '微信服务暂不可用，请稍后重试',
  10601: '尚未绑定就诊档案，请先完成绑定',
  10602: '未匹配到就诊档案，请联系门诊工作人员',
  10603: '该就诊档案已被其他微信绑定，请联系门诊工作人员',
  10604: '手机号授权信息有误，请重新授权',
  10605: '操作已过期，请重新发起',
  // 设备域 2xxxx（device-service/internal/model/model.go:82-88、data-service model.go:70-78）
  20400: '设备信息有误，请核对后重试',
  20402: '数据时间超出可提交范围，请稍后重试',
  20403: '无权操作该设备，请联系工作人员',
  20404: '设备或记录不存在，请核对设备编号后重试',
  20409: '设备状态冲突，请稍后重试',
  20429: '操作过于频繁，请稍后重试',
  // 数据域 3xxxx（data-service/internal/model/model.go:75-85）
  30001: '查询条件有误，请调整后重试',
  30400: '设备标识与请求不一致，请重新进入页面',
  30403: '无权查看该数据，请联系管理员',
  // 告警域 4xxxx（alert-service code_lock_t402_test.go:27-28、user-service model.go:34）
  40301: '登录凭证范围不足，请重新登录',
  40403: '无权查看该告警，请联系管理员',
  40404: '告警记录不存在，请刷新后重试',
  // 消息域 5xxxx（msg-service/internal/model/model.go:109-115）
  50001: '内部通道校验未通过，请联系管理员',
  50400: '消息设置参数有误，请检查后重试',
  50403: '无权操作该消息设置，请联系管理员',
  50404: '消息记录不存在，请刷新后重试',
  54002: '通知额度已用完，请联系门诊工作人员',
  // 文件域 6xxxx（file-service/internal/handler/error_response.go:13-26）
  60001: '文件参数有误，请重新选择文件后重试',
  60002: '登录状态已失效，请重新登录后再上传',
  60403: '无权访问该文件，请联系管理员',
  61001: '文件不存在或已被清理，请重新上传',
  61002: '文件上传失败，请稍后重试',
  61003: '文件上传通道暂不可用，请稍后重试',
  69999: '文件服务暂不可用，请稍后重试',
  // 系统域 9xxxx（各服务 model.go CodeInternal、alert-service config.go:32）
  90001: '服务暂时异常，请稍后重试',
  90712: '告警阈值配置异常，请联系管理员',
}

/**
 * 患者端覆盖：PRD §7A.1.1 逐码给定给患者的表现（docs/prd/PRD_V3.md），
 * 这些句子的措辞是稿面/PRD 定的，不许被基础表改成通用句。
 */
export const PATIENT_ERROR_CODE_COPY: Record<number, string> = {
  10001: '登录失败，请联系门诊工作人员',
  10401: '授权信息已失效，请重新登录',
  10502: '微信服务暂不可用，请稍后重试',
  10601: '尚未绑定就诊档案，请先完成绑定',
  10602: '未匹配到就诊档案，请联系门诊工作人员',
  10603: '该就诊档案已被其他微信绑定，请联系门诊工作人员',
  10604: '授权信息已失效，请重新授权手机号',
  10605: '操作已过期，请重新绑定',
}

/**
 * 运营后台覆盖：登录页与医护账号页都要求「10401 两个成因（凭据错误／账号禁用）
 * 压成一条中文」（docs/design/admin/登录.html:134、医护账号.html:190），
 * 措辞沿用 adminLogin 既有句，防账号枚举。
 */
export const ADMIN_ERROR_CODE_COPY: Record<number, string> = {
  10401: '用户名或密码错误',
}

/** 技师端覆盖：登录为手机号 + 密码，与 admin 同因不同措辞 */
export const TECH_ERROR_CODE_COPY: Record<number, string> = {
  10401: '手机号或密码错误，请重试',
}

const SCOPE_TABLES: Record<ErrorCopyScope, Record<number, string> | undefined> = {
  admin: ADMIN_ERROR_CODE_COPY,
  patient: PATIENT_ERROR_CODE_COPY,
  tech: TECH_ERROR_CODE_COPY,
}

/** 未知码兜底里码位的取值（数字码 / HTTP 状态 / 传输层 NET） */
export type UserErrorCodeToken = number | string

export interface UserErrorLike {
  message?: string
  code?: unknown
  errorCode?: unknown
  httpStatus?: unknown
  statusCode?: unknown
  userCopy?: unknown
  trace?: { errorCode?: unknown; requestId?: unknown } | null
}

/** 取信封业务码：T464 的 trace.errorCode 优先，回退信封 code（两者同值） */
export function userErrorCode(err: unknown): number | null {
  const e = (err ?? {}) as UserErrorLike
  for (const cand of [e.trace?.errorCode, e.code, e.errorCode]) {
    const n = typeof cand === 'number' ? cand : typeof cand === 'string' ? Number(cand) : Number.NaN
    if (Number.isFinite(n) && n > 0) return n
  }
  return null
}

/** 取 HTTP 状态（只用于未知码兜底的码位展示，不参与查表） */
export function userErrorHttpStatus(err: unknown): number | null {
  const e = (err ?? {}) as UserErrorLike
  for (const cand of [e.httpStatus, e.statusCode]) {
    const n = typeof cand === 'number' ? cand : typeof cand === 'string' ? Number(cand) : Number.NaN
    if (Number.isFinite(n) && n > 0) return n
  }
  return null
}

/** 未知码兜底的码位：数字码 > HTTP<status> > NET */
export function userErrorCodeToken(err: unknown): UserErrorCodeToken {
  const code = userErrorCode(err)
  if (code != null) return code
  const status = userErrorHttpStatus(err)
  if (status != null) return `HTTP${status}`
  return 'NET'
}

/**
 * 代码里刻意写好的中文提示（会话失效、登录凭据错误等）打标：
 * 展示层优先原样用这句，日志面仍可读技术原文。
 */
export function markUserCopy<T extends Error>(err: T, copy: string): T {
  ;(err as T & { userCopy?: string }).userCopy = copy
  return err
}

/**
 * 把信封码／HTTP 状态挂到错误对象上（三端传输层调用）。
 * 只加字段，不改 message，技术原文保持给日志。
 */
export function attachErrorMeta<T extends Error>(
  err: T,
  meta: { code?: unknown; httpStatus?: unknown; requestId?: unknown },
): T {
  const bag = err as T & { code?: unknown; httpStatus?: unknown; trace?: { errorCode?: unknown; requestId?: unknown } }
  if (typeof meta.code === 'number' && Number.isFinite(meta.code)) bag.code = meta.code
  if (typeof meta.httpStatus === 'number' && Number.isFinite(meta.httpStatus)) bag.httpStatus = meta.httpStatus
  if (meta.requestId != null) bag.trace = { errorCode: bag.code, requestId: meta.requestId }
  return err
}

export interface UserErrorCopyOptions {
  /** 作用域：决定用哪张覆盖表，默认基础表 */
  scope?: ErrorCopyScope
  /** 该页原有的中文兜底句（未知码时以它为主语，后接错误码） */
  fallback?: string
}

/** 已知码查表；覆盖表优先于基础表 */
export function errorCodeCopy(code: number, scope?: ErrorCopyScope): string | undefined {
  const table = scope ? SCOPE_TABLES[scope] : undefined
  return table?.[code] ?? ERROR_CODE_COPY[code]
}

/**
 * 展示层唯一出口：任何情况下都不把技术原文当作用户面文案直接返回。
 *
 * 取值顺序：
 *  1) userCopy 标记位（刻意自撰且要求逐字显示的提示，如会话失效、防枚举的登录提示）；
 *  2) 数字码命中码表（覆盖表优先）；
 *  3) 进程内抛出、既无信封码也无 HTTP 状态、且 message 本身是中文 —— 这是 mock 层与服务层
 *     自己写的中文业务提示（如「团队名称已存在」），当作用户面主语，但仍补码位 NET；
 *  4) 其余（后端英文原文、传输层失败）一律用调用页自己的中文兜底句 + 码位，没给就用 Boss 整句。
 *
 * 🔴 后端英文 message 永远进不到返回值：3) 这一格用「含中文」把关，不是「有 message 就用」。
 */
const CJK = /[一-龥]/

export function userErrorCopy(err: unknown, opts: UserErrorCopyOptions = {}): string {
  const message = (err as { message?: unknown } | null | undefined)?.message
  const authored = (err as UserErrorLike | null | undefined)?.userCopy
  if (typeof authored === 'string' && authored.trim().length > 0) return authored

  const code = userErrorCode(err)
  if (code != null) {
    const copy = errorCodeCopy(code, opts.scope)
    if (copy) return copy
  }

  const token = userErrorCodeToken(err)
  // 有码位信号（信封码或 HTTP 状态）说明这错来自链路，主语用页面自己的句子；
  // 没有任何信号又写着中文，那是进程内自撰提示，主语用这句中文。
  const fromTransport = code != null || userErrorHttpStatus(err) != null
  const inProcessChinese =
    !fromTransport && typeof message === 'string' && CJK.test(message)
  const label = (inProcessChinese ? message : opts.fallback ?? '').trim()
  // 无该页兜底句时，用 Boss 定的整句；有则保留中文句子、把码追加进去
  return label ? `${label}（错误码 ${token}）` : `操作失败（错误码 ${token}），请截图反馈`
}

/**
 * 日志面唯一出口：技术原文照旧（给 logger / bleLog / console），
 * 这样「裸 e.message」在展示层归零、日志细节不受损。
 */
export function logErrorText(err: unknown): string {
  if (err instanceof Error) return err.message
  if (typeof err === 'string') return err
  if (err == null) return String(err)
  try {
    return JSON.stringify(err)
  } catch {
    return String(err)
  }
}
