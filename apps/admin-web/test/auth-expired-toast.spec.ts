// T384 第二件：admin-web 此前「无 toast」——AUTH_EXPIRED_MESSAGE 只作 reject 原因，
// 用户看到的是一条转瞬即逝的错误（甚至没有，因为页面紧接着整页跳走），
// 而技师端 T326 forceRelogin 是同文案 + toast。本文件锁两端提示文案同源，且登录页真的弹一次。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus, { ElMessage } from 'element-plus'
import { createRouter, createMemoryHistory } from 'vue-router'
import LoginPage from '../src/pages/login/index.vue'
import { AUTH_EXPIRED_MESSAGE, markAuthExpiredNotice } from '../src/utils/sessionExpiry'
import { AUTH_EXPIRED_MESSAGE as TECH_AUTH_EXPIRED_MESSAGE } from '../../tech-miniapp/src/utils/authError'

async function mountLogin() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: LoginPage },
      { path: '/:pathMatch(.*)*', component: LoginPage },
    ],
  })
  await router.push('/login')
  await router.isReady()
  const wrapper = mount(LoginPage, { global: { plugins: [router, createPinia(), ElementPlus] } })
  await flushPromises()
  return wrapper
}

describe('T384 登录页落地补提示', () => {
  function spyWarning() {
    return vi.spyOn(ElMessage, 'warning')
  }
  let warningSpy: ReturnType<typeof spyWarning>

  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    sessionStorage.clear()
    warningSpy = vi.spyOn(ElMessage, 'warning')
  })
  afterEach(() => {
    warningSpy.mockRestore()
  })

  it('带着失效留言进登录页：弹一次同文案提示', async () => {
    markAuthExpiredNotice()
    await mountLogin()

    expect(warningSpy).toHaveBeenCalledTimes(1)
    expect(warningSpy).toHaveBeenCalledWith(AUTH_EXPIRED_MESSAGE)
  })

  it('反证：没有留言（主动登出 / 直接访问登录页）不弹「登录已过期」', async () => {
    await mountLogin()
    expect(warningSpy).not.toHaveBeenCalled()
  })

  it('留言取用即作废：同一轮里再进一次登录页不重复弹', async () => {
    markAuthExpiredNotice()
    const first = await mountLogin()
    first.unmount()
    warningSpy.mockClear()

    await mountLogin()
    expect(warningSpy).not.toHaveBeenCalled()
  })

  it('两端文案同源：admin-web 与技师端 T326 用的是同一串字面量', () => {
    expect(TECH_AUTH_EXPIRED_MESSAGE).toBe(AUTH_EXPIRED_MESSAGE)
  })
})
