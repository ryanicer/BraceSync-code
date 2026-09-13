# T173 · 患者端监测数据校准 — 设计文档

> 状态：**待 PM/Boss 评审**（评审通过前不改任何代码）
> 执行人：Winner｜分支：`t173-calibration`｜派发时间：2026-09-13 01:05
> 本文只覆盖设计。所有结论均为本 worktree（HEAD `ca9136d`）实测，逐条给 `文件:行号`。

---

## 0. 结论先行（定性）

**0.1 卡片"本仓当前无任何校准逻辑"不成立 —— 本仓已有完整的「采集 + 落库」半条链路，缺的只有「读取 + 应用」半条。**

已存在并可用：技师端 BLE 采 5 帧取均值 → `POST /api/v1/baselines` → `baselines.offset_values REAL[]`（长度 CHECK=20，FK 完整，事务写入）。
确实缺失：全仓没有任何一处 `SELECT offset_values` 的生产读路径（唯一的读在测试里：`services/device-service/internal/repo/integration_test.go:333`）。`services/data-service` 整服务对 `baseline` / `calibrat` / `offset_values` **零引用**。

→ 本卡性质应重新定性为：**补齐读取侧 + 收口双端口径**，不是从零建校准功能。这会显著缩小改动面，也改变了"复用现有 Baseline 模型"这句指令的含义：要复用的是一张**有活着的写入方、有约束、有 UNIQUE 语义的真表**，不是占位骨架。

**0.2 卡片"技师端此前校准后显示趋近 0"不能作为"技师端已校准"的证据 —— 那是硬编码模板。**

`apps/tech-miniapp/src/pages/install/index.vue:113-117`：校准完成页的 20 格矩阵是 `v-for="i in 20"` 渲染的字面量 `<text>0.00</text>`。其下三项 ✓ 校验（`index.vue:118-121`）由 `:298-302` 无条件写死 `checks: { pointCount: true, range: true, stability: true }`，注释自陈"3 项校验（mock/模拟都通过）"。**设备加 500 N 载荷，这一页照样显示全 0。**

→ 推论：两端不是"一个校准了、一个没校准"，而是**两端都没有应用校准**。真实缺陷比卡片描述的更靠前一步。

**0.3 最关键的疑点：108–119 这个量级更像「量纲」问题，而非「零点偏移」问题。若如此，卡片验收标准 ②（患者端无压力时显示趋近 0）用"减偏移"是达不成的。** 详见 §2 与 §8 阶段 0。必须先取证定量纲，再谈校准。

**0.4 单位语义目前在本仓有三处互相矛盾的断言，且唯一的权威文档不在仓内。** `services/data-service/internal/model/model.go:108` 引用 `device-protocol.md §4.1`，但**本仓不存在任何 protocol 文档**（`find . -iname "*protocol*"` 为空）。→ 这是实现的前置阻断项，不是收尾的文案工作。

---

## 1. 现状实测（带证据）

### 1.1 写入链路：完整、在用

| 环节 | 位置 |
|---|---|
| BLE 收帧（20×uint16 小端，注释称 值 = N×100） | `apps/tech-miniapp/src/utils/ble.ts:611-618` |
| 5 帧逐点取均值 = 20 点 offset（**全仓唯一的偏移计算**） | `apps/tech-miniapp/src/pages/install/index.vue:291-296` |
| 提交 | `apps/tech-miniapp/src/api/baseline.ts:20-24` → `POST /api/v1/baselines` |
| 网关白名单 | `services/gateway/cmd/server/proxy_services.go:38` |
| handler | `services/device-service/internal/handler/handler.go:73`, `:389-421` |
| 服务层校验（仅长度=20、非空，**不校验数值范围**） | `services/device-service/internal/service/device.go:312-330` |
| 事务落库 | `services/device-service/internal/repo/repo.go:419-462`（`:445` INSERT、`:454` 回填 `baseline_id` + `calibrate_time=now()`） |

`apps/tech-miniapp/src/api/baseline.ts:4,15` 两处注释写着"后端 T084 未实现 → mock 先行"，**与上述实现矛盾，是过期注释**（mock 仅 dev 生效，`.env.production` / `.env.staging` 均 `VITE_USE_MOCK=false`）。建议随手清理，但不属本卡范围。

### 1.2 卡片"BaselineID/CalibrateTime 是设备元数据、未被消费"两点均不成立

- **归属错**：这两列在 `install_records`，不在 `devices`。`model.Device`（`services/device-service/internal/model/model.go:131-145`）无此二字段；`devices` DDL（`scripts/db/migrations/000001_init_schema.up.sql:91-105`）无此二列。
- **"未被消费"错**：二者写方 3 处（`repo.go:391` INSERT、`repo.go:454` UPDATE、`seed.sql:185/200/215`），读方含逻辑判定与三张 DTO：`repo.go:406`、`repo/query.go:110,123`、`handler/handler.go:382-385`、`handler/query.go:151-154`；UI 读者 `apps/tech-miniapp/src/pages/records/index.vue:57`（`rec.baselineId || '未保存'`）、`apps/admin-web/src/pages/install-records/index.vue:30`；**逻辑读者** `repo.go:438-440`（1:1 冲突判定）。
- **真正没人消费的只有 `baselines.offset_values` 这一列本身**，以及 TS 侧 `Baseline` 接口（`packages/shared-types/src/index.ts:150-157`，唯一"引用"是 `apps/tech-miniapp/src/stores/install.ts:3` 一个未使用的 import）与 Go 侧 `model.Baseline` 结构体（生产代码从不构造，仅 `testutil/fakestore.go:272` 用）。

