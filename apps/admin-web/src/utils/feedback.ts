/**
 * T708 运维反馈 iframe 宿主页的取址逻辑（案 C：页面本体由独立容器的代理 service 提供，
 * 后台只负责按部署环境把它内嵌进来）。
 *
 * 两个值都来自构建变量（部署侧注入，见 scripts/deploy/deploy-staging.sh 的 VITE_* 口径）：
 * - VITE_FEEDBACK_SERVICE_URL：代理 service 静态页基址（T709/Andy 落地后给出）
 * - VITE_FEEDBACK_ENV：本包所属环境，只认 prod / staging，与 service 侧 env 白名单同集合
 * 任一不合格就不渲染 iframe（返 null）——宁可不显示入口内容，也不把单报到错环境。
 */
export const FEEDBACK_ENVS = ['prod', 'staging'] as const

export type FeedbackEnv = (typeof FEEDBACK_ENVS)[number]

export function feedbackEnvOf(raw: string | undefined): FeedbackEnv | null {
  const v = (raw ?? '').trim()
  return (FEEDBACK_ENVS as readonly string[]).includes(v) ? (v as FeedbackEnv) : null
}

/** 拼 iframe src：环境不合法或未配置服务地址 ⇒ null（页面走未开通占位态） */
export function buildFeedbackFrameSrc(
  serviceUrl: string | undefined,
  envRaw: string | undefined,
): string | null {
  const base = (serviceUrl ?? '').trim()
  const env = feedbackEnvOf(envRaw)
  if (!base || !env) return null
  return `${base}${base.includes('?') ? '&' : '?'}env=${env}`
}
