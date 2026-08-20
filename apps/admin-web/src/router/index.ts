import { createRouter, createWebHistory, type RouterHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { canAccess } from './permissions'

// 12 页路由（对齐架构 §5.4 / PRD §7D，meta.title 用于顶栏与菜单）
export const pageRoutes: RouteRecordRaw[] = [
  { path: '/dashboard', name: 'Dashboard', component: () => import('../pages/dashboard/index.vue'), meta: { title: '数据概览', icon: '📊' } },
  { path: '/monitor', name: 'Monitor', component: () => import('../pages/monitor/index.vue'), meta: { title: '实时监控', icon: '🔍' } },
  { path: '/patients', name: 'Patients', component: () => import('../pages/patients/index.vue'), meta: { title: '患者管理', icon: '👤' } },
  { path: '/teams', name: 'Teams', component: () => import('../pages/teams/index.vue'), meta: { title: '团队管理', icon: '👥' } },
  { path: '/devices', name: 'Devices', component: () => import('../pages/devices/index.vue'), meta: { title: '设备管理', icon: '📱' } },
  { path: '/alerts', name: 'Alerts', component: () => import('../pages/alerts/index.vue'), meta: { title: '告警管理', icon: '🚨' } },
  { path: '/communication', name: 'Communication', component: () => import('../pages/communication/index.vue'), meta: { title: '患者沟通', icon: '💬' } },
  { path: '/orthosis-log', name: 'OrthosisLog', component: () => import('../pages/orthosis-log/index.vue'), meta: { title: '矫形日志', icon: '📝' } },
  { path: '/install-records', name: 'InstallRecords', component: () => import('../pages/install-records/index.vue'), meta: { title: '安装记录', icon: '📋' } },
  { path: '/technicians', name: 'Technicians', component: () => import('../pages/technicians/index.vue'), meta: { title: '技师管理', icon: '🔧' } },
  { path: '/roles', name: 'Roles', component: () => import('../pages/roles/index.vue'), meta: { title: '权限控制', icon: '🔐' } },
  { path: '/settings', name: 'Settings', component: () => import('../pages/settings/index.vue'), meta: { title: '系统配置', icon: '⚙️' } },
]

export const routes: RouteRecordRaw[] = [
  { path: '/login', name: 'Login', component: () => import('../pages/login/index.vue'), meta: { title: '登录' } },
  { path: '/403', name: 'Forbidden', component: () => import('../pages/forbidden/index.vue'), meta: { title: '无权限' } },
  {
    path: '/',
    component: () => import('../layout/MainLayout.vue'),
    children: [
      { path: '', redirect: '/dashboard' },
      ...pageRoutes,
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
]

// 权限路由守卫：未登录跳登录；已登录但无页面权限跳 403（PRD §7D.11 权限矩阵）
export function registerPermissionGuard(router: ReturnType<typeof createRouter>): void {
  router.beforeEach((to) => {
    if (to.path === '/login' || to.path === '/403') return true
    const auth = useAuthStore()
    if (!auth.isLoggedIn) {
      return { path: '/login', query: { redirect: to.fullPath } }
    }
    if (!canAccess(auth.role ?? '', to.path)) {
      return { path: '/403' }
    }
    return true
  })
}

export function createAppRouter(history?: RouterHistory) {
  const router = createRouter({
    history: history ?? createWebHistory(),
    routes,
  })
  registerPermissionGuard(router)
  return router
}

export const router = createAppRouter()
