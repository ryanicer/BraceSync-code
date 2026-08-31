# T053 实施计划：staging 真实模式 E2E

> 负责人：Ella · 分支：`feat/ella-T053-real-e2e` · 基线 SHA：`d5841d3`

***

## 一、仓库调研结论

### 1.1 现有 mock E2E 框架结构（已分析）

| 文件                                     | 用途                                                                                                                                                      | 复用价值                                         |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------- |
| `e2e/admin-playwright.config.ts`       | mock 配置：启动 vite `--port 5175`，USE\_MOCK=true，testMatch `admin-*.spec.ts`                                                                                | **不直接复用**（参考结构）                              |
| `e2e/admin-helpers.ts`                 | mock 专用 helper：`adminLogin()` 走角色下拉；另有 `pickSelectOption/tableRows/menuItems/gotoMenu/adminMessage/topBarUserName/adminLogout/adminRoutes` 等通用 selector | **部分复用**：selector 类 helper 直接复用；登录 helper 重写 |
| `e2e/t052-staging.config.ts`           | T052 staging 配置：baseURL `http://localhost:2080/admin`，无 webServer，trace/screenshot on                                                                   | **完全参考**（升级为独立目录）                            |
| `e2e/tests/t052-task2-staging.spec.ts` | T052 真实登录 3 条用例样本：定位 `.login-form input:first()`（用户名）+ `input[type="password"]`（密码）+ 按钮文案「登 录」（中间有空格）                                                   | **完全复用**登录定位逻辑                               |

### 1.2 登录页真实模式与 mock 模式差异（关键）

`apps/admin-web/src/pages/login/index.vue` 结构：

* `v-if="isMock"`（USE\_MOCK=true）：角色下拉选择 + 任意密码输入 → `adminLogin()`（mock 版本）

* `v-else`（USE\_MOCK=false）：用户名输入框 + 密码输入框 + 表单校验 + 按钮「登 录」 → 需**重写真实登录 helper**

真实 staging 账号：`ops_admin / admin123`（T051 seed）

### 1.3 T051 seed 基线（真实数据，区别于 mock）

| 表           | seed 行数 | mock 行数 | 影响                       |
| ----------- | ------- | ------- | ------------------------ |
| patients    | 5       | 6       | 患者列表断言数量不同（用 `≥5` 非精确匹配） |
| alerts      | 9       | 6       | 告警列表数量不同                 |
| feedbacks   | 17      | 4       | 沟通列表数量差异大                |
| teams       | 3       | 3       | 一致                       |
| doctors     | 3       | -       | 团队负责人/成员可选值              |
| technicians | 3       | -       | -                        |
| devices     | 5       | -       | 患者设备绑定状态                 |

**策略调整**：真实模式断言全部改为**存在性/非空/≥N 断言**，不用 mock 的精确数值（KPI 卡片、图表内容仅验证渲染不验证固定值）。

### 1.4 staging 访问前置条件

* SSH 隧道：`ssh -L 2080:localhost:81 ubuntu@106.52.39.208 -N`

* 前端地址：`http://localhost:2080/admin/`

* 密钥文件：`D:\env\ksd_prod_key.pem`（如缺失需提示用户）

* 不在配置中硬编码密钥/地址；全部依赖 `E2E_BASE_URL` env 或默认 `http://localhost:2080/admin`

***

## 二、修改文件与模块清单

### 2.1 新建文件（独立 `e2e-real/` 目录，与 mock E2E 完全隔离）

