# T061 患者沟通测试预置 — 执行计划

> 计划版本：v1.0
> 负责人：Ella（测试预置）→ 实现方待定（转绿）
> 状态：🔄 测试预置（KNOWN\_RED）
> 分支：`feat/ella-T061-communication-tests`（从 main 切出）
> 需求基准：`docs/design/admin/患者沟通.html` + PM 需求单（2026-08-29）

***

## 一、Repo 调研结论

### 1.1 前端页面现状（`apps/admin-web/src/pages/communication/index.vue`，168 行）

| 模块       | 现状                                                                                                                                    | 是否就绪             |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------- | ---------------- |
| 客服角色提示   | `el-alert` 顶部显示 "客服角色：仅可查看反馈与标记处理状态（PRD §7D.11）"，v-if="auth.role==='cs'"                                                              | ✅                |
| 工具栏      | `.page-toolbar`：搜索框 `el-input`（keyword）+ "查询" `el-button`                                                                             | ⚠️ 缺"打开微信客服后台"按钮 |
| 反馈列表     | `el-table` 列：反馈ID / 患者名 / 类型 / 内容 / 提交时间 / 状态（`el-tag` pending→warning / replied→primary / resolved→success）/ 处理人 / 操作列（"详情"+"标记已处理"） | ✅                |
| 详情/回复对话框 | `el-dialog` 标题含 `feedbackId`，`el-descriptions` 展示患者/类型/内容/提交时间/回复（若有）。status=pending 时显示 textarea + "回复并标记"按钮                         | ✅                |
| API 对接   | `fetchFeedbacks({ keyword })` → 列表；`processFeedbackApi(feedbackId, reply?)` → 回复或标记                                                   | ✅                |
| **核心缺口** | **工具栏无"打开微信客服后台"按钮（跳转** **`https://mpkf.weixin.qq.com/`，target=\_blank）**                                                             | ❌ **本任务唯一实现缺口**  |

### 1.2 前端 Mock 数据（`apps/admin-web/src/mock/communication.ts`）

* 4 条预置反馈（FB-001 pending，FB-002 replied，FB-003 resolved，FB-004 pending），覆盖全部状态

* 患者姓名映射：PT-001→林小雨 等

* 关键词过滤逻辑已实现

* ✅ 足够支撑 6 条 E2E 用例，无需补 mock

### 1.3 API 层（`apps/admin-web/src/api/index.ts` L197-207）

* `fetchFeedbacks(params)` → `GET /api/v1/feedbacks`（USE\_MOCK 时走 mockFeedbacks）

* `processFeedbackApi(feedbackId, reply?)` → `POST /api/v1/feedbacks/:feedbackId/process`（body: `{ replyContent }`）

* ✅ 无新增端点需求，本任务不碰 API 层

### 1.4 后端（PM 已核实，不查代码）

* `GET /api/v1/feedbacks`（列表 + keyword 过滤）已就绪

* `POST /api/v1/feedbacks/:feedbackId/process`（replyContent ≤500 字符，回复落库 + 标记处理）已就绪

* ✅ **后端零改动**，本任务不写 Go 测试

### 1.5 测试基础设施（已就绪）

| 资产            | 文件                                                      | 说明                                             |
| ------------- | ------------------------------------------------------- | ---------------------------------------------- |
| 路由            | `e2e/admin-helpers.ts L22-37`                           | `adminRoutes.communication = '/communication'` |
| 角色页面          | `e2e/admin-helpers.ts L47`                              | `CS_PAGES = ['/communication']`（客服角色仅限此页）      |
| 登录 Helper     | `adminLogin(page, 'admin' \| 'cs' \| 'doctor')`         | 支持客服角色账号（"客服小美"）                               |
| 通用 Helper     | `tableRows()` / `adminMessage()` / `pickSelectOption()` | 复用                                             |
| KNOWN\_RED 范例 | `e2e/tests/admin-patient-writes.spec.ts`                | `test.fail()` 标记预期红态的模式可复用                     |

***

## 二、缺口与需求对齐分析

### 2.1 核心实现缺口（仅 1 处）

前端页面 `apps/admin-web/src/pages/communication/index.vue` 的 `.page-toolbar`（L11-21）当前结构：

```
[搜索框] [查询按钮]
```

实现方转绿时需改为：

```
[搜索框] [查询按钮] [打开微信客服后台]    ← 新增，type 可与查询不同（如 success 或 default），按钮 tooltip 或紧随文案标注："小程序客服消息 · 需使用运营账号登录微信公众平台"
```

点击行为：`window.open('https://mpkf.weixin.qq.com/', '_blank')`（不离开运营后台，新窗口打开）

### 2.2 范围排除（写在测试规格中供 Boss 评审标注）

1. **会话式左右分栏 UI**：现有是表格 + 对话框模式，先保核心可用口径，后续迭代
2. **微信客服消息自动同步**：仅提供入口跳转，运营后台不做消息同步（小程序客服消息在微信平台内闭环）
3. **统计条（总数/已处理/待处理卡片）**：可前端顺手聚合（基于列表数据），不涉及新端点；本规格不做强制要求

