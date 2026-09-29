// T487 ⑥ 登录页双凭证文案（源码级锁定）
//
// 为什么读源码而不挂 DOM：本卡改了登录页的真实模式文案（「请输入用户名」→「请输入用户名或手机号」），
// 而 e2e-real 打的是 staging 上**已部署的那一个包**，换构建前仍是旧文案 —— 那条用例只能两种都收
// （e2e-real/tests/01-login.spec.ts 的正则），逐字口径必须由这条读源码的用例来钉，
// 否则「双凭证提示」这件事在门禁里等于没锁。写法沿用 test/base-mount-contract.spec.ts 的先例。
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const appRoot = join(dirname(fileURLToPath(import.meta.url)), '..') // apps/admin-web
const loginSrc = readFileSync(join(appRoot, 'src/pages/login/index.vue'), 'utf8')
const apiSrc = readFileSync(join(appRoot, 'src/api/index.ts'), 'utf8')

describe('T487 登录页双凭证文案', () => {
  it('真实模式的账号格标题同时点名的两种凭证', () => {
    expect(loginSrc).toContain('label="用户名或手机号"')
  })

  it('占位语把「手机号得是已录入的那一个」说在前面', () => {
    const placeholder = loginSrc.match(/placeholder="请输入用户名[^"]*"/)?.[0]
    expect(placeholder, '账号输入框的 placeholder 里没有「用户名」开头的这句').toBeTruthy()
    expect(placeholder).toContain('手机号')
    expect(placeholder).toContain('11 位')
  })

  it('必填提示语不许再只写「用户名」', () => {
    expect(loginSrc).toContain(`message: '请输入用户名或手机号'`)
  })

  it('反证：旧文案「请输入用户名」不再作为独立整句残留（rules 里不许有两套口径）', () => {
    expect(loginSrc).not.toContain(`message: '请输入用户名'`)
    expect(loginSrc).not.toContain('label="用户名"')
  })

  it('请求体仍只有 username/password 两个键（双凭证靠同一个字段承载，契约零破坏）', () => {
    const body = apiSrc.match(/export async function adminLogin[\s\S]*?JSON\.stringify\(\{([^}]*)\}\)/)
    expect(body, 'adminLogin 里找不到 JSON.stringify 的请求体').toBeTruthy()
    expect(body![1].split(',').map((k) => k.trim()).sort()).toEqual(['password', 'username'])
  })
})