→ 对本卡的意义：**不需要为"字段没落库"做任何事**；要做的是"字段落了库但没人读"。

### 1.3 患者端读数路径：确认透传，无任何算术

`services/data-service/internal/model/model.go:234-248` `BuildSensorPoints` —— 值仅 `float64(v)` 加宽（`:243`），无 scale / offset / clamp / 平均。调用者只有两处：`PressureRecord.ToDTO()`（`model.go:335`）与 Redis 兜底路径（`service/record.go:519`）。
兄弟函数 `BuildHeatmap`（`model.go:264-285`）同样透传，喂给同一响应的 `pressureHeatmap` 字段，且在**原始值**上选 `IsMax`（`:277-283`）。

患者端展示层唯一变换是 `toFixed(2)`：`apps/patient-miniapp/src/pages/monitor/index.vue:97-99`（hero）+ `:30` 单位标签 `N`。趋势曲线另有一处 `.filter(x => x.value > 0)`（`:144`）—— 注意它对 108–119 这种未校准值**永远不会过滤掉任何东西**。

### 1.4 两端根本不是同一条数据链路（"口径不一致"的真实成因）

| | 患者端 | 技师端 |
|---|---|---|
| 传输 | HTTP `GET /api/v1/patients/:id/realtime`（`monitor/index.vue:163`） | **BLE GATT B513 Notify**（`ble.ts:589-620`），不经后端 |
| 线上格式 | JSON float | 40 字节 / 20×int16 定点 |
| 客户端缩放 | **无** | **÷ 100**（`ble.ts:617`） |
| 应用校准 | 无 | offset 算了但不减（显示写死 0.00，见 §0.2） |
| 刷新 | 仅 `onMounted` + 下拉刷新（无轮询、无长连接，两小程序均无 `connectSocket`） | 采集期 1 Hz |

技师端 API 面全量只有 `/baselines`、`/devices/:id/bind`、`/devices/:id/wifi`、`/install-records[/:id]`、`/admin/patients/:id`、`/devices/:id/provision-key` —— **它从后端读过一次压力值都没有**。
→ "技师端显示趋近 0、患者端显示 113"这两件事之间**没有因果关系**，不是同一条链路的两个视图。

（附带：患者端页面标题"实时监测"名不副实，无轮询。属体验问题，不在本卡范围，另卡。）

### 1.5 顺带确认的一个独立缺陷（本卡范围外，建议单开卡）

设备模拟器**上报不到后端**，三重不匹配：
1. 路径：模拟器发 `/api/v1/device/report`（`scripts/dev/device-simulator/cmd/simulator.go:151`）与 `/report/batch`（`main.go:80`），而注册路由是 `/api/v1/device/records`、`/records/batch`（`services/data-service/internal/handler/handler.go:63-64`、`gateway/cmd/server/proxy_services.go:43-44`）→ 404。
2. 字段：模拟器发 `pressures`（`simulator.go:20`），后端 DTO 只认 `points`（`services/data-service/internal/model/model.go:117`）→ `len(Points)!=20` 直接 400（`service/record.go:121-123`）。
3. 单位：模拟器标 `kPa`，后端标 `N`。

→ 用途影响：**这条链路测试阶段如果用过模拟器，那批"成功上报"是假的**；同时也反证 §2 的 108–119 不来自模拟器（其 fault 模式 `80+rand(40)` = 80–119 虽与观测同带，但它进不来）。种子数据也不是来源（`scripts/db/seed/seed.sql:116` 的 p01–p20 是 12–47 带）。**108–119 最可能来自真实设备直传或库内手工写入 —— 请 PM/Boss 指认取证来源，这决定 §2 的判据怎么跑。**

---

## 2. 单位语义澄清（对应卡片 ②第 4 条）—— 前置阻断项

### 2.1 仓内三处矛盾断言

| 位置 | 断言 |
|---|---|
| `services/data-service/internal/model/model.go:117` | HTTP 上报 `points` 「单位 N」，引 `device-protocol.md §4.1` —— **该文档不在本仓** |
| `apps/tech-miniapp/src/utils/ble.ts:572-573` | BLE `uint16 = N×100`（定点，精度 0.01 N），÷100 后才是 N |
| `scripts/dev/device-simulator/cmd/simulator.go:20` | 上报 `pressures` 「单位 kPa」 |

所有阈值都按 N 写死：佩戴 0.5（`data-service/internal/model/model.go:29`，`alert-service/internal/consumer/consumer.go:45` 各存一份）、漂移 2.8（`alert-service/internal/engine/engine.go:20`）、偏高 45（`engine.go:17`，展示带 `model.go:206-208` 的 33.75/45.0）、热力图上界 60（`model.go:251`）、患者端曲线上界 75（`apps/patient-miniapp/src/pages/monitor/index.vue:60`）。
"113"这个读数同时**超过所有告警阈值与全部展示上界**：物理上，无载的支具不太可能有 113 N；工程上，若真是 113 N，患者端应当在刷屏告警（见 §2.3，此点待验）。

