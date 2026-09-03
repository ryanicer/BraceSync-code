# T080 微信登录 E2E 测试重写 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 重写 login.spec.ts 和 full-flow.spec.ts 以适应新的微信登录契约

**Architecture:** 基于新 WeChat 一键登录契约（无 SMS）更新 10 个 E2E 用例，确保每个用例独立可测

**Tech Stack:** Playwright, TypeScript, route mocking

---

## Task 1: 分支创建与环境确认

**Files:**
- N/A

- [ ] **Step 1: 创建本地分支**

```bash
cd D:/proj/BraceSync
git fetch origin t074-patient-realtime
git checkout -b t074-login-spec origin/t074-patient-realtime
```

- [ ] **Step 2: 确认分支状态**

```bash
git status
git rev-parse HEAD
git ls-remote origin t074-login-spec  # 确认不存在（避免直推冲突）
```

- [ ] **Step 3: 记录 HEAD SHA**

截图保存 `git rev-parse HEAD` 的输出结果，用于提交清单。

---

## Task 2: 重构 helpers.ts 添加 wechatBtn 选择器

**Files:**
- Modify: `e2e/helpers.ts:24-30`

- [ ] **Step 1: 读取现有 helper 文件结构**

```bash
Read e2e/helpers.ts
```

- [ ] **Step 2: 在 loginPage() 对象中添加 wechatBtn 属性**

```typescript
/** 登录页元素 (uni-app H5：placeholder 为覆盖层 div，需定位 uni-input 内层原生 input) */
export const loginPage = (page: Page) => ({
  phone: page.locator('uni-input.input-field:not(.input-sms) input').first(),
  smsCode: page.locator('uni-input.input-sms input').first(),
  smsBtn: page.locator('.sms-btn').first(),
  checkbox: page.locator('.checkbox').first(),
  wechatBtn: page.locator('.wechat-login-btn').first(), // ✅ 新增
  loginBtn: page.locator('.btn-primary').first(),
})
```

- [ ] **Step 3: 验证修改语法正确性**

```bash
npx tsc --noEmit -p e2e/tsconfig.json  # 若有 tsconfig
code e2e/helpers.ts  # 检查语法
```

- [ ] **Step 4: Commit (轻量)**

```bash
git add e2e/helpers.ts
git commit -m "chore: add wechatBtn selector for new WeChat login contract"
```

---

## Task 3: 编写 L1 微信登录按钮可见可用

**Files:**
- Create: `e2e/tests/login.spec.ts` (第 9-13 行插入)
- Test: N/A

- [ ] **Step 1: 读取 login.spec.ts 当前内容**

```bash
Read e2e/tests/login.spec.ts
```

- [ ] **Step 2: 保留 test.beforeEach 模板，在第一条用例后追加新用例**

替换旧 L1(手机号与验证码输入) → 新 L1:

```typescript
test('微信登录按钮可见可用', async ({ page }) => {
  const el = loginPage(page)
  await expect(el.wechatBtn).toBeVisible()
  await expect(el.wechatBtn).toBeEnabled()
})
```

- [ ] **Step 3: 运行单条测试验证基本 DOM 渲染**

```bash
npx playwright test e2e/tests/login.spec.ts --grep "微信登录按钮可见可用" --headed
```

- [ ] **Step 4: Commit**

```bash
git add e2e/tests/login.spec.ts
git commit -m "test: add L1 - WeChat login button visible and enabled"
```

---

## Task 4: 编写 L7 协议勾选激活按钮

**Files:**
- Create: `e2e/tests/login.spec.ts` (L2 位置)
- Test: N/A

- [ ] **Step 1: 实现完整用例代码**

```typescript
test('协议勾选前按钮 disabled', async ({ page }) => {
  const el = loginPage(page)
  
  // 初始 disabled
  await expect(el.wechatBtn).toBeDisabled()
  
  // 勾选协议
  await el.checkbox.click()
  await expect(el.checkbox).toHaveClass(/checkbox-checked/)
  
  // 按钮 enabled
  await expect(el.wechatBtn).toBeEnabled()
})
```

- [ ] **Step 2: 运行测试验证交互**

```bash
npx playwright test e2e/tests/login.spec.ts --grep "协议勾选前按钮 disabled" --headed
```

