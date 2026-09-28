# 错误码 → 用户面中文文案映射表（T465，三端共用）

> 代码真源：`packages/shared-utils/src/errorCopy.ts`（import 名 `@bracesync/shared-utils`）。
> 本文件在 code 仓（`packages/shared-utils/docs/`）与 docs 仓（`docs/api/`）各存一份，要求逐字相同：
> 逐行判据＝`packages/shared-utils/test/error-copy-doc.test.ts`（CI 锁 errorCopy.ts ↔ code 仓这份）；
> 两仓是否同字＝code 仓脚本 `node scripts/ci/check-error-copy-doc-sync.mjs <code 份> <docs 份>`（本机跑，
> code CI 不检出 docs 仓，跨仓判据进不了 CI）。改文案的顺序：先改 `errorCopy.ts` 及其上的
> 语义来源注释，再同步两份 md。
> 码位语义本身以 `docs/api/api-contracts.ts` 与各服务 model.go 为准，本表只管用户面措辞。

取码顺序：响应体 `trace.errorCode`（T464，Winner）优先，回退信封 `code`；两者同值时结果一致。
**按键只有数字码一个维度**——不按 message 文本、不按 HTTP status 查表。

## 一、基础表（三端默认措辞）

| 错误码 | 用户面中文文案 | 语义来源 |
|---|---|---|
| 10001 | 登录状态已失效，请重新登录 | user-service model.go:19-34 |
| 10400 | 提交的信息有误，请检查后重试 | user-service model.go:19-34 |
| 10401 | 登录信息已失效，请重新登录 | user-service model.go:19-34 |
| 10403 | 没有该操作的权限，请联系管理员 | user-service model.go:19-34 |
| 10404 | 要操作的数据不存在，请刷新后重试 | user-service model.go:19-34 |
| 10409 | 当前状态与该操作冲突，请刷新后重试 | user-service model.go:19-34 |
| 10502 | 微信服务暂不可用，请稍后重试 | user-service model.go:19-34 |
| 10601 | 尚未绑定就诊档案，请先完成绑定 | user-service model.go:19-34 |
| 10602 | 未匹配到就诊档案，请联系门诊工作人员 | user-service model.go:19-34 |
| 10603 | 该就诊档案已被其他微信绑定，请联系门诊工作人员 | user-service model.go:19-34 |
| 10604 | 手机号授权信息有误，请重新授权 | user-service model.go:19-34 |
| 10605 | 操作已过期，请重新发起 | user-service model.go:19-34 |
| 20400 | 设备信息有误，请核对后重试 | device-service model.go:82-88 |
| 20402 | 数据时间超出可提交范围，请稍后重试 | device-service model.go:82-88 |
| 20403 | 无权操作该设备，请联系工作人员 | device-service model.go:82-88 |
| 20404 | 设备或记录不存在，请核对设备编号后重试 | device-service model.go:82-88 |
| 20409 | 设备状态冲突，请稍后重试 | device-service model.go:82-88 |
| 20429 | 操作过于频繁，请稍后重试 | device-service model.go:82-88 |
| 30001 | 查询条件有误，请调整后重试 | data-service model.go:70-85 |
| 30400 | 设备标识与请求不一致，请重新进入页面 | data-service model.go:70-85 |
| 30403 | 无权查看该数据，请联系管理员 | data-service model.go:70-85 |
| 40301 | 登录凭证范围不足，请重新登录 | user-service model.go:34 |
| 40403 | 无权查看该告警，请联系管理员 | alert-service code_lock_t402_test.go:27 |
| 40404 | 告警记录不存在，请刷新后重试 | alert-service code_lock_t402_test.go:28 |
| 50001 | 内部通道校验未通过，请联系管理员 | msg-service model.go:109-115 |
| 50400 | 消息设置参数有误，请检查后重试 | msg-service model.go:109-115 |
| 50403 | 无权操作该消息设置，请联系管理员 | msg-service model.go:109-115 |
| 50404 | 消息记录不存在，请刷新后重试 | msg-service model.go:109-115 |
| 54002 | 通知额度已用完，请联系门诊工作人员 | msg-service model.go:109-115 |
| 60001 | 文件参数有误，请重新选择文件后重试 | file-service error_response.go:13-26 |
| 60002 | 登录状态已失效，请重新登录后再上传 | file-service error_response.go:13-26 |
| 60403 | 无权访问该文件，请联系管理员 | file-service error_response.go:13-26 |
| 61001 | 文件不存在或已被清理，请重新上传 | file-service error_response.go:13-26 |
| 61002 | 文件上传失败，请稍后重试 | file-service error_response.go:13-26 |
| 61003 | 文件上传通道暂不可用，请稍后重试 | file-service error_response.go:13-26 |
| 69999 | 文件服务暂不可用，请稍后重试 | file-service error_response.go:13-26 |
| 90001 | 服务暂时异常，请稍后重试 | 各服务 model.go CodeInternal |
| 90712 | 告警阈值配置异常，请联系管理员 | alert-service config.go:32 |

