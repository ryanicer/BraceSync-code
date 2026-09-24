// 权限路由守卫单测：未登录跳登录、角色越权跳 403、合法访问放行。
// 用 stub 组件 + 真实路由路径验证纯守卫逻辑，不触发页面组件懒加载。
import { describe, it, expect, beforeEach } from 'vitest'
import { createRouter, createMemoryHistory, type RouteRecordRaw } from 'vue-router'
import { defineComponent } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { registerPermissionGuard, pageRoutes, authLandingPath } from '../src/router'
import { useAuthStore } from '../src/stores/auth'

const Stub = defineComponent({ render: () => null })

function buildRouter() {
  const children: RouteRecordRaw[] = pageRoutes.map((r) => ({ path: r.path, component: Stub }))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: Stub },
      { path: '/403', component: Stub },
      { path: '/', component: Stub, children: [{ path: '', redirect: () => authLandingPath() }, ...children] },
      { path: '/:pathMatch(.*)*', redirect: () => authLandingPath() },
    ],
  })
  registerPermissionGuard(router)
  return router
}

async function navigateAs(role: 'admin' | 'doctor' | 'cs' | null, path: string) {
  localStorage.clear()
  setActivePinia(createPinia())
  const auth = useAuthStore()
  if (role) auth.login(role)
  const router = buildRouter()
  await router.push(path)
  await router.isReady()
  return router.currentRoute.value.path
}

describe('权限路由守卫', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('未登录访问受保护页面 → 重定向 /login（带 redirect 参数）', async () => {
    localStorage.clear()
    setActivePinia(createPinia())
    const router = buildRouter()
    await router.push('/dashboard')
    await router.isReady()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/dashboard')
  })

  it('admin 可访问全部 16 页', async () => {
    const pages = [
      '/dashboard', '/monitor', '/patients', '/abnormal-report', '/teams', '/devices', '/alerts',
      '/communication', '/orthosis-log', '/install-records', '/review-records', '/review-templates',
      '/technicians', '/doctor-accounts', '/roles', '/settings',
    ]
    for (const page of pages) {
      expect(await navigateAs('admin', page)).toBe(page)
    }
  })

  it('doctor 访问允许页面放行，越权页面跳 403', async () => {
    expect(await navigateAs('doctor', '/orthosis-log')).toBe('/orthosis-log')
    expect(await navigateAs('doctor', '/review-templates')).toBe('/review-templates') // T135 医生可下载空白模板
    expect(await navigateAs('doctor', '/abnormal-report')).toBe('/abnormal-report') // T372 §7D.11 第 4 行医护 ✅
    expect(await navigateAs('doctor', '/patients')).toBe('/403')
    expect(await navigateAs('doctor', '/settings')).toBe('/403')
    expect(await navigateAs('doctor', '/doctor-accounts')).toBe('/403') // T315 医护不能管账号
  })

  it('cs 仅可访问患者沟通，其余跳 403', async () => {
    expect(await navigateAs('cs', '/communication')).toBe('/communication')
    expect(await navigateAs('cs', '/dashboard')).toBe('/403')
  })

  it('未登录访问根路径 → 登录页', async () => {
    localStorage.clear()
    setActivePinia(createPinia())
    const router = buildRouter()
    await router.push('/')
    await router.isReady()
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('各角色访问根路径落到自己的有权首页（T269 D3）', async () => {
    expect(await navigateAs('admin', '/')).toBe('/dashboard')
    expect(await navigateAs('doctor', '/')).toBe('/dashboard')
    expect(await navigateAs('cs', '/')).toBe('/communication')
  })

  it('未知路径同样按角色落地，客服不再被弹进 403 死循环（T269 D3）', async () => {
    expect(await navigateAs('cs', '/not-exist-page')).toBe('/communication')
    expect(await navigateAs('admin', '/not-exist-page')).toBe('/dashboard')
  })
})
