# T057 患者管理功能补全 — 测试预置（KNOWN_RED）执行计划

> 负责人：Ella（测试预置）｜ 分支：`feat/ella-T057-patient-tests` ｜ 模式：TDD 红（只预置测试，不写实现）
> 任务文档：`D:/proj/BraceSync-docs/docs/tasks/ella/T057-患者管理测试预置.md`
> 设计源：`D:/proj/BraceSync-docs/docs/design/admin/患者管理.html`

---

## 一、Summary（摘要）

为患者管理页 3 个缺失的写功能（添加患者 / 管理团队 / 批量绑定）预置 KNOWN_RED 测试，定义 3 个新端点契约。产出：1 份测试规格文档 + 后端 Go 契约测试（stub 骨架保证编译，断言失败为红）+ 前端 E2E 用例（test.fail 预期失败）。不写任何业务实现。

## 二、Current State Analysis（现状分析，Phase 1 探索结论）

### Worktree 健康（已验证）
- `git worktree list`：`.worktrees/ella` 在册，detached `52b4052`（= 远程 main SHA `52b405244d9ce19bf1dcde48f6fa6fbd4469d60f`）
- 4 个 T052 遗留未跟踪文件（`.trae/`、`acceptance_test.ps1`、`e2e/t052-staging.config.ts`、`e2e/tests/t052-task2-staging.spec.ts`）——保留不动
- 无 refs 陈旧坑（`git rev-parse origin/main` 正常返回）

### 后端现状（user-service）
- `services/user-service/internal/handler/handler.go`：`Router()` 注册了患者 GET（`listPatients`/`getPatient`），**无任何写端点**
- `services/user-service/internal/repo/store.go`：`Store` 接口患者域仅 `ListPatients`/`GetPatient`（只读）
- `services/user-service/internal/model/model.go`：有 `AdminPatientDTO`、错误码 `CodeInvalidParam=10400/CodeNotFound=10404/CodeConflict=10409/CodeInternal=90001`、`AppError`+`ErrInvalidParam/ErrNotFound/ErrConflict/ErrInternal`
- gateway `services/gateway/cmd/server/proxy_admin.go`：`userServiceRoutes` 仅 `GET /admin/patients`、`GET /admin/patients/:patientId`，3 个写端点未代理（属实现方下阶段，Ella 不碰）
- **测试模式**：`handler_impl_test.go` 用 `fakeStore`（实现 `repo.Store`）+ `testEnv{do()}` + httptest；`rbac_test.go` 用 `t.Log("KNOWN_RED: ...")` + 失败断言实现预期红（stub 返回 nil → 断言失败）
- **命名约定**：实现侧 = `*_impl_test.go`；测试专家（Ella）= `*_test.go`（无 `_impl`，不重叠）

### 前端现状（admin-web）
- `apps/admin-web/src/pages/patients/index.vue`（145 行）：仅列表 + 搜索 + 团队筛选 + 详情抽屉，**无添加/管理团队/批量绑定 UI**
- `apps/admin-web/src/api/index.ts`：仅有 `fetchPatients`/`fetchPatientDetail`，无写 API
- `apps/admin-web/src/mock/patients.ts`：6 名 mock 患者（PT-001~PT-006），无写 mock
- **E2E 模式**：`e2e/tests/admin-patients.spec.ts` 用 `adminLogin` + `tableRows` + Element Plus 类名定位；`admin-playwright.config.ts` 启 dev server（5175 端口，USE_MOCK=true）；`test.skip` 既有先例（`tech-bind.spec.ts`）

### 设计源 3 个写功能（患者管理.html）
1. **+ 添加患者**（顶栏按钮 → 表单：姓名/性别/年龄/诊断等）
2. **管理团队**（每行"管理团队/分配团队"按钮 → 弹窗 WB-08：当前团队 + 更改为 + 确认绑定）
3. **批量患者-团队绑定**（独立 card WB-09：多选患者 + 分配至团队 + 确认分配）

## 三、Proposed Changes（变更清单）

### 阶段 0：建分支（worktree 纪律）
```bash
cd D:/proj/BraceSync/.worktrees/ella
git fetch origin --prune
git checkout -b feat/ella-T057-patient-tests 52b405244d9ce19bf1dcde48f6fa6fbd4469d60f
git status --short   # 须干净（除 4 个 T052 遗留未跟踪文件）
```
> 用 SHA 而非 `origin/main`（规避 refs 陈旧坑）。