***

## 三、交付物清单（按顺序产出）

### 交付物 1：测试规格文档

**路径**：`docs/tasks/ella/T061-患者沟通测试规格.md`
**结构**（参考 T057 规格）：

```
1. 一、范围（需求 + 范围排除 x3）
2. 二、现状盘点（前端 168 行实现度 / 后端端点就绪 / 核心缺口高亮）
3. 三、前端交互验收标准（6 条：按钮渲染 / 跳转 / 列表 / 详情 / 回复处理 / 角色提示）
4. 四、权限（客服角色提示说明）
5. 五、KNOWN_RED 测试清单
   - 前端 E2E 6 条明细（标注哪 2 条为真·KNOWN_RED，哪 4 条已有实现应 PASS）
   - 后端 Go 测试：无（声明"后端无改动"）
6. 六、实现方转绿清单（仅 1 处：补按钮 + window.open）
7. 七、参考（设计源路径 + 现有页面 + mock + helpers）
```

### 交付物 2：前端 E2E 测试

**路径**：`e2e/tests/admin-communication.spec.ts`
**执行前准备**：`test.beforeEach` 用 `adminLogin(page, 'admin')` 登录 → `page.goto(adminRoutes.communication)`

**6 条用例设计**：

| # | 用例名                                    | Playwright 写法                                                                                                                                   | 标记               | 预期红/绿                                                                             |
| - | -------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ---------------- | --------------------------------------------------------------------------------- |
| 1 | 页面渲染"打开微信客服后台"按钮                       | 定位 `.page-toolbar` 内 `getByRole('button', { name: '打开微信客服后台' })`，断言 `toBeVisible()` 且 `toHaveText(/打开微信客服后台/)`                                  | `test.fail(...)` | **🔴 KNOWN\_RED**：当前无此按钮，定位 throw → FAIL 属预期                                      |
| 2 | 点击按钮新窗口打开微信客服 URL 含 mpkf.weixin.qq.com | `page.waitForEvent('popup')` 获取 popup 页 → `expect(popup.url()).toContain('mpkf.weixin.qq.com')`；mock 无网环境可降级：spy `window.open` 调用参数             | `test.fail(...)` | **🔴 KNOWN\_RED**：按钮不存在 → 无法 click → FAIL 属预期                                     |
| 3 | 列表展示已有反馈（患者/内容/状态）                     | `tableRows(page)` 断言至少 4 行；首行 `toContainText('林小雨')` 且含反馈内容；状态列含 `el-tag` pending-warning / resolved-success                                    | `test(...)`（正常）  | 🟢 现有实现已覆盖，应 PASS                                                                 |
| 4 | 详情对话框可打开，显示反馈内容                        | 点首行"详情"按钮 → `el-dialog` `toBeVisible()`；内容含 `el-descriptions` 且有"反馈内容/类型/患者"等字段                                                                 | `test(...)`（正常）  | 🟢 现有实现已覆盖，应 PASS                                                                 |
| 5 | 回复并标记已处理后状态变 resolved + 列表刷新           | 找 pending 行（FB-001/林小雨）→ 点"详情"→ 填 `replyText` → 点"回复并标记"→ `expect(adminMessage(page)).toContainText('成功')` → dialog 关闭 → 行内状态 tag 变为"已回复"或"已解决" | `test(...)`（正常）  | 🟢 现有 mock 支持 processFeedbackApi no-op，应 PASS（若 mock 未更新 status 字段则需规格标注 mock 预期） |
| 6 | 客服角色权限提示可见                             | 另起 `test.describe` 用 `adminLogin(page, 'cs')` 登录 → 跳转后断言 `.role-hint` 或 `el-alert` 可见，文案含 `客服角色：仅可查看反馈与标记处理状态`                                  | `test(...)`（正常）  | 🟢 现有实现已覆盖，应 PASS                                                                 |

**注意**：#5 用例的 mock 端 `processFeedbackApi` 走 USE\_MOCK 分支时目前只 `delay()` return，不更新 FEEDBACKS 数组内存状态。列表刷新后原对象 status 仍为 pending。实现方转绿时 mock 需补"找到该条反馈并更新 status/replyContent/handler/replyTime"，否则用例可能 FAIL。规格文档需标注此为**转绿前置**。

**红态判定口径**：文件级不以 `test.describe.fail` 包裹，仅前 2 条因按钮缺失用 `test.fail()` 标记 KNOWN\_RED。其余 4 条正常运行，如 #5 因 mock 内存更新缺失 FAIL，属于「mock 未补全」的已知弱失败，在规格中以备注形式说明，不额外标记 test.fail（否则 mock 补齐后会报意外通过）。

### 交付物 3：后端 Go 测试

**不产出**（PM 已核实后端无新端点）。测试规格中显式声明"后端零改动，不补 Go 测试"。

***

## 四、执行步骤（用户批准后依序执行）