### 2.2 两种解释及其完全不同的处置

- **H1（缺一次缩放）**：固件 HTTP 上报与 BLE 同为 N×100 定点，而入库路径照原值存（`service/record.go:663-669` `toPendingFrame` 仅 float64→float32 窄化，无缩放）。则 108–119 ⇒ 真实 **1.08–1.19 N**。→ 处置：**入口 ÷100（量纲归一）**，之后才轮到减零点偏移。
- **H2（读数即 N）**：113 N 是真的过大 ⇒ 传感器/贴合/标定异常，**校准无法修复**（零点偏移只能平移，不能缩小量程），需固件方介入。

**判据（阶段 0，可执行、一次定论）**：同一台设备、同一空载状态，
1. 抓一条 HTTP `points` 原始值 `v_http`；
2. 同时 BLE 抓一帧原始 int16 `v_raw`，算 `v_raw/100`；
3. `v_http ≈ v_raw` ⇒ H1 成立（HTTP 缺 ÷100）；`v_http ≈ v_raw/100` ⇒ H2 成立（HTTP 已是 N）。
不需要新工具：`v_http` 从 `pressure_records.p01..p20` 读，`v_raw` 从 `ble.ts:619` 那行现成日志读（它已经打进 `N` 口径值，把 `signed` 除之前的原始 int16 一起打出来即可，属一行观测点）。

### 2.3 为什么这一条必须在设计/实现之前钉死（三挡致命性）

1. **offset 与读数必须在同一数值空间。** 技师端算 offset 用的是 **÷100 之后**的 BLE 值（`install/index.vue:291-296` 对 `onRealtimeFrame` 回调帧取均值，回调值已 ÷100）；入库读数若是 H1（未 ÷100），则"减偏移"是拿 1.1 去减 113 —— **减了等于没减，且永远达不到"趋近 0"**。这是本卡最容易被静默实现错的点。
2. **种子数据自带一个反证**：`scripts/db/seed/seed.sql:179-183,194-198,209-213` 三组 offset 全在 **0.0–0.3**。若空载读数真是 113，这组基线在物理上不可能把它归零 —— 说明"113"与"offset 0.1"不同量纲，或"113"本身不是稳态空载值。
3. **连带的告警风暴推断（待验，不作为结论）**：若入库即 108–119 N，则 `engine.go:39`（`p>45` → pressure_high）与 `engine.go:104`（`p>2.8` → sensor_drift）会对**每一帧每一点**命中，佩戴判定（`maxP>0.5`）恒为真。现实中未见刷屏 ⇒ 反过来支持 H1。**请 Boss/PM 用 staging 告警表实况证实或否证**（我无 staging 访问，此项仅按代码推演）。

### 2.4 显示标签是否要改

- 若判 H1 并在入口归一：`N` 标签**保留**，无需动 30+ 处文案；同时**必须**修 §7.2 的派生量与历史口径。
- 若判定语义其实是 kPa 或原始 ADC 计数：单位是**跨栈改名**，不是一处常量 —— 全仓 `牛顿` 零命中、无统一 unit 常量，`formatPressure`（`packages/shared-utils/src/index.ts:6-8`）**零 importer（死代码）**，30+ 处各自手写 `'N'`（患者端 `monitor/index.vue:30`、`:35`「20-60N 正常范围」、`PressureHeatmap.vue:22`、`PressureCurve.vue:68`；技师端 `install/index.vue:96,97,130`；后台 `monitor/index.vue:54,68,113,222,225,235,257,265`、`settings/index.vue:12,22`；**且烤进了字段名**：`shared-types/index.ts:392,395` 的 `pressureHighThresholdN`/`sensorDriftN`，Go 侧 `WearingThresholdN`/`HeatmapMaxN`/`CALIBRATION_OFFSET_N`）。
- **我的建议**：本卡**不改标签**，只做一件事 —— 在 `packages/shared-types` 为 `pressureValue` 补一行单位约定注释，并把 `formatPressure` 真正接入或删除。**改名另立卡**。

> **决策点 D1（阻断）**：请 Boss/PM 指定单位权威来源（出具 `device-protocol.md §4.1` 或等价固件约定），并批准 §2.2 的阶段 0 取证。取证明显前，本卡"减偏移"的实现不得开工。

---

## 3. 校准偏移存哪（对应卡片 ②第 1 条）

### 3.1 结论：**复用 `baselines`，不新建表、不给 `devices` 加列**

理由：
- 表已存在且约束完备：`baselines`（`000001_init_schema.up.sql:110-118`，`offset_values REAL[] NOT NULL` + `chk_baselines_offset_len CHECK(array_length=20)`）、双向 FK（`000001:137-140` 补 install FK）、写方在用（`repo.go:445`）。
- 定长 20 向量的**本仓既定表达法就是数组列 + 长度 CHECK**，不是 JSONB（JSONB 全库仅 2 处，且都是不透明袋：`000001:22` roles、`:308` audit_logs）；也不是 20 宽列（那种写法只用于需要逐点查询的 `pressure_records`）。
- 卡片"避免另起炉灶"的指令在此成立。给 `devices` 加 20 列或 JSONB 是明确的倒退，且会制造"两份校准源"。

