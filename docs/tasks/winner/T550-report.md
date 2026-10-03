# T550 报告 · 校时端点被验签时间窗挡死形成死锁（Winner，2026-10-04）

> 派发单：`docs/tasks/winner/T550-prompt.md`（PM 维德 2026-10-04 00:1x，severity urgent）
> 代码分支：`t550-device-time-window-winner`（起自代码仓 `origin/main` = `6de852f`）
> 现象侧实测数据（104296 次请求、0 次入库、最后一条压力记录 10-01 09:38 UTC）取自派发单，
> 是我**引用** PM 的现网读数，不是我在 staging 上重测的 —— 我没有 staging 凭据，也未部署。

## 一、死锁成因图（判据 D4，供 Joe 独立复验）

代码位置（改前）：`services/gateway/cmd/server/proxy_services.go:81-92`、
`services/gateway/cmd/server/middleware.go:121-175`、
`services/gateway/internal/auth/verifier.go:23-40`、`internal/auth/types.go:26`（`SignatureTimeWindow = 5`）。

```
设备时钟不准（偏差 > ±5min）
   |
   | 带 X-Timestamp 发 POST /api/v1/device/records
   v
网关设备验签组 deviceSigAuth
   |
   | VerifySignature 第一步：IsTimestampInWindow(ts, now, 5) == false
   v
HTTP 401 · code 20402 "timestamp outside ±5min window"
   |
   | 请求被拒在网关层，device-service 零日志、data-service 零写入
   v
设备要修时钟，只能问 GET /api/v1/device/time
   |
   | 而 /device/time 注册在同一个 dev 组里（dev.Use(deviceSigAuth(agt))）
   | —— 同一个 5 分钟窗先把它也拒了（401/20402）
   v
拿不到校时 -> 时钟仍不准 -> 上报仍被拒  ......  （回到起点，闭环）
```

闭环成立的关键一条：**校时端点自己也在它要修的那个窗里面**。协议本意（硬件清单 §4.4）是
「设备先校时再上报」，实现把两步放在了同一道门禁之后。
断流 3 天无人知是另一条独立缺口（见 §五）。

## 二、修法与取舍

选**方案 B：只放宽校时端点的时间窗，不摘鉴权**（`DeviceTimeSyncWindow = 24 * 60` 分钟）。

- 改动形态：`VerifySignature` 增加按分钟入参的 `VerifySignatureWindowed`（默认档仍取
  `SignatureTimeWindow`，`VerifySignature` 变成它的一行委托，旧调用点与旧测试一字未改）；
  中间件相应拆出 `deviceSigAuthWindow(agt, windowMinutes)`；`/api/v1/device/time` 单独成组挂宽窗。
- 为什么不选方案 A（把 `/device/time` 移出验签组，裸端点）：
  移出后该端点对公网任意客户端开放。它本身只回一个服务器时间，泄露量不大，
  但代价是**丢掉设备身份这一维**：无法区分是谁在打、无法按设备限流、无法在密钥吊销后止血，
  而且任何脚本都能拿它做时钟探测的放大器。方案 B 保留「必须先证明自己是已注册设备」，
  死锁照样解开，暴露面反而更小。
- 放宽会不会削弱防重放：对**上报链路不会**（`/device/records` 与 `/records/batch` 仍走 ±5min 默认窗，
  有实测断言）；对**校时端点本身**，一枚已签名的 `GET /device/time` 的有效期从 5 分钟变 24 小时 ——
  这是真延长，但该端点无状态变更、无数据写入、重放只回当前服务器时间，
  重放它的收益与成本（要持有设备密钥才能签）不成比例。
- 宽窗不是免窗：派发单说「放宽时间窗」容易被读成「这一格不校验了」。实现里窗口仍是有限值，
  25 小时偏差的校时请求照样 401/20402（有用例断言）。
- 验签与注册状态不随窗口变：错密钥签的校时请求回 20401，未注册设备回 20404（有用例断言）。

## 三、Nonce 防重放评估（派发单任务 2，本卡不要求实现）

