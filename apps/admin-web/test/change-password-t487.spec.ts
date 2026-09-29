// T487 ⑥ 后台自助改密（admin-web 侧）
//
// 三条关注点：
//  1 前端强度判定与 Go 侧 validNewAdminPassword 同形（量纲按字节、字母取 L、数字取 Nd）。
//    不同形的后果不是「报错难看」而是「前端放行、后端 10400」，而 10400 在这条端点上
//    同时是旧密码错／弱密码／新旧同值三种原因，用户看不出自己哪儿不对。
//    ⇒ 长度边界从 Go 源文件里读出来对拍，后端改数值而前端没跟上时这里先判红。
//  2 弹窗的三格与提交链：不合格不发请求（省一次必然失败的后端往返），合格才 POST。
//  3 错误文案：10400 走端点自撰句，其余仍走 T464 的按码查表（不许把后端英文原文透给用户）。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory, type RouteRecordRaw } from 'vue-router'
import { defineComponent } from 'vue'
import ElementPlus, { ElMessage } from 'element-plus'
import { attachErrorMeta, markUserCopy } from '@bracesync/shared-utils'
import MainLayout from '../src/layout/MainLayout.vue'
import { pageRoutes, registerPermissionGuard } from '../src/router'
import { useAuthStore } from '../src/stores/auth'
import { changeOwnPasswordApi } from '../src/api'
import {
  ADMIN_PWD_MAX_BYTES, ADMIN_PWD_MIN_BYTES,
  changePasswordErrorCopy, isWeakAdminPassword, passwordByteLen, pwdFormIssue,
} from '../src/utils/password'

vi.mock('../src/api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('../src/api')>()
  return { ...mod, changeOwnPasswordApi: vi.fn() }
})

const appRoot = join(dirname(fileURLToPath(import.meta.url)), '..') // apps/admin-web
const repoRoot = join(appRoot, '..', '..')
const goHandler = readFileSync(
  join(repoRoot, 'services/user-service/internal/handler/change_password_t487.go'), 'utf8')

const Stub = defineComponent({ render: () => null })

async function mountLayout() {
  localStorage.clear()
  setActivePinia(createPinia())
  useAuthStore().login('admin')
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
  await router.push('/dashboard')
  await router.isReady()
  const wrapper = mount(MainLayout, { global: { plugins: [router, ElementPlus] } })
  await flushPromises()
  return wrapper
}

/**
 * VTU 会把 Teleport 内容收进 wrapper 自己的容器（stubTeleport 默认开），
 * 所以这里全部经 wrapper 查，不能 document.querySelector —— 实测 body 是空的。
 */
type Layout = Awaited<ReturnType<typeof mountLayout>>

async function openDialog(wrapper: Layout) {
  const btn = wrapper.findAll('.top-nav-right button').find((b) => b.text().includes('修改密码'))
  expect(btn, '顶栏个人操作区没有「修改密码」入口').toBeTruthy()
  await btn!.trigger('click')
  await flushPromises()
  expect(wrapper.find('.el-dialog').exists(), '点入口后弹窗没渲染出来').toBe(true)
}

async function fillAndSubmit(
  wrapper: Layout,
  oldPwd: string,
  newPwd: string,
  confirmPwd = newPwd,
) {
  const inputs = wrapper.findAll('.el-dialog input')
  expect(inputs).toHaveLength(3)
  await inputs[0].setValue(oldPwd)
  await inputs[1].setValue(newPwd)
  await inputs[2].setValue(confirmPwd)
  const submit = wrapper.findAll('.el-dialog__footer button').find((b) => b.text().includes('确认修改'))
  expect(submit, '弹窗底部没有「确认修改」按钮').toBeTruthy()
  await submit!.trigger('click')
  await flushPromises()
}

