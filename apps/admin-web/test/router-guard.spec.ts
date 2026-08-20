// 权限路由守卫单测：未登录跳登录、角色越权跳 403、合法访问放行。
// 用 stub 组件 + 真实路由路径验证纯守卫逻辑，不触发页面组件懒加载。
import { describe, it, expect, beforeEach } from 'vitest'
import { createRouter, createMemoryHistory, type RouteRecordRaw } from 'vue-router'
import { defineComponent } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { registerPermissionGuard, pageRoutes } from '../src/router'
import { useAuthStore } from '../src/stores/auth'

const Stub = defineComponent({ render: () => null })

function buildRouter() {
  const children: RouteRecordRaw[] = pageRoutes.map((r) => ({ path: r.path, component: Stub }))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: Stub },
      { path: '/403', component: Stub },
      { path: '/', component: Stub, children: [{ path: '', redirect: '/dashboard' }, ...children] },
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

  it('admin 可访问全部 12 页', async () => {
    const pages = [
      '/dashboard', '/monitor', '/patients', '/teams', '/devices', '/alerts',
      '/communication', '/orthosis-log', '/install-records', '/technicians', '/roles', '/settings',
    ]
    for (const page of pages) {
      expect(await navigateAs('admin', page)).toBe(page)
    }
  })

  it('doctor 访问允许页面放行，越权页面跳 403', async () => {
    expect(await navigateAs('doctor', '/orthosis-log')).toBe('/orthosis-log')
    expect(await navigateAs('doctor', '/patients')).toBe('/403')
    expect(await navigateAs('doctor', '/settings')).toBe('/403')
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
})