### 3.2 但复用它会立刻撞上 4 个硬约束（这才是本卡真正的设计含量）

| # | 约束 | 证据 | 影响 |
|---|---|---|---|
| 1 | **1 install : 1 baseline，二次校准硬 409** | `000002_p0_fixes.up.sql:23-26` `uk_install_baseline UNIQUE(baseline_id)` + `repo.go:438-440` `ErrConflict` | **今天无法重校准。** 而 `sensor_drift` 告警规则的存在（`engine.go:97-124`）本身就证明零点会漂 —— 一个"漂移是已知故障模式"的系统却禁止重设零点，是设计不自洽。 |
| 2 | `baselines` **零索引**（`grep -i 'index.*baseline' scripts/db/migrations/` → 空），`device_id`/`install_id` 均无 | 邻居表都有显式索引（`000001:133-135,162`） | 实时接口每次 JOIN 走 seq scan。需 `000013` 补索引。 |
| 3 | 一机多装：`install_records` 每 `device_id` 多行、`baselines` 每 install 一行 | `000001:122` + `:133` | "这台设备当前的校准是哪一版"必须由 install 时间序决定，不能假设 device→baseline 1:1。 |
| 4 | 无 `GET /baselines`；data-service 与 device-service **只经 HTTP 互通**，data-service 无 device-service 客户端 | `handler.go:60-83` 路由全表；`data-service/cmd/server/main.go:26-28` | 读取必须选一条跨服务路径（§3.3）。 |

### 3.3 跨服务怎么读：D3

| 方案 | 评估 |
|---|---|
| **(a) 同库只读 JOIN**（data-service 直接 SELECT `baselines`） | **推荐。** 单库 shared database 是既成架构（`000001:3`），且 data-service **已有只读他服务表的先例**：`repo/device.go:30` `SELECT ... FROM devices WHERE device_id=$1`。表级写归属不破：`baselines` 写仍归 device-service，data-service 只读。零新模块、零新 HTTP 依赖、实时路径无额外跳数。 |
| (b) data-service → device-service 新增 `GET /internal/baselines` | 与 §1.5/T137 教训同型的风险：多一条"设计上存在、可能没人触发、CI 看不见"的跨服务调用。给热路径加一跳 RPC。仅当 Boss 要求"跨服务读写必须走 internal 端点、禁止跨表 JOIN"时选它。 |
| (c) 校准值冗余进 pressure_records | 否：破坏"可重校准"，且是 20 列 × 分区表的数据复制。 |

> **决策点 D3**：确认 (a)（同库只读 JOIN）是否被本仓表归属纪律允许。我判定允许（有 `repo/device.go:30` 先例），但因涉及跨服务读语义，请 Boss 明确背书。

### 3.4 重校准语义：**决策点 D2（需 Boss 裁）**

卡片验收"患者端无压力时趋近 0"在**当前 schema 下不可重复达成**：技师装完一次后，任何再校准（换贴位置、传感器漂移、复检）都会 409。
候选：
- **D2-1 放宽为 1 install : N baseline + 生效时间窗**（推荐）：`baselines` 加 `effective_from TIMESTAMPTZ NOT NULL DEFAULT now()`、`superseded_at TIMESTAMPTZ NULL`（或 `is_active` 生成列 + 部分唯一索引，**这正是本仓既定写法**：`000002:7-21` `device_bindings` 的 `unbind_at IS NULL` + `uk_bindings_active` 部分唯一索引）。读取按 `ts` 选窗 → **历史读数按其当时的基线呈现，天然可回算**。代价：新迁移 + data-service 侧查询多一条件。
- **D2-2 允许覆盖式重校准**（`UPDATE offset_values`）：改动最小，但**抹掉历史校准版本**，与 §6"历史数据口径"冲突。
- **D2-3 本卡不做重校准**，只读首版基线：范围最小、可交付最快；代价是把一个已知不自洽留在系统里，需 PM 在 TRACKING 记欠账。

我推荐 **D2-1**（复用本仓既有 `device_bindings` 模式，一致性最好），但**建议按 D2-3 先收口、D2-1 拆后续卡**，避免本卡把"显示趋近 0"和"重校准生命周期"两件事捆在一起做不完。请 Boss 选。

### 3.5 本卡需要的 schema 变更（若 D2-3）

`scripts/db/migrations/000013_baselines_read_index.up.sql`（+ `.down.sql`）：仅加 `baselines` 读取索引（按选定的 JOIN 键，`device_id` 或 `(install_id, ...)`）。
按本仓迁移风格：`BEGIN;/COMMIT;` 包裹、头部写 T173 与目的、内联预检 SQL、注明表写归属。**不动任何历史行、不加 NOT NULL。**

> 卡片红线：DB schema 变更须先报 Boss —— 本节即上报，未批准前不写迁移文件。

---

## 4. realtime 与 records 如何减偏移（对应卡片 ②第 2 条）

### 4.1 硬事实：**"只改 `BuildSensorPoints`"必然造成口径分裂**

卡片要求"校准逻辑放在共享层，确保 realtime 与 records 同源"——方向对，但**同源范围比这两个端点大**。实测全仓共 12 条读数路径，其中 **6 条**会绕过任何只在 DTO 层生效的校准：