### 阶段 1：测试规格文档
**新建** `docs/tasks/ella/T057-患者管理测试规格.md`（worktree 内新建 `docs/tasks/ella/` 目录树）
- 3 个新端点契约（请求/响应字段、校验规则、幂等、权限、错误码）
- 前端交互验收标准
- 内容要点见下方「契约定义」节

### 阶段 2：后端 KNOWN_RED（stub 骨架 + 断言红）

#### 2.1 契约定义（stub 骨架，不实现业务逻辑）

**`services/user-service/internal/repo/store.go`**（扩展 Store 接口 + 新类型）
- 新增入参/出参类型：
  - `PatientInput struct { Name string; Gender *string; Age *int; Diagnosis *string; CobbAngle *float64; DeviceID *string; TeamID *string; DoctorID *string }`
  - `BatchBindResult struct { Success []string; Failed []BatchBindFailure }`
  - `BatchBindFailure struct { PatientID string; Reason string }`
- `Store` 接口新增 3 方法：
  - `CreatePatient(ctx, in PatientInput) (*PatientRow, error)`
  - `AssignPatientTeam(ctx, patientID, teamID string) (*PatientRow, error)`
  - `BatchBindPatients(ctx, patientIDs []string, teamID string) (*BatchBindResult, error)`

**`services/user-service/internal/repo/pg.go`**（PGStore stub，保证编译）
- 新增 3 方法 stub：均 `return nil, errors.New("not implemented: T057")`（或 `return nil`/`false` 零值）——运行期不被 Ella 测试触达

**`services/user-service/internal/model/model.go`**（新增请求/响应 DTO）
- `CreatePatientRequestDTO struct { Name string; Gender *string; Age *int; Diagnosis *string; CobbAngle *float64; DeviceID *string; TeamID *string; DoctorID *string }`
- `AssignTeamRequestDTO struct { TeamID string }`
- `BatchBindRequestDTO struct { PatientIDs []string; TeamID string }`
- `BatchBindResultDTO struct { SuccessCount int; FailedCount int; Failures []BatchBindFailureDTO }`
- `BatchBindFailureDTO struct { PatientID string; Reason string }`
- 响应复用 `AdminPatientDTO`（创建/分配返回单条患者）

**`services/user-service/internal/handler/handler.go`**（注册 3 路由 + stub handler）
- `Router()` 新增：
  - `v1.POST("/admin/patients", h.createPatient)`
  - `v1.PUT("/admin/patients/:patientId/team", h.assignPatientTeam)`
  - `v1.POST("/admin/patients/batch-bind", h.batchBindPatients)`
- 3 个 stub handler：直接 `fail(c, model.ErrInternal("not implemented: T057"))`——不调用 store、不含校验/业务逻辑
  > 注：stub 返回 500 → Ella 测试断言 200 → 断言失败 = 预期红

**`services/user-service/internal/handler/handler_impl_test.go`**（fakeStore 补 stub）
- `fakeStore` 结构体新增字段：`createdPatient *repo.PatientRow; createPatientErr error; assignedPatient *repo.PatientRow; assignPatientErr error; batchBindResult *repo.BatchBindResult; batchBindErr error`
- 实现 3 个新接口方法（返回上述字段零值）——仅满足接口编译，不被 stub handler 调用

#### 2.2 Ella 的 KNOWN_RED 测试文件
**新建** `services/user-service/internal/handler/patient_writes_test.go`（`package handler`，复用 `fakeStore`/`testEnv`/`do()`）

3 个 `Test*` 函数，每个用 `t.Log("KNOWN_RED: ...")` 标注 + 断言失败（stub 返回 500，断言期望 200）：

1. **`TestCreatePatient`**（创建患者契约 + 业务规则）
   - 成功：合法 body → 断言 `200` + `resp.Data` 含 `patientId`（stub 500 → 红）
   - 校验红：name 空 → `400/CodeInvalidParam`（stub 500，断言 400 → 红；待实现后转绿）
   - 校验红：gender 非法 / age 越界 / cobbAngle 越界 → 400
   - 团队不存在：teamId=NOPE → 400
   - 重复创建：同 name+age+diagnosis 二次提交 → 409/CodeConflict
   - store 错误 → 500
   - 非法 JSON body → 400

