// T708 运维反馈前端（案 C：零表、零后端接口）三件事各自有钉：
//   ① 不进权限矩阵/模块词表 —— 否则要加 DB 模块键 + 迁移，违反 Boss 已批的「零表零接口」；
//   ② 守卫放行口径 —— 任意已登录角色可达 /feedback，未登录仍跳登录（门禁 = 登录态 + 内网隔离，拍板④A）；
//   ③ env 由构建变量注入并随 iframe 透传 —— 运维不可见不可改，非 prod/staging 一律不渲染（不报错环境单）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory, type RouteRecordRaw } from 'vue-router'
import { defineComponent } from 'vue'
import ElementPlus from 'element-plus'
import MainLayout from '../src/layout/MainLayout.vue'
import FeedbackPage from '../src/pages/feedback/index.vue'
import { registerPermissionGuard, pageRoutes, internalToolRoutes, authLandingPath } from '../src/router'
import {
  INTERNAL_TOOL_PATHS, ROLE_PAGE_MATRIX, PAGE_MODULES, canAccess, isInternalToolPath,
} from '../src/router/permissions'
import { buildFeedbackFrameSrc, feedbackEnvOf } from '../src/utils/feedback'
import { useAuthStore } from '../src/stores/auth'

const Stub = defineComponent({ render: () => null })

type Role = 'admin' | 'doctor' | 'cs'

/** 与 src/router/index.ts 同一装配：业务页 + 内部工具页都在 MainLayout 之下 */
function buildRouter() {
  const children: RouteRecordRaw[] = [
    ...pageRoutes.map((r) => ({ path: r.path, component: Stub })),
    ...internalToolRoutes.map((r) => ({ path: r.path, component: Stub, meta: r.meta })),
  ]
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: Stub },
      { path: '/403', component: Stub },
      { path: '/', component: Stub, children: [{ path: '', redirect: () => authLandingPath() }, ...children] },
    ],
  })
  registerPermissionGuard(router)
  return router
}

async function navigateAs(role: Role | null, path: string) {
  localStorage.clear()
  setActivePinia(createPinia())
  const auth = useAuthStore()
  if (role) auth.login(role)
  const router = buildRouter()
  await router.push(path)
  await router.isReady()
  return router.currentRoute.value
}

async function mountLayout(role: Role) {
  localStorage.clear()
  setActivePinia(createPinia())
  const auth = useAuthStore()
  auth.login(role)
  const router = buildRouter()
  await router.push(role === 'cs' ? '/communication' : '/dashboard')
  await router.isReady()
  const wrapper = mount(MainLayout, { global: { plugins: [router, ElementPlus] } })
  await flushPromises()
  return wrapper
}

describe('T708 ①：/feedback 不占业务权限词表（零表零接口自证）', () => {
  it('内部工具路由与守卫放行路径集同源、只此一条', () => {
    expect(internalToolRoutes.map((r) => r.path)).toEqual(INTERNAL_TOOL_PATHS)
    expect(INTERNAL_TOOL_PATHS).toEqual(['/feedback'])
  })

  it('/feedback 不在 pageRoutes / PAGE_MODULES / 任何角色矩阵里（加了就等于要配库迁移）', () => {
    expect(pageRoutes.map((r) => r.path)).not.toContain('/feedback')
    expect(PAGE_MODULES.map((m) => m.path)).not.toContain('/feedback')
    expect(PAGE_MODULES.map((m) => m.key)).not.toContain('feedback')
    for (const role of ['admin', 'doctor', 'cs'] as const) {
      expect(ROLE_PAGE_MATRIX[role], `${role} 的权限矩阵不该登记内部工具页`).not.toContain('/feedback')
      expect(canAccess(role, '/feedback'), `矩阵口径里 ${role} 无权（准入由 isInternalToolPath 放行）`).toBe(false)
    }
  })

  it('业务页基数仍是 17（本页没把它抬成 18）', () => {
    expect(pageRoutes).toHaveLength(17)
    expect(PAGE_MODULES).toHaveLength(17)
  })

  it('isInternalToolPath 只认登记过的路径', () => {
    expect(isInternalToolPath('/feedback')).toBe(true)
    expect(isInternalToolPath('/settings')).toBe(false)
    expect(isInternalToolPath('/feedback/extra')).toBe(false)
  })

  it('路由 meta 齐备（顶栏标题「📮 问题反馈」由此而来）', () => {
    expect(String(internalToolRoutes[0].meta?.title)).toBe('问题反馈')
    expect(String(internalToolRoutes[0].meta?.icon)).toBe('📮')
  })
})