表内不含 12345（Go 测试常量）与 40029（微信上游 errcode，非本系统码位）。

## 二、端内覆盖（同一码，该端措辞不同）

覆盖表只登记「与基础表不同」的码；查表顺序为覆盖表 → 基础表。

| 作用域 | 错误码 | 用户面中文文案 | 措辞依据 |
|---|---|---|---|
| patient | 10001 | 登录失败，请联系门诊工作人员 | PRD §7A.1.1 患者端逐码文案 |
| patient | 10401 | 授权信息已失效，请重新登录 | PRD §7A.1.1；T434 |
| patient | 10502 | 微信服务暂不可用，请稍后重试 | PRD §7A.1.1 |
| patient | 10601 | 尚未绑定就诊档案，请先完成绑定 | PRD §7A.1.1 |
| patient | 10602 | 未匹配到就诊档案，请联系门诊工作人员 | PRD §7A.1.1 |
| patient | 10603 | 该就诊档案已被其他微信绑定，请联系门诊工作人员 | PRD §7A.1.1 |
| patient | 10604 | 授权信息已失效，请重新授权手机号 | PRD §7A.1.1 补充状态码表 |
| patient | 10605 | 操作已过期，请重新绑定 | PRD §7A.1.1 补充状态码表 |
| admin | 10401 | 用户名或密码错误 | docs/design/admin/登录.html:134、医护账号.html:190（两因压一条、防枚举） |
| tech | 10401 | 手机号或密码错误，请重试 | 技师端登录页既有句 |

10604 / 10605 按 PRD_V3.md:270 只用于开发对接，**码位数字本身禁止展示给患者**；
患者端这两句是 PRD 给定的表现文案，因此刻意不带「（错误码 X）」后缀（该判据在
`apps/patient-miniapp/tests/unit/auth-state.spec.ts`）。

## 三、未知码与网络层兜底（卡面第 ② 条）

码位 token 的取法：数字码 > `HTTP<status>` > `NET`（传输层失败，无信封码也无 HTTP 状态）。

| 情形 | 用户面文案形状 |
|---|---|
| 该页有自研中文兜底句 | `<页面原有中文>（错误码 <token>）` |
| 该页没给兜底句 | `操作失败（错误码 <token>），请截图反馈` |
| 代码里刻意写好的中文提示（`markUserCopy`） | 原样使用该句，不追加码位 |
| 进程内自研中文（mock 层／校验器，无码无 HTTP 状态） | `<该句>（错误码 NET）` |

三端一律经 `userErrorCopy(err, { scope, fallback })` 出文案；`logErrorText(err)` 只给日志面，
返回后端英文原文，禁止接到 toast／ElMessage／模板插值上（防回潮门禁：
`packages/shared-utils/test/no-bare-message-display.test.ts`）。