| #  | 新建文件路径                                    | 功能说明                                                                                                                                                                                                                                                                         |
| -- | ----------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1  | `e2e-real/playwright.config.ts`           | 真实模式 Playwright 配置：baseURL `http://localhost:2080/admin`（env 可覆盖）；**无 webServer**；串行 workers=1；timeout 加大到 120s；trace+screenshot on；project=chromium 桌面                                                                                                                      |
| 2  | `e2e-real/real-helpers.ts`                | 真实模式 helper：① `realLogin(page, username?, password?)` 用户名密码登录（默认 ops\_admin/admin123）；② `realLogout(page)` 退出；③ **re-export** `adminRoutes/pickSelectOption/tableRows/menuItems/gotoMenu/adminMessage/topBarUserName` 等 selector helper（从 `../e2e/admin-helpers` 导入再导出，避免重复） |
| 3  | `e2e-real/tests/01-login.spec.ts`         | 登录模块 4 条：① 登录页渲染（用户名/密码/按钮）；② ops\_admin 登录成功跳 Dashboard + localStorage token；③ 错误密码提示「用户名或密码错误」；④ 退出后访问受保护页被重定向到 /login?redirect=                                                                                                                                           |
| 4  | `e2e-real/tests/02-dashboard.spec.ts`     | Dashboard 模块 3 条：① 6 张 KPI 卡片渲染（只验证 label 存在 + value 非空，不验证固定值）；② 4 张图表 canvas 渲染 + 两张排行表（5 行以上）；③ 切换周期（今日/本周/本月）后 KPI 值有变化或不报错                                                                                                                                              |
| 5  | `e2e-real/tests/03-alerts.spec.ts`        | 告警模块 4 条：① 列表渲染（≥9 行 + 分页显示总数）；② 类型筛选（压力偏高/佩戴中断 → 结果有变化）；③ 状态筛选（待处理/已处理 → 过滤生效）；④ **待处理告警处理流程**：打开对话框 → 填写备注（T053测试+时间戳）→ 确认处理 → ElMessage 成功                                                                                                                                |
| 6  | `e2e-real/tests/04-monitor.spec.ts`       | 实时监控模块 3 条：① 默认患者加载 + 实时同步标签 + 最近更新时间戳；② 4×5 热力图 20 格渲染（P01-P20 + 数值 + 最大点标记 + 图例）；③ 患者切换（2-3 名 seed 患者）→ 状态/设备提示更新                                                                                                                                                          |
| 7  | `e2e-real/tests/05-patients.spec.ts`      | 患者模块 5 条：① 列表渲染（≥5 行 + 列信息存在：ID/姓名/角度/团队/医生/设备/状态）；② 关键词搜索（seed 中存在的姓名，如「林小雨」或 seed 其他姓名）；③ 团队筛选（3 个团队，筛后结果数 ≤5）；④ **添加患者**（唯一命名：`T053测试-<timestamp>` + 必填字段 → 提交成功 → 搜索可找到）；⑤ **分配团队**（给新患者或无团队患者分配团队 → 更新成功提示）                                                             |
| 8  | `e2e-real/tests/06-teams.spec.ts`         | 团队模块 5 条：① 列表渲染（3 行 seed 团队 + 列信息完整）；② **新建团队**（唯一命名 `T053团队-<timestamp>` + 选负责人 → 保存成功 → 列表出现）；③ **编辑团队**（修改新建团队名称 → 更新成功）；④ **删除团队（引用拒绝）**：删除 seed 中已有团队（含患者/成员）→ 提示被引用（409 处理成功）；⑤ **删除新建团队（成功）**：删除自己新建的空团队 → 删除成功消失                                                     |
| 9  | `e2e-real/tests/07-communication.spec.ts` | 沟通模块 3 条：① 列表渲染（≥17 行 feedbacks + 状态 tag 含「待处理/已解决」等）；② 详情对话框：点「详情」→ 显示字段（患者/类型/内容/提交时间）；③ **回复处理**：取 1 条 pending 反馈 → 填回复（`T053回复-<timestamp>`）→ 提交成功 → 状态变更为「已回复」或「已解决」                                                                                                    |
| 10 | `e2e-real/tests/08-cleanup.spec.ts`       | **数据清理** 1 条：① 清理本任务创建的资源：删除 `T053团队-*` 团队、删除 `T053测试-*` 患者（使用 API 或 UI 删除）；用 test.describe `afterAll` 或独立 spec 保证可重复运行                                                                                                                                                      |

> 用例总计：4+3+4+3+5+5+3+1 = **28 条**（超 15-25 区间，可在执行时将 cleanup 合并进 patients/teams 末尾，压缩到 25 条以内）

### 2.2 修改文件（无）

* 不修改 `e2e/` 下任何文件（保证 mock E2E CI 零污染）

* 不修改 `apps/admin-web/`（红线：不碰业务代码）