结论：**「时间窗放宽后防重放更依赖 nonce」这句话，对本次改动的两个端点要分开说。**

1. 先摆现状：`VerifyNonce`（`internal/auth/verifier.go`）自陈是占位实现，恒放行，
   nonce 为空亦放行。也就是说**防重放今天就不存在**，不是被我削弱的。
   当前唯一实际生效的重放约束是时间窗（±5min）加签名串里含 nonce（改 nonce 就签名不符）。
2. 上报端点：窗口未变，重放约束面未变。这一格**无倒退**。
3. 校时端点：可重放窗口从 5 分钟变 24 小时，且这 24 小时内重放的是同一枚已签名请求。
   因为该请求幂等且无副作用，重放的业务收益为零；剩余风险只有流量占用，
   而流量占用要付「持有某台已注册设备的密钥」这个前提。
4. 因此真正的结论是：**nonce 必须实现，但它的紧迫性不由本卡决定，而是本卡把它照出来了。**
   一旦将来有任何一个端点需要宽窗（这次是校时，下次可能是补传或批量），
   防重放就只能落在 nonce 上。建议单独立卡（Redis 接入 + `SET NX sec:nonce:{device}:{hash} EX 600`），
   按纪律我不自建卡，报 PM 定夺（派发单 §三.2 也只要求评估）。

## 四、存量补传（派发单任务 3）

代码侧能给的部分：补传端点 `POST /api/v1/device/records/batch` 与单帧上报在**同一个验签组、同一个默认窗**里
（`deviceReportRoutes` 两条路由同挂 `dev` 组）。这意味着：

- 死锁期间设备若在本地攒了批量数据，攒下的请求**同样带设备时间戳**，
  时钟没修准之前补传也会被 20402 拒 —— 所以「修好校时」是「补传能成功」的前置条件，
  这一格本卡的改动正好把它打通。
- 攒的数据如果用的是**采集时刻**的时间戳（协议 §2.2 签名串里的 timestamp 语义是采集/发送时刻由实现定），
  那么即便时钟修好，历史帧的时间戳仍可能落在 ±5min 之外被拒。这一格我在本仓代码里量不出来：
  设备侧固件不在本仓，`device-simulator` 用的是发送时刻。
  ⇒ 需要真机或模拟器实测一次「修好校时后再补传 3 天前的攒帧」能否入库。我不自建卡，报 PM：
  建议另立卡触发补传并把这一格做成判据。

## 五、防复发：数据新鲜度守卫（判据 D3）

选型：**只读巡检脚本 + cron**，落 `scripts/deploy/data-freshness-guard.sh`，
配对行为测试 `data-freshness-guard-test.sh`。

- 为什么不选另两种：CI 定时（`.github/workflows`）跑不了这个判据，它要读 staging 库的
  `pressure_records.max(ts)`，把库凭据放进公开 runner 是倒退，而且会把「部署门禁」和「运行态巡检」
  混成一件；data-service 侧自检要改服务并走一轮部署，窗口最贵，且服务活着并不代表数据在长
  —— 这次事故恰恰就是「服务健康、数据断流」。
- 判据取「最后一行距今多久」而不是「今天有多少行」，因为事故形态是**完全静默**：
  行数阈值在天级粒度上对 0 行和 3 行都给出同样的绿。
- 退出码三态（与仓内 T393 守卫同形，可直接被 cron 侧当报警信号）：
  0 新鲜 / 1 断流报警 / 3 取不到读数。**读不到不判绿**是硬要求：
  夹具不可读、没给夹具又没给连接串、psql 失败，全部走 rc=3。
- 三条附加分辨力（都是把「守卫是摆设」这种失败模式按住的）：
  阈值两侧各喂一次（5h 绿 / 7h 红，差 1 小时必须翻面）；阈值可配（同一份 7h 夹具在阈值 8h 下应绿）；
  最新行落在未来判红（否则时钟倒退或写入超前会被当成「很新鲜」放过去）。