describe('T708 ②：守卫准入 = 登录态，与角色无关', () => {
  it('三个预置角色都能进 /feedback（客服只有 1 个业务页，照样可报障）', async () => {
    for (const role of ['admin', 'doctor', 'cs'] as Role[]) {
      const route = await navigateAs(role, '/feedback')
      expect(route.path, `${role} 应停在 /feedback，实际落到 ${route.path}`).toBe('/feedback')
    }
  })

  it('未登录访问 /feedback → 跳登录并带 redirect（登录后回得来）', async () => {
    const route = await navigateAs(null, '/feedback')
    expect(route.path).toBe('/login')
    expect(route.query.redirect).toBe('/feedback')
  })

  it('越权口径未被放宽：cs 直连业务页仍 403（内部工具放行不传染）', async () => {
    expect((await navigateAs('cs', '/settings')).path).toBe('/403')
  })
})

describe('T708 ③：env 构建变量注入', () => {
  it('env 白名单只认 prod / staging，其余（含大小写、空、未注入）判不合法', () => {
    expect(feedbackEnvOf('prod')).toBe('prod')
    expect(feedbackEnvOf('staging')).toBe('staging')
    expect(feedbackEnvOf('PROD')).toBeNull()
    expect(feedbackEnvOf('dev')).toBeNull()
    expect(feedbackEnvOf('tst')).toBeNull()
    expect(feedbackEnvOf('')).toBeNull()
    expect(feedbackEnvOf(undefined)).toBeNull()
  })

  it('iframe src = 服务基址 + ?env=<注入值>（透传，运维不参与）', () => {
    expect(buildFeedbackFrameSrc('https://fb.internal.example.com/', 'prod'))
      .toBe('https://fb.internal.example.com/?env=prod')
    expect(buildFeedbackFrameSrc('https://fb.internal.example.com', 'staging'))
      .toBe('https://fb.internal.example.com?env=staging')
  })

  it('基址自带 query 时用 & 追加，不产生第二个 ?', () => {
    expect(buildFeedbackFrameSrc('https://fb.internal.example.com/?k=1', 'prod'))
      .toBe('https://fb.internal.example.com/?k=1&env=prod')
  })

  it('未配置服务地址或 env 不合法 ⇒ null（页面走「未开通」占位，不渲染错环境的 iframe）', () => {
    expect(buildFeedbackFrameSrc(undefined, 'prod')).toBeNull()
    expect(buildFeedbackFrameSrc('', 'prod')).toBeNull()
    expect(buildFeedbackFrameSrc('https://fb.internal.example.com/', undefined)).toBeNull()
    expect(buildFeedbackFrameSrc('https://fb.internal.example.com/', 'dev')).toBeNull()
  })
})

describe('T708 反馈页渲染（iframe 宿主）', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('配置齐全：渲染 iframe，src 带 ?env=prod', () => {
    vi.stubEnv('VITE_FEEDBACK_SERVICE_URL', 'https://fb.internal.example.com/')
    vi.stubEnv('VITE_FEEDBACK_ENV', 'prod')
    const wrapper = mount(FeedbackPage)
    const frame = wrapper.find('[data-testid="feedback-frame"]')
    expect(frame.exists()).toBe(true)
    expect(frame.attributes('src')).toBe('https://fb.internal.example.com/?env=prod')
    expect(wrapper.find('[data-testid="feedback-placeholder"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('未配置：显示未开通占位、不渲染 iframe，且页面无第三方系统字样（Boss 拍板②）', () => {
    vi.stubEnv('VITE_FEEDBACK_SERVICE_URL', '')
    vi.stubEnv('VITE_FEEDBACK_ENV', '')
    const wrapper = mount(FeedbackPage)
    expect(wrapper.find('[data-testid="feedback-frame"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="feedback-placeholder"]').text()).toContain('尚未开通')
    expect(wrapper.text()).not.toMatch(/TAPD|tapd/)
    wrapper.unmount()
  })
})

describe('T708 侧栏入口', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('客服：业务菜单仍 1 项，工具区多「问题反馈」——两者不同容器，互不抬高计数', async () => {
    const wrapper = await mountLayout('cs')
    expect(wrapper.findAll('.sidebar-menu li.el-menu-item')).toHaveLength(1)
    const tools = wrapper.findAll('.sidebar-tool')
    expect(tools).toHaveLength(1)
    expect(tools[0].text()).toContain('问题反馈')
    expect(tools[0].attributes('href')).toBe('/feedback')
    wrapper.unmount()
  })

  it('运营管理员：业务菜单仍 17 项（e2e-real/14 逐角色钉的数不因本卡漂移）', async () => {
    const wrapper = await mountLayout('admin')
    expect(wrapper.findAll('.sidebar-menu li.el-menu-item')).toHaveLength(17)
    expect(wrapper.findAll('.sidebar-tool')).toHaveLength(1)
    wrapper.unmount()
  })
})
