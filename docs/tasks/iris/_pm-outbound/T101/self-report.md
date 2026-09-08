# T101 自报 — 技师端 Android BLE 权限重检失败修复

## 基本信息
- **执行人**：Iris
- **分支**：`feat/iris-T101-android-ble-permission-fix`
- **最终提交**：`ac74fe5`
- **PR**：https://github.com/ryanicer/BraceSync-code/pull/37
- **状态**：BLE 扫描/连接/B514 解析/绑定全链路真机验证通过（小顾 Android 机型）

## git fsck 结果
```
git fsck
仅 dangling commit / blob / tree（无 corrupt 或 missing 对象），仓库健康。
```

## 改动文件清单

| 文件 | 修复点 |
|------|--------|
| `apps/tech-miniapp/src/utils/ble.ts` | 见下方修复详情 |
| `apps/tech-miniapp/src/manifest.json` | 新增 `mp-weixin.permission.scope.userLocation.desc` 权限描述声明 |
| `apps/tech-miniapp/src/pages/bind/index.vue` | B514 的 `device_id` 填充绑定输入框；BLE 重连改用 MAC 地址 |

## 修复详情（共 6 个问题，迭代发现并修复）

### ① 权限重检失败（原任务目标）
**现象**：用户在系统设置授予权限后返回小程序，再次扫描仍提示无权限。
**根因**：
- `wx.openSetting` 返回的 `authSetting` 在部分 Android 机型上不即时刷新，直接判定为未授权。
- `wx.getSystemSetting` 在该机型不回调，导致 `ensureLocationPermission` 卡死。
**修复**：
- openSetting 返回后延迟 300ms 重新 `wx.getSetting` 取最新授权状态。
- `getSystemSetting` 加 500ms 超时兜底，超时自动回退到 `getSetting` 流程。
- 前置 `getSystemSetting` 检查系统位置服务开关（Android BLE 扫描依赖）。
- `discoverDevices` 扫描失败时若为权限类错误，重新触发授权引导。

### ② 扫描不稳定/时有时无
**现象**：扫描偶发找不到 BSYNC 设备。
**根因**：
- 扫描时长 3s 太短，设备 scan response（含名称）还没到就结束。
- Android 上广播名常在 `localName` 字段，`name` 为空导致 BSYNC- 过滤误判。
**修复**：
- 扫描时长 3s → 6s。
- 设备名取 `name || localName`。
- 扫描结束后调 `getBluetoothDevices` 拿系统缓存设备补齐。

### ③ B514 解析失败 `TextDecoder is not defined`
**现象**：连接设备后读取 B514 特征值解析失败。
**根因**：微信小程序环境不支持 `TextDecoder` Web API。
**修复**：手动实现 `decodeUtf8()` 函数，支持 1~4 字节 UTF-8 序列。

### ④ 绑定报错 `device not registered`
**现象**：绑定后端返回 `device "9C:CC:01:E2:EF:65" not registered`。
**根因**：选中扫描设备后，绑定输入框填的是 BLE MAC 地址，后端只认 B514 里的真实 `device_id`。
**修复**：`selectDevice` 读完 B514 后，用 `info.deviceId`（如 `PRS-ML05-RC-20260701001`）更新绑定输入框。

### ⑤ BLE 重连用错 ID
**现象**：绑定后自动 BLE 连接失败。
**根因**：`bindManual` 用设备 ID（非 MAC）调 `createBLEConnection`。
**修复**：改用 `installStore.bleDeviceId`（selectDevice 时存的 MAC 地址）。

### ⑥ B514 超时误报日志
**现象**：B514 解析成功后仍打印「B514 读取超时」。
**根因**：超时回调未判断 `settled` 状态。
**修复**：超时回调增加 `if (!settled)` 判断。

## 本地验证结果

| 检查项 | 结果 |
|--------|------|
| `npm -w apps/tech-miniapp run build:mp-weixin` | ✅ Build complete |
| `npm -w apps/tech-miniapp run test` | ✅ 13 tests passed |
| `npm run lint` | ✅ 通过 |
| CI Frontend Lint + Vitest | ✅ pass |
| CI Tech MiniApp E2E (Playwright, H5) | ✅ pass |
| CI Patient MiniApp E2E (Playwright, H5) | ✅ pass |
| CI Admin Web E2E (Playwright) | ✅ pass |

## 真机验证（小顾 Android 机型）

**设备环境**：Android 16 / 微信 8.0.76.3141 / 基础库 3.17.2（realme RMX6699）

**验证日志（09/08 09:02）**：
```
[BLE] wx.getSetting 成功，完整 authSetting {"scope.userLocation":true,"scope.bluetooth":true,...}
[BLE] 位置权限已授权（scope.userLocation=true）
[BLE] startBluetoothDevicesDiscovery 成功
[BLE] [匹配] BraceSync 设备: name=BSYNC-701001, deviceId=9C:CC:01:E2:EF:65, RSSI=-56
[BLE] getBluetoothDevices 缓存设备数=22
[BLE] 扫描结束，最终 BSYNC 设备数=1
[BLE] 连接成功 deviceId=9C:CC:01:E2:EF:65
[BLE] B514 原始文本={"device_id":"PRS-ML05-RC-20260701001","firmware":"bracesync-prod-1.1","battery":100}
[BLE] B514 解析成功 {"firmware":"bracesync-prod-1.1","battery":100}
```

**验证结论**：
- ✅ 权限检测正常（getSetting 返回已授权，无权限拦截弹窗）
- ✅ BLE 扫描发现 BSYNC-701001（RSSI -56dBm，信号良好）
- ✅ BLE 连接成功
- ✅ B514 解析成功，获取到真实 device_id
- ✅ 用户确认：蓝牙设备已可找到并解析，蓝牙问题已解决

## 患者端影响判断

**不影响患者端**。两端 BLE 代码完全独立：
- 技师端：`apps/tech-miniapp/src/utils/ble.ts`（本次修改）
- 患者端：`apps/patient-miniapp/src/utils/ble.ts`（无权限逻辑、无 TextDecoder、无 scope.userLocation/getSystemSetting 引用）

经 grep 验证患者端源码无 `TextDecoder`、`scope.userLocation`、`getSystemSetting` 引用，本次改动不涉及患者端。

## 测试文件影响

**未修改任何测试文件**。`apps/tech-miniapp/test/login-validation.spec.ts`（13 tests）与 BLE 无关，保持原样且全部通过。

## 红线自查
- ✅ 未把权限判断写死为「必须全部权限同时 granted」才允许进入设置页
- ✅ 未以 mock 扫描结果绕过权限
- ✅ 未修改后端接口或 device-protocol，纯前端修复
- ✅ 未使用 stash/rebase/merge（仅 push 到功能分支）
- ✅ 未删除或修改 Ella 的测试文件

## 提交记录（8 commits）

| 提交 | 说明 |
|------|------|
| `3c51d6d` | feat: fix Android BLE location permission re-check after openSetting |
| `343fb22` | docs: add self-report |
| `42eaf4f` | chore: add comprehensive BLE permission logs for remote debugging |
| `ac62625` | chore: add BLE scan debug logging + temporary full device list |
| `29071b4` | fix: BLE scan stability - use localName, extend scan to 6s, cache fallback |
| `8c50ecc` | fix: replace TextDecoder with manual UTF-8 decoder |
| `d8b01a0` | fix: use B514 device_id for binding, not BLE MAC address |
| `ac74fe5` | fix: avoid spurious B514 timeout log after successful read |
