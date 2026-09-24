// 权限矩阵单测（PRD §7D.11 预置角色权限矩阵）
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import {
  ROLE_PAGE_MATRIX, ROLE_HOME_PAGE, canAccess, landingPathFor,
  roleName, PRESET_ROLES, roleKeyFromRoleId,
  PAGE_MODULES, modulesForRole, moduleKeyOfPath, pathOfModule,
} from '../src/router/permissions'
import { pageRoutes } from '../src/router'

describe('ROLE_PAGE_MATRIX（PRD §7D.11）', () => {
  it('运营管理员可访问全部 15 页', () => {
    expect(ROLE_PAGE_MATRIX.admin).toHaveLength(15) // 断言更新：14→15，依据 T315 新增医护账号页
    for (const route of pageRoutes) {
      expect(canAccess('admin', route.path)).toBe(true)
    }
  })

  it('医生仅可访问 数据概览/实时监控/告警管理/矫形日志/复查报告/复查模板管理 6 页', () => {
    expect(ROLE_PAGE_MATRIX.doctor).toEqual(['/dashboard', '/monitor', '/alerts', '/orthosis-log', '/review-records', '/review-templates']) // 断言更新：5→6，依据 T135 医生可下载空白模板
    expect(canAccess('doctor', '/patients')).toBe(false)
    expect(canAccess('doctor', '/settings')).toBe(false)
    expect(canAccess('doctor', '/doctor-accounts')).toBe(false) // T315 账号管理页仅运营管理员
  })

  it('客服仅可访问 患者沟通 1 页', () => {
    expect(ROLE_PAGE_MATRIX.cs).toEqual(['/communication'])
    expect(canAccess('cs', '/dashboard')).toBe(false)
  })

  it('未知角色一律拒绝', () => {
    expect(canAccess('unknown', '/dashboard')).toBe(false)
    expect(canAccess('', '/dashboard')).toBe(false)
  })

  it('预置角色元信息完整', () => {
    expect(PRESET_ROLES.map((r) => r.key)).toEqual(['admin', 'doctor', 'cs'])
    expect(roleName('admin')).toBe('运营管理员')
    expect(roleName('doctor')).toBe('医护') // T343：显示名随 Boss 2026-09-22 14:21 裁定「医生 → 医护」，key 仍是 doctor（上一行钉住）
    expect(roleName('cs')).toBe('客服')
  })
})

describe('PAGE_MODULES 模块词表（T345 权限矩阵全量对齐）', () => {
  it('与 pageRoutes 一一对应：同序、同数、无重复', () => {
    expect(PAGE_MODULES.map((m) => m.path)).toEqual(pageRoutes.map((r) => r.path))
    expect(new Set(PAGE_MODULES.map((m) => m.key)).size).toBe(PAGE_MODULES.length)
  })

  it('每个预置角色的可见页面都能换算出模块键（不漏页）', () => {
    for (const role of ['admin', 'doctor', 'cs'] as const) {
      const paths = ROLE_PAGE_MATRIX[role]
      expect(modulesForRole(role)).toHaveLength(paths.length)
    }
  })

  it('key 与 path 双向可逆', () => {
    for (const m of PAGE_MODULES) {
      expect(moduleKeyOfPath(m.path)).toBe(m.key)
      expect(pathOfModule(m.key)).toBe(m.path)
    }
    expect(moduleKeyOfPath('/nope')).toBeUndefined()
    expect(pathOfModule('nope')).toBeUndefined()
  })

  it('运营管理员 = 15 页全集（基数变化必须显式改这里）', () => {
    expect(PAGE_MODULES.map((m) => m.key)).toEqual([
      'dashboard', 'realtime', 'patients', 'teams', 'devices', 'alerts',
      'comm', 'orthosis', 'install', 'review', 'review_tpl', 'tech',
      'doctor_acct', 'perm', 'config',
    ])
    expect(modulesForRole('admin')).toEqual(PAGE_MODULES.map((m) => m.key))
  })

  it('页面中文标签在路由里都有（矩阵与新建角色弹窗按它渲染）', () => {
    for (const r of pageRoutes) {
      expect(String(r.meta?.title ?? ''), r.path).toBeTruthy()
    }
  })
})

