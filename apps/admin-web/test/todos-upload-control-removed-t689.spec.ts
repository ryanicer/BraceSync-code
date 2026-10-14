/**
 * T689 —— 移除「我的待办」确认处理弹窗里的死控件「点击上传附件」并清死代码。
 *
 * 背景：todos/index.vue 的 <div class="upload">点击上传附件（选填，最多 20 个）</div>
 * 无 click handler、无 v-model、无 state（cursor:pointer 但点了无反应）⇒ 不可用死 UI。
 * 口径：删模板控件 + 删同文件 .upload 样式；弹窗其余（处理意见 textarea / 转派下拉 /
 * 确认·跳过·转派三按钮 / submitAction）逐字不动；其他页面在用的上传（如 flow/FlowRuntime
 * 的 .op-upload、api/index.ts 的上传 API）不属本卡，禁止误删。
 *
 * 判据用源面 DOM 断言（同 T379/契约漂移门禁的源码解析风格，稳定不脆）：
 *   1 todos 面模板里不再有该控件（class="upload" / 死文案），样式块不再有 .upload 规则；
 *   2 弹窗其余关键件仍在（守住「不许动其余」的反证）。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const pageSrc = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'pages', 'todos', 'index.vue'),
  'utf8',
)

// 拆出 <style> 段，样式断言只看样式，避免与模板里其它 "upload" 词混淆
const styleBlock = pageSrc.slice(pageSrc.lastIndexOf('<style'))

describe('T689 待办确认处理弹窗移除死控件「点击上传附件」+ 清死代码', () => {
  it('模板里不再有该死控件（class="upload" 与死文案归零）', () => {
    expect(pageSrc, '模板残留 class="upload" 控件').not.toMatch(/class="upload"/)
    expect(pageSrc, '模板残留「点击上传附件（选填，最多 20 个）」死文案').not.toContain('点击上传附件（选填，最多 20 个）')
  })

  it('样式块不再有 .upload 规则（含 cursor:pointer 死样式）', () => {
    expect(styleBlock, '样式残留 .upload 规则').not.toMatch(/\.upload\s*\{/)
  })

  it('弹窗其余功能件不受牵连（反证「不许动其余」）', () => {
    expect(pageSrc, '处理意见 textarea 应仍在').toMatch(/class="af-input af-textarea"/)
    expect(pageSrc, '转派下拉应仍在').toContain('选择转派的医护账号')
    expect(pageSrc, '提交按钮 submitAction 应仍在').toMatch(/@click="submitAction"/)
  })
})