- 模拟断流的实测读数（D3 要求的那一次）：把夹具最新行设为距今 72 小时（现网那一档），
  守卫回 `rc=1` 且文案含「断流报警」与「约 72 小时」。见 §六读数。
- 接线：与 T393 那两条 cron 同一侧外置部署（`bracesync-ops/bin`），
  本 PR 只交脚本与判据，**不改 crontab，也不改 `publish-cron-scripts.sh` 的签发集 REQUIRED**
  —— 后者一改就要同步动 `publish-cron-scripts-test.sh` 里硬编码的两条夹具（别人的测试），
  且签发集/ crontab 都是服务器侧动作。这两步留给值班席与部署轮（归 §八 第 4 条）。

## 六、本地读数（原文，未美化）

```
$ go vet ./services/gateway/...
VET_RC=0                      # 只有一行环境告警：both GOPATH and GOROOT are the same directory

$ go test ./services/gateway/...
ok      github.com/bracesync/bracesync/services/gateway/cmd/server      39.800s
ok      github.com/bracesync/bracesync/services/gateway/internal/auth   0.135s

$ go test ./services/gateway/cmd/server/ ./services/gateway/internal/auth/ -run TestT550 -v
--- PASS: TestT550_TimeEndpoint_WorksForClockSkewedDevice (0.02s)
--- PASS: TestT550_ReportRoute_StillRejectsBeyondFiveMinutes (0.00s)
--- PASS: TestT550_TimeEndpoint_WidenedWindowIsBounded (0.00s)
--- PASS: TestT550_TimeEndpoint_StillVerifiesSignatureAndRegistration (0.00s)
--- PASS: TestT550_VerifySignatureWindowed_UsesPassedWindow (0.00s)
--- PASS: TestT550_WidenedWindow_StillVerifiesSignature (0.00s)

# 牙齿证明：把 proxy_services.go 换成改前那一版（其余保留），同一条用例必须红
PRE_FIX_RUN   --- FAIL: TestT550_TimeEndpoint_WorksForClockSkewedDevice
PRE_FIX_FAILCOUNT_raw=2         # 改前这 4 条里有 2 条红（解开死锁那条 + 宽窗有界那条）
RESTORE 等值=true a38cf09aa43d  # 换回来的字节与改前备份 SHA256 前 12 位一致
POST_FIX_RUN  ok  .../cmd/server 0.255s
RESULT TEETH_PROVEN

$ bash scripts/deploy/data-freshness-guard-test.sh
[t550-fresh] 通过 15 项 / 失败 0 项
TEST_RC=0
```

## 七、判据逐条结论

| 判据 | 状态 | 说明 |
|---|---|---|
| D1 部署 staging 后设备上报返回 200 | **未做** | 我不自行部署。需 PM 派 Andy，并与 T395（第七轮部署）协调窗口，不单起轮。 |
| D2 设备上报成功入库的实测读数（200 计数 > 0、库内最新 ts 推进） | **未做** | 同 D1，需 staging 库凭据与真机/模拟器；我没有这两样。不许只报 CI 绿这条我照办，因此本卡交件**不含**任何「已入库」结论。 |
| D3 防复发监控可运行且断流时真报警 | **已给** | `data-freshness-guard-test.sh` 15/15；其中 72 小时夹具回 rc=1（模拟断流的报警读数）。 |
| D4 报告含死锁成因闭环图 | **已给** | 本文 §一。 |

## 八、遗留与需要谁

1. D1/D2 需要部署窗口：**等 PM 派 Andy**（并协调 T395）。
2. nonce 防重放实现（现为基础缺口，非本卡引入）：**等 PM 定是否立卡**。
3. 设备存量补传是否会自动触发、历史帧时间戳能否过 ±5min 窗：**等 PM 定是否立卡**，需真机或模拟器一格实测。
4. 新鲜度守卫接进 crontab：**等值班席**执行（脚本只读，不动 crontab）。
5. 本机没有 staging 库凭据，`--psql` 那一条腿只在有凭据的机器上可跑；测试与 CI 走夹具。
   这条不是我这次绕过的限制，是这条判据本身要在部署轮里做。
