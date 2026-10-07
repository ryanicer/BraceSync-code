# T612 交付报告 · 网关路由注册层的方法白名单校验

> 执行：winner（dell-tower，后端 Go）｜ 验收线：joe ｜ 卡号：T612
> 本卡只动路由注册层，未动验签、时间窗与任何业务码的既有形状（派发单第四节边界）。

---

## 一、缺陷现读基线（修复前，探针实测）

探针跑在两个路由上：设备域单独路由（`gin.New()` + `registerDeviceReportRoutes`）与生产全量路由（`setupRouter()`）。取的是 HTTP 面读数（状态码 / Content-Type / Allow / X-Request-Id / 响应体）与 zerolog 捕获面行数。

| 请求 | 状态码 | Content-Type | Allow | 响应体 |
|---|---|---|---|---|
| POST `/api/v1/device/time`（只注册 GET，签名头齐备） | 404 | text/plain | 空 | `404 page not found` |
| GET `/api/v1/device/records`（只注册 POST） | 404 | text/plain | 空 | `404 page not found` |
| GET `/api/v1/device/time-sync`（路径未注册） | 404 | text/plain | 空 | `404 page not found` |
| OPTIONS `/api/v1/device/time` | 404 | text/plain | 空 | `404 page not found` |
| （生产路由）POST `/api/v1/device/time` | 404 | text/plain | 空 | `404 page not found` |

同一轮里业务 404 那一格是另一种形状：DELETE `/api/v1/admin/technicians/T1` 回 404 + `application/json` + `{"code":404,...,"message":"该功能接口暂不可用，请刷新页面后重试"}`。

zerolog 捕获面：8 次请求只留下 1 行（那 1 行正是业务 404 的 `endpoint not available`），方法不匹配与路径未注册的请求**一行都没有**。后端侧 `received` 计数 0，说明请求确实死在网关。

根因不在业务代码：gin v1.10.0 的 `Engine.HandleMethodNotAllowed` 在 `New()` 里默认写死为 **false**（`gin.go:199`），因此方法不匹配不进 405 分支，直接落到 `allNoRoute` ⇒ `serveError(c, 404, "404 page not found")`。网关用的是 `gin.Default()`，未改这一格，所以设备无论把方法用错还是把路径写错，对外都是同一个 404，服务端还查不到线索。

## 二、修法

新增 `services/gateway/cmd/server/route_method_guard_t612.go`，在 `setupRouter()` 里两组路由注册完之后挂一道路由级兜底：

- `r.HandleMethodNotAllowed = true` —— 打开 gin 的方法不匹配分支；
- `r.NoMethod(...)` —— 路径已注册、方法不在白名单 ⇒ **405** + 业务码 **20405** + `Allow` 头 + `data.allowed_methods` + 一行 zerolog（method / path / http_status / code / request_id / 技术文本）；
- `r.NoRoute(...)` —— 路径根本没注册 ⇒ **状态码与响应体形状保持原样**（本卡不扩到 404 报文改造），只补上缺失的那一行日志，让「路由未匹配」在服务端留下证据。

业务码 `20405` 取 T402 甲-1 的形状「域号 2 + HTTP 三位 405」，与 20404（设备未注册）、裸 404（端点不可用）不同值；用户面中文文案加在 `trace_t464.go` 的 `userTextByCode` 里一格，技术文本只进日志（T464 双通道不破）。

`Allow` 取的是 gin 按路由树算出来后写进响应头的那一串，不是自己拿 `Engine.Routes()` 的 fullPath 做等值比对 —— 后者对 `/devices/:deviceId` 这类参数化路径必然漏判。

## 三、验收判据逐条对表

