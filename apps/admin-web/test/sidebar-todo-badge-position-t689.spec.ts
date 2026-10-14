/**
 * T689 —— 侧栏「我的待办」红角标位置修正（Boss 截图为准）。
 *
 * 缺陷：MainLayout 把 badge `<span class="menu-badge">` 写在 `<template #title>` **之外**，
 * 作为 el-menu-item 默认插槽的兄弟节点 ⇒ 早于 #title 渲染 ⇒ 角标落到「图标 ↔ 文字」之间
 * （截图所见），而非设计稿要求的行尾（`<a>` flex 行、badge 末位 margin-left:auto 推右）。
 *
 * 修法（甲）：文字 + 角标并入 #title 里的一层 flex（.menu-title，flex:1 + space-between），
 * 角标随剩余宽度推到行尾。本用例做**模板结构断言**（同 T379/契约门禁的源码解析风格，稳定不脆）：
 *   1 角标节点必须落在 #title 插槽内（在 .menu-title 之下），不再是插槽外的兄弟；
 *   2 .menu-title 用 flex + space-between 把角标推右；
 *   3 计数口径不动：值=todoBadge（fetchTodoBadgeCount→mineCount）、>0 才显示、拉取失败静默。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const src = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'layout', 'MainLayout.vue'),
  'utf8',
)

describe('T689 侧栏我的待办角标位置修正', () => {
  it('角标节点在 #title 插槽内（不再是插槽外兄弟节点）', () => {
    // 每一处 #title 插槽块内都应含 menu-badge；插槽外不应再出现裸 menu-badge
    const titleBlocks = [...src.matchAll(/<template #title>([\s\S]*?)<\/template>/g)].map((m) => m[1])
    expect(titleBlocks.length, '应有 #title 插槽块（桌面 + 移动 drawer 两处）').toBeGreaterThanOrEqual(2)
    const withBadge = titleBlocks.filter((b) => /class="menu-badge"/.test(b))
    expect(withBadge.length, '每处 #title 内都应承载 menu-badge').toBe(titleBlocks.length)
    // 反证：menu-badge 之前必须已在 .menu-title 容器内（即被 #title 包住）
    for (const b of titleBlocks) {
      if (!/class="menu-badge"/.test(b)) continue
      expect(b, '角标应包在 .menu-title flex 行内').toMatch(/class="menu-title"[\s\S]*class="menu-badge"/)
    }
  })

  it('.menu-title 用 flex + space-between 把角标推到行尾', () => {
    const style = src.slice(src.lastIndexOf('<style'))
    expect(style, '缺 .menu-title 规则').toMatch(/\.menu-title\s*\{[^}]*display:\s*flex/)
    expect(style, '.menu-title 应 space-between 推右角标').toMatch(/\.menu-title\s*\{[^}]*justify-content:\s*space-between/)
    expect(style, '.menu-title 应 flex:1 占满图标右侧剩余宽').toMatch(/\.menu-title\s*\{[^}]*flex:\s*1/)
  })

  it('计数口径不动：值=todoBadge、>0 才显示、拉取失败静默', () => {
    expect(src, '角标仍应绑 todoBadge > 0 条件').toMatch(/v-if="item\.path === '\/todos' && todoBadge > 0"/)
    expect(src, '角标内容应渲染 todoBadge').toMatch(/class="menu-badge">\{\{\s*todoBadge\s*\}\}/)
    expect(src, '应经 fetchTodoBadgeCount 取 mineCount').toMatch(/fetchTodoBadgeCount\(\)/)
    expect(src, '拉取失败须静默不阻断导航').toMatch(/\.catch\(\(\)\s*=>\s*\{[^}]*\}\)/)
  })
})
