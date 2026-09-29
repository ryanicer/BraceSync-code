import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

/**
 * T486 改密页（src/pages/password/index.vue）结构性判据
 *
 * 环境是 environment: 'node'（本 app 全部 spec 一致），不挂组件，
 * 因此按仓内既有做法（bind-scan / alert-advice / b512-resubscribe）对页面源码断言：
 * 卡面那四条要求——三输入、前端校验、错误提示、成功后 toast 跳登录页——都在源码上有唯一落点，
 * 每条都配一条「去掉即判红」的写法核对。
 */
const PAGE = fileURLToPath(new URL('../src/pages/password/index.vue', import.meta.url))
const HOME = fileURLToPath(new URL('../src/pages/home/index.vue', import.meta.url))
const PAGES_JSON = fileURLToPath(new URL('../src/pages.json', import.meta.url))
const src = readFileSync(PAGE, 'utf8')

/** 日志调用里不许出现的取值表达式（口令值 / token 值） */
const LEAK = /\b(oldPassword|newPassword|confirmPassword)\.value\b|\btoken\.value\b|getToken\(\)/

/** 截取 logger.xxx(...) 整段调用（含嵌套括号），用于脱敏核对 */
function loggerCalls(text: string): string[] {
  const out: string[] = []
  const re = /logger\.\w+\(/g
  let m: RegExpExecArray | null
  while ((m = re.exec(text))) {
    let depth = 1
    let i = re.lastIndex
    while (i < text.length && depth > 0) {
      if (text[i] === '(') depth++
      else if (text[i] === ')') depth--
      i++
    }
    out.push(text.slice(m.index, i))
  }
  return out
}

describe('T486 改密页：入口与注册', () => {
  it('pages.json 注册了该页且标题为「修改密码」', () => {
    // 仓内 pages.json 带 BOM（uni-app 侧接受），JSON.parse 不认，先按码位剥掉
    const bom = String.fromCharCode(0xfeff)
    const pages = JSON.parse(readFileSync(PAGES_JSON, 'utf8').replace(bom, '')).pages as {
      path: string
      style: { navigationBarTitleText: string }
    }[]
    const hit = pages.find((p) => p.path === 'pages/password/index')
    expect(hit).toBeDefined()
    expect(hit?.style.navigationBarTitleText).toBe('修改密码')
  })

  it('登录后首页有入口（navigateTo 到新页）', () => {
    const home = readFileSync(HOME, 'utf8')
    expect(home).toContain('function goPassword()')
    expect(home).toContain("uni.navigateTo({ url: '/pages/password/index' })")
    expect(home).toMatch(/@click="goPassword"/)
  })
})

describe('T486 改密页：三输入 + 前端校验', () => {
  it('旧/新/确认三个输入框都在，且默认掩码', () => {
    expect(src).toContain('v-model="oldPassword"')
    expect(src).toContain('v-model="newPassword"')
    expect(src).toContain('v-model="confirmPassword"')
    expect((src.match(/:password="!show/g) ?? []).length).toBe(3)
  })

  it('新密码强度用共享规则模块，不在页面里另写一份', () => {
    expect(src).toContain("import { TECH_PASSWORD_RULE_HINT, techPasswordError } from '../../utils/password'")
    expect(src).toContain('techPasswordError(newPassword.value)')
    // 页面自己再写一遍 6-16 就会和后端分叉
    expect(src).not.toMatch(/\.length\s*>=\s*6/)
    expect(src).not.toMatch(/\{6,16\}/)
  })

  it('校验链：旧口令非空 → 新口令强度 → 新旧不同 → 两次一致', () => {
    const atEmpty = src.indexOf("toast('请输入原密码')")
    const atStrength = src.indexOf('techPasswordError(newPassword.value)')
    const atSame = src.indexOf("'新密码不能与原密码相同'")
    const atMatch = src.indexOf("'两次输入的新密码不一致'")
    expect(atEmpty).toBeGreaterThan(-1)
    expect(atStrength).toBeGreaterThan(atEmpty)
    expect(atSame).toBeGreaterThan(atStrength)
    expect(atMatch).toBeGreaterThan(atSame)
  })
})

describe('T486 改密页：成功与失败处置', () => {
  it('成功：先清凭据再提示，最后 reLaunch 到登录页（不是 navigateBack）', () => {
    const atLogout = src.indexOf('authStore.logout()')
    const atToast = src.indexOf("uni.showToast({ title: '密码修改成功，请重新登录', icon: 'success' })")
    const atRelaunch = src.indexOf('uni.reLaunch({ url: LOGIN_PAGE })')
    expect(atLogout).toBeGreaterThan(-1)
    expect(atToast).toBeGreaterThan(atLogout)
    expect(atRelaunch).toBeGreaterThan(atToast)
    expect(src).not.toContain('uni.navigateBack')
  })

  it('失败提示走展示层唯一出口（userErrorCopy scope=tech）', () => {
    expect(src).toContain("userErrorCopy(e, { scope: 'tech', fallback: '修改失败' })")
  })

  it('未登录直接回登录页（页面自身不假设已登录）', () => {
    expect(src).toContain('if (!authStore.isLoggedIn)')
  })

  it('口令与 token 不进任何日志调用（本仓脱敏口径）', () => {
    // 正对照：判据本身咬得住泄漏样本
    expect("logger.info('[T486]', oldPassword.value, getToken())").toMatch(LEAK)
    const calls = loggerCalls(src)
    expect(calls.length).toBeGreaterThan(0)
    for (const call of calls) {
      expect(call, `日志调用里出现凭据值：${call}`).not.toMatch(LEAK)
    }
  })
})