2. **`TestAssignPatientTeam`**（分配团队契约 + 幂等）
   - 成功：patientId 存在 + teamId 存在 → 200 + 返回更新后 `AdminPatientDTO`（teamId 变更）
   - 患者不存在 → 404/CodeNotFound
   - 团队不存在 → 400/CodeInvalidParam
   - teamId 空 → 400
   - 幂等：分配到已绑定的同一 teamId → 200（no-op，返回当前）
   - store 错误 → 500

3. **`TestBatchBindPatients`**（批量绑定 + 部分失败）
   - 全部成功：3 个存在患者 + 合法 teamId → 200，`successCount=3, failedCount=0`
   - 部分失败：3 个中 1 个 patientId 不存在 → 200，`successCount=2, failedCount=1, failures[0].reason="patient not found"`
   - 空列表 patientIds=[] → 400/CodeInvalidParam
   - teamId 空 / 不存在 → 400
   - store 错误 → 500

> 预期红态说明：stub handler 全返回 `ErrInternal(500)`，所有断言 `200/400/404/409` 均失败 → `go test` 报失败测试。实现方转绿时：填 handler 业务逻辑 + fakeStore 配置返回值 → 断言通过。

### 阶段 3：前端 E2E KNOWN_RED（test.fail 预期失败）

**新建** `e2e/tests/admin-patient-writes.spec.ts`（admin-*.spec.ts 风格）

```ts
import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, tableRows, pickSelectOption } from '../admin-helpers'

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.patients)
})

// KNOWN_RED: 前端 patients/index.vue 当前无写操作 UI，以下用例预期失败；
// 实现方补"添加患者"弹窗 + mock 后转绿，届时移除 test.fail 标记。
```

3 个 `test.describe` + `test.fail`：

1. **添加患者流程**
   - `test.fail('点击+添加患者打开弹窗并提交', ...)`：点击顶栏「+ 添加患者」按钮 → 弹窗可见 → 填姓名/性别/年龄/诊断 → 确认 → 列表新增一行 + ElMessage 成功提示
   - `test.fail('姓名为空时禁用提交或提示校验', ...)`：提交空姓名 → 表单校验拦截

2. **分配团队流程**
   - `test.fail('点击管理团队打开绑定弹窗并更改团队', ...)`：行内「管理团队」按钮 → 弹窗 WB-08 可见 → 显示当前团队 → 选择新团队 → 确认绑定 → 行团队列更新
   - `test.fail('未分配患者显示分配团队按钮', ...)`：status=pending 患者行显示「分配团队」

3. **批量绑定流程**
   - `test.fail('批量选择患者并分配至团队', ...)`：批量绑定 card 可见 → 勾选 2 名患者 → 选择团队 → 确认分配 → 成功提示 + 患者团队更新

> 预期红态：mock 模式下前端无写 UI（按钮/弹窗不存在）→ 首个定位 `expect(...).toBeVisible()` 失败 → `test.fail` 标记为预期失败 → CI 绿。实现方补 UI + mock 后用例通过 → `test.fail` 报"意外通过" → 提示移除标记。

### 阶段 4：提交推送
```bash
cd D:/proj/BraceSync/.worktrees/ella
git add docs/tasks/ella/T057-患者管理测试规格.md \
  services/user-service/internal/repo/store.go \
  services/user-service/internal/repo/pg.go \
  services/user-service/internal/model/model.go \
  services/user-service/internal/handler/handler.go \
  services/user-service/internal/handler/handler_impl_test.go \
  services/user-service/internal/handler/patient_writes_test.go \
  e2e/tests/admin-patient-writes.spec.ts
git commit -m "test(T057): 预置患者管理写功能 KNOWN_RED 测试（创建/分配/批量绑定）"
git push -u origin feat/ella-T057-patient-tests
```

## 四、契约定义（写入测试规格文档的核心）