```
步骤 1：Git 分支准备
  - cd d:/proj/BraceSync（权威仓库根），或在 ella worktree 中操作：
    1.1 git fetch origin main
    1.2 git checkout -b feat/ella-T061-communication-tests origin/main
    （当前 worktree 是 detached HEAD，需明确从 main 切出新分支）

步骤 2：产出测试规格
  - 2.1 mkdir -p docs/tasks/ella（若不存在）
  - 2.2 Write docs/tasks/ella/T061-患者沟通测试规格.md
        按第三节"结构"撰写，重点标注：
          - 前 2 条 E2E 是真·KNOWN_RED（缺按钮）
          - #5 用例需要 mock 内存更新（转绿前置注记）
          - 实现方转绿清单：补 1 个按钮 + mock 补齐

步骤 3：产出前端 E2E
  - 3.1 Write e2e/tests/admin-communication.spec.ts
        参考 admin-patient-writes.spec.ts 结构：
          - import { test, expect } from '@playwright/test'
          - import { adminRoutes, adminLogin, adminMessage, tableRows } from '../admin-helpers'
          - beforeEach：adminLogin(page, 'admin') + goto /communication
          - describe 1："打开微信客服后台"入口（前 2 条 test.fail）
          - describe 2："反馈列表与详情"（#3、#4）
          - describe 3："回复与处理流程"（#5）
          - describe 4："客服角色权限提示"（#6， beforeEach 用 cs 角色登录）
        popup 测试：优先用 page.waitForEvent('popup')；如 mock 模式弹窗被拦截则降级
        为 page.addInitScript 注入 window.open spy，断言调用 URL

步骤 4：本地验证红态（快速冒烟，非强制）
  - 4.1 cd apps/admin-web && npm run dev 或 mock 模式启动
  - 4.2 npx playwright test e2e/tests/admin-communication.spec.ts --reporter=line
        期望：#1、#2 FAIL（test.fail 绿信号），#3 #4 #6 PASS，#5 若 mock 不更新可能 FAIL（可接受）

步骤 5：提交 & 推送
  - 5.1 git add docs/tasks/ella/T061-患者沟通测试规格.md e2e/tests/admin-communication.spec.ts
  - 5.2 git commit -m "test(T061): 患者沟通测试预置 KNOWN_RED
        - 新增测试规格文档
        - 新增 admin-communication.spec.ts 6 条用例（前 2 条 test.fail 预期红）"
  - 5.3 git push -u origin feat/ella-T061-communication-tests

步骤 6：回报 PM
  - 告知分支名、commit hash、文件清单、#5 用例 mock 注意事项
```

***

## 五、依赖与潜在风险

| 风险项                                                     | 等级 | 处置                                                                                           |
| ------------------------------------------------------- | -- | -------------------------------------------------------------------------------------------- |
| 当前 worktree 是 detached HEAD（95a822f），分支未创建              | 中  | 执行时显式从 `origin/main` 切 `feat/ella-T061-communication-tests`，不要基于 detached HEAD 提交            |
| #5 用例 mock 不更新内存 → 回复后列表不刷新 status → FAIL               | 低  | 规格文档显式注记为「实现方转绿时补 mock 内存更新」，当前用例允许弱失败（不标记 test.fail）                                        |
| popup 事件在 Playwright mock/dev 模式下受 window\.open 浏览器策略限制 | 低  | 测试同时提供两种断言路径：(a) `waitForEvent('popup')`；(b) addInitScript spy window\.open，取其一即可；避免 CI 环境差异 |
| 设计源 `患者沟通.html` 缺失无法对其按钮位置做像素级对齐验证                      | 低  | PM 已核实设计源，按需求描述"与查询按钮同排"定位 `.page-toolbar` 容器，文案匹配即可                                         |
| 客服角色账号在某些 mock 环境下可能无 `/communication` 入口               | 低  | 利用 `CS_PAGES` 已验证；若失败直接用 `page.goto(adminRoutes.communication)` 强制访问                         |

***

## 六、实现方转绿校验清单（规格中引用）

实现方（后续派 Winner）需完成并通过全部 6 条用例，使原标记 `test.fail` 的 #1 #2 不再 FAIL → 移除标记：

* [ ] `apps/admin-web/src/pages/communication/index.vue` L11-21 `.page-toolbar` 新增按钮：文案 = "打开微信客服后台"

* [ ] 按钮点击：`window.open('https://mpkf.weixin.qq.com/', '_blank')`

* [ ] 按钮 tooltip 或邻接小字："小程序客服消息 · 需使用运营账号登录微信公众平台"（可选，若设计源有则必加）

* [ ] mock 层 `apps/admin-web/src/mock/communication.ts` 补内存更新：`processFeedbackApi` 对应 mock 分支实现时，需更新 FEEDBACKS 数组该条记录的 `status / replyContent / replyTime / handler`

* [ ] 运行 `npx playwright test e2e/tests/admin-communication.spec.ts`，6/6 全部 PASS

* [ ] **移除** `admin-communication.spec.ts` 中 #1 #2 的 `test.fail` 标记（意外通过会报错，提示必须移除）