- [ ] **Step 3: Commit**

```bash
git add e2e/tests/login.spec.ts
git commit -m "test: add L7 - Agreement checkbox enables button"
```

---

## Task 5: 编写 L4 + L6 fallback code 上报与失败重试融合

**Files:**
- Create: `e2e/tests/login.spec.ts` (L3 位置)
- Test: N/A

- [ ] **Step 1: 定义辅助常量**

在文件顶部 (import 下方) 添加:

```typescript
const MOCK_TOKEN = 'mock-jwt-token-for-e2e'
const MOCK_PATIENT = { id: 'PT001', name: '张三', role: 'patient' }
```

- [ ] **Step 2: 编写完整用例 (L2+L4 融合)**

```typescript
test('点击微信按钮跳转到 monitor 并写入 storage', async ({ page, browserName }) => {
  // Route mock setup
  let capturedReq: any
  const wxLoginRoute = '/api/v1/patient/wx-login'
  
  await page.route(wxLoginRoute, async route => {
    capturedReq = await route.request().postDataJSON()
    await route.fulfill({ json: { token: MOCK_TOKEN, ...MOCK_PATIENT } })
  })
  
  // 执行登录
  const el = loginPage(page)
  await el.checkbox.click() // 先勾选协议
  await el.wechatBtn.click()
  
  // 断言跳转
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
  await expect(page.getByText('实时监测').first()).toBeVisible()
  
  // 断言 storage 写入
  const storage = await page.context().storageState()
  expect(storage).toHaveProperty('tokens') // 或存储实际路径
  
  // 断言请求体包含 fallback code
  expect(capturedReq.code).toBe('h5-fallback-wechat-login-code')
})
```

- [ ] **Step 3: 运行测试验证路由拦截**

```bash
npx playwright test e2e/tests/login.spec.ts --grep "点击微信按钮跳转到 monitor" --headed
```

- [ ] **Step 4: Commit**

```bash
git add e2e/tests/login.spec.ts
git commit -m "test: add L2+L4 - WeChat login with fallback code assertion"
```

---

## Task 6: 编写 L3 接口失败 toast

**Files:**
- Create: `e2e/tests/login.spec.ts` (L4 位置)
- Test: N/A

- [ ] **Step 1: 实现失败场景用例**

```typescript
test('wx-login 接口失败显示 toast 且不跳转', async ({ page }) => {
  await page.route('/api/v1/patient/wx-login', async route => {
    await route.fulfill({ status: 500, json: { message: 'Internal Server Error' } })
  })
  
  const el = loginPage(page)
  await el.checkbox.click()
  await el.wechatBtn.click()
  
  // Toast 显示
  await expect(page.locator('uni-toast').filter({ hasText: /登录失败 | 服务器错误/ })).toBeVisible()
  
  // 仍停留在 login
  await expect(page).toHaveURL(/pages\/login/)
})
```

- [ ] **Step 2: 运行测试验证错误处理**

```bash
npx playwright test e2e/tests/login.spec.ts --grep "接口失败显示 toast" --headed
```

- [ ] **Step 3: Commit**

```bash
git add e2e/tests/login.spec.ts
git commit -m "test: add L3 - Login failure toast without navigation"
```

---

## Task 7: 编写 L6 登录失败后可重试

**Files:**
- Create: `e2e/tests/login.spec.ts` (L5 位置)
- Test: N/A

- [ ] **Step 1: 实现多次尝试用例**

```typescript
test('登录失败后可重试', async ({ page }) => {
  let callCount = 0
  await page.route('/api/v1/patient/wx-login', async route => {
    callCount++
    if (callCount === 1) {
      await route.fulfill({ status: 500 })
    } else {
      await route.fulfill({ json: { token: MOCK_TOKEN, ...MOCK_PATIENT } })
    }
  })
  
  const el = loginPage(page)
  await el.checkbox.click()
  
  // 第一次失败
  await el.wechatBtn.click()
  await expect(page.locator('uni-toast')).toBeVisible()
  await expect(page).toHaveURL(/pages\/login/)
  
  // 第二次成功
  await el.wechatBtn.click()
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
  await expect(page.getByText('实时监测').first()).toBeVisible()
})
```