| 路径 | 位置 | 只改 DTO 层的后果 |
|---|---|---|
| realtime（DB 优先） | `service/record.go:430-477` | 覆盖 ✓ |
| realtime（**Redis 兜底，绕过 DB**） | `service/record.go:480-548`，`rt:frame:{device_id}` | **不覆盖** ⇒ 同一接口两分支两套数（`record.go:519` 是 `BuildSensorPoints` 的第二个调用者，改它可覆盖，但要确认 Redis 里存的是原始值） |
| records（history） | `service/record.go:383-397` → `ToDTO` | 覆盖 ✓ |
| 热力图 `pressureHeatmap` | `model.go:264-285`，`record.go:441` | 后台读的是**这个**字段（`apps/admin-web/src/pages/monitor/index.vue:201,218,349`），患者端读 `pressureRecords[0].points` —— **不改则患者端与后台互相矛盾** |
| 日聚合 avg/max/max_point | `repo/rollup_repo.go:90-135` | **不覆盖** ⇒ `daily_wear_stats`、健康报告 `avg_pressure`、周报月报、后台 dashboard KPI 仍是 raw 口径 |
| 内联告警评估 | `service/record.go:210-236` → `POST /internal/evaluate`（`alert-service/internal/engine/engine.go:39,73,104`） | **不覆盖** ⇒ 告警按 raw 判 |
| 补偿队列告警 | `service/record.go:239-264` → `alert-service/internal/consumer/consumer.go:65-85` | 同上 |
| 佩戴分钟统计 | `record.go:194,503`（`maxP>0.5`）+ `rollup_repo.go:96` | 不覆盖 ⇒ 佩戴时长虚高 |
| 冷归档 CSV 导出 | `service/archive.go:122-187`、`repo/partition_repo.go:164`（`COPY pressure_records_YYYYMM TO CSV`） | 不覆盖 ⇒ **导出与界面不同数**（合规追溯风险） |
| `max_pressure` **DB 生成列** | `000001:154-156` `GENERATED ALWAYS AS greatest(p01..p20) STORED` | **应用层无法校准它**。且 `rollup_repo.go:103-122` 的 20 路 `pXX = max_pressure` 平局判定直接比原始列 |

### 4.2 只有两个位置能"一处生效、全局一致"

| | 方案 P1：入口减（`toPendingFrame`，`record.go:663-669`） | 方案 P2：出口减（统一校准函数，多调用点） |
|---|---|---|
| 覆盖上面 10 行 | **自动全覆盖**（含生成列、rollup、告警、导出） | 需逐点改，其中**生成列 `max_pressure` 与 `rollup` 20 路平局判定无法在纯出口下校准** |
| 原始真值 | 丢失（不可逆） | 保留 |
| 重校准 | 不可能修正历史（且基线不存在时帧已入库，同列混两种语义） | 可按基线生效时间分段 |
| §2 量纲未定的风险 | **永久固化进分区表历史数据** | 取证后改函数即可 |
| 未授权生产数据变更 | 触发（写入即改口径） | 不触发（对齐 T137 先例：任何一次性 `UPDATE` 回填需 Boss 单独批准） |
| 改动面 | 最小（1 处） | 较大（约 6 处 + 生成列问题） |

### 4.3 推荐：**P2（出口减）+ 分两步收口派生量**，理由全部来自上面的风险行

关键判据是 §2.3(1)：**量纲未定期间绝不写库**。P1 的"一处生效"诱惑很大，但一旦 §2 判成 H1，写坏的是 `pressure_records` 分区表全部历史且不可回滚；而 P2 出错只需改一个函数。其次，`sensor_drift` 规则的存在证明零点会漂，入口减在语义上无法表达"此后换基线"。

P2 的落地要求（**这些不是可选项，是"两端一致"验收的必要条件**）：
1. 校准在**一个函数**里完成，签名 `(device_id, ts, [20]float32) -> [20]float32`，被 realtime 双分支、records、heatmap 共用；**`BuildSensorPoints` 与 `BuildHeatmap` 的入参在校准后**（即在两个调用点之前校准，而不是在 DTO 里各自减，避免又长出一份平行实现）。
2. 缺基线时**不减并且显式标记未校准**（§6.2），不得静默当 0。
3. **`max_pressure` 与 rollup 的一致性问题必须在阶段 2 一并解决**，做法二选一：(i) rollup SQL 内改成 `greatest(p01-o01, …, p20-o20)` 并放弃生成列（20 路平局判定同步重写）；(ii) 派生指标改在应用层聚合。**不接受"长期不一致"**；若 PM 要分阶段，中间态必须在 PR body 与 TRACKING 明写"哪些字段仍是 raw 口径"。
4. 基线变更后**失效 Redis `rt:frame` / `stat:today`**（`repo/cache.go:33,36`），否则两分支新旧口径并存。
5. 按 T168 教训：任何新增测试断言**线上键**（`map[string]any` + 正/反向断言），不得把 `resp.Data` 反序列化进实现自选的 DTO 自证。

### 4.4 "共享层"放哪 —— 卡片要求"校准逻辑放在共享层"的精确化