describe('模块词表与落库/后端模板同源（T345 跨端漂移门禁）', () => {
  // 缺陷成因就是「前端一套词表、库里一套词表、后端模板第三套」，谁都不知道自己对不上。
  // 这里直接读那三处源文本比对——任何一侧改词表而没同步，本用例变红。
  const appRoot = join(dirname(fileURLToPath(import.meta.url)), '..') // apps/admin-web
  const repoRoot = join(appRoot, '..', '..')
  const read = (rel: string) => readFileSync(join(repoRoot, rel), 'utf8')

  const parseArray = (raw: string, where: string): string[] => {
    let arr: string[]
    try {
      arr = JSON.parse(raw)
    } catch {
      throw new Error(`${where}：模块数组解析失败，请同步本用例的正则（改格式可以，别悄悄改）`)
    }
    return arr
  }

  it('seed.sql 的 ROLE_ADMIN.modules = PAGE_MODULES', () => {
    const m = read('scripts/db/seed/seed.sql').match(
      /'ROLE_ADMIN',[\s\S]*?"modules":(\[[^\]]*\])/,
    )
    expect(m, 'seed.sql 里没定位到 ROLE_ADMIN 的 modules').not.toBeNull()
    expect(parseArray(m![1], 'seed.sql')).toEqual(PAGE_MODULES.map((x) => x.key))
  })

  it('迁移 000026 前滚后的 ROLE_ADMIN.modules = PAGE_MODULES', () => {
    const m = read('scripts/db/migrations/000026_t345_admin_perm_modules_align_joe.up.sql').match(
      /'\{modules\}',\s*'(\[[^\]]*\])'::jsonb/,
    )
    expect(m, '000026 up 里没定位到 jsonb_set 的 modules 数组').not.toBeNull()
    expect(parseArray(m![1], '000026 up')).toEqual(PAGE_MODULES.map((x) => x.key))
  })

  // ↓ T368：医护侧栏/路由越权（Ella T345 验收唯一不通过项 D-A）。缺陷成因正是
  //   「前端准入矩阵 6 页、库里 ROLE_DOCTOR.modules 只 4 项」两套来源同屏，而上面的三条用例
  //   只镜像了 ROLE_ADMIN —— admin 恰好两边相等，所以这条分叉没有任何门禁能看见。
  //   Boss 2026-09-24 09:3x 裁 (a)「补库」⇒ 这里把 doctor 也钉成同源。
  it('seed.sql 的 ROLE_DOCTOR.modules = 前端 doctor 可见页换算出的模块键', () => {
    const m = read('scripts/db/seed/seed.sql').match(
      /'ROLE_DOCTOR',[\s\S]*?"modules":(\[[^\]]*\])/,
    )
    expect(m, 'seed.sql 里没定位到 ROLE_DOCTOR 的 modules').not.toBeNull()
    expect(parseArray(m![1], 'seed.sql')).toEqual(modulesForRole('doctor'))
    // 逐值再钉一遍基数：换算函数若哪天吞了某页，上一行会两边一起变短而看不出来
    expect(modulesForRole('doctor'), 'T135 起医护 6 页（T368 补库后的基数）').toEqual([
      'dashboard', 'realtime', 'alerts', 'orthosis', 'review', 'review_tpl',
    ])
  })

  it('迁移 000028 前滚后的 ROLE_DOCTOR.modules = 前端 doctor 可见页（up 与 down 对称）', () => {
    const parse = (file: string, where: string): string[] => {
      const m = read(file).match(/'\{modules\}',\s*'(\[[^\]]*\])'::jsonb/)
      expect(m, `${where} 里没定位到 jsonb_set 的 modules 数组（改格式可以，别悄悄改）`).not.toBeNull()
      return parseArray(m![1], where)
    }
    expect(
      parse('scripts/db/migrations/000028_t368_doctor_review_modules_iris.up.sql', '000028 up'),
      '000028 前滚后的库必须与前端矩阵逐元素相等（含顺序）',
    ).toEqual(modulesForRole('doctor'))
    // down 回到补库前的 4 项 —— 不是「和 up 一样」就完事，回滚目标本身也要钉住
    expect(
      parse('scripts/db/migrations/000028_t368_doctor_review_modules_iris.down.sql', '000028 down'),
      '000028 down 应退回 Boss 裁定前的 4 项',
    ).toEqual(['dashboard', 'realtime', 'alerts', 'orthosis'])
  })

  it('客服模块键未被 T368 顺手改动（仍只 comm 一项）', () => {
    const m = read('scripts/db/seed/seed.sql').match(
      /'ROLE_CS',[\s\S]*?"modules":(\[[^\]]*\])/,
    )
    expect(m, 'seed.sql 里没定位到 ROLE_CS 的 modules').not.toBeNull()
    expect(parseArray(m![1], 'seed.sql ROLE_CS')).toEqual(['comm'])
  })

  it('后端 admin 角色模板 = PAGE_MODULES（前端弹窗按它预勾）', () => {
    const m = read('services/user-service/internal/handler/roles_t252.go').match(
      /Key: "admin"[\s\S]*?Modules: \[\]string\{([\s\S]*?)\}/,
    )
    expect(m, 'roles_t252.go 里没定位到 admin 模板的 Modules').not.toBeNull()
    const keys = [...m![1].matchAll(/"([^"]+)"/g)].map((x) => x[1])
    expect(keys).toEqual(PAGE_MODULES.map((x) => x.key))
  })
})

describe('roleKeyFromRoleId（T046 真实登录 roleId 映射）', () => {  it('后端 roleId → 前端 RoleKey', () => {
    expect(roleKeyFromRoleId('ROLE_ADMIN')).toBe('admin')
    expect(roleKeyFromRoleId('ROLE_DOCTOR')).toBe('doctor')
    expect(roleKeyFromRoleId('ROLE_CS')).toBe('cs')
  })

  it('未知 roleId / 空串 → null（fail-closed）', () => {
    expect(roleKeyFromRoleId('ROLE_X')).toBeNull()
    expect(roleKeyFromRoleId('')).toBeNull()
    expect(roleKeyFromRoleId('admin')).toBeNull()
  })
})

describe('ROLE_HOME_PAGE 落地页（T269 D3 客服 403 死循环）', () => {
  it('每个角色的落地页必须是该角色有权访问的页面', () => {
    for (const [role, home] of Object.entries(ROLE_HOME_PAGE)) {
      expect(canAccess(role, home), `${role} 落地页 ${home} 越权`).toBe(true)
    }
  })

  it('客服落地到患者沟通，不是 dashboard', () => {
    expect(landingPathFor('cs')).toBe('/communication')
    expect(landingPathFor('admin')).toBe('/dashboard')
    expect(landingPathFor('doctor')).toBe('/dashboard')
  })

  it('未知角色 / role=null → /403（守卫 fail-closed 口径不变）', () => {
    expect(landingPathFor('ninja')).toBe('/403')
    expect(landingPathFor(null)).toBe('/403')
    expect(landingPathFor(undefined)).toBe('/403')
  })
})