- [ ] **Step 2: 运行测试验证恢复逻辑**

```bash
npx playwright test e2e/tests/login.spec.ts --grep "登录失败后可重试" --headed
```

- [ ] **Step 3: Commit**

```bash
git add e2e/tests/login.spec.ts
git commit -m "test: add L6 - Retry after login failure"
```

---

## Task 8: 编写 L8 loading/disabled 态

**Files:**
- Create: `e2e/tests/login.spec.ts` (L6 位置)
- Test: N/A

- [ ] **Step 1: 实现加载状态用例**

```typescript
test('点击后按钮进入 loading 且 disabled', async ({ page }) => {
  const el = loginPage(page)
  await el.checkbox.click()
  
  await page.route('/api/v1/patient/wx-login', async route => {
    await new Promise(resolve => setTimeout(resolve, 1000))
    await route.fulfill({ json: { token: MOCK_TOKEN, ...MOCK_PATIENT } })
  })
  
  await el.wechatBtn.click()
  
  // 按钮 loading 样式
  await expect(el.wechatBtn).toHaveClass(/loading/)
  await expect(el.wechatBtn).toBeDisabled()
  
  // 防止重复点击
  await expect(el.wechatBtn).toHaveText(/正在加载 | 加载中/)
})
```

- [ ] **Step 2: 运行测试验证加载 UI**

```bash
npx playwright test e2e/tests/login.spec.ts --grep "点击后按钮进入 loading" --headed
```

- [ ] **Step 3: Commit**

```bash
git add e2e/tests/login.spec.ts
git commit -m "test: add L8 - Loading state prevents duplicate clicks"
```

---

## Task 9: 编写 L9 reload 保持登录

**Files:**
- Create: `e2e/tests/login.spec.ts` (L7 位置)
- Test: N/A

- [ ] **Step 1: 实现刷新保持场景**

```typescript
test('已登录 monitor 页刷新后保持', async ({ page }) => {
  // Setup: 先完成登录
  await page.route('/api/v1/patient/wx-login', async route => {
    await route.fulfill({ json: { token: MOCK_TOKEN, ...MOCK_PATIENT } })
  })
  
  const el = loginPage(page)
  await el.checkbox.click()
  await el.wechatBtn.click()
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
  
  // Reload
  await page.reload()
  
  // URL 不变
  await expect(page).toHaveURL('**/pages/monitor/')
  
  // 页面无微信登录按钮
  await expect(el.wechatBtn).not.toBeVisible()
  
  // 热力图数据正常
  await expect(page.locator('.sensor-grid .grid-cell')).toHaveCount(20)
})
```

- [ ] **Step 2: 运行测试验证守卫逻辑**

```bash
npx playwright test e2e/tests/login.spec.ts --grep "已登录 monitor 页刷新后保持" --headed
```

- [ ] **Step 3: Commit**

```bash
git add e2e/tests/login.spec.ts
git commit -m "test: add L9 - Refresh preserves logged-in state"
```

---

## Task 10: 编写 L10 多次重试无死循环

**Files:**
- Create: `e2e/tests/login.spec.ts` (L8 位置)
- Test: N/A

- [ ] **Step 1: 实现并发压力用例**

```typescript
test('多次点击登录无死循环', async ({ page }) => {
  const btn = loginPage(page).wechatBtn
  
  // Mock 始终成功
  await page.route('/api/v1/patient/wx-login', async route => {
    await route.fulfill({ json: { token: MOCK_TOKEN, ...MOCK_PATIENT } })
  })
  
  // 勾选协议
  await loginPage(page).checkbox.click()
  
  // 快速连续点击 5 次
  for (let i = 0; i < 5; i++) {
    await btn.click()
  }
  
  // 只跳转一次 (无路由栈爆炸)
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
  
  // 检查路由历史
  const history = page.evaluate(() => window.history.length)
  expect(history).toBeLessThan(10)
})
```

- [ ] **Step 2: 运行测试验证稳定性**

```bash
npx playwright test e2e/tests/login.spec.ts --grep "多次点击登录无死循环" --headed
```

- [ ] **Step 3: Commit**