| 派发单判据 | 实测 |
|---|---|
| 1 复现一条：返回体能看出「路由未匹配」而非「业务拒绝」 | 修复前 POST `/api/v1/device/time` 回 404 纯文本（上表第一行，已复现）；修复后回 405 + `{"code":20405,...,"allowed_methods":["GET"]}` |
| 2 路由注册层补方法白名单校验，返回可辨识错误码，不与业务 404 混同 | 405 + 20405 两维都可辨识；对照腿 `TestT612_BusinessNotFoundCodesUnchanged` 锁住 20404 与裸 404 两格形状不变 |
| 3 回归：既有正常上报链路不受影响 | `TestT612_NormalReportAndTimeSyncUnaffected` 绿；全量 `go test`（CI-Go 那条命令逐服务列出）40 个包 ok、FAIL 0 行 |
| 4 防假绿：日志里要真的出现测试执行行 | `go test -run TestT612 -v` 逐条打印 `--- PASS`；日志腿 `TestT612_MismatchLeavesOneTechnicalLogLine` 断言的是 zerolog 捕获面「恰好 1 行 code=20405」，报 0 时同屏给出被扫面总行数 |

## 四、判据有牙注入证

把 `registerRouteMethodGuard` 临时改成空操作（`if true { return }`）重跑本卡用例，红/绿分布 = **7 红 / 2 绿**：

- 红：`PostOnGetOnlyTimeRoute` / `GetOnPostOnlyReportRoute` / `ParamRouteMismatch` / `MismatchLeavesOneTechnicalLogLine` / `ResponseBodyCarriesUserTextNotTechnicalDetail` / `UnregisteredPathKeepsPlain404ButLogsRouteMiss` / `ProductionWiring_SetupRouterCarriesGuard`（读数回到 `404 page not found`、日志捕获面 0 行、`HandleMethodNotAllowed` 为 false）；
- 绿：`BusinessNotFoundCodesUnchanged`、`NormalReportAndTimeSyncUnaffected` —— 这两条锁的是「本卡不许改动的面」，注入后仍绿是它们的正确形状。

注入腿已删除，不进 PR。

## 五、被本卡有意翻面的既有用例（请 Joe 重点核这一节）

`services/gateway/cmd/server/jwt_auth_impl_test.go` 里有两条断言，把「GET 打只注册 POST 的登录入口」期望成 404：

- `TestJWT_Whitelist_LoginWithoutToken`：`GET /api/v1/auth/login` 404 → **405**
- `TestJWT_Whitelist_TechPatientLogin`：`GET /api/v1/tech/login` 404 → **405**

这两条原本就是本卡缺陷的另一种表现（注释写的「未注册」其实不准确 —— 路径是注册的，只是方法不在白名单），改期望值属修复本体，不是掩盖回归。两条都不改变「免 JWT 白名单」那一格的绿读数，也不让请求触达后端。

## 六、边界与遗留

- 🔴 生产零写；本次只跑本地 httptest，无外呼、无 seed 写。
- 方法不匹配的请求仍不进取签/鉴权链（修复前后都一样，因为决定发生在引擎路由阶段）；本卡把这一格的对外形状和服务端证据补齐，没有改变它是否被鉴权这一点。
- **未做**：各后端服务（data-service / device-service）自己的路由同样没有方法校验与未匹配日志。现网设备只连网关，服务间调用是自家代码且方法匹配，故本卡按「只动设备侧入口的路由注册层」收口；服务层是否同样补，留给 PM 定。
- **副作用（已核面）**：`HandleMethodNotAllowed = true` 之后，OPTIONS 打已注册路径会从 404 变 405（gin 不为 GET 路由自动生成 OPTIONS 处理）。这一格的暴露面现读过两条：`services/**` 的 `*_test.go` 里 `MethodOptions`/`"OPTIONS"` 零命中（没有任何用例依赖「OPTIONS ⇒ 404」）；网关侧没有 CORS 中间件（全仓 `cors`/`Access-Control-Allow` 命中只在 `cloudfunctions/hello-http` 与各处「网关无 CORS，页面只能同源调接口」的注释与断言里，`apps/*/test*/h5-mount-contract.spec.ts` 锁的就是这条同源约束）⇒ 浏览器不会向网关发预检 OPTIONS，设备侧与自家服务间调用也都不发。
- 当日跑过的其它门禁：`scripts/deploy/check-routes.sh` rc=0（含自证三形 合规0/漂移1/空面2）；`node scripts/contract/go-json-tag-audit.mjs` 回「0 项不一致」（本卡新增的是 `gin.H` 里的 map 键，不涉及 DTO 结构体）。

署名：winner（dell-tower，后端 Go）。
