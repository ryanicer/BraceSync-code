# T053 URL 断言根路径还原计划（第 3 轮返工）

## Summary（一句话）

PR #15 最新 commit 把 `toHaveURL/startsWith/endsWith/toContain` 断言改成了 `/admin/` 前缀，PM 实跑 26 条全挂；但 staging SPA 用 `router.createWebHistory()` 无 base，应用内跳转 URL 是根路径（`/dashboard`、`/login`），所以断言必须用根路径。本次把断言/守卫**还原回根路径**，`page.goto()` 仍走 `realRoutes.xxx`（`/admin/xxx`，nginx 入口）。

## 真相（PM 实证 + 截图）

* 登录成功后实际 URL：`http://localhost:2080/dashboard`（根，无 `/admin/`），Dashboard 渲染正常（顶栏"运营小张"、6 KPI、侧边栏完整）→ 功能没问题，是断言错。

* 根因：前端 `router.createWebHistory()` 无 base → `router.push('/dashboard')` 生成根路径；nginx `location /admin/` alias SPA + `location / → 302 /admin/`，但 SPA history 模式 pushState 不触发服务器请求，URL 停在应用设置的根值。

* 所以：**进入页面**用 `/admin/xxx`（nginx 入口），**应用内跳转后断言**用根路径。

## Current State Analysis（Phase 1 探查结果）

对 `e2e-real/` 全量 grep `toHaveURL|startsWith|endsWith|toContain|waitForURL` + `/admin\/`，结论：

### 断言/守卫代码——当前磁盘**已经全部是根路径**（无需再改代码逻辑）

| 文件:行                      | 代码                                                       | 状态   |
| ------------------------- | -------------------------------------------------------- | ---- |
| `real-helpers.ts:114`     | `(url) => !url.pathname.startsWith('/login')`            | ✅ 已根 |
| `01-login.spec.ts:36`     | `await expect(page).toHaveURL(/\/dashboard/...)`         | ✅ 已根 |
| `01-login.spec.ts:60`     | `expect(path === '/login' \|\| path.endsWith('/login'))` | ✅ 已根 |
| `01-login.spec.ts:68`     | `await expect(page).toHaveURL(/\/dashboard/...)`         | ✅ 已根 |
| `01-login.spec.ts:71`     | `await expect(page).toHaveURL(/\/login/...)`             | ✅ 已根 |
| `01-login.spec.ts:79`     | `expect(urlAfter).toContain('/login')`                   | ✅ 已根 |
| `02-dashboard.spec.ts:81` | `toContain('/dashboard')`                                | ✅ 已根 |
| `02-dashboard.spec.ts:88` | `toContain('/dashboard')`                                | ✅ 已根 |
| `04-monitor.spec.ts:139`  | `toContain('/monitor')`                                  | ✅ 已根 |

### `/admin/` 残留——仅 `realRoutes` 定义 + 注释（应保留/微调）

| 文件:行                       | 内容                             | 处理                                     |
| -------------------------- | ------------------------------ | -------------------------------------- |
| `real-helpers.ts:31-44`    | `realRoutes` 15 键 `/admin/xxx` | **保留**（`page.goto` 入口用）                |
| `real-helpers.ts:14,27,28` | 注释说明 realRoutes 带 /admin/ 前缀   | 保留（准确）                                 |
| `playwright.config.ts:8`   | 注释 baseURL 说明                  | 保留（准确）                                 |
| `01-login.spec.ts:75`      | 注释"直接访问 /admin/patients"       | 保留（goto 用 realRoutes.patients，准确）      |
| `real-helpers.ts:110`      | 注释"离开 /admin/login"            | **改**→"离开 /login"（与 L114 代码一致）         |
| `real-helpers.ts:118`      | 注释"redirect 回 /admin/login"    | **改**→"redirect 回 /login"（与 L114 代码一致） |

### 判断

PM 实跑 26 挂的是 PR #15 最新 commit（带 `/admin/` 断言）。当前 worktree 磁盘的断言代码已是根路径（可能 PM 本地已部分还原或上次 Edit 落地状态）。**本次任务实质：把磁盘已正确的根路径断言状态提交覆盖 PR 分支**，让 PR HEAD 与磁盘一致。同时修正 2 行不准注释。

## Proposed Changes

### 变更 1：修正 2 行事实不准的注释（`real-helpers.ts`）

