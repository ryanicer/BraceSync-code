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

  it('admin 侧边栏展示全部 16 页菜单', async () => {
    const wrapper = await mountLayout('admin')
    const items = wrapper.findAll('.sidebar-menu li.el-menu-item')
    expect(items.length).toBe(16) // 断言更新：14→15（T315 医护账号页）→16（T372 异常报告独立页）
    expect(wrapper.text()).toContain('数据概览')
    expect(wrapper.text()).toContain('系统配置')
    expect(wrapper.text()).toContain('复查报告')
    expect(wrapper.text()).toContain('医护账号') // T315：设计稿侧栏第 12 项，位于技师管理与权限控制之间
    expect(wrapper.text()).toContain('异常报告') // T372：设计稿侧栏第 4 项，位于患者管理与团队管理之间
    expect(items[3].text()).toContain('异常报告') // 位置也钉住：稿面侧栏顺序 = 数据概览/实时监控/患者管理/异常报告
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
    // T289 G3：设计稿 数据概览.html:87 顶栏标题带 emoji 前缀（meta.icon）
    expect(wrapper.find('.top-nav-title').text()).toBe('📊 数据概览')
    expect(wrapper.text()).toContain('退出')
    wrapper.unmount()
  })

  it('顶栏展示当前登录人头像首字（T289 G2）', async () => {
    const wrapper = await mountLayout('admin')
    // mock admin 姓名「运营管理员」→ 头像取首字
    expect(wrapper.find('.user-avatar').text()).toBe('运')
    expect(wrapper.find('.user-name').text()).toBe('运营管理员')
    wrapper.unmount()
  })
})