**Go 侧今天不存在跨服务共享包**：`go.work` 8 个模块全是 service/testhelper（`go.work`），`packages/*` 是纯 TS、Go 不可导入；唯一被两服务共用的 Go 模块是 `services/testhelper`，而其包注释自陈为"集成测试共享工具"（`testhelper.go:1`）。新建第 9 个模块要付：`go.work` 条目 + 两个 `go.mod` 的 `require/replace` + `go.work.sum` 与两份 `go.sum` 抖动，且这四个文件**都在 `ci-go.yml:11-15` 的触发列表和 `:35-38` 的 cache-dependency-path 里**，`.golangci.yml:7` 还是 `readonly`。

→ **推荐：先落在 `services/data-service/internal/calibration/`（单服务内共享层）。** 因为 §4.1 全部读数点都在 data-service 一个服务内；alert-service 拿的是 data-service **传过去的帧**（`service/alertclient.go:21-28`）⇒ 只要入口侧传校准后的帧，告警自动同源，不需要 alert-service 也持有校准码。**不预先建模块**（避免过早抽象）；真出现第二个消费方再抽，抽取成本已知且小。

> **决策点 D6**：是否要求校准实现下沉为跨服务共享 Go 模块（付上面 go.work 代价，换"alert-service 独立复算能力"）。我判定当前不需要，请 Boss 确认。

---

## 5. 技师端 / 患者端是否共用同一校准源（对应卡片 ②第 3 条）

**共用，且唯一源 = `baselines` 表；减法只在服务端做一处；客户端一律不做偏移。** 但要区分"采集"与"展示"两种用途，二者**故意**使用不同口径：

| 用途 | 数据 | 是否应用 offset | 理由 |
|---|---|---|---|
| 技师端**采基线**（BLE 5 帧取均值算 offset） | 设备原始帧 | **必须不应用** | 这是 offset 的定义来源；对原始帧再减 offset 等于"拿 offset 减 offset"，会把基线越校越漂。 |
| 技师端**展示/复检读数** | 后端已校准响应 | 应用（由服务端做） | 技师复测若看到 raw，就会再采一次，形成漂移累积 |
| 患者端 | 后端已校准响应 | 应用（由服务端做） | 本卡主目标 |
| 后台 admin-web | 后端已校准响应 | 应用（同一源） | §4.1：它读 `pressureHeatmap`，与患者端不同字段 —— **必须同源** |

配套必做（否则"共用同一校准源"只是文档承诺）：
1. **删掉技师端校准完成页的硬编码 `0.00`/✓✓✓**（`install/index.vue:113-121`、`:298-302`），改为回读服务端已校准值或如实显示"待服务端生效"。这是 §0.2 那个 placebo 的收口。
2. **offset 质量门禁补齐**（见 §7.1）—— 现在的"范围校验：通过"是假的，等于任何脏数据都能进 `baselines`。
3. 把 `patient-miniapp` 纳入契约漂移门禁：根 `package.json:17` 的 `test:contract-gate` 只跑 `admin-web` + `tech-miniapp`，**患者端压力路径（`pressureRecords[0].points[].pressureValue`）历史上无任何字段名门禁**（T168 同型盲区）。注：门禁只校验键名，不校验数值语义，故单位/缩放仍需 §2 的人工取证兜。

> **决策点 D4**：技师端 placebo 收口（上面 1、2）是否并入 T173。它不是患者端显示问题，但**不修则 §3.4 的重校准与 §7.1 的可信度都无落点**。我建议并入（改动小、同一条链路），请 Boss 裁；若不并，请指定卡片归属，我会在交付里记为已知限制。

---

## 6. 已有数据如何补 baseline（对应卡片 ②第 5 条）

### 6.1 现状与来源判定

`scripts/db/seed/seed.sql:179-183,194-198,209-213` 三条种子基线（install 1/2/3，offset 0.0–0.3）+ 其余 install 的 `baseline_id IS NULL`。真实设备若无基线，则其全部历史读数**没有可补的依据** —— 空载零点必须当场采，不能事后从数据里反推（无法区分"载荷"与"零点漂"）。

### 6.2 推荐策略：**不回算、不回填、显式标记未校准**

1. **无基线 ⇒ 不减偏移**（数学上与"减 0"同值，语义上必须区分）。给已校准状态一个显式表达：建议在 `RealtimeSnapshot` / DTO 增加如 `calibrated: bool` + `baselineId: string|null` 的**读侧派生字段**，三端一致渲染（如"未校准"角标）。理由：让"未校准"变成用户可见事实，而不是一个看起来像真数的 113。
2. **一次性 `UPDATE pressure_records` 回填：明确不做。** 三条理由：(i) 违反 §2.3(1) 量纲未定；(ii) 属生产数据变更，按 T137 先例需 Boss 单独批准（那不是 bug，是流程）；(iii) `pressure_records` 是 `PARTITION BY RANGE (ts)` 的分区表（`000001:160`）+ 生成列，大表 UPDATE 有锁与复制成本。
3. **种子策略**：`seed.sql` 三组 0.0–0.3 的 offset 与真实空载量级不符（§2.3(2)）。若 §2 判 H1，这些种子值在归一后仍是"看起来像 N"的小数，会掩盖缩放缺失。**建议把种子 offset 改成能与 §2 判据自洽的值，或加注释标注其量纲假设** —— 需 Boss 批准（碰种子数据）。
4. **可回算性留给 D2-1**：若采纳"1 install : N baseline + 生效窗"，则历史按当时基线呈现是可实现的；若采 D2-3，则"重校准前的历史读数"永久停在旧口径，须写进已知限制。