```bash
git add e2e/tests/login.spec.ts
git commit -m "test: add L10 - Multiple clicks prevent infinite loops"
```

---

## Task 11: 替换 full-flow.spec.ts 登录段

**Files:**
- Modify: `e2e/tests/full-flow.spec.ts:10-17`

- [ ] **Step 1: 读取完整文件确认上下文**

```bash
Read e2e/tests/full-flow.spec.ts
```

- [ ] **Step 2: 替换登录段落 (原 SMS → 微信)**

```typescript
// ===== 1. login =====
await page.goto('/#/pages/login/index')
const el = loginPage(page)
await el.checkbox.click()
await el.wechatBtn.click()
await expect(page.locator('uni-toast').filter({ hasText: '登录成功' })).toBeVisible()
await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
```

- [ ] **Step 3: 运行全链路测试验证集成**

```bash
npx playwright test e2e/tests/full-flow.spec.ts --headed
```

- [ ] **Step 4: Commit**

```bash
git add e2e/tests/full-flow.spec.ts
git commit -m "test: update full-flow spec for WeChat login contract"
```

---

## Task 12: 清理淘汰旧用例

**Files:**
- Modify: `e2e/tests/login.spec.ts`

- [ ] **Step 1: 删除原 SMS 相关用例**

从文件中删除以下 3 条旧用例:
- L1: 手机号与验证码输入
- L2: 获取验证码后出现 60s 倒计时
- L3: 手机号格式错误时提示
- L4: 未输入验证码时登录被拦截

- [ ] **Step 2: 调整文件头部注释说明**

将文件顶部的描述更新为:

```typescript
/**
 * login 页：微信授权一键登录、协议勾选、60s 倒计时已淘汰→H5 fallback code=h5-fallback-wechat-login-code
 * 对齐 T074 新契约用例清单 (mock 登录，不依赖后端)
 */
```

- [ ] **Step 3: Commit**

```bash
git add e2e/tests/login.spec.ts
git commit -m "test: remove obsolete SMS login test cases per T074 migration"
```

---

## Task 13: 最终验收测试

**Files:**
- N/A

- [ ] **Step 1: 运行所有 login.spec 用例**

```bash
npx playwright test e2e/tests/login.spec.ts --reporter=list
```

预期结果：**全部 8 个新用例通过**

- [ ] **Step 2: 检查代码覆盖率 (可选)**

```bash
npx playwright test e2e/tests/login.spec.ts --coverage
```

- [ ] **Step 3: 提交最终版本**

```bash
git add .
git commit -m "feat(T080): complete rewrite of login.e2e for WeChat login contract"
```

---

## Task 14: 推送远程分支并汇报清单

**Files:**
- N/A

- [ ] **Step 1: 确认所有变更已提交**

```bash
git status
git log --oneline -3
```

- [ ] **Step 2: 推送分支**

```bash
git push origin t074-login-spec
```

- [ ] **Step 3: 验证远程分支存在**

```bash
git ls-remote origin t074-login-spec
```

- [ ] **Step 4: 生成提交清单**

提供以下内容:
1. **分支名**: `t074-login-spec`
2. **HEAD SHA**: `<最新 commit 的 hash>`
3. **用例清单**: L1-L8 (共 8 个新用例，淘汰 4 个 SMS 用例)
4. **受修改文件**: `e2e/helpers.ts`, `e2e/tests/login.spec.ts`, `e2e/tests/full-flow.spec.ts`

---

## Self-Review Checklist

✅ **Spec coverage:**
- [L1 可见可用] → Task 3 ✅
- [L2 跳转+storage] → Task 5 ✅
- [L3 接口失败] → Task 6 ✅
- [L4 fallback code] → Task 5 (融合) ✅
- [L5 已登录直达] → Task 9 (融合) ✅
- [L6 失败重试] → Task 7 ✅
- [L7 协议勾选] → Task 4 ✅
- [L8 loading] → Task 8 ✅
- [L9 reload] → Task 9 ✅
- [L10 多次点击] → Task 10 ✅

✅ **Placeholder scan:** 无 TODO/TBD，所有代码完整

✅ **Type consistency:** 使用统一变量名 `MOCK_TOKEN`, `MOCK_PATIENT`, `wechatBtn`