describe('T487 改密：强度判定与 Go 侧同形', () => {
  it('长度边界从后端源文件读出来对拍（后端改了数值而前端没跟上，这里先红）', () => {
    const min = Number(goHandler.match(/adminPasswordMinBytes\s*=\s*(\d+)/)?.[1])
    const max = Number(goHandler.match(/adminPasswordMaxBytes\s*=\s*(\d+)/)?.[1])
    expect(min, 'Go 侧读不到 adminPasswordMinBytes（常量改名了？同步这条对拍）').toBe(ADMIN_PWD_MIN_BYTES)
    expect(max, 'Go 侧读不到 adminPasswordMaxBytes').toBe(ADMIN_PWD_MAX_BYTES)
    expect(ADMIN_PWD_MIN_BYTES).toBe(8)
    expect(ADMIN_PWD_MAX_BYTES).toBe(64)
  })

  it('按字节量长度，不是按字符数（汉字一格 3 字节）', () => {
    expect(passwordByteLen('abcdefgh')).toBe(8)
    expect(passwordByteLen('矫智通运营平台口令')).toBeGreaterThan(8)
    // 5 个汉字＝15 字节：按字符数只有 5，会被误判成「不足 8 位」
    expect(passwordByteLen('矫智通运营')).toBe(15)
  })

  it('七格判定：短／边界／超限／纯字母／纯数字／汉字＋数字／全角数字', () => {
    expect(isWeakAdminPassword('abcdefg')).toBe(true) // 7 字节，差一格
    expect(isWeakAdminPassword('abcdefg1')).toBe(false) // 8 字节，字母＋数字
    expect(isWeakAdminPassword('a'.repeat(64) + '1')).toBe(true) // 65 字节，越过 bcrypt 余量
    expect(isWeakAdminPassword('abcdefghijkl')).toBe(true) // 没数字
    expect(isWeakAdminPassword('12345678')).toBe(true) // 没字母
    expect(isWeakAdminPassword('矫智通运营平台1')).toBe(false) // Go 的 unicode.IsLetter 认汉字
    // 上标数字属于 Unicode No，Go 的 IsDigit 只认 Nd ⇒ 两侧都当「没有数字位」
    // （全角数字 １２３ 反而在 Nd 内，两侧都算数字位，不能拿来当反例）
    expect(isWeakAdminPassword('abcdefgh²²²')).toBe(true)
  })
})

describe('T487 改密：失败文案按端点收口', () => {
  it('10400 出「可操作」的端点自撰句，不是码表那句通用「提交的信息有误」', () => {
    const err = attachErrorMeta(new Error('old password mismatch'), { code: 10400, httpStatus: 400 })
    const copy = changePasswordErrorCopy(err)
    expect(copy).toContain('当前密码不正确')
    expect(copy).toContain('8-64 位')
    expect(copy).not.toContain('old password mismatch') // 后端英文原文绝不给用户
  })

  it('其余码仍走 T464 按码查表；自撰 userCopy 优先级最高', () => {
    const internal = attachErrorMeta(new Error('query admin failed'), { code: 90001, httpStatus: 500 })
    expect(changePasswordErrorCopy(internal)).toBe('服务暂时异常，请稍后重试')
    const marked = markUserCopy(new Error('x'), '登录已过期，请重新登录')
    expect(changePasswordErrorCopy(marked)).toBe('登录已过期，请重新登录')
    // 无码无状态的进程内中文（mock 层自撰）：主语用这句，仍补码位
    expect(changePasswordErrorCopy(new Error('本地未接入改密'))).toContain('本地未接入改密')
  })
})

describe('T487 改密：三格判定只此一份（pwdFormIssue）', () => {
  it('逐格点名：缺哪一格就说哪一格，全合格返回 null', () => {
    expect(pwdFormIssue({ oldPassword: '', newPassword: 'Charlie2026new', confirmPassword: 'Charlie2026new' }))
      .toBe('请输入当前密码')
    expect(pwdFormIssue({ oldPassword: 'Bravo2026old', newPassword: 'short1', confirmPassword: 'short1' }))
      .toBe('新密码需8-64 位，且同时含字母和数字')
    expect(pwdFormIssue({ oldPassword: 'Bravo2026old', newPassword: 'Bravo2026old', confirmPassword: 'Bravo2026old' }))
      .toBe('新密码不能与当前密码相同')
    expect(pwdFormIssue({ oldPassword: 'Bravo2026old', newPassword: 'Charlie2026new', confirmPassword: 'Charlie2026xx' }))
      .toBe('两次输入的新密码不一致')
    expect(pwdFormIssue({ oldPassword: 'Bravo2026old', newPassword: 'Charlie2026new', confirmPassword: '' }))
      .toBe('请再次输入新密码')
    expect(pwdFormIssue({ oldPassword: 'Bravo2026old', newPassword: 'Charlie2026new', confirmPassword: 'Charlie2026new' }))
      .toBeNull()
  })
})