### 6.3 迁移与门禁（评审通过后执行）

- 编号顺延 `000013_*`（当前最高 `000012_devices_patient_partial_unique`），成对 `.up/.down.sql`，slug 带 T173；
- 内联预检 SQL + 停手条件（照 `000012:6-9` 的写法）；
- 集成测试**不**用 `migrate` 二进制，自行按序应用全部 `*.up.sql`（`services/device-service/internal/repo/integration_test.go:64-92`）—— 迁移只要放进目录即被 CI 集成层覆盖。

---

## 7. 顺带发现（本卡范围外，建议单开卡，勿静默）

**7.1 校准结果没有任何质量门禁** —— 直接决定"患者端能否趋近 0"能否验收：
`threshold_calibration_offset`（`seed.sql:331`，"空载校准偏差上限（N）"）在 Go/TS **零读者**（不在 `alert-service/internal/config/config.go:18-23` 的 `Keys()`，也不在 `Thresholds`）；`DEFAULT_THRESHOLDS.CALIBRATION_OFFSET_N`（`packages/constants/src/index.ts:26-27`）除自身测试（`test/index.test.ts:44-45`）外**零消费者**；技师端 `checks.range` 硬编码 `true`（`install/index.vue:298-302`）；服务层只校验**长度**不校验值（`service/device.go:318-320`）；DB CHECK 只管长度（`000001:117`）。
→ 脏/超界 offset 可无阻力入库。注意其**反向含义**：若 §2 判 H1（空载真值 ≈1.1 N），则所需 offset ≈1.1 N **已超过 0.5 N 的既定上限** —— 那意味着这套"空载校准"按自家标准就该被判失败，属硬件/流程问题而非软件缺口。此点请 Boss 一并裁。

**7.2 `PointCount=20` 已有 5 份平行定义**：`data-service/internal/model/model.go:15`、`device-service/internal/model/model.go:16-17`、`000001:117` CHECK、`packages/constants/src/index.ts:37`、`install/index.vue:233,292` 的 `Array(20)`。本卡新增读取侧会第 6 次触碰这个约定 —— 实现时须显式对齐，不要再造一份。

**7.3 患者端 `PressureHeatmap.vue:22` 无 `toFixed`**（`{{ activePoint?.pressureValue }}N`）：`p01..p20 REAL`（float32）会打出 `118.64999389648438N`。校准后仍在。属一行修，建议并入 D4。

**7.4 §1.5 模拟器三重不匹配** —— 影响后续所有链路测试的可信度。

---

## 8. 实现拆分（**评审通过后**才动手，每阶段独立可交付 PR）

| 阶段 | 内容 | 验收与证据形式 |
|---|---|---|
| **0** | 量纲取证（§2.2 判据）+ 指认 108–119 来源（§1.5 末） | 一份取证记录：同一空载设备的 `v_http` 与 `v_raw` 对照。**产出为 D1 裁决输入，不改业务码** |
| **1** | 读侧最小闭环：`internal/calibration` 单函数 + realtime(DB/Redis 双分支)/records/heatmap 同源 + `000013` 索引 + `calibrated` 标记 | 集成测试打 `t.Logf` 前后值：无压力时 20 点趋近 0；同 device+ts 的 realtime 与 records 逐点相等 |
| **2** | 派生量同源：rollup（含 `max_pressure` 生成列问题）、告警入参、CSV 导出、Redis 失效 | 集成测试断言 `daily_wear_stats.avg/max_pressure` 与校准后明细一致；导出 CSV 抽查 |
| **3** | 重校准语义（按 D2 裁决）+ 技师端 placebo 收口（按 D4） | 二次校准不再 409（或按 D2-3 记欠账） |
| **4** | 契约与前端：患者端纳入 contract-gate、`calibrated` 渲染、§7.3 | `npm run test:contract-gate` 覆盖患者端压力路径并**反向验证**（改错键名即红） |

> **卡片验收标准 ②「实现后：患者端 monitor 在无压力时显示趋近 0」我判定阶段 1 单独不足** —— 它还要求 §2 判为 H1/H2 之后的对应处置（H1 需入口缩放、H2 需硬件侧），且患者端当前**无轮询**（§1.4），"实时趋近 0"在交互上还依赖下拉刷新。请 PM 在评审时确认这条验收的准确边界，避免实现完在验收口径上对不上。

---

## 9. 可观测性设计（按 T164 纪律）

**纪律实况（先纠正一处常见理解）**：T164（PR #69 / `675b123`，实际观测点 commit `07ee6eb`）自称"根因定位后将回归删除"，但 HEAD 上 15 处 `-dbg` 全部还在，T166 又在其上加了 2 处 → **可观察到的规则是"打标签 + 保留"，不是"用完删"**。"软注销"在仓内也无命名机制，实测有 8 种手法（`000011:10` `COMMENT ON COLUMN '[DEPRECATED]'`、`patients.device_id` 停读不删列、feature flag 置灰 `patient.ts:36`、`testhelper.go:112-119` documented-but-unwired、`ci-go.yml:74-88` 注释掉门禁、`app-extends.d.ts:6` 模块增补替代改契约 等）。
⚠️ 一条硬边界：**`scripts/deploy/check-routes.sh:24` 会过滤 `^\s*//`** —— 注释掉一条路由在门禁眼里等价于删除。本卡若新增路由，不可用"注释保留"来软注销。