**文件**：`e2e-real/real-helpers.ts`

* **L110**：`// 登录成功：离开 /admin/login（成功跳 dashboard...）` → `// 登录成功：离开 /login（成功跳 /dashboard...SPA 根路径，非 /admin/）`

* **L118**：`// 兜底：如果被 redirect 回 /admin/login（账号异常）...` → `// 兜底：如果被 redirect 回 /login（账号异常）...`

**Why**：L114 代码已是 `startsWith('/login')`，注释却说 `/admin/login`，误导后续维护者。2 行注释微调，零逻辑改动。

### 变更 2（兜底）：若执行时 grep 发现任何残留 `/admin/` 断言，按下表还原

执行阶段先 grep；若发现以下任一模式仍带 `/admin/`，按 PM 还原表改回根（基于当前探查，预计无需触发，但作为决策完整兜底）：

| 模式（若残留）                           | 还原为                        |
| --------------------------------- | -------------------------- |
| `toHaveURL(/\/admin\/dashboard/)` | `toHaveURL(/\/dashboard/)` |
| `toHaveURL(/\/admin\/login/)`     | `toHaveURL(/\/login/)`     |
| `endsWith('/admin/login')`        | `endsWith('/login')`       |
| `startsWith('/admin/login')`      | `startsWith('/login')`     |
| `toContain('/admin/login')`       | `toContain('/login')`      |

**不动**：`realRoutes.xxx`（`/admin/xxx`，给 `page.goto` 用，nginx 入口必须带）。

## Assumptions & Decisions

1. **baseURL 保持** **`http://localhost:2080`（根，无 /admin）**——上次返工已对，不动。
2. **`realRoutes`** **保持** **`/admin/xxx`** **前缀**——`page.goto()` 进入页面走 nginx 入口，必须带；不动。
3. **断言/守卫用根路径**——SPA 应用内跳转 URL 是根，断言匹配根；本次确保全部根路径并提交。
4. **不动 03/05/06/07 specs 的业务断言**——它们无 URL 路径断言（grep 确认），仅 01/02/04 有 URL 断言，均已根。
5. **git 提交可能仍受 TRAE 沙盒** **`index.lock`** **阻塞**——若沙盒内 `git add/commit` 失败，提供 PM 本机命令清单（同前两轮模式）。
6. **.trae/ 不入库**——本计划文件 `.trae/documents/T053-url-assertion-root-fix.md` 是 Plan Mode 产物，不提交；上轮已 `git rm --cached .trae/documents/T053-real-e2e_plan.md`，本轮不新增任何 .trae/ 入库。

## Verification Steps

1. **Grep 残留核对**（执行后必跑）：

   * `Grep e2e-real 'toHaveURL\([^)]*admin|startsWith\(['"]\/admin|endsWith\(['"]\/admin|toContain\(['"]\/admin'` → 期望 **No matches found**

   * `Grep e2e-real 'adminRoutes'` → 期望仅 `real-helpers.ts:14` 注释 1 处
2. **语法冒烟**：

   * `cd e2e-real; npx playwright test --list --config=playwright.config.ts` → 期望 **27 tests in 7 files**（exit 0）
3. **提交推送**：

   * 沙盒内尝试 `git add e2e-real/real-helpers.ts e2e-real/tests/01-login.spec.ts e2e-real/tests/02-dashboard.spec.ts e2e-real/tests/04-monitor.spec.ts` + commit + push

   * 若 `index.lock` 受阻 → 提供 PM 本机 10 行命令（精确暂存 4 个改动文件 + commit + push feat/ella-T053-real-e2e）
4. **PM 本机实跑**（提交后）：

   * `ssh -L 2080:localhost:81 ubuntu@106.52.39.208 -N -o ServerAliveInterval=30`

   * `cd e2e-real; npx playwright test --config=playwright.config.ts --reporter=list --workers=1 --output D:/proj/t053-out/test-results`

   * 期望：1.2 `toHaveURL(//dashboard/)` 通过、1.3 `endsWith('/login')` 通过、1.4 守卫通过；后续 23 条暴露真实通过/失败

## 执行注意

* 仅改 `real-helpers.ts` 2 行注释（变更 1），零代码逻辑改动。

* 变更 2 是兜底：执行时先 grep，若已无残留（预计），跳过；若有，按下表还原。

* 不创建新文件，不动业务代码，不动 realRoutes，不动 baseURL。