* 不修改根 `playwright.config.ts`

### 2.3 根 `package.json` 可选改动（低优先级，若执行方便则加）

* 新增 script：`"e2e:real": "cd e2e-real && npx playwright test"`（可选；若不加则命令手敲）

* 如添加需在计划中标注；否则不在 CI/CD 中注册，纯手动触发

***

## 三、修改步骤（按执行顺序）

### Step 1：建 `e2e-real/` 基础框架（配置 + helper）

**操作**：

1. 新建 `e2e-real/` 目录
2. 写入 `playwright.config.ts`：

   * `testDir: './tests'`

   * `testMatch: '**/*.spec.ts'`

   * `fullyParallel: false`（真实接口串行，避免资源竞争）

   * `workers: 1`

   * `timeout: 120_000`；`expect.timeout: 15_000`

   * `reporter: [['list'], ['html', { open: 'never' }], ['json', { outputFile: 'test-results/result.json' }]]`（加 JSON 报告便于自报统计）

   * `use.baseURL: process.env.E2E_BASE_URL || 'http://localhost:2080/admin'`

   * `trace: 'retain-on-failure'`；`screenshot: 'on'`

   * **无** **`webServer`** **字段**（关键：不启动本地 vite，直连 staging）

   * `projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }]`
3. 写入 `real-helpers.ts`：

   * 从 `../e2e/admin-helpers` import 并 export：`adminRoutes / pickSelectOption / tableRows / menuItems / gotoMenu / adminMessage / topBarUserName`

   * 从 `../e2e/admin-helpers` import `adminLogout`（退出逻辑真实/mock 一致：顶栏「退出」按钮 + ElMessageBox 确认）

   * 新写 `realLogin(page, username?, password?)`：

     * goto `/login`

     * 定位用户名框：`.login-form input:not([type="password"])` 或更稳：按 label 找

     * 定位密码框：`.login-form input[type="password"]`

     * 定位登录按钮：`.login-form .el-button[type="primary"]` 或 `getByRole('button', { name: '登 录' })`（注意中间空格，对齐 T052 成功案例）

     * fill → click → waitForURL 非 `/login`

   * 导出工具：`uniqueName(prefix): string` → `\`${prefix}-${Date.now().toString().slice(-6)}\`\`（保证 6 位秒级时间戳唯一）

   * 导出 `E2E_PATIENT_NAME_PREFIX = 'T053测试'` / `E2E_TEAM_NAME_PREFIX = 'T053团队'` / `E2E_REPLY_PREFIX = 'T053回复'`

### Step 2：编写 01-login.spec.ts（4 条）

**用例设计**：

1. **登录页渲染**：goto /login → 验证用户名输入框 visible + 密码输入框 visible + 「登 录」按钮 visible + 标题含「矫智通运营平台」
2. **ops\_admin 登录成功**：填 ops\_admin/admin123 → 登录 → waitForURL /dashboard → 顶栏用户名非空（seed 中显示名可能是「运营管理员」或 ops\_admin 对应 name，用非空而非精确匹配）→ localStorage.getItem('admin\_token') truthy
3. **错误密码拒绝**：填 ops\_admin/wrongpass123 → 登录 → ElMessage 含「用户名或密码错误」 → URL 仍在 /login
4. **路由守卫与退出**：登录成功 → 退出（顶栏退出 + 确认）→ 回 /login → 访问 /dashboard → 被重定向到 /login 且 URL 含 redirect=

### Step 3：编写 02-dashboard.spec.ts（3 条）

**策略**：seed 聚合数据不固定（随 feed 写入变化），全部改为存在性 + 非空断言。

1. **KPI 卡片渲染**：登录后 goto /dashboard → `.kpi-card` 有 6 张 → 每张 label 是 6 个预期名称之一（累计患者/今日活跃佩戴/今日告警次数/平均佩戴时长/设备在线率/本月新增患者）→ `.kpi-value` 文本非空（含数字即可，不校验具体值）
2. **图表与排行渲染**：4 张图表标题存在 + 4 个 canvas 可见 + 团队佩戴达标排行表 ≥3 行（seed 有 3 团队）+ 医生管理患者排行表 ≥3 行
3. **周期切换不崩溃**：点「本周」→ 等待 2s → ElMessage 不含 error 字样 → 点「本月」→ 等待 2s → 页面仍在 /dashboard

### Step 4：编写 03-alerts.spec.ts（4 条）

1. **列表渲染**：登录 goto /alerts → 等待表格行 → `tableRows(page).count() ≥ 9`（seed 9 条）+ 首行列信息非空（告警类型/患者名/设备号/数值/状态）+ 分页组件含「共」字样（9 条或更多）
2. **类型筛选**：打开类型筛选下拉（.filter-select 第一个）→ 选任一存在的类型（如「压力偏高」）→ 表格行数变化（>0 即可，不要求精确 2 条）→ 每行都含选中的类型文案（用 filter/hasText 验证每行）
3. **状态筛选**：状态下拉（.filter-select 第二个）→ 选「待处理」→ 结果行每一行都含「待处理」tag → 再选「已处理」→ 每行都含「已处理」
4. **处理告警（写）**：找 1 条待处理行 → 点「处理」按钮 → 对话框出现 → textarea 填 `\`${E2E\_REPLY\_PREFIX}-${ts} 已联系患者调整佩戴\`\` → 点「确认处理」→ ElMessage 含「成功」→ 对话框关闭 → 该行状态从「待处理」变「已处理」或处理人字段出现

### Step 5：编写 04-monitor.spec.ts（3 条）

1. **页面默认渲染**：goto /monitor → `.realtime-tag` 含「实时同步中」→ `.patient-card .el-select` 有选中患者名 → `.update-time` 匹配 `/\d{2}:\d{2}:\d{2}/`（快照时间戳）→ `.status-indicator` 存在（佩戴中/未佩戴/异常任一）
2. **热力图 20 格**：`.hm-cell` count = 20 → `.hm-cell-id` count = 20，首尾 P01/P20 → `.hm-cell-val` 全部含数字 → `.hm-cell-max` count = 1 → `.hm-lg-item` count ≥ 3（图例项）
3. **患者切换**：打开患者下拉 → 选项数 ≥ 5（seed 5 患者）→ 选 1 名姓名存在的患者 → `.status-indicator` 更新或设备号更新（验证其中之一即可，不要求精确对应）→ 再切 1 名不同患者 → 时间戳有刷新

### Step 6：编写 05-patients.spec.ts（5 条）

1. **列表渲染**：goto /patients → `tableRows` count ≥ 5 → 首行列信息：患者 ID（PT- 前缀）+ 姓名 + 团队（3 个团队之一）+ 状态（活跃/未绑定）
2. **关键词搜索**：.search-input 填种子患者名（从列表读第一个患者名）→ 点查询 → 行数减少且每行都含该姓名 → 清空查询恢复 ≥5 行
3. **团队筛选**：团队下拉选任一团队名（从列表取第一个团队）→ 筛选后每行都含该团队名 → 清空恢复
4. **添加患者（写 + 可重放）**：

   * 点「添加患者」或「新建患者」按钮（注：如当前 staging UI 无此按钮 → 用例标 `test.fail` 并记录为问题）

   * 唯一姓名 `uniqueName('T053测试')` + 必填项（至少姓名 + 生日/诊断/联系方式 seed 要求的最小集，实际以 UI 表单为准）

   * 保存 → ElMessage 成功 → 回到列表搜索该姓名 → 行数 = 1 → 记录此患者名用于后续分配团队
5. **分配团队**：给第 4 步新建的患者（或列表中「待分配」患者）→ 行操作「分配团队」或「编辑」→ 选一个团队 → 保存 → ElMessage 成功 → 列表该行团队列显示所选名称

### Step 7：编写 06-teams.spec.ts（5 条）

1. **列表渲染**：goto /teams → `tableRows` count = 3（seed 3 团队）→ 列信息：团队名 + 负责人（3 医生之一）+ 成员数 ≥0 + 患者数 ≥0
2. **新建团队（写）**：

   * 点「新建团队」（如 UI 无按钮 → `test.fail` + 记问题）

   * 名称 `uniqueName('T053团队')` + 负责人下拉选 1 名医生 → 保存

   * ElMessage 成功 → 列表出现该名称（行 count = 4）→ 记录新建团队名
3. **编辑团队**：找到第 2 步新建的团队 → 点「编辑」（如无按钮 → test.fail）→ 名称末尾加「-改」→ 保存 → 成功 → 列表名称已更新
4. **删除引用团队（拒绝）**：找 seed 中 3 团队的第一个 → 点删除 → ElMessageBox 确认 → 如删除成功提示成功（如果后端不做引用拒绝则不算 bug，通过即可）→ 如拒绝则 ElMessage 含「引用」/「占用」/「无法删除」任一即可（两种结果都接受，只验证 UI 不崩溃）
5. **删除自建团队（成功）**：删除第 3 步已改名的自建团队 → 确认 → 成功提示 → 列表中名称消失（count 回到 3 或 4）

### Step 8：编写 07-communication.spec.ts（3 条）

1. **列表渲染**：goto /communication → `tableRows` count ≥ 17（feedbacks 可能含旧重复数据，≥17 即可）→ 每行有患者名 + 内容摘要 + 状态 tag → status tag 同时存在「待处理」和「已解决」两种（取所有 tagText 验证）
2. **详情对话框**：任选一行（第 0 行即可）→ 点「详情」→ ElDialog 显示「反馈」或「详情」标题 → 描述块（el-descriptions 或详情区）含 患者 / 类型 / 内容 / 提交时间 → 关闭对话框
3. **回复处理（写）**：找 1 行 pending 状态（含「待处理」tag）→ 点详情 → 回复输入框 textarea → 填 `uniqueName('T053回复')` → 点「回复并标记」或「提交」按钮 → ElMessage 含「成功」→ 对话框关闭 → 该行状态 tag 变更为「已回复」或「已解决」

### Step 9：数据清理（不单独建文件，合并到 teams 末尾）

**策略**：在 `06-teams.spec.ts` 最后加 1 条 describe.afterAll 或独立 test：

* 删除所有名称含 `T053团队-` 的团队（UI 删除或 API 直接调）

* 在 `05-patients.spec.ts` 最后删除所有名称含 `T053测试-` 的患者

* 如果 UI 无删除入口（患者删除未实现），则用 API：`page.request.delete(\`/api/v1/patients?name=...\`)\`（需带 JWT，从 localStorage 取 admin\_token 拼 header）

### Step 10：本地语法冒烟 + staging 实跑

**前置**：

* 开 SSH 隧道：`ssh -L 2080:localhost:81 ubuntu@106.52.39.208 -N`（如失败提示检查密钥 D:\env\ksd\_prod\_key.pem）

* 浏览器确认 `http://localhost:2080/admin/login` 能打开 + 手动登录 ops\_admin/admin123 成功

**执行命令**：

```bash
cd D:/proj/BraceSync/.worktrees/ella/e2e-real
# 首次如需要
npx playwright install chromium
# 运行
npx playwright test --config=playwright.config.ts
# 或根目录（如配好 npm script）
cd .. && npm run e2e:real
```

**收集产出**：

* `e2e-real/test-results/result.json`（通过/失败统计）

* `e2e-real/playwright-report/index.html`（HTML 报告）

* 失败用例的 trace.zip + screenshot（自动保存在 test-results/）

**自报问题清单**（每项 6 字段）：

| 编号  | 模块 | 用例标题 | 操作步骤 | 预期结果 | 实际结果 | 严重度（P0/P1/P2） |
| --- | -- | ---- | ---- | ---- | ---- | ------------- |
| ... | -  | -    | -    | -    | -    | -             |

### Step 11：提交推送 + 建 PR

**红线**：提交前 `git status --short` 只包含 `e2e-real/` 新文件 + `.trae/documents/`；严禁混入 `apps/` `e2e/` `services/` 变更；严禁提交 test-results 截图 trace（这些在 .gitignore 中，如未覆盖需手动 add -f 排除）。

```bash
git add e2e-real/ .trae/documents/T053-real-e2e_plan.md
git commit -m "test(ella/T053): 新增 staging 真实模式 E2E 框架 + 5 模块 25 条核心链路用例

- 新建 e2e-real/ 独立目录 + playwright.config.ts（无 webServer，baseURL 指向 localhost:2080/admin 隧道）
- 新建 real-helpers.ts：重写真实登录（用户名/密码）+ 复用 admin-helpers selector；导出 uniqueName/前缀常量
- 8 个 spec（login/dashboard/alerts/monitor/patients/teams/communication + 清理合并）共约 25 条
- 写操作使用唯一命名前缀 T053测试- / T053团队- / T053回复-，避免污染 seed 基线
- 范围：运营后台 Web 5 模块（告警/监控/患者/团队/沟通 + Dashboard + 登录）
- 范围外：技师端/患者端小程序（排 T054）
- 执行环境：staging 隧道 + ops_admin 账号 + T051 seed 数据"
git push origin feat/ella-T053-real-e2e
# 建 PR（ready 状态）
gh pr create --base main --head feat/ella-T053-real-e2e \
  --title "Ella T053: staging 真实模式 E2E（5 模块 ~25 条核心链路）" \
  --body "$(cat docs/tasks/ella/T053-pr-body.md 2>/dev/null || echo "详见 T053-real-e2e.md 任务规格 + plan.md")"
```

***

## 四、潜在依赖与注意事项

### 4.1 staging 连通性依赖

| 依赖            | 检查方法                                                                      | 失败处理                                             |
| ------------- | ------------------------------------------------------------------------- | ------------------------------------------------ |
| SSH 隧道可达      | 手动 `ssh 106.52.39.208 echo ok` + 浏览器开 <http://localhost:2080/admin/login> | 报告 PM：staging 不可达；**本地语法冒烟用例先写完推送**，staging 实跑延后 |
| 7 服务 healthy  | 服务器内 `curl -s http://localhost:81/health` 或部署面板                           | 未 healthy 时：写好用例推送，实跑延后，自报注明 "staging 服务未就绪"     |
| T051 seed 已执行 | API 查 `GET /api/v1/patients` 返回 ≥5                                        | seed 缺失时：报告 PM/Boss 安排 Andy 执行 seed              |

### 4.2 真实接口差异带来的断言调整

* **精确值断言禁止**：KPI 数值、列表行数（feedbacks 重复执行导致可能 >17）改为「≥N」或「非空」

* **写操作部分接口未实现风险**：

  * 患者管理写功能（T057/T058）、团队写（T059）、沟通处理（T061）这些任务可能已修复部分但未完全闭环

  * **策略**：如果 staging UI 中找不到「新建团队」「添加患者」「回复并标记」等按钮 → 对应 test 标记为 `test.fixme(...)`（非 fail，不阻塞整体报告），并在问题清单中记录为「功能未上线/未实现」

* **并发问题**：workers=1 串行执行，避免真实数据竞争（同一告警处理两次、同一团队删两次）

### 4.3 数据污染防范（红线）

* **所有写操作资源前缀唯一**：`T053测试- / T053团队- / T053回复-` + 时间戳后缀

* **清理步骤必做**：teams/patients spec 末尾删除新建资源；如 UI 删除入口缺失则用 `page.request` 调 REST API 删除（Authorization: Bearer \<admin\_token>）

* **绝对禁止操作 seed 中已有团队/患者**（如「脊柱侧弯一组」「骨科一组」等已有固定名资源）→ 团队删除仅删除自建团队；引用拒绝用例可以操作已有团队点一下（但不确认删除，或确认后接受拒绝/成功两种结果）

### 4.4 依赖的 selector 稳定性

* 复用 `admin-helpers.ts` 中 selector 策略：Element Plus 类名 + 文案定位

* 登录页 selector 需特别注意：**真实模式没有角色下拉**，如果 helper 不小心 import 了 mock 登录会直接挂

* `real-helpers.ts` 顶部注释明确：`⚠️ 本文件为真实模式专用，禁止使用 mock 角色下拉登录`

***

## 五、风险与处理

| 风险项                                        | 概率 | 影响         | 处理策略                                                                                                                 |
| ------------------------------------------ | -- | ---------- | -------------------------------------------------------------------------------------------------------------------- |
| staging 网络不通/服务挂（无法实跑）                     | 中  | 无法验证通过/失败  | ① 完成代码 + 本地 `npx playwright test --list` 语法冒烟通过 → ② 推送建 PR → ③ 自报注明「本地语法冒烟通过 + staging 实跑待环境恢复」                      |
| 部分模块写功能在 staging 未部署（T057/T059/T061 修复未合入） | 高  | 对应写用例找不到按钮 | 写用例统一用 `test.fixme()` 标记（区别于 test.fail：fixme = 预期暂不过，CI/报告中不计入失败），问题清单注明「功能未部署」                                      |
| seed 数据与预期不一致（患者名/团队名不同）                   | 中  | 硬编码姓名断言失败  | 所有姓名/团队名断言改为「从列表读第一个存在的名字」+ 动态用该值搜索/筛选，禁止硬编码 seed 特定名字；写操作全部用自己新建的唯一命名资源                                             |
| Playwright 环境缺 chromium                    | 低  | 无法启动浏览器    | `npx playwright install chromium` 执行；若服务器端无头环境缺依赖则用 `playwright install-deps`                                        |
| localStorage token 名称与 admin-helpers 不一致   | 低  | 登录后刷新重登    | 从 T052 staging 实跑经验：key 为 `admin_token` + `admin_user`；如果不一致用 DevTools 查 Application → localStorage 实际 key，再改 helper |

***

## 六、产物交付清单

| 产物                  | 路径/形式                                                              | 验收标准                                                                                                                           |
| ------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| 真实 E2E 配置           | `e2e-real/playwright.config.ts`                                    | 无 webServer 字段；baseURL 默认 `http://localhost:2080/admin`；env 可覆盖；串行执行                                                           |
| 真实模式 helper         | `e2e-real/real-helpers.ts`                                         | 有 `realLogin()`（用户名/密码登录）；re-export 了 admin-helpers 的 selector；有 uniqueName + 前缀常量                                             |
| 8 个 spec 文件         | `e2e-real/tests/01-08*.spec.ts`                                    | 共约 25 条用例；登录/Dashboard/告警/监控/患者/团队/沟通 7 模块覆盖 + 清理；无精确数值断言；写操作使用唯一命名                                                            |
| 本地语法冒烟报告            | 命令截图或退出码 0                                                         | `npx playwright test --list` 成功列出全部 spec；`npx playwright test --project=chromium --reporter=list`（实跑前可用 `--grep "不存在"` 验证解析成功） |
| staging 实跑报告（如环境可达） | `e2e-real/playwright-report/index.html` + test-results/result.json | 报告中通过 X / 失败 Y 清楚；失败用例有截图 + trace                                                                                              |
| 问题清单（如失败或 fixme）    | PR body 或 docs/tasks/ella/T053-问题清单.md                             | 每条 6 字段：编号/页面/操作/预期/实际/严重度                                                                                                     |
| 提交推送                | commit hash + 分支名                                                  | `git log --oneline -1` 显示 commit；`git ls-remote origin feat/ella-T053-real-e2e` 存在                                             |
| PR                  | gh ready 状态 PR                                                     | PR 链接可访问；base=main；title 含 "Ella T053"；body 含用例清单 + 执行结果摘要                                                                     |

***

## 七、执行退出标准（成功 / 部分成功判定）

### 成功（Full Pass）

* ✅ 工作区合规 + 分支 `feat/ella-T053-real-e2e` 基于 d5841d3 创建

* ✅ e2e-real/ 框架文件齐全（config + helpers + 8 specs）

* ✅ staging 实跑 ≥80% 用例通过（如：25 条中 ≥20 条通过）

* ✅ 失败/未实现项在问题清单完整记录

* ✅ commit 推送 + PR 创建（ready 状态）

* ✅ 自报数据齐全：分支名/commit hash/PR 链接/用例清单/通过 X 失败 Y/问题明细

### 部分成功（Partial Pass）

* ✅ 工作区合规 + 分支 + 框架 + specs 齐全

* ⚠️ staging 不可达或服务未就绪 → 本地语法冒烟通过 + 问题清单注明环境阻塞

* ✅ 推送 + PR + 自报如实写明「staging 待实跑」不写通过