T173 日志点清单（zerolog 链式、snake_case 字段、`"<域>: <事件> (T173-dbg)"` 格式；Warn→Info 提升以规避 staging 按级别过滤，先例 `gateway/cmd/server/proxy_admin.go:50-51,71-72`）：

1. 每次校准：`Str("device_id") Int64("baseline_id") Str("baseline_source"){join|none} Bool("applied")` —— 回答"这一帧减没减、按哪版基线"。
2. 跨服务 JOIN 基线：命中/未命中/多命中三态 `Str("result","ok\|no_baseline\|ambiguous_baseline")`（低成本谓词代替原始值，先例 `user-service/internal/handler/bind_phone.go:163-164`）。多命中即 §3.2(3) 的一机多装歧义，是本卡最可能出错的分支。
3. 兜底路径归因：realtime 走 DB 还是 Redis `Bool("redis_fallback")` + `Str("rt_frame_offset_state","raw\|calibrated")`。
4. `max_pressure` / rollup 口径分歧：`Bool("derived_stale")`（阶段 2 前用于量化不一致面积）。
5. Redis 基线失效动作、CSV 导出是否校准各一条。
6. 校准采集侧（技师端）：保留 §2.2 判据所需的"÷100 之前原始 int16"观测点（`ble.ts:619` 现成日志扩展），阶段 0 之后**按软注销保留**。

⚠️ **device-service 的 `internal/handler/`、`internal/repo/` 目前零日志**（非 main 的日志仅 `service/provision.go` 5 处），其 handler 层也没有 user-service 的 `ctxLogger`（`user-service/internal/handler/handler.go:234-251`）。本卡若在 device-service 打点，需自带 logger 构造，不能假设已有请求级 logger。

---

## 10. 待 Boss/PM 裁决汇总（评审需逐条给结论）

| # | 事项 | 我的推荐 | 不裁的后果 |
|---|---|---|---|
| **D1** | 单位权威 + 阶段 0 取证批准（§2.4） | 先取证再实现 | **减偏移在错误量纲下静默失效，验收必不过** |
| **D2** | 重校准语义（§3.4） | 本卡按 D2-3 收口，D2-1 拆后续卡 | 二次校准永远 409，"趋近 0"不可维持 |
| **D3** | 同库只读 JOIN `baselines`（§3.3） | 允许（有 `repo/device.go:30` 先例） | 读侧无实现路径 |
| **D4** | 技师端 placebo + 质量门禁收口是否并入（§5.3、§7.1） | 并入 | 校准源可信度无落点 |
| **D5** | 历史数据不回算、种子 offset 是否改（§6.2、6.3） | 不回算；种子改注释 | 种子数据掩盖量纲缺失 |
| **D6** | 校准码是否下沉跨服务 Go 模块（§4.4） | 不下沉（现阶段单消费方） | go.work 四文件抖动、CI 缓存重排 |
| **D7** | `000013` 迁移（baselines 索引）批准 | 批准 | 实时路径 seq scan |
| **D8** | 阶段 1 先交付、派生量阶段 2 补齐期间"哪些字段仍是 raw"是否可接受（§4.3(3)） | 接受但 TRACKING 记名 | 界面/报表/告警/导出四套口径并存 |

---

## 11. 红线自查

- [x] 分支 `t173-calibration`，无 `/`；`git rev-parse --abbrev-ref HEAD` → `t173-calibration`（已验）
- [x] 未使用 `--orphan`；未 `reset --hard`；未 `rebase`；未在本地 main 提交
- [x] 只在 `D:/proj/BraceSync/.worktrees/winner` 内操作，未碰他人目录；宿主 `D:/proj/BraceSync` 未写
- [x] 本轮**只交付本设计文档，未改任何业务代码 / schema / 种子数据**
- [x] DB schema 变更（D7）与生产数据/种子变更（D5）均**先上报、未动手**
- [ ] PR + `gh pr checks` 实测：本文档 PR 提交后附

## 12. 已知限制

- 本机无 Docker/PG：跨服务端到端与集成层证据须在 CI 的 `go-integration`（testcontainers 真实 PG15）里以 `t.Logf` 打前后值，从 `gh run view <id> --log` 捞取；本地只能可靠自证 `go vet ./services/<svc>/...`、`go test` 同列表、`gofmt -s -l <改动文件>`、`bash scripts/deploy/check-routes.sh`（本地 `golangci-lint` 因工具链版本报 `unsupported version: 2`，属环境噪音，lint 交 CI 裁决）。
- 无 staging 访问：§2.3(3) 的告警风暴推断、§1.5 的 108–119 来源指认，均需 Boss/PM 侧取证实测，我在文档中标为**待验**而非结论。
- 固件真实上报语义不在本仓：`device-protocol.md` 缺失（§2.1），单位权威只能来自仓外。
- 本文所有 `文件:行号` 基于 HEAD `ca9136d`；评审期间若 main 前进，行号可能漂移。