describe('T487 改密：顶栏弹窗与提交链', () => {
  beforeEach(() => {
    vi.mocked(changeOwnPasswordApi).mockReset().mockResolvedValue(undefined)
    // 只 spy 不换实现（仓内既有写法，见 test/auth-expired-toast.spec.ts）：弹条本身在 jsdom 里无害
    vi.spyOn(ElMessage, 'success')
    vi.spyOn(ElMessage, 'warning')
    vi.spyOn(ElMessage, 'error')
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('顶栏有「修改密码」入口，点开是旧/新/确认三格', async () => {
    const wrapper = await mountLayout()
    await openDialog(wrapper)
    const dialog = wrapper.find('.el-dialog')
    const text = dialog.text()
    expect(text).toContain('当前密码')
    expect(text).toContain('新密码')
    expect(text).toContain('确认新密码')
    // 强度口径写在「新密码」格子的 placeholder 里，不是正文（正文那句是「忘记密码仍可由管理员重置」）
    const inputs = wrapper.findAll('.el-dialog input')
    expect(inputs).toHaveLength(3)
    expect(inputs[1].attributes('placeholder')).toContain('8-64 位')
    expect(text).toContain('下次登录请使用新密码')
    wrapper.unmount()
  })

  it('合格的两格一致才发请求，且只带旧密与新密两个参数', async () => {
    const wrapper = await mountLayout()
    await openDialog(wrapper)
    await fillAndSubmit(wrapper, 'Bravo2026old', 'Charlie2026new')
    expect(changeOwnPasswordApi).toHaveBeenCalledWith('Bravo2026old', 'Charlie2026new')
    expect(ElMessage.success).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('反证：弱新密码／两次不一致／新旧同值都在前端截住，一次请求都不发', async () => {
    // 这道反证锁的是 submitPwdChange 里的 pwdFormIssue 自证，不是 el-form 的 rules：
    // 实测本环境（jsdom + EP 2.14.4）await formRef.validate() 对必填/min/自定义 validator 一律放行，
    // 浏览器里靠 rules 出红字，测试里能验的只有这道自证 ⇒ 两样都留着。
    const cases: Array<[string, string, string, string]> = [
      ['Bravo2026old', 'short1', 'short1', '新密码需8-64 位'], // 不足 8 字节
      ['Bravo2026old', 'Charlie2026new', 'Charlie2026xx', '两次输入的新密码不一致'],
      ['Bravo2026old', 'Bravo2026old', 'Bravo2026old', '新密码不能与当前密码相同'],
    ]
    for (const [oldPwd, newPwd, confirmPwd, expectedIssue] of cases) {
      vi.mocked(changeOwnPasswordApi).mockClear()
      vi.mocked(ElMessage.warning).mockClear()
      const wrapper = await mountLayout()
      await openDialog(wrapper)
      await fillAndSubmit(wrapper, oldPwd, newPwd, confirmPwd)
      expect(changeOwnPasswordApi, `这组本该前端拦下：${oldPwd}/${newPwd}`).not.toHaveBeenCalled()
      const warned = String(vi.mocked(ElMessage.warning).mock.calls[0]?.[0] ?? '')
      expect(warned, `拦住却没说清是哪一格：${oldPwd}/${newPwd}`).toContain(expectedIssue)
      wrapper.unmount()
    }
  })

  it('后端回 10400：把端点自撰句给用户，而不是「操作失败」这类无信息句', async () => {
    vi.mocked(changeOwnPasswordApi).mockRejectedValue(
      attachErrorMeta(new Error('old password mismatch'), { code: 10400, httpStatus: 400 }))
    const wrapper = await mountLayout()
    await openDialog(wrapper)
    await fillAndSubmit(wrapper, 'Bravo2026old', 'Charlie2026new')
    expect(ElMessage.error).toHaveBeenCalledTimes(1)
    const shown = String(vi.mocked(ElMessage.error).mock.calls[0][0])
    expect(shown).toContain('当前密码不正确')
    expect(shown).not.toContain('old password mismatch')
    expect(ElMessage.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
