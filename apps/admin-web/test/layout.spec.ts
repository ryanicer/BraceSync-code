// 布局单测：侧边栏按角色过滤菜单 + 顶栏用户信息。
// 页面子路由用 stub 组件，避免 MainLayout 经路由重复嵌套渲染。
import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory, type RouteRecordRaw } from 'vue-router'
import { defineComponent } from 'vue'
import ElementPlus from 'element-plus'
import MainLayout from '../src/layout/MainLayout.vue'
import { registerPermissionGuard, pageRoutes } from '../src/router'
import { useAuthStore } from '../src/stores/auth'

const Stub = defineComponent({ render: () => null })

async function mountLayout(role: 'admin' | 'cs') {
  localStorage.clear()
  setActivePinia(createPinia())
  const auth = useAuthStore()
  auth.login(role)
  const children: RouteRecordRaw[] = pageRoutes.map((r) => ({ path: r.path, component: Stub, meta: r.meta }))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: Stub },
      { path: '/403', component: Stub },
      { path: '/', component: Stub, children: [{ path: '', redirect: '/dashboard' }, ...children] },
    ],
  })
  registerPermissionGuard(router)
  await router.push(role === 'cs' ? '/communication' : '/dashboard')
  await router.isReady()
  const wrapper = mount(MainLayout, {
    global: { plugins: [router, ElementPlus] },
  })
  await flushPromises()
  return wrapper
}

describe('MainLayout 布局', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('admin 侧边栏展示全部 12 页菜单', async () => {
    const wrapper = await mountLayout('admin')
    const items = wrapper.findAll('.sidebar-menu li.el-menu-item')
    expect(items.length).toBe(12)
    expect(wrapper.text()).toContain('数据概览')
    expect(wrapper.text()).toContain('系统配置')
    expect(wrapper.text()).toContain('运营管理员')
    wrapper.unmount()
  })

  it('cs 侧边栏仅展示患者沟通 1 项', async () => {
    const wrapper = await mountLayout('cs')
    const items = wrapper.findAll('.sidebar-menu li.el-menu-item')
    expect(items.length).toBe(1)
    expect(wrapper.text()).toContain('患者沟通')
    expect(wrapper.text()).not.toContain('数据概览')
    wrapper.unmount()
  })

  it('顶栏展示当前页面标题与退出按钮', async () => {
    const wrapper = await mountLayout('admin')
    expect(wrapper.find('.top-nav-title').text()).toBe('数据概览')
    expect(wrapper.text()).toContain('退出')
    wrapper.unmount()
  })
})