| 端点 | 方法 | 请求 | 响应 | 校验/幂等 |
|---|---|---|---|---|
| 创建患者 | `POST /api/v1/admin/patients` | `{name*, gender?, age?, diagnosis?, cobbAngle?, deviceId?, teamId?, doctorId?}` | `AdminPatientDTO`（含系统生成 patientId） | name 非空(400)；gender∈{male,female}；age∈[0,150]；cobbAngle∈[0,180]；teamId 存在性(400)；**重复(name+age+diagnosis 完全相同)→409**；系统生成 patientId（如 P20260xxx） |
| 分配团队 | `PUT /api/v1/admin/patients/:patientId/team` | `{teamId*}` | `AdminPatientDTO`（更新后） | patientId 不存在→404；teamId 空/不存在→400；**幂等：分配到当前已绑定 teamId→200 no-op** |
| 批量绑定 | `POST /api/v1/admin/patients/batch-bind` | `{patientIds: string[], teamId*}` | `{successCount, failedCount, failures:[{patientId, reason}]}` | patientIds 空→400；teamId 空/不存在→400；**部分失败：不存在患者计入 failures（不整体回滚），HTTP 仍 200** |

权限：3 端点均 admin 域，gateway JWT+RBAC（ROLE_ADMIN；ROLE_DOCTOR 限本团队——实现方细化，Ella 测试仅 admin 角色）。
错误码：复用 `model.CodeInvalidParam(10400)/CodeNotFound(10404)/CodeConflict(10409)/CodeInternal(90001)`。

## 五、Assumptions & Decisions（假设与决策）

1. **后端红态机制**：Stub 骨架 + 断言红（用户确认）。扩展 `repo.Store` 接口 = 契约定义；pg.go/fakeStore 补 stub 保编译；handler stub 返回 500 → 断言失败为红。
2. **E2E 红态机制**：`test.fail` 预期失败（用户确认）。mock 模式下无 UI → 定位失败 → test.fail → CI 绿。
3. **测试文件命名**：Ella 文件用 `*_test.go`（非 `*_impl_test.go`），遵循"测试专家不与实现侧重叠"约定。
4. **后端测试层**：放 `handler` 包（HTTP 契约+业务规则属 handler 层），复用 `handler_impl_test.go` 的 `fakeStore`/`testEnv`。
5. **gateway 不改**：handler 测试经 `Handler.Router()` 直连 user-service，绕过 gateway。gateway 代理路由注册属实现方下阶段。
6. **E2E 不碰后端**：USE_MOCK=true，仅测前端 UI；写 mock 由实现方补。
7. **创建患者幂等键**：设计表单无手机号，用 `name+age+diagnosis` 完全相同判重 → 409。**此为待 Boss 评审的决策点**（规格文档标注）。
8. **患者手机号不在本范围**：设计"添加患者"表单无手机号字段；患者登录手机号绑定属后续（规格文档标注）。
9. **范围排除**：设备换绑 / 分配医生（设计有但 PM 判后续）——规格文档标注，本阶段不做。

## 六、Verification（验证步骤）

- [ ] `git status` 干净（除 4 个 T052 遗留未跟踪文件）
- [ ] `cd services/user-service && go build ./...` 编译通过（stub 骨架保证）
- [ ] `go test ./internal/handler/ -run 'TestCreatePatient|TestAssignPatientTeam|TestBatchBindPatients' -v` → **失败**（KNOWN_RED 预期），失败信息含 `KNOWN_RED:` 日志
- [ ] `go test ./internal/handler/ -run 'TestListPatients|TestGetPatient'` → 既有用例仍通过（未破坏）
- [ ] `cd e2e && npx playwright test --config admin-playwright.config.ts admin-patient-writes` → 用例 **预期失败**（test.fail 标记，CI 绿）
- [ ] `go vet ./...` 无新增告警
- [ ] 测试规格文档可读、契约完整、3 写功能覆盖

## 七、自报字段（提交后回报 PM）

- 分支名：`feat/ella-T057-patient-tests`
- commit hash：`<push 后填>`
- 测试规格文档路径：`docs/tasks/ella/T057-患者管理测试规格.md`
- KNOWN_RED 清单：
  - 后端 3 个：`TestCreatePatient` / `TestAssignPatientTeam` / `TestBatchBindPatients`（`services/user-service/internal/handler/patient_writes_test.go`）
  - E2E 7 个：添加患者×2 / 分配团队×2 / 批量绑定×3（`e2e/tests/admin-patient-writes.spec.ts`，test.fail 标记）
- 预期红态：后端 stub handler 返回 500 → 断言 200/400/404/409 失败；E2E 无写 UI → 定位失败（test.fail 预期失败）
